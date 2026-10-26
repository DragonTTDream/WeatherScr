//go:build windows

// WeatherScr 安装器。
//
// 四项口径：
// 1. 装到 C:\Windows\System32\WeatherScr.scr
// 2. 自提权（弹 UAC）
// 3. **不改 ScreenSaveTimeOut**（保留机器现有等待时间）
// 4. **无静默参数，只有图形界面**（B3 起为 4 页向导：欢迎 → 说明与准备 → 安装 → 完成）
//
// 桌面两个快捷方式："天气屏保 · 设置"→ WeatherScr_setup.exe --settings
// （安装器用直接 CreateProcess 起 WeatherScr.scr /c，不经 shell 关联）
// "天气屏保 · 启动"→ WeatherScr.scr /s
// 卸载：恢复安装前的 SCRNSAVE.EXE（本机现值 = SRPSS.scr），删快捷方式，删 Add/Remove Programs 项。
//
// 提权顺序（关键）：HKCU 与桌面快捷方式**必须以登录账户身份写**（提权子进程写 HKCU 会写进
// 提权账户的 hive）⇒ 账户级写入全部由**未提权的父进程**执行，只为 System32 那一次文件复制
// 弹一次 UAC。publish-install-verify A3：账户级写入挪到「提权复制 + 落盘校验通过」之后
// （证据 out/b18-run.log:51-60：半安装态 SCRNSAVE.EXE 指向不存在的 .scr），
// 校验不过 ⇒ 一个账户级值都不写，当场回滚。
package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"
)


//go:embed payload/WeatherScr.scr
var payload embed.FS

const (
	appName      = "WeatherScr 天气屏保"
	appVersion   = "1.0.0"
	installName  = "WeatherScr.scr"
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\WeatherScr`
	stateKey     = `Software\WeatherScr`
	desktopKey   = `Control Panel\Desktop`
	warmupExe    = `C:\Windows\System32\WeatherScr.scr`
)


// setupLogPath 安装器日志的**落盘副本**（%TEMP%\WeatherScr_setup.log，追加写）。
//
// B15（可诊断性缺口）：日志此前只进安装页的 EDIT 控件，带外**读不到** —— 2026-10-21 定因时
// 探针只能看到完成页那句「安装失败（已回滚）/ 原因：System32 写入失败」，拿不到每一步的真实
// 错误码（out/b45-run.log 的 EDIT 采样全程为空）。用户报问题时也带不走日志。
// ⇒ 同时追加一份到临时目录；写失败只忽略（日志不该让安装失败）。
func setupLogPath() string {
	return filepath.Join(os.TempDir(), "WeatherScr_setup.log")
}

// appendLog 从工作线程调用：跨线程 SendMessage 到 GUI 线程拥有的控件是合法的（系统会封送）。
func appendLog(s string) {
	logLines = append(logLines, s)
	if f, err := os.OpenFile(setupLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_, _ = f.WriteString(s + "\r\n")
		_ = f.Close()
	}
	if hLog == 0 {
		return
	}
	all := strings.Join(logLines, "\r\n")
	p := utf16Ptr(all)
	procSendMessageW.Call(uintptr(hLog), wmSetText, 0, uintptr(unsafe.Pointer(p)))
	// 滚到底
	procSendMessageW.Call(uintptr(hLog), 0x00B7 /*WM_VSCROLL*/, 7 /*SB_BOTTOM*/, 0)
}

func setStatus(s string) {
	if hStatus != 0 {
		setText(hStatus, s)
	}
}

func logf(format string, a ...any) {
	appendLog(time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, a...))
}

func main() {
	runtime.LockOSThread()
	args := os.Args[1:]

	switch {
	case len(args) >= 1 && args[0] == "--settings":
		// 设置直达（桌面"天气屏保 · 设置"快捷方式的目标）：
		// 不建 GUI、不弹 UAC，直接 CreateProcess 起 .scr /c 后自行退出。
		os.Exit(runSettingsShortcut())
	case len(args) >= 3 && args[0] == "--copy-only":
		// args: --copy-only <src> <dst> <marker>
		marker := args[3]
		src, dst := args[1], args[2]
		err := copyFile(src, dst)
		if err != nil {
			_ = os.WriteFile(marker, []byte("COPY_FAIL "+err.Error()), 0o644)
			return
		}
		// A2 落盘校验（子进程侧，spec publish-install-verify §1 A2）：copyFile 成功后
		// 校验落地文件的 size 与 SHA256 与源一致。marker 前缀 COPY_OK / COPY_FAIL 逐字不变，
		// 只在 COPY_OK 之后追加落地文件的 SHA256（父进程据此比对，见 runInstall）。
		// sha256Of 读失败返回 "?"、读一半出错返回截断值的哈希 ⇒ 一并比 size 兜住。
		srcSHA, dstSHA := sha256Of(src), sha256Of(dst)
		if srcSHA == "?" || dstSHA == "?" || fileSize(src) != fileSize(dst) || !shaMatches(dstSHA, srcSHA) {
			_ = os.WriteFile(marker, []byte("COPY_FAIL 校验不一致"), 0o644)
			return
		}
		_ = os.WriteFile(marker, []byte("COPY_OK "+dstSHA), 0o644)
		return
	case len(args) >= 2 && args[0] == "--delete-only":
		marker := args[2]
		// 提权侧兜底（spec §1 A1/A2）：子进程在删除之前做同一件事
		// （停同名进程 + 轮询可删；提权侧对 System32 有 DELETE 权，轮询判据在此最有效）。
		if n := stopRunningScrImages(args[1]); n > 0 {
			logf("已停止 %d 个运行中的屏保进程", n)
		} else {
			logf("无运行中的屏保进程")
		}
		waitFileDeletable(args[1])
		// B12（spec publish-rename-swap.md §3.2）：这里**也**不得对原名文件调 os.Remove ——
		// 有 DELETE 权不等于删得掉映射中的文件（FileDisposition 仍 STATUS_CANNOT_DELETE），
		// 删成功也会把名字送进 delete-pending（之后连重命名都 FAIL）。
		// ⇒ 先重命名成 .del-<时间戳> 让开，再轮询删**重命名后的**路径；删不掉留着不报错。
		// 机制出处：unprotect.it/technique/process-ghosting、stackoverflow.com/questions/799656。
		// marker 字面量 DEL_OK / DEL_FAIL 逐字不变（简报 §5 禁改清单）。
		switch {
		case !fileExists(args[1]):
			// 文件本就不在 ⇒ 与旧口径（os.IsNotExist ⇒ DEL_OK）一致
			_ = os.WriteFile(marker, []byte("DEL_OK"), 0o644)
		case deleteRenamed(args[1], logf):
			_ = os.WriteFile(marker, []byte("DEL_OK"), 0o644)
		default:
			_ = os.WriteFile(marker, []byte("DEL_FAIL 重命名或删除未成功"), 0o644)
		}
		return
	case len(args) >= 1 && args[0] == "--uninstall":
		// 从 Add/Remove Programs 进来：起 GUI 并直接执行卸载
		// （向导口径：直进"安装"页看日志；卸载逻辑本身一字未动）
		startPage = pageProgress
		uninstalling = true
		go func() { time.Sleep(400 * time.Millisecond); runUninstall() }()
		initAndRun()
		return
	default:
		initAndRun()
	}
}

func initAndRun() {
	// hInstance 传 0 即可（RegisterClassExW 里 hInstance=0 表示用当前进程镜像）
	runGUI()
}

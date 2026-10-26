//go:build windows

package main

// R5 拆分卡 10：payload 读取/提权复制写 System32/桌面快捷方式创建删除/卸载主流程。
// 原 cmd/setup/main.go:671-1092、1234-1363、1475-1501 逐字搬移，零行为变化。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ───────────────────────── 提权 ─────────────────────────

// isElevated 当前进程是否已提权。
func isElevated() bool {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer tok.Close()
	var elev struct{ value uint32 }
	n := uint32(0)
	if err := windows.GetTokenInformation(tok, windows.TokenElevation, (*byte)(unsafe.Pointer(&elev)), uint32(unsafe.Sizeof(elev)), &n); err != nil {
		return false
	}
	return elev.value != 0
}

// relaunchElevated 以 runas 动词重启自身并传参数（弹一次 UAC）。
func relaunchElevated(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(args)
	r, _, err := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		0, uintptr(1 /*SW_SHOWNORMAL*/))
	if int(r) <= 32 {
		if err == windows.ERROR_CANCELLED { // UAC 弹窗被取消
			return fmt.Errorf("UAC 提权被取消")
		}
		return fmt.Errorf("ShellExecute runas 失败 rc=%d %v", int(r), err)
	}
	return nil
}

// ───────────────────────── 安装 ─────────────────────────

func runInstall() {
	// R16-A（spec r16-wizard §1A）：重入锁置于函数最前——取不到锁直接 return，
	// 此时 defer wmAppDone 尚未挂上，重复触发不会把第一轮的完成页/状态机打乱。
	if !acquireInstall() {
		logf("安装已在进行，忽略重复触发")
		return
	}
	defer releaseInstall()
	installOK = false // D5（spec §1.1）：入口复位结果状态，失败分支保持 false
	defer procPostMessageW.Call(uintptr(hMain), wmAppDone, 0, 0)
	defer func() {
		if r := recover(); r != nil {
			logf("内部错误：%v", r)
			setStatus("安装失败")
		}
	}()

	// 证据 E4（spec §0.4 / §1.4）：一进入安装就禁用安装/上一步/下一步（只留"取消"）。
	// 旧版只发了不存在的 WM_ENABLE 消息 ⇒ 按钮实际未禁用，第二次点击又跑了一遍 runInstall，
	// 把 PreviousScr 覆盖成"安装后的值"（C:\Windows\System32\SRPSS.scr → ...\WeatherScr.scr）。
	// EnableWindow 必须在 GUI 线程调用 ⇒ 经 wmAppButtons 转投给窗口过程。
	procPostMessageW.Call(uintptr(hMain), wmAppButtons, 0, 0)

	logf("=== 安装开始 ===")
	logf("提权状态：%v（HKCU 与桌面快捷方式必须以登录账户身份写 ⇒ 账户级写入由本进程在落盘校验通过之后做，只为 System32 复制弹一次 UAC）", isElevated())

	// 1 记录安装前的 SCRNSAVE.EXE（卸载要恢复它）
	prev, err := readScrSave()
	if err != nil {
		logf("读取当前屏保设置失败：%v", err)
		setStatus("安装失败")
		return
	}
	logf("当前屏保程序 = %q", prev)
	if err := saveState("PreviousScr", prev); err != nil {
		logf("记录回退值失败：%v", err)
		setStatus("安装失败")
		return
	}
	if err := saveState("PreviousActive", fmt.Sprint(readCPInt(desktopKey, "ScreenSaveActive"))); err != nil {
		logf("记录 ScreenSaveActive 失败：%v", err)
	}

	// 2 落地文件：先解到临时目录，供提权子进程复制
	tmp, err := extractPayload()
	if err != nil {
		logf("解包失败：%v", err)
		setStatus("安装失败")
		return
	}
	defer os.Remove(tmp)
	logf("已解包 %s（%d 字节，SHA256 %s）", tmp, fileSize(tmp), sha256Of(tmp))

	// 3 提权复制（唯一一次 UAC）
	// A1（spec publish-install-verify §1 A1）：提权复制**之前**轮询「目标可写」，
	// 节奏复用卸载批的 stopPollEveryMS=200 ms × stopPollMax=50 = 10 s（见 procname.go 常量、
	// procstop_windows.go:waitFileWritable）。证据 out/b18-run.log:100-105：卸载删除成功后
	// 立刻 Copy-Item 覆盖同名文件 ⇒ Access is denied（delete-pending 锁）；
	// out/b19-run.log:2 = COPY_OK attempt=0（约 40 s 后可写）⇒ 删除后的同名路径长时间不可创建。
	logf("请求提权以写入 System32（会弹一次 UAC）…")
	setStatus("等待 UAC 授权…")
	t0 := time.Now()
	if waitFileWritable(warmupExe) {
		if s := int(time.Since(t0).Seconds()); s > 0 {
			logf("等待 %d 秒后目标可写", s)
		} else {
			logf("目标可写")
		}
	} else {
		logf("目标在 10 秒内不可写，仍尝试写入")
	}
	// B12（spec publish-rename-swap.md §3.1）：10 秒轮询后目标仍不可写 ⇒ **重命名让开**，
	// 映射节跟着旧名字走，原名对新写入开放；让开出来的 .old 尽力删（见本函数末尾）。
	// 机制出处（简报 §4 逐字引用）：
	//   unprotect.it/technique/process-ghosting —「Once a file is mapped into an image section,
	//   attempts to open it with FILE_WRITE_DATA will fail with ERROR_SHARING_VIOLATION …
	//   the delete-pending state also blocks external file-open attempts」
	//   stackoverflow.com/questions/799656（Renaming A Running Process' File Image On Windows）
	// 重命名失败 ⇒ 不回滚、不阻断（不比现状更差）：照旧弹 UAC 尝试写入，写不进去由 A2 落盘校验判。
	if swapped, _ := swapIn(warmupExe, tmp, logf); swapped {
		logf("原名已让开，提权子进程按原名写入 %s", warmupExe)
	}
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("wssetup_copy_%d.txt", os.Getpid()))
	_ = os.Remove(marker)
	ok := false
	markerSHA := ""
	// B13（定因证据 out/b43-run.log 17:12:55 的 marker 原文）：
	//   COPY_FAIL open C:\Windows\System32\WeatherScr.scr: The process cannot access
	//   the file because it is being used by another process.
	// 同批读数：本机 EnableLUA=0（UAC 整个关着）、全程 0 个 consent.exe、seclogon=STOPPED，
	// 而父进程 SELF_ELEVATED=True 且带 Mandatory Label\High Mandatory Level
	// （out/b42-run.log）⇒ 绕 runas 重启自己只会白等 60 s，完成页还会显示与真实原因无关的 UAC 文案。
	// ⇒ 已提权就在本进程直接写：copyFile 与 --copy-only 子进程用的是同一个函数、同一套落盘校验。
	if isElevated() {
		logf("本进程已提权 ⇒ 直接写入 %s（不绕 runas 重启自己）", warmupExe)
		setStatus("正在写入 System32…")
		if cerr := copyFile(tmp, warmupExe); cerr != nil {
			logf("直接写入失败：%v", cerr)
			rollback("System32 写入失败")
			setStatus("安装失败（已回滚）")
			return
		}
		markerSHA = sha256Of(warmupExe)
		ok = true
	} else {
		if err := relaunchElevated(fmt.Sprintf("--copy-only %s %s %s", tmp, warmupExe, marker)); err != nil {
			logf("提权失败：%v", err)
			rollback("提权被取消或失败")
			setStatus("安装失败（已回滚）")
			return
		}
		// 等 marker
		for i := 0; i < 120; i++ {
			if b, err := os.ReadFile(marker); err == nil && strings.Contains(string(b), "COPY_OK") {
				ok = true
				// A2：marker 口径 = "COPY_OK <sha256>"（前缀逐字不变，只在其后追加 SHA）
				markerSHA = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "COPY_OK"))
				break
			}
			time.Sleep(500 * time.Millisecond)
			if (i+1)%20 == 0 { // 第 10/20/30… 秒各一行（120×500 ms 的节奏不变）
				logf("仍在等待提权复制…（已等 %d 秒）", (i+1)/2)
			}
		}
		_ = os.Remove(marker)
	}
	if !ok {
		logf("提权复制未完成（System32 写入失败）")
		rollback("System32 写入失败")
		setStatus("安装失败（已回滚）")
		return
	}
	logf("已写入 %s（%d 字节）", warmupExe, fileSize(warmupExe))

	// A2 落盘校验（父进程侧，spec §1 A2）：marker 里的 SHA 与解包文件比对，
	// **并独立**对落地文件算一次 sha256Of 比对（读 System32 文件不需要写权）。
	// 任一不符 ⇒ 回滚（证据 out/b18-run.log:51-60：半安装态 = SCRNSAVE.EXE 指向不存在的 .scr，
	// 且窗口关掉后连回滚都不跑 ⇒ 校验必须在父进程侧也做一遍）。
	// B12（spec publish-rename-swap.md §3.3）：父进程侧的读要**重试** 10 × 500 ms
	// （sha256OfRetry，sha256Of 返回 "?" 视作读失败继续重试）——刚写完的文件可能被扫描器
	// 短暂占用，**读失败 ≠ 写入失败**。证据 out/b23-run4.log PH1：文件确实落成功
	// （SYS32=PRESENT 1169194F = 期望新二进制的 SHA 前 8 位），一次读失败 ⇒ 判失败并把
	// 好结果回滚掉（完成页「安装失败（已回滚）」+ 注册表回滚 = 假阴性）。
	// 重试到超时仍读不到 ⇒ 按「大小相等 + 子进程 marker 的 SHA 匹配」放行并记日志，**不回滚**；
	// 只有大小不符（或 marker 是 COPY_FAIL ⇒ 上面 !ok 分支已经先回滚）才回滚。
	wantSHA := sha256Of(tmp)
	gotSHA, gotOK := sha256OfRetry(warmupExe, shaRetryAttempts, time.Duration(shaRetryEveryMS)*time.Millisecond)
	if !gotOK {
		if fileSize(warmupExe) == fileSize(tmp) && shaMatches(markerSHA, wantSHA) {
			logf("落地校验读取失败，按大小与子进程校验通过放行（%s %d 字节，SHA256 读不到，marker=%s 与期望一致）",
				warmupExe, fileSize(warmupExe), markerSHA)
		} else {
			logf("校验失败：目标 SHA256 %s（重试 %d 次读不到），大小 %d，期望 %d，marker %s",
				gotSHA, shaRetryAttempts, fileSize(warmupExe), fileSize(tmp), markerSHA)
			rollback("System32 落盘校验失败")
			setStatus("安装失败（已回滚）")
			return
		}
	} else if !shaMatches(markerSHA, wantSHA) || !shaMatches(gotSHA, wantSHA) {
		logf("校验失败：目标 SHA256 %s，期望 %s", gotSHA, wantSHA)
		lastVerify = "警告：已装件 SHA256 与构建件不一致（已回滚）" // R16-C：失败态也留自验记录
		rollback("System32 落盘校验失败")
		setStatus("安装失败（已回滚）")
		return
	}
	logf("已校验 %s SHA256 与解包一致", warmupExe)

	// R16-C（spec r16-wizard §1C）：完成页自验行——校验通过的 SHA 现场写进 lastVerify，
	// 完成页正文追加（finishpage.go finishVerifyLine）。用户可见判据 = 「SHA256 与构建件一致」逐字。
	lastVerify = fmt.Sprintf("已装件 SHA256 与构建件一致（%.16s…）", wantSHA)
	if !gotOK {
		lastVerify = "已装件 SHA256 现场读取失败，按 marker 与大小核验放行（无逐字比对）"
	}

	// 4 账户级：写 HKCU + Add/Remove Programs
	// A3（spec §1 A3）：整块从「提权复制之前」挪到「落盘校验通过之后」——
	// 校验失败时上面已经 return ⇒ 不会留下指向不存在文件的 SCRNSAVE/ARP 状态。
	// 仍在未提权的本进程里写 ⇒ hive 归属不变（登录账户）。
	if err := writeScrSave(warmupExe); err != nil {
		logf("写 HKCU\\Control Panel\\Desktop\\SCRNSAVE.EXE 失败：%v", err)
		setStatus("安装失败")
		return
	}
	logf("已写 SCRNSAVE.EXE = %s", warmupExe)
	if err := setCPStr(desktopKey, "ScreenSaveActive", 1); err != nil {
		logf("写 ScreenSaveActive 失败：%v", err)
	} else {
		logf("已写 ScreenSaveActive = 1")
	}
	logf("按批准口径**不改 ScreenSaveTimeOut**（保留机器现有等待时间 = %d 秒）", readCPInt(desktopKey, "ScreenSaveTimeOut"))

	if err := writeARP(); err != nil {
		logf("写 Add/Remove Programs 失败：%v", err)
	} else {
		logf("已登记 Add/Remove Programs 项")
	}

	// 5 广播设置变更（注册表改了要广播，explorer 才认）
	if err := broadcastSettingChange(); err != nil {
		logf("广播 WM_SETTINGCHANGE 失败：%v", err)
	} else {
		logf("已广播 WM_SETTINGCHANGE")
	}

	// 6 桌面快捷方式
	// "设置"的目标 = 安装器自身 + --settings（安装器用直接 CreateProcess 起
	// .scr /c）。取证 36.16.5：.scr 的 shell 关联是 "%1" /S，走 shell 的启动
	// 参数一律被换成 /S ⇒ 目标指 .scr 的"设置"快捷键永远开屏保。
	// "启动"保持 WeatherScr.scr /s 不变；两者的工作目录与图标不变。
	settingsTarget, settingsArgs := selfExe(), "--settings"
	if settingsTarget == "" {
		// 取不到安装器自身路径（罕见）⇒ 退回旧口径，快捷键至少可用
		settingsTarget, settingsArgs = warmupExe, "/c"
		logf("取安装器自身路径失败 ⇒ \"设置\"快捷方式退回 %s /c", warmupExe)
	}
	d, err := desktopDirFast()
	if err != nil {
		logf("取桌面目录失败：%v", err)
	} else {
		logf("桌面目录 = %s", d)
		if p, err := makeShortcut(d, "天气屏保 · 设置", settingsTarget, settingsArgs, warmupExe); err != nil {
			logf("建\"设置\"快捷方式失败：%v", err)
		} else {
			logf("已建快捷方式 %s 目标=%s 参数=%s", p, settingsTarget, settingsArgs)
		}
		if p, err := makeShortcut(d, "天气屏保 · 启动", warmupExe, "/s", warmupExe); err != nil {
			logf("建\"启动\"快捷方式失败：%v", err)
		} else {
			logf("已建快捷方式 %s", p)
		}
	}

	// B12（spec publish-rename-swap.md §3.1）：让开出来的 .old 尽力删（轮询 10 s），
	// 删不掉就**留着**（不报错、不阻塞安装）——下次安装/卸载开头还会再清一次（swapIn 开头）。
	cleanupAsideLeftovers(warmupExe, renameSuffixOld, logf)

	installOK = true // D5（spec §1.1）：成功路径在「=== 安装完成 ===」之前置真
	logf("=== 安装完成 ===")
	logf("屏保程序 = %s；设置 = %s %s；启动 = %s /s", warmupExe, settingsTarget, settingsArgs, warmupExe)
	setStatus("安装完成")
}

func rollback(why string) {
	lastFail = why // D5（spec §1.1）：记录失败原因，供完成页正文取值（空则文案回落「未记录」）
	logf("回滚：%s", why)
	if prev, err := loadState("PreviousScr"); err == nil {
		if prev == "" {
			if err := deleteScrSave(); err != nil {
				logf("删除 SCRNSAVE.EXE 失败：%v", err)
			} else {
				logf("已删除 SCRNSAVE.EXE（安装前没有屏保）")
			}
		} else {
			if err := writeScrSave(prev); err != nil {
				logf("恢复 SCRNSAVE.EXE 失败：%v", err)
			} else {
				logf("已恢复 SCRNSAVE.EXE = %q", prev)
			}
		}
	}
	_ = broadcastSettingChange()
	_ = os.RemoveAll(filepath.Join(mustDesktop(), "天气屏保 · 设置.lnk"))
	_ = os.RemoveAll(filepath.Join(mustDesktop(), "天气屏保 · 启动.lnk"))
	_ = removeARP()
}

func runUninstall() {
	// R16-A（spec r16-wizard §1A）：卸载与安装共用同一把锁，只许一轮在跑；
	// 同 runInstall 的形状，锁在 defer wmAppDone 之前。
	if !acquireInstall() {
		logf("安装/卸载已在进行，忽略重复触发")
		return
	}
	defer releaseInstall()
	defer procPostMessageW.Call(uintptr(hMain), wmAppDone, 0, 0)
	defer func() {
		if r := recover(); r != nil {
			logf("内部错误：%v", r)
			setStatus("卸载失败")
		}
	}()
	// R13（spec §4）：每次跑卸载都重置结果态，避免复用上一次的值污染完成页文案。
	uninstallOK = false
	lastFail = ""
	logf("=== 卸载开始 ===")
	prev, _ := loadState("PreviousScr")
	logf("安装前记录的屏保程序 = %q", prev)
	// 证据 E5（spec §0.5）：记录值就是本程序自己的安装路径时（双安装把回滚值覆盖成了安装后的值），
	// 恢复它等于把 SCRNSAVE.EXE 指向一个即将被卸载删除的文件 ⇒ 按"安装前没有屏保"处理。
	if isSelfScrPath(prev, warmupExe) {
		logf("记录值 = 本程序安装路径（自指）⇒ 按\"安装前没有屏保\"处理，卸载后删除 SCRNSAVE.EXE 值")
		prev = ""
	}

	// 删 System32 里的文件需要提权
	// 证据 E-uninstall（spec publish-uninstall-stopscr §0，源 out/notes-b5c.md §3）：
	// 运行中的同名进程以镜像方式占用该文件 ⇒ 提权删除也被拒（DEL_FAIL Access is denied），
	// 且进程刚被终止后镜像锁仍会短暂残留 ⇒ 先停进程、再轮询「可删」，最后才请求提权。
	// 轮询在提权**之前**，不替换下面的 marker 轮询（120×500 ms 口径不变）。
	if n := stopRunningScrImages(warmupExe); n > 0 {
		logf("已停止 %d 个运行中的屏保进程", n)
	} else {
		logf("无运行中的屏保进程")
	}
	waitFileDeletable(warmupExe)
	if fileExists(warmupExe) {
		// B12（spec publish-rename-swap.md §3.2）：停进程后**不要**对原名文件调 os.Remove/DeleteFile
		// —— 删除会把名字送进 delete-pending，之后连重命名都 FAIL（证据 out/b23-run4.log：
		// DELETABLE=False after poll、RESTORE_SHA=LOCKED；b30 的访问掩码矩阵全 FAIL）。
		// 机制出处（简报 §4 逐字引用）：unprotect.it/technique/process-ghosting —
		// 「Once a file is mapped into an image section, attempts to open it with FILE_WRITE_DATA
		// will fail with ERROR_SHARING_VIOLATION … the delete-pending state also blocks external
		// file-open attempts」；stackoverflow.com/questions/799656（正在运行的 exe 可以被重命名）。
		// ⇒ 先重命名成 .del-<时间戳> 让开，再轮询删**重命名后的**路径；删不掉留着不报错。
		// 父进程未提权 ⇒ 对 System32 多半没有 DELETE 权（重命名也要 DELETE）：让开失败就交回
		// 下面的提权子进程（子进程有 DELETE 权，同样走「先重命名再删」）。
		if deleteRenamed(warmupExe, logf) {
			logf("已删除 %s", warmupExe)
		} else if isElevated() {
			// B13：已提权（EnableLUA=0 的机器上父进程就是 High 完整令牌）⇒ 不绕 runas。
			// 走到这里说明 deleteRenamed 已失败 = 文件被占用，成败交末尾核验判。
			logf("本进程已提权仍删不掉 %s（文件被占用）⇒ 不绕 runas，由末尾核验判成败", warmupExe)
			marker := filepath.Join(os.TempDir(), fmt.Sprintf("wssetup_del_%d.txt", os.Getpid()))
			_ = os.Remove(marker)
			logf("请求提权以删除 System32 里的文件（会弹一次 UAC）…")
			setStatus("等待 UAC 授权…")
			if err := relaunchElevated("--delete-only " + warmupExe + " " + marker); err != nil {
				logf("提权失败：%v（注册表与快捷方式继续回退）", err)
			} else {
				ok := false
				for i := 0; i < 120; i++ {
					if b, err := os.ReadFile(marker); err == nil && strings.Contains(string(b), "DEL_OK") {
						ok = true
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				_ = os.Remove(marker)
				if ok {
					logf("已删除 %s", warmupExe)
				} else {
					logf("未删除 %s（文件可能仍在）", warmupExe)
				}
			}
		}
	}

	if prev == "" {
		if err := deleteScrSave(); err != nil {
			logf("删除 SCRNSAVE.EXE 失败：%v", err)
		} else {
			logf("已删除 SCRNSAVE.EXE")
		}
	} else {
		if err := writeScrSave(prev); err != nil {
			logf("恢复 SCRNSAVE.EXE 失败：%v", err)
		} else {
			logf("已恢复 SCRNSAVE.EXE = %q", prev)
		}
	}
	if v, err := loadState("PreviousActive"); err == nil && v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		_ = setCPStr(desktopKey, "ScreenSaveActive", n)
	}
	_ = broadcastSettingChange()
	if d, err := desktopDirFast(); err == nil {
		for _, n := range []string{"天气屏保 · 设置.lnk", "天气屏保 · 启动.lnk"} {
			p := filepath.Join(d, n)
			if fileExists(p) {
				if err := os.Remove(p); err != nil {
					logf("删除 %s 失败：%v", n, err)
				} else {
					logf("已删除快捷方式 %s", n)
				}
			}
		}
	}
	// 证据 E-arp（spec §0 / §1 A3，卸载页日志实测 b17_u1.png）：键本就不存在时
	// RegDeleteKey 返回 ERROR_FILE_NOT_FOUND，旧代码打印「失败」属噪声 ⇒ 幂等视作成功。
	if err := removeARP(); !arpDeleteIsOk(err) {
		logf("删除 Add/Remove Programs 项失败：%v", err)
	} else {
		logf("已删除 Add/Remove Programs 项")
	}
	// B13 / D9（证据 out/b41-run.log PH2 与 out/b42-run.log PH2：完成页写 卸载完成，
	// 而探针断言行 SYS32_EXISTS_AFTER_UNINSTALL=True —— 文件从头到尾没被删掉）。
	// 卸载成功判据 = 文件真的不在了；marker 的 DEL_OK 与流程走完都不算。
	if fileExists(warmupExe) {
		logf("卸载核验失败：%s 仍然存在", warmupExe)
		logf("=== 卸载未完成（文件仍在）===")
		// R13（spec §4）：核验失败原因写进 lastFail，完成页正文的 %s 取它（finishBodyFor(opUninstall,false,…)）。
		lastFail = "文件 C:\\Windows\\System32\\WeatherScr.scr 仍存在"
		setStatus("卸载失败（文件仍在，未删除）")
		return
	}
	logf("卸载核验通过：%s 已不存在", warmupExe)
	// R13（spec §4）：核验通过 ⇒ 卸载成功态，完成页取「卸载完成」文案。
	uninstallOK = true
	logf("=== 卸载完成 ===")
	setStatus("卸载完成")
}

// ───────────────────────── 文件与快捷方式 ─────────────────────────

func extractPayload() (string, error) {
	b, err := payload.ReadFile("payload/WeatherScr.scr")
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(os.TempDir(), "WeatherScr_payload.scr")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	return tmp, nil
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func sha256Of(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return "?"
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<16)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// desktopDir 解析当前登录账户的桌面目录。三档回退：
// 1. windows.KnownFolderPath(FOLDERID_Desktop, KF_FLAG_DEFAULT) —— canonical 做法，
// 能正确处理 OneDrive 重定向过的桌面；
// 2. 注册表 User Shell Folders\Desktop（**REG_EXPAND_SZ**）+ 从令牌补 USERPROFILE 再展开。
// 实测踩坑：直接 os.ExpandEnv 不展开（远程会话起的进程环境里没有 USERPROFILE），
// 结果 "%USERPROFILE%\Desktop" 原样留下被当相对路径 ⇒ DirectoryNotFoundException；
// 3. %USERPROFILE%\Desktop。
func desktopDirFast() (string, error) {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, windows.KF_FLAG_DEFAULT); err == nil && p != "" {
		return p, nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err == nil {
		s, _, gerr := k.GetStringValue("Desktop")
		k.Close()
		if gerr == nil && s != "" {
			if !strings.Contains(s, "%") {
				return s, nil
			}
			// 把令牌里的登录账户配置文件目录补进进程环境，注册表那串才展得开
			var tok windows.Token
			if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok) == nil {
				defer tok.Close()
				if prof, perr := tok.GetUserProfileDirectory(); perr == nil && prof != "" {
					_ = os.Setenv("USERPROFILE", prof)
				}
			}
			if es, eerr := registry.ExpandString(s); eerr == nil && es != "" && !strings.Contains(es, "%") {
				return es, nil
			}
		}
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		return filepath.Join(up, "Desktop"), nil
	}
	return "", fmt.Errorf("无法确定桌面目录")
}

func mustDesktop() string {
	d, err := desktopDirFast()
	if err != nil {
		return filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	}
	return d
}

// makeShortcut 用 PowerShell 的 WScript.Shell COM 建 .lnk（LTSC 17763 自带 PowerShell 5.1）。
// icon 是 IconLocation 的目标文件（图标来源与快捷方式目标解耦：
// "设置"快捷方式的目标换成了安装器，图标仍取屏保文件的 0 号图标）。
func makeShortcut(dir, name, target, args, icon string) (string, error) {
	lnk := filepath.Join(dir, name+".lnk")
	ps := fmt.Sprintf(
		"$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');"+
			"$s.TargetPath='%s';$s.Arguments='%s';$s.WorkingDirectory='C:\\Windows\\System32';"+
			"$s.Description='WeatherScr';$s.IconLocation='%s,0';$s.Save()",
		lnk, target, args, icon)
	return lnk, runPS(ps)
}

// selfExe 当前安装器可执行文件的完整路径（= 安装目录里的 WeatherScr_setup.exe）。
// 取不到时返回空串，调用方负责回退口径。
func selfExe() string {
	p, err := os.Executable()
	if err != nil || p == "" {
		return ""
	}
	return p
}

func runPS(script string) error {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		return err
	}
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v：%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 1<<20)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

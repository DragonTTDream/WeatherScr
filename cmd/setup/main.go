//go:build windows

// WeatherScr 安装器。
//
// 四项口径：
// 1. 装到 C:\Windows\System32\WeatherScr.scr
// 2. 自提权（弹 UAC）
// 3. **不改 ScreenSaveTimeOut**（保留机器现有等待时间）
// 4. **无静默参数，只有图形界面**
//
// 桌面两个快捷方式："天气屏保 · 设置"→ WeatherScr.scr /c"天气屏保 · 启动"→ WeatherScr.scr /s
// 卸载：恢复安装前的 SCRNSAVE.EXE（本机现值 = SRPSS.scr），删快捷方式，删 Add/Remove Programs 项。
//
// 提权顺序（关键）：HKCU 与桌面快捷方式**必须以登录账户身份写**，
// 提权后写 HKCU 会写进提权账户的 hive。⇒ 先做账户级（无需 UAC），
// 只为 System32 那一次文件复制弹一次 UAC；复制失败当场回滚注册表。
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procUpdateWindow       = user32.NewProc("UpdateWindow")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procSendMessageW       = user32.NewProc("SendMessageW")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procSetWindowTextW     = user32.NewProc("SetWindowTextW")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procGetStockObject     = user32.NewProc("GetStockObject")
	procSendNotifyMessageW = user32.NewProc("SendNotifyMessageW")

	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmSetText       = 0x000C
	wmGetText       = 0x000D
	wmSetTextLen    = 0x000F
	wmSize          = 0x0005
	wmNotify        = 0x004E
	wmSettingChange = 0x001A

	bnClicked = 1

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsBorder           = 0x00800000
	wsVScroll          = 0x00020000
	wsTabStop          = 0x00000020

	esReadOnly  = 0x0001
	esMultiline = 0x0004

	// 控件 id
	idBtnInstall = 101
	idBtnUninst  = 102
	idBtnQuit    = 103
	idLog        = 110
	idStatus     = 111

	// 自定义消息：工作线程请 GUI 线程追加一行日志 / 结束
	wmAppLog    = 0x0400
	wmAppDone   = 0x0401
	wmAppStatus = 0x0402
)

var (
	hInst    windows.Handle
	hMain    windows.HWND
	hLog     windows.HWND
	hStatus  windows.HWND
	hBtnIns  windows.HWND
	hBtnUn   windows.HWND
	hBtnQuit windows.HWND
	fontH    windows.Handle

	logLines []string
)

// ───────────────────────── GUI ─────────────────────────

// winMsg = Win32 MSG（winuser.h），GetMessageW/DispatchMessageW 的入参布局
type winMsg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      winPoint
}

type winPoint struct{ x, y int32 }

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hBackground   windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func setText(h windows.HWND, s string) {
	p := utf16Ptr(s)
	procSendMessageW.Call(uintptr(h), wmSetText, 0, uintptr(unsafe.Pointer(p)))
}

// appendLog 从工作线程调用：跨线程 SendMessage 到 GUI 线程拥有的控件是合法的（系统会封送）。
func appendLog(s string) {
	logLines = append(logLines, s)
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

const (
	btnInstallLabel = "安装屏保"
	btnUninstLabel  = "卸载"
	btnQuitLabel    = "关闭"
)

func wndProc(hwnd windows.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmCommand:
		id := wp & 0xffff
		switch id {
		case idBtnInstall:
			setButtonsEnabled(false)
			setStatus("安装中…")
			go runInstall()
		case idBtnUninst:
			setButtonsEnabled(false)
			setStatus("卸载中…")
			go runUninstall()
		case idBtnQuit:
			procPostQuitMessage.Call(0)
		}
		return 0
	case wmClose:
		procPostQuitMessage.Call(0)
		return 0
	case wmAppDone:
		setButtonsEnabled(true)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return r
}

func setButtonsEnabled(on bool) {
	var en uintptr
	if on {
		en = 1
	}
	for _, h := range []windows.HWND{hBtnIns, hBtnUn} {
		if h != 0 {
			procSendMessageW.Call(uintptr(h), 0x00C7 /*WM_ENABLE*/, en, 0)
		}
	}
}

func mkWindow(class, title string, style, x, y, w, h int32, parent windows.HWND, id uintptr) windows.HWND {
	r, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr(class))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), uintptr(id), 0, 0) // hInstance=0 ⇒ 当前进程镜像
	return windows.HWND(r)
}

// fitToWorkArea 算安装向导窗口的尺寸：设计值收进主屏工作区。
//
// 上限 = 工作区 - 边距（40 px，给任务栏与窗口边框让位）。
// 下限 = 三个按钮并排所需的宽度：14 + 150 + 10 + 150 + 10 + 150 + 14 = 498，
// 再低就会把"卸载"与"关闭"挤到一起；高度下限 300 保证日志区可读
// （日志区高 = H - 64 - 58）。
// 工作区指标取不到（0）时退回整屏尺寸。
// 只算尺寸，不碰安装逻辑。
func fitToWorkArea(workw, workh, scrw, scrh, wantW, wantH int32) (int32, int32) {
	const margin = 40
	const minW, minH = 498, 300
	if workw <= 0 {
		workw = scrw
	}
	if workh <= 0 {
		workh = scrh
	}
	W, H := wantW, wantH
	if workw > 0 && workw-margin < W {
		W = workw - margin
	}
	if workh > 0 && workh-margin < H {
		H = workh - margin
	}
	if W < minW {
		W = minW
	}
	if H < minH {
		H = minH
	}
	return W, H
}

func runGUI() {
	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	cur, _, _ := procLoadCursorW.Call(0, 32512 /*IDC_ARROW*/)
	wc.hCursor = windows.Handle(cur)
	wc.hBackground = 6 // COLOR_WINDOW+6
	wc.lpszClassName = utf16Ptr("WeatherScrSetupWnd")
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Fprintln(os.Stderr, "注册窗口类失败")
		os.Exit(2)
	}

	smw, _, _ := procGetSystemMetrics.Call(0)    // SM_CXSCREEN
	smh, _, _ := procGetSystemMetrics.Call(1)    // SM_CYSCREEN
	workw, _, _ := procGetSystemMetrics.Call(76) // SM_CXWORKAREA
	workh, _, _ := procGetSystemMetrics.Call(77) // SM_CYWORKAREA
	// 尺寸口径：按主屏工作区（整屏去掉任务栏的可用区）自适应。
	// 设计值 620×460 不变；工作区比设计值小的屏上自动收进
	// "工作区 - 边距"，窗口不再超出可用区（同族缺陷：窗口要手动拉伸才显示全）。
	// 竖屏 1080×1920 的工作区 1080×1880 ⇒ 620×460 原样保留。
	// 只算尺寸与居中，安装逻辑、注册表操作、文件复制顺序一律不动。
	W, H := fitToWorkArea(int32(workw), int32(workh), int32(smw), int32(smh), 620, 460)
	x := (int32(workw) - W) / 2
	y := (int32(workh) - H) / 2
	if x < 0 {
		x = 40
	}
	if y < 0 {
		y = 40
	}

	hMain = mkWindow("WeatherScrSetupWnd", appName+" 安装器 v"+appVersion,
		wsOverlappedWindow|wsVisible, x, y, W, H, 0, 0)
	if hMain == 0 {
		fmt.Fprintln(os.Stderr, "创建窗口失败")
		os.Exit(2)
	}

	// 标题与说明
	hStatus = mkWindow("STATIC", "就绪。点击\"安装屏保\"将安装到 C:\\Windows\\System32，并只弹一次 UAC 用于系统目录写入。",
		wsChild|wsVisible|0x0001 /*SS_LEFT*/, 14, 12, W-28, 46, hMain, idStatus)

	// 日志区
	hLog = mkWindow("EDIT", "",
		wsChild|wsVisible|wsBorder|wsVScroll|esReadOnly|esMultiline|wsTabStop,
		14, 64, W-28, H-64-58, hMain, idLog)

	// 按钮
	by := int32(H - 46)
	hBtnIns = mkWindow("BUTTON", btnInstallLabel, wsChild|wsVisible|wsTabStop, 14, by, 150, 34, hMain, idBtnInstall)
	hBtnUn = mkWindow("BUTTON", btnUninstLabel, wsChild|wsVisible|wsTabStop, 174, by, 150, 34, hMain, idBtnUninst)
	hBtnQuit = mkWindow("BUTTON", btnQuitLabel, wsChild|wsVisible|wsTabStop, W-164, by, 150, 34, hMain, idBtnQuit)

	procShowWindow.Call(uintptr(hMain), 5 /*SW_SHOW*/)
	procUpdateWindow.Call(uintptr(hMain))

	// x/sys/windows 没有 MSG 结构，自己按 Win32 布局定义（user32 GetMessageW 的入参）
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int16(r) <= 0 { // 0 = WM_QUIT，-1 = 出错
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

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
	defer procPostMessageW.Call(uintptr(hMain), wmAppDone, 0, 0)
	defer func() {
		if r := recover(); r != nil {
			logf("内部错误：%v", r)
			setStatus("安装失败")
		}
	}()

	logf("=== 安装开始 ===")
	logf("提权状态：%v（HKCU 与桌面快捷方式必须以登录账户身份写，故先做账户级，只为 System32 复制弹一次 UAC）", isElevated())

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

	// 3 账户级：写 HKCU + 快捷方式 + Add/Remove Programs
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

	// 4 提权复制（唯一一次 UAC）
	logf("请求提权以写入 System32（会弹一次 UAC）…")
	setStatus("等待 UAC 授权…")
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("wssetup_copy_%d.txt", os.Getpid()))
	_ = os.Remove(marker)
	if err := relaunchElevated(fmt.Sprintf("--copy-only %s %s %s", tmp, warmupExe, marker)); err != nil {
		logf("提权失败：%v", err)
		rollback("提权被取消或失败")
		setStatus("安装失败（已回滚）")
		return
	}
	// 等 marker
	ok := false
	for i := 0; i < 120; i++ {
		if b, err := os.ReadFile(marker); err == nil && strings.Contains(string(b), "COPY_OK") {
			ok = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(marker)
	if !ok {
		logf("提权复制未完成（System32 写入失败）")
		rollback("System32 写入失败")
		setStatus("安装失败（已回滚）")
		return
	}
	logf("已写入 %s（%d 字节）", warmupExe, fileSize(warmupExe))

	// 5 广播设置变更（注册表改了要广播，explorer 才认）
	if err := broadcastSettingChange(); err != nil {
		logf("广播 WM_SETTINGCHANGE 失败：%v", err)
	} else {
		logf("已广播 WM_SETTINGCHANGE")
	}

	// 6 桌面快捷方式
	d, err := desktopDirFast()
	if err != nil {
		logf("取桌面目录失败：%v", err)
	} else {
		logf("桌面目录 = %s", d)
		if p, err := makeShortcut(d, "天气屏保 · 设置", warmupExe, "/c"); err != nil {
			logf("建\"设置\"快捷方式失败：%v", err)
		} else {
			logf("已建快捷方式 %s", p)
		}
		if p, err := makeShortcut(d, "天气屏保 · 启动", warmupExe, "/s"); err != nil {
			logf("建\"启动\"快捷方式失败：%v", err)
		} else {
			logf("已建快捷方式 %s", p)
		}
	}

	logf("=== 安装完成 ===")
	logf("屏保程序 = %s；设置 = %s /c；启动 = %s /s", warmupExe, warmupExe, warmupExe)
	setStatus("安装完成")
}

func rollback(why string) {
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
	defer procPostMessageW.Call(uintptr(hMain), wmAppDone, 0, 0)
	defer func() {
		if r := recover(); r != nil {
			logf("内部错误：%v", r)
			setStatus("卸载失败")
		}
	}()
	logf("=== 卸载开始 ===")
	prev, _ := loadState("PreviousScr")
	logf("安装前记录的屏保程序 = %q", prev)

	// 删 System32 里的文件需要提权
	if fileExists(warmupExe) {
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
	if err := removeARP(); err != nil {
		logf("删除 Add/Remove Programs 项失败：%v", err)
	} else {
		logf("已删除 Add/Remove Programs 项")
	}
	logf("=== 卸载完成 ===")
	setStatus("卸载完成")
}

// ───────────────────────── 注册表 ─────────────────────────

func readScrSave() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue("SCRNSAVE.EXE")
	if err == registry.ErrNotExist {
		return "", nil
	}
	return s, err
}

func writeScrSave(v string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("SCRNSAVE.EXE", v)
}

func deleteScrSave() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue("SCRNSAVE.EXE"); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// readCPInt 读 Control Panel\Desktop 里的数值项。
// **这些项在低性能 x86 小主机上是 REG_SZ 字符串**（2026-10-16 实测：
// SCRNSAVE.EXE / ScreenSaveActive / ScreenSaveTimeOut 类型全 = String，
// 所以读也按字符串读、解析成 int；写同理必须写字符串（见 setCPStr）。
func readCPInt(key, name string) int {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	sv, _, err := k.GetStringValue(name)
	if err == nil {
		var n int
		if _, e := fmt.Sscanf(sv, "%d", &n); e == nil {
			return n
		}
		return 0
	}
	// 兼容：万一某台机器上是 DWORD
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0
	}
	return int(v)
}

// setCPStr 以 **REG_SZ** 写 Control Panel\Desktop 的数值项（注册表类型 = String）。
// 写成 DWORD 会让屏保设置失效，故这里一律写字符串。
func setCPStr(key, name string, v int) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, strconv.Itoa(v))
}

func saveState(name, v string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, stateKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, v)
}

func loadState(name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, stateKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue(name)
	if err == registry.ErrNotExist {
		return "", nil
	}
	return s, err
}

func writeARP() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	exe, _ := os.Executable()
	if v := k.SetStringValue("DisplayName", appName); v != nil {
		return v
	}
	if v := k.SetStringValue("DisplayVersion", appVersion); v != nil {
		return v
	}
	if v := k.SetStringValue("Publisher", "weatherscr"); v != nil {
		return v
	}
	if v := k.SetStringValue("InstallLocation", `C:\Windows\System32`); v != nil {
		return v
	}
	if v := k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" --uninstall`, exe)); v != nil {
		return v
	}
	// Add/Remove Programs 这三项是标准 DWORD（与 Control Panel\Desktop 的字符串口径不同）
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", 12) // KB
	return nil
}

func removeARP() error {
	return registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
}

// broadcastSettingChange 广播 WM_SETTINGCHANGE（注册表改了 explorer 才认 SCRNSAVE.EXE）。
func broadcastSettingChange() error {
	p, _ := windows.UTF16PtrFromString("Environment")
	r, _, err := procSendNotifyMessageW.Call(0xffff, // HWND_BROADCAST
		uintptr(wmSettingChange), 0, uintptr(unsafe.Pointer(p)))
	_ = r
	if err != nil && err != syscall.Errno(0) {
		return err
	}
	return nil
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
func makeShortcut(dir, name, target, args string) (string, error) {
	lnk := filepath.Join(dir, name+".lnk")
	ps := fmt.Sprintf(
		"$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');"+
			"$s.TargetPath='%s';$s.Arguments='%s';$s.WorkingDirectory='C:\\Windows\\System32';"+
			"$s.Description='WeatherScr';$s.IconLocation='%s,0';$s.Save()",
		lnk, target, args, target)
	return lnk, runPS(ps)
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

// ───────────────────────── 提权子进程模式 ─────────────────────────

func main() {
	runtime.LockOSThread()
	args := os.Args[1:]

	switch {
	case len(args) >= 3 && args[0] == "--copy-only":
		// args: --copy-only <src> <dst> <marker>
		marker := args[3]
		err := copyFile(args[1], args[2])
		if err == nil {
			_ = os.WriteFile(marker, []byte("COPY_OK"), 0o644)
		} else {
			_ = os.WriteFile(marker, []byte("COPY_FAIL "+err.Error()), 0o644)
		}
		return
	case len(args) >= 2 && args[0] == "--delete-only":
		marker := args[2]
		err := os.Remove(args[1])
		if err == nil || os.IsNotExist(err) {
			_ = os.WriteFile(marker, []byte("DEL_OK"), 0o644)
		} else {
			_ = os.WriteFile(marker, []byte("DEL_FAIL "+err.Error()), 0o644)
		}
		return
	case len(args) >= 1 && args[0] == "--uninstall":
		// 从 Add/Remove Programs 进来：起 GUI 并直接执行卸载
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

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
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	// GetStockObject 由 **gdi32.dll** 导出，不在 user32.dll。挂在 user32 上时
	// x/sys 的 LazyProc 在首次调用处直接 panic（实机取证：
	// `panic: Failed to find GetStockObject procedure in user32.dll`，
	// 栈帧 main.wndProc cmd/setup/main.go:264 ← showPage:537 的 UpdateWindow 触发绘制）。
	procGetStockObject        = gdi32.NewProc("GetStockObject")
	procAdjustWindowRectEx    = user32.NewProc("AdjustWindowRectEx") // 证据 E3：客户区 ⇄ 窗口尺寸换算
	procGetWindowLongW        = user32.NewProc("GetWindowLongW")     // 取 GWL_STYLE / GWL_EXSTYLE（64 位即 GetWindowLongPtrW 的 32 位形态）
	procEnableWindow          = user32.NewProc("EnableWindow")       // 证据 E4：禁用按钮必须用 EnableWindow
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procSendNotifyMessageW    = user32.NewProc("SendNotifyMessageW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")

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
	// 证据 E1（spec §0.2）：STATIC 控件自带 COLOR_BTNFACE 灰底 ⇒ 截图里正文区是一整块灰面板。
	// WM_CTLCOLORSTATIC 返回 GetStockObject(WHITE_BRUSH) 后，正文底色与窗口白底（hbrBackground=1）一致。
	wmCtlColorStatic = 0x0138

	bnClicked = 1

	whiteBrush = 0 // GetStockObject 的 WHITE_BRUSH 索引

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
	idBtnBack    = 104
	idBtnNext    = 105
	idBtnFinish  = 106
	idBtnCancel  = 107
	idLog        = 110
	idStatus     = 111

	// 向导页（固定页序：欢迎 → 说明与准备 → 安装（日志+进度）→ 完成）
	pageWelcome  = 0
	pageIntro    = 1
	pageProgress = 2
	pageFinish   = 3

	// SystemParametersInfoW 取当前显示器工作区（窗口尺寸自适应用）
	SPI_GETWORKAREA = 0x0030

	// 向导主窗口样式：wizardWindowSize 与 CreateWindowExW 必须用同一个值，
	// 这样 AdjustWindowRectEx 算出的非客户区才是这个窗口真正的非客户区（证据 E3）。
	wizardStyle = wsOverlappedWindow

	// GetWindowLongW 的索引（nIndex）
	gwlStyle      int32 = -16 // GWL_STYLE
	gwlExStyle    int32 = -20 // GWL_EXSTYLE
	swpNoZorder         = 0x0040
	swpNoActivate       = 0x0010

	// 自定义消息：工作线程请 GUI 线程追加一行日志 / 结束
	wmAppLog    = 0x0400
	wmAppDone   = 0x0401
	wmAppStatus = 0x0402
	// 证据 E4：禁用/恢复按钮必须在 GUI 线程做（EnableWindow 是窗口所属线程的调用），
	// 工作线程用这条消息请 GUI 线程代做。
	wmAppButtons = 0x0403
)

var (
	hInst      windows.Handle
	hMain      windows.HWND
	hLog       windows.HWND
	hStatus    windows.HWND
	hBtnCancel windows.HWND
	fontH      windows.Handle

	// 向导状态：页状态机 + 页内控件（每页独立创建销毁）+ **客户区**尺寸（控件坐标是客户区坐标，见证据 E3）
	currentPage      int32
	startPage        int32 = pageWelcome
	pageChildren     []windows.HWND
	pageBtns         []windows.HWND
	clientW, clientH int32
	installing       bool
	uninstalling     bool

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

// winRect = Win32 RECT（winuser.h），AdjustWindowRectEx / SPI_GETWORKAREA 的入参布局
type winRect struct{ left, top, right, bottom int32 }

// getWindowLong 取窗口的 GWL_STYLE / GWL_EXSTYLE（证据 E3：换算前要知道窗口真正的样式）。
// 64 位下 GetWindowLongPtrW 是规范名，GetWindowLongW 对 GWL_STYLE/GWL_EXSTYLE 同样可用
// （样式位是 32 位，取低 32 位即可）。
func getWindowLong(h windows.HWND, idx int32) uint32 {
	r, _, _ := procGetWindowLongW.Call(uintptr(h), uintptr(idx))
	return uint32(r)
}

// nonClientSize 用 user32.AdjustWindowRectEx 求某样式下的非客户区尺寸：
// 对空矩形 {0,0,0,0} 调用，返回的就是标题栏 + 边框 + 菜单的总尺寸。
// 窗口尺寸 = 客户区尺寸 + 该尺寸（AdjustWindowRectEx 只做加法，减法即取反）。
func nonClientSize(style, exStyle uint32) (int32, int32) {
	rc := winRect{}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), uintptr(style), 0, uintptr(exStyle))
	return rc.right - rc.left, rc.bottom - rc.top
}

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
	btnInstallLabel = "安装"
	btnBackLabel    = "上一步"
	btnNextLabel    = "下一步"
	btnFinishLabel  = "完成"
	btnCancelLabel  = "取消"
)

func wndProc(hwnd windows.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmCtlColorStatic:
		// 证据 E1（spec §0.2 / §1.1）：STATIC 默认灰底（COLOR_BTNFACE）⇒ 正文区在截图里是一整块灰面板。
		// 返回 GetStockObject(WHITE_BRUSH) 的句柄 ⇒ 正文底色与窗口白底一致。
		brush, _, _ := procGetStockObject.Call(whiteBrush)
		return brush
	case wmAppButtons:
		setButtonsEnabled(wp != 0)
		return 0
	case wmCommand:
		id := wp & 0xffff
		switch id {
		case idBtnNext:
			if currentPage < pageProgress {
				showPage(currentPage + 1)
			}
		case idBtnBack:
			if currentPage > pageWelcome && currentPage < pageFinish {
				showPage(currentPage - 1)
			}
		case idBtnInstall:
			// 证据 E4（spec §0.4）：安装进行中第二次点最右按钮又跑了一遍 runInstall，
			// 把 PreviousScr 覆盖成"安装后的值"。安装中直接忽略安装按钮。
			if installing {
				return 0
			}
			installing = true
			setStatus("安装中…")
			go runInstall()
		case idBtnFinish, idBtnCancel:
			procPostQuitMessage.Call(0)
		}
		return 0
	case wmClose:
		procPostQuitMessage.Call(0)
		return 0
	case wmAppDone:
		if installing {
			installing = false
			showPage(pageFinish)
		} else if !uninstalling {
			// --uninstall 模式下安装按钮在本页创建时就已禁用（证据 E4），卸载结束后不恢复它
			setButtonsEnabled(true)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return r
}

// setButtonsEnabled 安装/卸载进行中禁用页内导航按钮（安装/上一步/下一步），"取消"保持可用。
//
// 证据 E4（spec §0.4）：旧实现发的是 `SendMessageW(h, 0x00C7 /*WM_ENABLE*/)` —— Win32 没有
// WM_ENABLE 这条消息，控件走 DefWindowProc 直接忽略 ⇒ 实机上安装进行中按钮仍可点，
// 第二次点击又跑了一遍 runInstall，把 PreviousScr 覆盖成安装后的值。
// 正确做法是 user32!EnableWindow（必须在窗口所属线程调用，工作线程经 wmAppButtons 转投）。
func setButtonsEnabled(on bool) {
	var en uintptr
	if on {
		en = 1
	}
	for _, h := range pageBtns {
		if h != 0 && h != hBtnCancel {
			procEnableWindow.Call(uintptr(h), en)
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

// wizardWindowSize 向导窗口尺寸：**入参是期望的客户区尺寸**，返回客户区尺寸 + 窗口尺寸 + 左上角。
//
// 客户区 vs 窗口尺寸（证据 E3，spec §0.3）：控件坐标是**客户区**坐标，CreateWindowExW 的 w/h 是
// **窗口**尺寸，两者差一圈非客户区。旧代码把窗口高度 460 当客户区高度布局 ⇒ 按钮 y=414 起、高 34
// ⇒ 414..448 落在客户区（460 − 标题栏 31 − 上下边框 8 = 421）之外，被裁约 27px。
//
// 换算用 user32.AdjustWindowRectEx（procAdjustWindowRectEx，样式取 wizardStyle，扩展样式 0，
// 菜单 0；WS_CHILD 不需要，本窗口是 overlapped 顶层窗口）：对空矩形调用得到该样式的非客户区尺寸，
// 窗口尺寸 = 客户区尺寸 + 非客户区尺寸。
//
// 工作区 = SystemParametersInfoW(SPI_GETWORKAREA=0x0030)：当前显示器去掉任务栏的可用区
// （SM_CXWORKAREA 只反映主屏，竖屏/多屏下会算错，故换 SPI）。
// 顺序：客户区 ⇒ 窗口 ⇒ 夹到工作区 90% ⇒ 夹最小窗口 480×420（三个 150px 按钮并排的宽度下限 +
// 日志区可读下限）⇒ 居中 ⇒ 反算客户区。判据：按钮底边 ≤ 客户区高度（见 buttonTopY 的测试）。
// 只算尺寸与居中，不碰安装逻辑。
func wizardWindowSize(wantW, wantH int32) (clientW, clientH, winW, winH, x, y int32) {
	var area [4]int32 // RECT：left, top, right, bottom
	procSystemParametersInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&area[0])), 0)
	workw := area[2] - area[0]
	workh := area[3] - area[1]
	if workw <= 0 || workh <= 0 {
		smw, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
		smh, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
		if workw <= 0 {
			workw = int32(smw)
		}
		if workh <= 0 {
			workh = int32(smh)
		}
		area[0], area[1] = 0, 0
	}
	ncW, ncH := nonClientSize(wizardStyle, 0) // 标题栏 + 边框（AdjustWindowRectEx 的同一套数值）
	const minW, minH = 480, 420               // 下限作用在**窗口**尺寸上
	W, H := wantW+ncW, wantH+ncH              // 客户区 ⇒ 窗口
	if max := workw * 9 / 10; W > max {
		W = max
	}
	if max := workh * 9 / 10; H > max {
		H = max
	}
	if W < minW {
		W = minW
	}
	if H < minH {
		H = minH
	}
	x = area[0] + (workw-W)/2
	y = area[1] + (workh-H)/2
	if x < area[0] {
		x = area[0]
	}
	if y < area[1] {
		y = area[1]
	}
	// 窗口 ⇒ 客户区（控件布局用的是客户区坐标，按钮行必须落在这里面）
	return W - ncW, H - ncH, W, H, x, y
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

	// 尺寸口径：内容需要**客户区** 620×460，换算成窗口尺寸后夹到工作区 90%、最小窗口 480×420、居中。
	cw, ch, W, H, x, y := wizardWindowSize(620, 460)
	clientW, clientH = cw, ch

	hMain = mkWindow("WeatherScrSetupWnd", appName+" 安装器 v"+appVersion,
		wizardStyle|wsVisible, x, y, W, H, 0, 0)
	if hMain == 0 {
		fmt.Fprintln(os.Stderr, "创建窗口失败")
		os.Exit(2)
	}

	// 用窗口**实际**样式复核非客户区：GetWindowLongW 取 GWL_STYLE/GWL_EXSTYLE ⇒ AdjustWindowRectEx。
	// 若与 wizardStyle 假设值不同（主题/DPI 差异），按实际值重算窗口尺寸并落位，
	// 保证按钮行仍落在客户区内（证据 E3 的判据）。
	ncW, ncH := nonClientSize(getWindowLong(hMain, gwlStyle), getWindowLong(hMain, gwlExStyle))
	if ncW != ncWidthPx || ncH != ncHeightPx {
		W, H = cw+ncW, ch+ncH
		if W < 480 {
			W = 480
		}
		if H < 420 {
			H = 420
		}
		procSetWindowPos.Call(uintptr(hMain), 0, uintptr(x), uintptr(y), uintptr(W), uintptr(H),
			uintptr(swpNoZorder|swpNoActivate))
		clientW, clientH = W-ncW, H-ncH
	}

	showPage(startPage)

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

// mkStatic/mkButton 建页内控件并登记（切页时统一销毁，见 showPage）。
func mkStatic(label string, x, y, w, h int32) windows.HWND {
	// 证据 E2（spec §0.1）：Win32 里 SS_LEFT = 0x0000、SS_CENTER = 0x0001，
	// 旧代码在样式里加上了 SS_CENTER 的那个位、却注释成 SS_LEFT ⇒ 实机正文是居中排列。
	// 正解：SS_LEFT = 0x0000，样式就是 wsChild|wsVisible（不再带那个位）。
	hwnd := mkWindow("STATIC", label, wsChild|wsVisible /*SS_LEFT = 0x0000*/, x, y, w, h, hMain, 0)
	pageChildren = append(pageChildren, hwnd)
	return hwnd
}

func mkButton(label string, x, y, w, h int32, id uintptr) windows.HWND {
	hwnd := mkWindow("BUTTON", label, wsChild|wsVisible|wsTabStop, x, y, w, h, hMain, id)
	pageChildren = append(pageChildren, hwnd)
	pageBtns = append(pageBtns, hwnd)
	return hwnd
}

// showPage 向导页状态机：销毁上一页控件、创建新页控件。
// 现有控件是绝对坐标，分页后每页各自布局；日志 EDIT 只在"安装"页存在，
// 跨线程写入方式不变（appendLog 的 SendMessageW 口径，见其注释）。
func showPage(p int32) {
	for _, h := range pageChildren {
		if h != 0 {
			procDestroyWindow.Call(uintptr(h))
		}
	}
	pageChildren = nil
	pageBtns = nil
	hLog, hStatus, hBtnCancel = 0, 0, 0
	currentPage = p
	W, H := clientW, clientH // 客户区坐标：控件坐标与窗口尺寸不是一回事（证据 E3）
	by := buttonTopY(H)      // 按钮行顶边 = 客户区高度 − (按钮高 34 + 底边留白 12)
	switch p {
	case pageWelcome:
		mkStatic("欢迎安装 "+appName+" v"+appVersion, 14, 12, W-28, 24)
		mkStatic("本向导将把屏保安装到 C:\\Windows\\System32，并创建桌面快捷方式。\r\n\r\n"+
			"· 只为写入 System32 弹一次 UAC\r\n"+
			"· 不修改 ScreenSaveTimeOut（保留机器现有等待时间）\r\n"+
			"· 卸载时恢复安装前的屏保设置", 14, 44, W-28, H-44-58)
		hBtnCancel = mkButton(btnCancelLabel, 14, by, btnW, btnH, idBtnCancel)
		mkButton(btnNextLabel, W-164, by, btnW, btnH, idBtnNext)
	case pageIntro:
		prevLine := ""
		if prev, err := readScrSave(); err != nil {
			prevLine = fmt.Sprintf("当前屏保值读取失败：%v（安装第一步会重试并记录回退值）", err)
		} else if prev == "" {
			prevLine = "当前屏保程序 = 无（卸载时将删除 SCRNSAVE.EXE 值）"
		} else {
			prevLine = fmt.Sprintf("当前屏保程序 = %q（卸载时将恢复此值）", prev)
		}
		mkStatic("说明与准备", 14, 12, W-28, 24)
		mkStatic("安装路径：C:\\Windows\\System32\\WeatherScr.scr\r\n\r\n"+
			"UAC 说明：只为写入 System32 弹一次 UAC；HKCU 注册表与桌面快捷方式\r\n"+
			"以当前登录账户身份写入（提权进程写 HKCU 会落进提权账户的 hive）。\r\n\r\n"+
			"提权复制失败时当场回滚注册表。\r\n\r\n"+prevLine, 14, 44, W-28, H-44-58)
		mkButton(btnBackLabel, 14, by, btnW, btnH, idBtnBack)
		hBtnCancel = mkButton(btnCancelLabel, 164, by, btnW, btnH, idBtnCancel)
		mkButton(btnNextLabel, W-164, by, btnW, btnH, idBtnNext)
	case pageProgress:
		hStatus = mkStatic("就绪。点击\"安装\"开始；日志实时显示在下方。", 14, 12, W-28, 24)
		hLog = mkWindow("EDIT", "",
			wsChild|wsVisible|wsBorder|wsVScroll|esReadOnly|esMultiline|wsTabStop,
			14, 44, W-28, H-44-58, hMain, idLog)
		pageChildren = append(pageChildren, hLog)
		if len(logLines) > 0 {
			setText(hLog, strings.Join(logLines, "\r\n"))
		}
		procSendMessageW.Call(uintptr(hLog), 0x00B7 /*WM_VSCROLL*/, 7 /*SB_BOTTOM*/, 0)
		mkButton(btnBackLabel, 14, by, btnW, btnH, idBtnBack)
		hBtnCancel = mkButton(btnCancelLabel, 164, by, btnW, btnH, idBtnCancel)
		hInstall := mkButton(btnInstallLabel, W-164, by, btnW, btnH, idBtnInstall)
		if uninstalling {
			// --uninstall 直进本页：只跑卸载，安装按钮禁用（证据 E4：必须用 EnableWindow，
			// 旧的 0x00C7 /*WM_ENABLE*/ 不是 Win32 消息，控件直接忽略 ⇒ 实机上按钮仍可点）
			procEnableWindow.Call(uintptr(hInstall), 0)
		}
	case pageFinish:
		mkStatic("安装完成", 14, 12, W-28, 24)
		mkStatic("屏保已安装：C:\\Windows\\System32\\WeatherScr.scr\r\n\r\n"+
			"· 桌面快捷方式 \"天气屏保 · 设置\" 打开设置窗口\r\n"+
			"· 桌面快捷方式 \"天气屏保 · 启动\" 立即预览\r\n"+
			"· 卸载：控制面板 › 程序和功能，或运行本安装器 --uninstall", 14, 44, W-28, H-44-58)
		mkButton(btnFinishLabel, W-164, by, btnW, btnH, idBtnFinish)
	}
	procUpdateWindow.Call(uintptr(hMain))
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

	// 证据 E4（spec §0.4 / §1.4）：一进入安装就禁用安装/上一步/下一步（只留"取消"）。
	// 旧版只发了不存在的 WM_ENABLE 消息 ⇒ 按钮实际未禁用，第二次点击又跑了一遍 runInstall，
	// 把 PreviousScr 覆盖成"安装后的值"（C:\Windows\System32\SRPSS.scr → ...\WeatherScr.scr）。
	// EnableWindow 必须在 GUI 线程调用 ⇒ 经 wmAppButtons 转投给窗口过程。
	procPostMessageW.Call(uintptr(hMain), wmAppButtons, 0, 0)

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

	logf("=== 安装完成 ===")
	logf("屏保程序 = %s；设置 = %s %s；启动 = %s /s", warmupExe, settingsTarget, settingsArgs, warmupExe)
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
	// 证据 E5（spec §0.5）：记录值就是本程序自己的安装路径时（双安装把回滚值覆盖成了安装后的值），
	// 恢复它等于把 SCRNSAVE.EXE 指向一个即将被卸载删除的文件 ⇒ 按"安装前没有屏保"处理。
	if isSelfScrPath(prev, warmupExe) {
		logf("记录值 = 本程序安装路径（自指）⇒ 按\"安装前没有屏保\"处理，卸载后删除 SCRNSAVE.EXE 值")
		prev = ""
	}

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

// ───────────────────────── 提权子进程模式 ─────────────────────────

// runSettingsShortcut 实现 `WeatherScr_setup.exe --settings`：
// 用**直接 CreateProcess** 启动 System32 里的屏保并传 `/c`（配置对话框），
// 启动成功后本进程立即退出。
//
// 取证 36.16.5：`HKCR\scrfile\shell\open\command = "%1" /S` ⇒ 任何经 shell
// 关联的启动（双击、快捷方式、Start-Process）参数都被强制替换成 `/S` ⇒
// "设置"快捷方式永远开全屏屏保。同目标用直接 CreateProcess 传 `/c`
// 得到的是设置窗口（title=WeatherScr 设置，client=624x1061）。
//
// 因此本分支**不得**用 ShellExecute / 文件关联：那正是被关联劫持的那条路。
// lpApplicationName = 目标 exe，lpCommandLine = `"目标" /c`（带引号，
// 与取证里那条命令行逐字同口径）。
func runSettingsShortcut() int {
	app, err := windows.UTF16PtrFromString(warmupExe)
	if err != nil {
		return 2
	}
	cl, err := windows.UTF16PtrFromString(`"` + warmupExe + `" /c`)
	if err != nil {
		return 2
	}
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	// 继承句柄关掉、创建标志 0：目标是自己家的 GUI 程序，不需要控制台，
	// 也不需要新窗口组（置顶由被启动方 normalizeDialog 负责）。
	if err := windows.CreateProcess(app, cl, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		logf("启动设置窗口失败：%v", err)
		return 1
	}
	// 进程句柄不接（fire-and-forget）：设置窗口自己跑完生命周期
	_ = windows.CloseHandle(pi.Process)
	_ = windows.CloseHandle(pi.Thread)
	return 0
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

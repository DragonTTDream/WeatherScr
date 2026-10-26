//go:build windows

package main

// R5 拆分卡 10（plans/active.md:217，证据 evidence/p8-split-wizard-settings-20261023.md）：
// 向导页面机、页面顺序与完成页文案构造（showPage / wndProc / 完成按钮）。
// 原 cmd/setup/main.go:56-266、305-669 逐字搬移，零行为变化；完成页文案常量本体在 finishpage.go。

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	// kernel32：卸载前停运行中的屏保进程 + 轮询「文件可删除」（证据 out/notes-b5c.md §3：
	// 运行中的同名进程以镜像方式占用 System32 文件 ⇒ 删除被拒；终止后锁仍短暂残留）。
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

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
	// 标题栏图标（证据 spec/publish-window-icons.md §0：注册类处从未给 hIcon/hIconSm 赋值）。
	// LoadImageW / LoadIconW 均属 user32.dll ⇒ 不进 nonUser32Exports 表
	// （internal/setupcheck/procdll_test.go 的默认方向就是 user32）。
	procLoadImageW = user32.NewProc("LoadImageW")
	procLoadIconW  = user32.NewProc("LoadIconW")
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

	// 进程枚举与终止（kernel32）。过程名与 DLL 的配对由 internal/setupcheck/procdll_test.go
	// 闸门强制登记（挂错 DLL 会在首次调用处 panic，Linux 侧编译发现不了）。
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procTerminateProcess         = kernel32.NewProc("TerminateProcess")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procCreateFileW              = kernel32.NewProc("CreateFileW")
	// 图标资源的模块句柄：spec §1 点名 kernel32!GetModuleHandleW(nil)（不用 CurrentProcess）。
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	// B12 重命名交换：MoveFileExW 属 **kernel32.dll**（简报 spec/publish-rename-swap-brief.md §2），
	// 配对已逐字登记进 internal/setupcheck/procdll_test.go 的 nonUser32Exports（"MoveFileExW":"kernel32"）。
	// 机制出处：unprotect.it/technique/process-ghosting + stackoverflow.com/questions/799656
	// （正在运行的 exe 可以被重命名 ⇒ 改名让开、原名重建）。实现见 renameswap_windows.go:moveFileEx。
	procMoveFileExW = kernel32.NewProc("MoveFileExW")
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
	// D5（spec §1.2）：完成页标题/正文句柄化，写法照 hStatus（main.go:532 + setStatus）
	hFinishTitle windows.HWND
	hFinishBody  windows.HWND
	fontH        windows.Handle

	// 向导状态：页状态机 + 页内控件（每页独立创建销毁）+ **客户区**尺寸（控件坐标是客户区坐标，见证据 E3）
	currentPage      int32
	startPage        int32 = pageWelcome
	pageChildren     []windows.HWND
	pageBtns         []windows.HWND
	clientW, clientH int32
	installing       bool
	uninstalling     bool
	// D5（spec §1.1）：安装结果状态与失败原因，供完成页文案取值
	installOK bool
	// R13（spec §2）：卸载结果状态（核验通过=true），供完成页文案取值
	uninstallOK bool
	lastFail    string
	// R16-C：lastVerify 声明在无 build tag 的 installguard.go（finishpage.go 无 tag，需要跨平台可见）

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
			// B17（2026-10-21 实机定因，out/setup-full.log 18:41:32）：`--uninstall` 起的是同一套
			// 向导，卸载页上的"安装"按钮只是 **disabled**；程序化 BM_CLICK（探针/自动化工具）
			// 对 disabled 按钮照样能触发 BN_CLICKED ⇒ 卸载完顺手又跑了一遍 runInstall，
			// 把刚删掉的 System32 文件写回来（"卸载后文件仍在"的第三个来源）。
			// 卸载态直接忽略安装按钮，不只靠 EnableWindow。
			if uninstalling {
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
			// D5（spec §1.3）：runInstall 的**全部** return（含失败分支）都发 wmAppDone
			// （main.go:603），旧代码在这里无条件切完成页、标题写死「安装完成」⇒ 失败时也说「安装完成」。
			// 现在按 installOK 改写标题与正文，文案只取自 finishpage.go 的纯函数。
			if hFinishTitle != 0 {
				setText(hFinishTitle, finishTitle(installOK))
			}
			if hFinishBody != 0 {
				setText(hFinishBody, finishBody(installOK, lastFail))
			}
		} else if uninstalling {
			// R13（spec §3，根因 main.go:369-372 旧 `else if !uninstalling` ⇒ 卸载跑完无任何 showPage，
			// 页面停在 pageProgress）：卸载跑完（成功与失败都算跑完）也切完成页，并启用「完成」按钮。
			// showPage 会重建页并把控件追加进 pageBtns（main.go:581/595），pageFinish 页只有「完成」按钮。
			// 不调用 setButtonsEnabled（其本体禁改，且跳过 hBtnCancel 的语义与本页无关）。
			showPage(pageFinish)
			if hFinishTitle != 0 {
				setText(hFinishTitle, finishTitleFor(opUninstall, uninstallOK))
			}
			if hFinishBody != 0 {
				setText(hFinishBody, finishBodyFor(opUninstall, uninstallOK, lastFail))
			}
			for _, h := range pageBtns {
				if h != 0 {
					procEnableWindow.Call(uintptr(h), 1)
				}
			}
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

// loadModuleIcon 按候选 ID 依次 LoadImageW，返回命中的 ID 与句柄（都没命中 ⇒ (0, 0)）。
// 本函数只做 Win32 调用；「取第一个非 0 句柄」的择一逻辑在 iconres.go 的 pickIconResourceID
// （无 build tag ⇒ Linux 侧有表驱动单测）。
func loadModuleIcon(hInst uintptr, ids []int, px int32) (int, uintptr) {
	handles := make([]uintptr, len(ids))
	for i, id := range ids {
		h, _, _ := procLoadImageW.Call(hInst, uintptr(id), uintptr(imageIcon),
			uintptr(px), uintptr(px), uintptr(lrShared))
		handles[i] = h
	}
	id, hand, _ := pickIconResourceID(ids, handles)
	return id, hand
}

// wizardIcons 从**本模块的图标资源**取标题栏/任务栏图标，填 wndClassExW.hIcon / hIconSm。
//
// 证据 spec/publish-window-icons.md §0：注册窗口类处从未给这两个字段赋值 ⇒ 标题栏与
// 任务栏是系统通用图标。资源 ID 首选 1（brief §2：`rsrc -ico` 嵌进 syso 的是
// RT_GROUP_ICON ID 1；实测 cmd/scr 的 syso 就是 1）；实测 cmd/setup 的 syso（图标 + manifest）
// 把图标组排到了 ID 2，故候选序列 = iconResourceIDs（见 iconres.go 的注释与
// out/notes-window-icons.md 的解析输出）。
//
// 口径：
//   - 模块句柄 = kernel32!GetModuleHandleW(nil)（spec §1 点名）；
//   - 边长 = iconSizes()：hIcon 32 px、hIconSm = GetSystemMetrics(SM_CXSMICON) px（brief §1）；
//   - MAKEINTRESOURCE(id) 在 Go 侧 = 把 uintptr(id) 当 *uint16 传（brief §2）；
//   - 任何一路取不到都走 LoadIconW(0, IDI_APPLICATION) 兜底，**绝不 panic**（brief §2）；
//   - 禁止硬编码路径读 .ico —— 发布树里 .ico 只作为构建期资源（spec §1）。
//
// 纯逻辑（ID 候选择一、尺寸换算、兜底选择）在 iconres.go（无 build tag，Linux 侧有表驱动单测）。
func wizardIcons() (windows.Handle, windows.Handle) {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	cxIcon, _, _ := procGetSystemMetrics.Call(smCxIcon)
	cxSmIcon, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	bigPx, smallPx := iconSizes(int32(cxSmIcon))
	bigID, big := loadModuleIcon(hInst, iconResourceIDs, bigPx)
	smallID, small := loadModuleIcon(hInst, iconResourceIDs, smallPx)
	fallback, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	icon, iconSm, usedFallback := iconHandles(big, small, fallback)
	logf("标题栏图标：SM_CXICON=%d SM_CXSMICON=%d ⇒ %dpx/%dpx；句柄 大=%#x(ID %d) 小=%#x(ID %d) 兜底=%#x 走兜底=%v",
		int32(cxIcon), int32(cxSmIcon), bigPx, smallPx, big, bigID, small, smallID, fallback, usedFallback)
	return windows.Handle(icon), windows.Handle(iconSm)
}

func runGUI() {
	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	cur, _, _ := procLoadCursorW.Call(0, 32512 /*IDC_ARROW*/)
	wc.hCursor = windows.Handle(cur)
	wc.hBackground = 6 // COLOR_WINDOW+6
	// 证据 spec/publish-window-icons.md §0：这两项此前从未赋值 ⇒ 标题栏/任务栏是通用图标。
	wc.hIcon, wc.hIconSm = wizardIcons()
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
	hFinishTitle, hFinishBody = 0, 0
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
		// D5（spec §1.2）：标题与正文句柄化（写法照 hStatus），文案取自 finishpage.go 的纯函数；
		// 建页时先按成功态落文案，wmAppDone 分支（main.go:316 起）随后按 installOK 改写。
		hFinishTitle = mkStatic(finishTitle(true), 14, 12, W-28, 24)
		hFinishBody = mkStatic(finishBody(true, ""), 14, 44, W-28, H-44-58)
		mkButton(btnFinishLabel, W-164, by, btnW, btnH, idBtnFinish)
	}
	procUpdateWindow.Call(uintptr(hMain))
}

//go:build windows

package main

import (
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/DragonTTDream/WeatherScr/ui"
)

// scrDbgFlag 由 main 设置的调试态（coverLoop 独立线程要用，不能调 scrDebug()
// 以免与本线程的样式判定分叉。
var scrDbgFlag bool

// 窗口显示与铺满。
//
// 根因（实测）：`ShowWindow` 向**创建该窗口的线程**
// 同步发送 `WM_SHOWWINDOW`；gioui 在 `app/os_windows.go:100-130` 用
// `runtime.LockOSThread()` 的专用线程建窗并跑消息循环，本实现的帧循环在另一个
// goroutine ⇒ 从帧循环或任何 goroutine 直接调 `ShowWindow` 都跨线程 ⇒ 永久阻塞。
//
// 正道：`app.Window.Run(f)` —— `app/window.go:321-326` 原文
// "Run f in the same thread as the native window event loop, and wait for f to
// return. If the window has not yet been created, Run calls f directly."
// 本文件里的 showAndCover / ensureCover **只允许在 w.Run 内部调用**（main.go 负责）。
//
// 目标样式（社区 wischik + 上游 SRPSS 同口径）：
//
//	GWL_STYLE   := WS_POPUP | WS_VISIBLE
//	GWL_EXSTYLE := WS_EX_TOPMOST | WS_EX_TOOLWINDOW   // 必须清掉 WS_EX_APPWINDOW
//	SetWindowPos(hwnd, HWND_TOPMOST, rcMonitor, SWP_NOACTIVATE|SWP_FRAMECHANGED)
//	ShowWindow(hwnd, SW_SHOW)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procShowWindow         = user32.NewProc("ShowWindow")
	procFindWindowW        = user32.NewProc("FindWindowW")
	procMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procSetForegroundWndow = user32.NewProc("SetForegroundWindow")
	procSetFocus           = user32.NewProc("SetFocus")
	procGetForegroundWndow = user32.NewProc("GetForegroundWindow")

	// 主显示器工作区（扣掉任务栏）：SystemParametersInfoW(SPI_GETWORKAREA)。
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")

	// 样式位读写。gioui 自己用的就是 Get/SetWindowLongPtrW
	// （app/internal/windows/windows.go:467,495），32 位下退回 ...W。
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongW   = user32.NewProc("SetWindowLongW")
	procGetWindowLongW   = user32.NewProc("GetWindowLongW")

	// 以下三件是 findOwnWindow 的备用查找路径（见 findOwnWindow 注释）。
	procEnumWindows        = user32.NewProc("EnumWindows")
	procGetClassNameW      = user32.NewProc("GetClassNameW")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
)

// enumCB 是 EnumWindows 的回调目标（由 windows.NewCallback 生成 trampoline）。
var enumCB func(hwnd, lparam uintptr) bool

// enumTrampoline 被 C 侧调用；返回 0 让 EnumWindows 提前停止遍历。
//
//go:noinline
func enumTrampoline(hwnd, lparam uintptr) uintptr {
	if enumCB == nil {
		return 1
	}
	if enumCB(hwnd, lparam) {
		return 1
	}
	return 0
}

var enumProcAddr = windows.NewCallback(enumTrampoline)

// winClassName 取窗口类名（字符串，不走原子表）。
func winClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16PtrToString(&buf[0])
}

// winPID 取窗口所属进程 id。
func winPID(hwnd uintptr) uint32 {
	var id uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&id)))
	return id
}

const (
	// WS_POPUP / WS_VISIBLE / swShow* / hwndTop* 的官方值与口径改判记录
	// 已下沉到可移植层 covermath.go（闸门单测钉死在那边跑）。
	wsExTopmost    = 0x00000008
	wsExToolwindow = 0x00000080
	wsExAppwindow  = 0x00040000 // 必须清掉（实测 EXSTYLE=00040100 含它）

	swpNoMove       = 0x0002
	swpNoSize       = 0x0001
	swpNoZorder     = 0x0040
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	monitorDefaultToNearest = 2

	// GWL_STYLE = -16、GWL_EXSTYLE = -20、GWL_HWNDPARENT = -8
	// 来源：winuser.h（Microsoft docs SetWindowLongA 参数表）；
	// GWL_STYLE 亦见 gioui app/internal/windows/windows.go:199。
	gwlStyle      = int32(-16)
	gwlExstyle    = int32(-20)
	gwlHwndParent = int32(-8)

	// WS_OVERLAPPEDWINDOW = WS_CAPTION|WS_SYSMENU|WS_THICKFRAME|
	// WS_MINIMIZEBOX|WS_MAXIMIZEBOX = 0x00CF0000（实测 STYLE=06cf0000 含它）。
	// 判据实测：该组位必须为 0。
	wsOverlappedwindow = 0x00CF0000
)

type winRect struct {
	left, top, right, bottom int32
}

// monInfo 是 Win32 MONITORINFO：cbSize + rcMonitor + rcWork + dwFlags。
type monInfo struct {
	cbSize uint32
	rcMon  winRect
	rcWork winRect
	flags  uint32
}

// setWindowLong 读写样式位。64 位走 ...PtrW（gioui 用的那对），32 位退回 ...W。
func setWindowLong(hwnd uintptr, idx int32, val uintptr) uintptr {
	if procSetWindowLongPtr.Find() != nil {
		r, _, _ := procSetWindowLongPtr.Call(hwnd, uintptr(idx), val)
		return r
	}
	r, _, _ := procSetWindowLongW.Call(hwnd, uintptr(idx), val)
	return r
}

func getWindowLong(hwnd uintptr, idx int32) uintptr {
	if procGetWindowLongPtr.Find() != nil {
		r, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(idx))
		return r
	}
	r, _, _ := procGetWindowLongW.Call(hwnd, uintptr(idx))
	return r
}

// monitorRect 取窗口所在显示器的**整块** rcMonitor（含任务栏那条 40 px）。
func monitorRect(hwnd uintptr) (x, y, w, h int32, ok bool) {
	hmon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if hmon == 0 {
		logf("COVER=MonitorFromWindow 失败")
		return 0, 0, 0, 0, false
	}
	var mi monInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	r, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if r == 0 {
		logf("COVER=GetMonitorInfo 失败")
		return 0, 0, 0, 0, false
	}
	return mi.rcMon.left, mi.rcMon.top,
		mi.rcMon.right - mi.rcMon.left, mi.rcMon.bottom - mi.rcMon.top, true
}

// saverRect 目标矩形：正常 = 整块 rcMonitor；
// DEBUG = 1/4 尺寸且居中。
func saverRect(hwnd uintptr, dbg bool) (x, y, w, h int32, ok bool) {
	x, y, w, h, ok = monitorRect(hwnd)
	if !ok {
		return
	}
	if dbg {
		// 几何口径在 covermath.go:dbgQuarterRect（纯函数，闸门单测钉死）。
		qx, qy, qw, qh := dbgQuarterRect(x, y, w, h)
		return qx, qy, qw, qh, qw > 0 && qh > 0
	}
	return
}

// saverStyle 目标样式位与 z-order 插入点。
//
// DEBUG 下**不加 WS_EX_TOPMOST**，z-order 用 HWND_TOP。
func saverStyle(dbg bool) (style, exstyle, insertAfter uintptr) {
	style = wsPopup | wsVisible
	exstyle = wsExToolwindow // 不占任务栏格；WS_EX_APPWINDOW 因此被清掉
	insertAfter = hwndTop
	if !dbg {
		exstyle |= wsExTopmost
		insertAfter = hwndTopmost
	}
	return
}

var (
	coverLogged bool
	lastStyle   uintptr
	lastExstyle uintptr
)

// applySaverStyle 按实测顺序执行：
//
//	SetWindowLong(GWL_STYLE) → SetWindowLong(GWL_EXSTYLE) →
//	ShowWindow(SW_SHOWNORMAL) →
//	SetWindowPos(insertAfter, rcMonitor, SWP_NOACTIVATE|SWP_FRAMECHANGED|SWP_SHOWWINDOW) →
//	ShowWindow(SW_SHOW) → SetForegroundWindow → SetFocus
//
// 理由（2026-10-22 实机取证，b-cover-v1）：
//   - WS_EX_TOPMOST 必须经 SetWindowPos(HWND_TOPMOST) 才生效；
//     SWP_FRAMECHANGED 让样式变更立即重算非客户区。
//   - gioui 的 Fullscreen 模式用 `SW_SHOWMAXIMIZED`（app/os_windows.go:921-923），
//     Win32 的**最大化显示态**把窗口钳到工作区（实测帧日志 size=1080x1880、
//     截图底部 y=1880–1919 共 40 px 纯白 = 任务栏）。⇒ 铺满前必须先
//     `SW_SHOWNORMAL` 清掉最大化显示态，`SetWindowPos` 才能落到整块 rcMonitor。
//   - 样式位 WS_POPUP 必须是真的 0x80000000（旧值 0 让窗口带边框并服从工作区）。
//
// **必须在窗口自己的线程上调用**（经 w.Run）。
func applySaverStyle(hwnd uintptr, dbg bool) {
	style, exstyle, insert := saverStyle(dbg)
	logf("COVER=step saverStyle 完成 style=%08x exstyle=%08x", style, exstyle)
	x, y, w, h, ok := saverRect(hwnd, dbg)
	if !ok {
		logf("COVER=step saverRect 返回 !ok")
		return
	}
	logf("COVER=step saverRect 完成 rect=%d,%d,%dx%d", x, y, w, h)
	setWindowLong(hwnd, gwlStyle, style)
	logf("COVER=step setWindowLong(style) 完成")
	setWindowLong(hwnd, gwlExstyle, exstyle)
	logf("COVER=step setWindowLong(exstyle) 完成")
	procShowWindow.Call(hwnd, swShowNormal) // 清最大化/最小化显示态（同线程）
	logf("COVER=step shownormal 完成")
	procSetWindowPos.Call(hwnd, insert,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoActivate|swpFrameChanged|swpShowWindow)
	logf("COVER=setwindowpos_done %dx%d", w, h)
	procShowWindow.Call(hwnd, swShow) // 同线程调用（w.Run 内部），不阻塞
	logf("COVER=showwindow_done")
	// 前台与焦点：既有行为（未指定，按口径不删）。
	procSetForegroundWndow.Call(hwnd)
	procSetFocus.Call(hwnd)

	st, ex := getWindowLong(hwnd, gwlStyle), getWindowLong(hwnd, gwlExstyle)
	// 样式位只在**首次应用**或**发生变化**时打一行：ensureCover 前 90 帧
	// 每帧调用，逐次打会刷屏（实测只要 DBG= 里能看到样式位）。
	changed := coverLogged && (st != lastStyle || ex != lastExstyle)
	if dbg {
		// 实测：DBG= 前缀打印窗口样式位
		if !coverLogged || changed {
			logf("DBG=style STYLE=%08x EXSTYLE=%08x WS_VISIBLE=%v WS_OVERLAPPEDWINDOW=%v TOPMOST=%v TOOLWINDOW=%v APPWINDOW=%v rect=%d,%d,%d,%d",
				st, ex, st&wsVisible != 0, st&wsOverlappedwindow != 0,
				ex&wsExTopmost != 0, ex&wsExToolwindow != 0, ex&wsExAppwindow != 0,
				x, y, w, h)
		}
	} else if !coverLogged || changed {
		logf("COVER=style STYLE=%08x EXSTYLE=%08x WS_VISIBLE=%v WS_OVERLAPPEDWINDOW=%v TOPMOST=%v TOOLWINDOW=%v APPWINDOW=%v rect=%dx%d @%d,%d 工作区=%dx%d",
			st, ex, st&wsVisible != 0, st&wsOverlappedwindow != 0,
			ex&wsExTopmost != 0, ex&wsExToolwindow != 0, ex&wsExAppwindow != 0,
			w, h, x, y, workAreaW(hwnd), workAreaH(hwnd))
	}
	coverLogged = true
	lastStyle, lastExstyle = st, ex
}

func workAreaW(hwnd uintptr) int32 {
	hmon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	var mi monInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	return mi.rcWork.right - mi.rcWork.left
}

func workAreaH(hwnd uintptr) int32 {
	hmon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	var mi monInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	return mi.rcWork.bottom - mi.rcWork.top
}

var (
	taskbarHidden bool
	taskbarHwnd   uintptr
)

func findTaskbar() uintptr {
	cls, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	return hwnd
}

// hideTaskbar 隐藏任务栏（真屏保行为）。DEBUG 下不调用。
func hideTaskbar() {
	hwnd := findTaskbar()
	if hwnd == 0 {
		logf("TASKBAR=未找到 Shell_TrayWnd")
		return
	}
	taskbarHwnd = hwnd
	procShowWindow.Call(hwnd, swHide)
	taskbarHidden = true
	logf("TASKBAR=已隐藏")
}

// restoreTaskbar 还原任务栏。退出前必须调用。
func restoreTaskbar() {
	if !taskbarHidden {
		return
	}
	if taskbarHwnd == 0 {
		taskbarHwnd = findTaskbar()
	}
	if taskbarHwnd != 0 {
		procShowWindow.Call(taskbarHwnd, swShowNa)
	}
	taskbarHidden = false
	logf("TASKBAR=已还原")
}

// foregroundWindow 取当前前台窗口（`/c` 无数字时的配置父窗口）。
func foregroundWindow() uintptr {
	r, _, _ := procGetForegroundWndow.Call()
	return r
}

// applyOwner 把顶层窗口的 owner 设为 parent（GWL_HWNDPARENT）。
// 配置对话框的父窗口 = GetForegroundWindow() / 参数给的 HWND；
// gioui 建的是顶层窗口（os_windows.go:190），不能改父成 WS_CHILD，
// 故取 Win32 里"顶层窗口的 owner"这一等价语义（winuser.h：GWL_HWNDPARENT
// 用于设置顶层窗口的 owner。**必须在窗口线程上调用**（经 w.Run）。
func applyOwner(hwnd, parent uintptr) {
	if hwnd == 0 || parent == 0 {
		return
	}
	setWindowLong(hwnd, gwlHwndParent, parent)
	logf("CFG_OWNER=hwnd=%x parent=%x", hwnd, parent)
}

// raiseWindow 确保窗口在前台（对抗前台锁定）。
func raiseWindow(hwnd uintptr) {
	procSetForegroundWndow.Call(hwnd)
	procSetFocus.Call(hwnd)
}

// findOwnWindow 用 gioui 注册窗口类时用的类名（= app.ID）找到自己的窗口。
//
// 2026-10-16 实测改道：`FindWindowW(类名, NULL)` 在低性能 x86 小主机上**找不到**自己的窗口
// （低性能 x86 小主机上 60 秒轮询一条 COVER= 都没打出来），而自建探针 `cmd/winlist` 用
// `EnumWindows` + `GetClassNameW` 能枚举到 `class="WeatherScrSaver"` 的窗口
// （`WIN#21 ... pid=2956`）。原因：按类名查找走的是**进程局部原子**解析，
// 跨进程语义与同进程不一致；`GetClassNameW` 拿的是**类名字符串**，不受此影响。
//
// 所以主路径改为 EnumWindows 遍历顶层窗口，按"**属于本进程** 且 类名相等"匹配
// —— PID 相等这一条把误配的可能性彻底排除（探针已证明该窗口 pid 就是本进程）。
// FindWindowW 保留为兜底。
//
// EnumWindows 从任意线程调用都安全（不向窗口线程发消息）。
func findOwnWindow(className string) uintptr {
	mine := uintptr(0)
	count := 0
	var firstClass string
	var firstPID uint32
	enumCB = func(hwnd, _ uintptr) bool {
		count++
		if count == 1 {
			firstClass, firstPID = winClassName(hwnd), winPID(hwnd)
		}
		if winPID(hwnd) != uint32(os.Getpid()) {
			return true // 继续遍历
		}
		if winClassName(hwnd) == className {
			mine = hwnd
			return false // 停止遍历
		}
		return true
	}
	procEnumWindows.Call(enumProcAddr, 0)
	enumCB = nil
	logf("COVER_PROBE polls_done enum_count=%d own_pid=%d first_class=%q first_pid=%d mine=%x",
		count, os.Getpid(), firstClass, firstPID, mine)
	if mine != 0 {
		return mine
	}
	cls, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	logf("COVER_PROBE findwindoww=%x", hwnd)
	return hwnd
}

// showAndCover 显示 + 铺满。
//
// **只允许在 `w.Run(func(){...})` 内部调用**：
// ShowWindow/SetWindowPos/SetWindowLong 必须跑在创建该窗口的线程上，
// 跨线程调用会永久阻塞在 `procShowWindow.Call` 里
// `COVER=found hwnd=17079a after_polls=5` 之后没有任何后续行。
func showAndCover(hwnd uintptr, dbg bool) {
	if hwnd == 0 {
		// 首帧常早于 coverLoop 的轮询（首帧 ~60 ms，轮询 100 ms 一次），
		// 所以这里自己取句柄。EnumWindows 从任意线程调用都安全（不发消息）。
		hwnd = findOwnWindow(windowClass)
	}
	if hwnd == 0 {
		logf("COVER=showandcover 拿不到句柄")
		return
	}
	ownHwnd = hwnd
	applySaverStyle(hwnd, dbg)
	if !dbg {
		hideTaskbar() // DEBUG 下不隐藏任务栏
	}
	logf("COVER=showandcover_done hwnd=%x dbg=%v", hwnd, dbg)
}

// coverWatcher 轮询等窗口出现，出现后**只记录句柄**。
//
// 轮询上限 600 次 × 100 ms = 60 秒（需求 3：10 秒上限会在窗口
// 出现晚于 10 秒时永久放弃。
//
// 实测结论：这里**一律不得调用 ShowWindow / coverFullMonitor /
// hideTaskbar** —— 跨线程 ShowWindow 会阻塞（实测）。
func coverWatcher(className string) uintptr {
	for i := 1; i <= 600; i++ {
		hwnd := findOwnWindow(className)
		if hwnd != 0 {
			ownHwnd = hwnd
			logf("COVER=found hwnd=%x after_polls=%d 显示动作交 w.Run", hwnd, i)
			return hwnd
		}
		time.Sleep(100 * time.Millisecond)
	}
	logf("COVER=60 秒内没找到自己的窗口")
	return 0
}

// coverLoop 常驻 goroutine：**只找窗口并记录 ownHwnd**。
//
// 实测结论：本 goroutine 内**不准** ShowWindow / coverFullMonitor /
// hideTaskbar。显示与铺满一律由帧循环经 `w.Run` 在窗口线程上执行。
//
// 保留常驻轮询的意义：窗口尚未建立时（gioui 建窗晚于 10 秒）由它补上句柄，
// 帧循环的 ensureCover 因此不会因 ownHwnd==0 而空转。
func coverLoop() {
	// 铺满与样式**不能**在 gioui 的 w.Run 回调里做：实测 panic
	// `runtime error: index out of range [0] with length 0`
	// io/input.(*keyQueue).BoundsFor  key.go:247
	// io/input.(*Router).RevealFocus  router.go:638
	// app.(*Window).Run.func1         window.go:334
	// app.(*eventLoop).deliverEvent   os.go:320
	// app.windowProc                  os_windows.go:407
	// 退出码 2。机制：在 Run 回调里调 SetWindowPos/ShowWindow 会同步给
	// 同一窗口发 WM_WINDOWPOSCHANGING / WM_NCCALCSIZE / WM_SIZE ⇒ 重入
	// windowProc → update → draw → ProcessEvent → RevealFocus，而此时
	// 新状态的 keyQueue.dirOrder 刚被 `q.dirOrder = q.dirOrder[:0]`
	// （key.go:101）清空、尚未由 inputOp 重建（key.go:325） ⇒ 越界。
	// gioui 自己的 SetWindowPos 在 os_windows.go:278/937 是在**它自己的
	// 消息处理里**调的，不是Run 回调里 ⇒ 本线程独立调用。
	runtime.LockOSThread()
	// gioui 每帧在自己的 update/draw 路径里重申它管理的样式
	// （os_windows.go:251/793/937），会把设的 WS_EX_TOPMOST 抹掉、
	// 加回 WS_EX_APPWINDOW。实测（winlist）：
	// WIN#22 class="WeatherScrSaver" rect=(0,0,1080,1920)
	// topmost=false STYLE=17000000 EXSTYLE=00040000 fg=false
	// WIN#21 class="ConsoleWindowClass" rect=(87,156,1080,675) fg=true
	// ⇒ 窗口不在顶，指针事件被上层窗口拿走，
	// `SCR_TICK ... pointerEvents=0 keyEvents=0 dist=0.0` 恒为 0。
	// 对策：样式全量重申保持 500 ms 一次，**topmost 单独 100 ms 一次**
	// 只改 z 序不动几何（SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE），
	// 这是社区 "keep on top" 的标准做法。
	var topmostTick time.Time
	var styleTick time.Time // 零值 ⇒ 首轮立即做一次全量样式重申
	for {
		if ownHwnd == 0 {
			coverWatcher(windowClass)
			if ownHwnd == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		// 直接在本线程做样式 + 铺满 + 显示（不经 w.Run ⇒ 不重入 gioui 窗口过程）
		//
		// 实测修正：注释口径是"样式全量重申 500 ms 一次"，
		// 但代码原先在 100 ms 循环里**每轮都调** applySaverStyle
		// （= setWindowLong×2 + SetWindowPos + ShowWindow，40 次/秒），
		// 每次窗口调用都会让 gioui 渲染线程收 WM_WINDOWPOSCHANGED 等消息重做工作。
		// 现按注释口径：样式全量重申 500 ms 一次，topmost 单独 100 ms 一次。
		// A3（C4 保层循环节流，spec/a3-coverloop.md 要求 1/2/3/5）：两条重申都改
		// **检测式** —— 先读当前 style / exstyle，**只在漂移时才调 API**；稳态一轮
		// 两条分支都不调 ⇒ 也不更新 tick ⇒ COVER= 系列日志只随真发生的 API 动作出。
		// 节拍与阈值逐字不动：500 ms / 100 ms 阈值、!scrDbgFlag 门、Sleep 100 ms。
		styleWant, exstyleWant, _ := saverStyle(scrDbgFlag) // 纯位运算，不调 API
		if time.Since(styleTick) >= 500*time.Millisecond {
			// 首轮零值语义保留（:481 注释口径）：styleTick 零值 ⇒ 首轮立即做一次全量
			// 样式重申。走 IsZero() 短路 ⇒ **StyleDrift 判为 false 时绝不进这个调用**。
			if styleTick.IsZero() || StyleDrift(
				getWindowLong(ownHwnd, gwlStyle), styleWant,
				getWindowLong(ownHwnd, gwlExstyle), exstyleWant) {
				applySaverStyle(ownHwnd, scrDbgFlag)
				styleTick = time.Now()
			}
		}
		if time.Since(topmostTick) >= 100*time.Millisecond && !scrDbgFlag {
			// 阈值与门语义不变（零值 ⇒ 首轮立即判定；诊断态整条自动停 = 实机 A/B 开关）。
			// 动作改检测式：**只在 WS_EX_TOPMOST 位缺失时**才 SetWindowPos（spec 要求 1），
			// 漂移动作真发生后才更新 topmostTick。
			if ExStyleDrift(getWindowLong(ownHwnd, gwlExstyle), coverExTopmost) {
				procSetWindowPos.Call(ownHwnd, hwndTopmost, 0, 0, 0, 0,
					swpNoMove|swpNoSize|swpNoActivate)
				topmostTick = time.Now()
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var ownHwnd uintptr

// ensureCover 重申样式与铺满。**只允许在 `w.Run` 内部调用**。
// 对抗 gioui 建窗后的按工作区最大化（os_windows.go:937-939）与系统前台锁定。
func ensureCover(dbg bool) bool {
	if ownHwnd == 0 {
		return false
	}
	applySaverStyle(ownHwnd, dbg)
	return true
}

// ---- 设置窗口（/c）的风格夺回 ----
//
// 为什么 `.scr` 启动路径需要显式夺回风格：
//
//	`.scr` 由系统的屏保加载路径启动，STARTUPINFO 里带的是**整块屏幕**的
//	尺寸与位置；gioui 建窗时四个几何参数全用 `CW_USEDEFAULT`
//	（app/os_windows.go:195-196），`CW_USEDEFAULT` 的取值被这份 STARTUPINFO
//	接管 ⇒ 窗口落成满屏且无边框（STYLE 无 WS_CAPTION/WS_THICKFRAME，
//	EXSTYLE 无 WS_EX_APPWINDOW）。同一份字节以 `.exe` 名义启动时
//	STARTUPINFO 不带这些值 ⇒ 表现正常。`app.Size` 的 `SetWindowPos`
//	（app/os_windows.go 约 900-940，经 UpdateConfig 落地）只改尺寸、
//	不碰样式位，夺不回来。
//
// 因此设置窗口路径必须显式把风格、扩展样式与尺寸定位一并夺回。
// 屏保窗口（/s）的风格不在本函数职责内：它按本文件头部记录的
// 目标样式走 applySaverStyle，两者互不影响。
//
// 线程规则（同文件头）：本函数内部是 SetWindowLong/SetWindowPos/ShowWindow，
// **只允许在 `app.Window.Run` 回调里调用**（app/window.go:321-326：
// "Run f in the same thread as the native window event loop"）；
// 从其它 goroutine 直接调用会永久阻塞在 ShowWindow 里。
const (
	// 普通对话框样式位（winuser.h；gioui app/internal/windows/windows.go:396-404 同值）。
	// 六位置起来 = WS_OVERLAPPEDWINDOW|WS_VISIBLE（wsOverlappedwindow = 0x00CF0000）。
	wsCaption     = 0x00C00000
	wsSysmenu     = 0x00080000
	wsThickframe  = 0x00040000
	wsMinimizebox = 0x00020000
	wsMaximizebox = 0x00010000

	// WS_POPUP 在 winuser.h 里是 0x80000000。本文件既有 wsPopup = 0x00000000
	// 取的是 WS_OVERLAPPED（gioui windows.go:397），清位用本值。
	wsPopupBit = 0x80000000

	// SPI_GETWORKAREA = 0x0030（winuser.h）
	spiGetWorkarea = 0x0030

	// SW_RESTORE = 9（gioui app/internal/windows/windows.go:268）
	swRestore = 9

	// SWP_NOZORDER = 0x0004（winuser.h；gioui windows.go:275 同值）。
	// 注：本文件既有 swpNoZorder = 0x0040 在 winuser.h 里是 SWP_SHOWWINDOW；
	// 屏保路径沿用不改，设置窗口用本值。
	swpNoZorderReal = 0x0004

	// 设置窗口竖屏档的下限尺寸，与 app.MinSize(720, 420) 同口径（b11-setrect §1.1
	// 竖屏钳制口径：minW=min(720,sizeW)、minH=min(420,sizeH) 的基准下限；工作区
	// 更窄/更矮时 settingsSizeFor 再按工作区收紧，normalizeDialog 的工作区夹取兜底）。
	// 旧竖档 320×480 随竖长条形态一起退役（用户 2026-10-27「设置页改横向矩形」）。
	// 96 DPI（缩放 100%）下 gioui 的 dp 与物理像素 1:1。
	// TODO(未取证)：高 DPI 缩放下 dp→px 的换算口径未取证，本函数按 px 处理。
	dialogMinWpx = 720
	dialogMinHpx = 420

	// 横屏档下限（spec/b5-settings-landscape.md 判据 3「横屏请求 1920,1080
	// （MinSize 900,540）」）：双栏每栏要 ≈ 900/2 = 450 px 宽才放得下右栏的
	// 长标签行，所以横屏的下限比竖屏高一档，与 main.go 的 app.MinSize(900, 540)
	// 同口径。normalizeDialog 的工作区夹取按朝向取这一对值。
	dialogMinWpxLand = 900
	dialogMinHpxLand = 540
)

// dialogMinPx 按朝向给出设置窗口的下限尺寸（b11-setrect 二值口径：横屏 900×540 / 竖屏 720×420）。
func dialogMinPx(landscape bool) (int, int) {
	if landscape {
		return dialogMinWpxLand, dialogMinHpxLand
	}
	return dialogMinWpx, dialogMinHpx
}

// workAreaIsLandscape 主显示器工作区是否横屏（spec/b5-settings-landscape.md
// 判据 3 点位「foreground_windows.go 已有的 SPI_GETWORKAREA 同款查询」）。
//
// 判定本体复用面板的 ui.PanelIsLandscape（宽>高 且 宽≥900，b11 阈值）⇒ 窗口朝向与
// 面板版式同一条判据，不会窗口要 1920×1080 而面板排单列。
// 查询失败（SPI_GETWORKAREA 返回 0）按竖屏处理：竖屏 = 现行行为，最保守。
func workAreaIsLandscape() bool {
	_, _, w, h, ok := workAreaRect()
	if !ok {
		return false
	}
	return ui.PanelIsLandscape(int(w), int(h))
}

// workAreaRect 主显示器工作区（SPI_GETWORKAREA，已扣掉任务栏）。
func workAreaRect() (x, y, w, h int32, ok bool) {
	var rc winRect
	r, _, _ := procSystemParametersInfoW.Call(
		uintptr(spiGetWorkarea), 0, uintptr(unsafe.Pointer(&rc)), 0)
	if r == 0 {
		return 0, 0, 0, 0, false
	}
	return rc.left, rc.top, rc.right - rc.left, rc.bottom - rc.top, true
}

// workAreaSizePx 主显示器工作区尺寸（px），与 workAreaIsLandscape 同源（同一条
// SPI_GETWORKAREA 查询）：b11-setrect §1.1 设置窗尺寸的工作区钳制入参。
// 查询失败返回 ok=false ⇒ 调用方按不钳制口径处理（请求值直出）。
func workAreaSizePx() (int, int, bool) {
	_, _, w, h, ok := workAreaRect()
	if !ok {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// normalizeDialog 把设置窗口恢复成带标题栏的普通对话框，并按主显示器
// 工作区居中定位。**只允许在 `w.Run` 回调里调用**（见上方线程规则）。
//
// 执行顺序（固定）：
//
//  1. GetWindowLong(GWL_STYLE) 读原值（与改后值一起写进 CFG_STYLE 行）
//  2. 样式置 WS_CAPTION|WS_SYSMENU|WS_THICKFRAME|WS_MINIMIZEBOX|
//     WS_MAXIMIZEBOX|WS_VISIBLE，清 WS_POPUP
//  3. 扩展样式清 WS_EX_TOOLWINDOW、置 WS_EX_APPWINDOW（进任务栏）
//     并置 WS_EX_TOPMOST(0x00000008)
//  4. 尺寸 = wpx×hpx（调用方按 settingsSizeFor 钳制值传入：横屏 1920×1080 / 竖屏 1100×640 钳制）；
//     位置 = 主显示器工作区居中；工作区小于目标尺寸时夹到不小于**该朝向的
//     MinSize**（dialogMinPx(landscape)）且不小于工作区可用尺寸
//  5. SetWindowPos(HWND_TOPMOST, x, y, wpx, hpx, SWP_FRAMECHANGED)
//     —— 不带 SWP_NOZORDER：本步必须改 z 序
//  6. ShowWindow(SW_RESTORE)
//  7. SetWindowPos(HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
//     —— 置顶落定（SW_RESTORE 会把窗口插回非置顶带，故补这一步）
//
// 置顶的取证依据（36.16.1 之 1）：设置窗口 EX=00040100 无 WS_EX_TOPMOST，
// 同帧运行中的全屏屏保在它上面 ⇒ 设置窗口被盖住。
// WS_EX_TOPMOST 必须经 SetWindowPos(HWND_TOPMOST=-1) 才生效（同屏保路径口径，
// 见本文件上方"目标样式"注释与 gioui windows.go:211-213）。
// 本函数只作用于设置窗口（调用点在 main.go runSettings 的 styleDone 守卫内）；
// 屏保窗口不走本函数，样式与 z 序由 coverLoop 那条路管。
//
// 样式位用"置位 + 清位"的写法：置上面列出的六个位、清 WS_POPUP，
// 其余位保留（gioui 建窗时加的 WS_CLIPSIBLINGS|WS_CLIPCHILDREN，
// app/os_windows.go:193，清掉会影响渲染裁剪）。
func normalizeDialog(hwnd uintptr, wpx, hpx int, landscape bool) {
	if hwnd == 0 {
		logf("CFG_STYLE=skip hwnd=0")
		return
	}
	styleBefore := getWindowLong(hwnd, gwlStyle)
	exstyleBefore := getWindowLong(hwnd, gwlExstyle)

	style := (styleBefore &^ uintptr(wsPopupBit)) |
		wsCaption | wsSysmenu | wsThickframe | wsMinimizebox | wsMaximizebox | wsVisible
	// WS_EX_TOPMOST 与 WS_EX_APPWINDOW 同处置、WS_EX_TOOLWINDOW 同处清。
	exstyle := (exstyleBefore &^ uintptr(wsExToolwindow)) | uintptr(wsExAppwindow) | uintptr(wsExTopmost)

	// 尺寸与定位：目标尺寸按传入值；工作区装不下时夹到
	// 不小于 MinSize 且不小于工作区可用尺寸的取值。
	w, h := wpx, hpx
	x, y := 0, 0
	minW, minH := dialogMinPx(landscape) // 下限按朝向取（b11-setrect：横屏 900×540 / 竖屏 720×420）
	wx, wy, aw, ah, ok := workAreaRect()
	if ok {
		awa, ahh := int(aw), int(ah)
		if awa > 0 && awa < w {
			w = max(awa, minW)
		}
		if ahh > 0 && ahh < h {
			h = max(ahh, minH)
		}
		x = int(wx) + (awa-w)/2
		y = int(wy) + (ahh-h)/2
		// 夹回工作区内（工作区小于目标尺寸时居中值为负）
		if x < int(wx) {
			x = int(wx)
		}
		if y < int(wy) {
			y = int(wy)
		}
	}
	// TODO(未取证)：SPI_GETWORKAREA 失败时的定位口径（此处按 0,0 左上角）。

	setWindowLong(hwnd, gwlStyle, style)
	setWindowLong(hwnd, gwlExstyle, exstyle)
	// 第 5 步：带尺寸/定位的一次 SetWindowPos 走 HWND_TOPMOST，且**不带
	// SWP_NOZORDER**（带它就等于不改 z 序，置顶不落地）。
	procSetWindowPos.Call(hwnd, hwndTopmost,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(swpFrameChanged))
	procShowWindow.Call(hwnd, swRestore)
	// 第 7 步：SW_RESTORE 会把窗口插回非置顶带，这里再落一次置顶，
	// 只改 z 序（SWP_NOMOVE|SWP_NOSIZE），不动尺寸、位置、激活态。
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize))

	st, ex := getWindowLong(hwnd, gwlStyle), getWindowLong(hwnd, gwlExstyle)
	logf("CFG_STYLE before=%08x after=%08x exstyle_before=%08x exstyle_after=%08x TOPMOST=%v TOOLWINDOW=%v APPWINDOW=%v rect=%d,%d,%dx%d 工作区=%dx%d",
		uint32(styleBefore), uint32(st), uint32(exstyleBefore), uint32(ex),
		ex&wsExTopmost != 0, ex&wsExToolwindow != 0, ex&wsExAppwindow != 0,
		x, y, w, h, aw, ah)
}

//go:build windows

package main

import (
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
	// 指定的常量，逐字使用。
	// WS_POPUP 按口径取 0x00000000（= WS_OVERLAPPED，
	// gioui app/internal/windows/windows.go:397。
	// 判据口径是"WS_OVERLAPPEDWINDOW 位为 0"，本值满足该判据。
	// 注：Microsoft winuser.h 里 WS_POPUP = 0x80000000；差异已记入工程记录。
	wsPopup   = 0x00000000
	wsVisible = 0x10000000

	wsExTopmost    = 0x00000008
	wsExToolwindow = 0x00000080
	wsExAppwindow  = 0x00040000 // 必须清掉（实测 EXSTYLE=00040100 含它）

	swpNoMove       = 0x0002
	swpNoSize       = 0x0001
	swpNoZorder     = 0x0040
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	swShow   = 5
	swHide   = 0
	swShowNa = 8 // 显示但不激活

	monitorDefaultToNearest = 2

	// HWND_TOPMOST = -1、HWND_TOP = 0
	// 来源：gioui app/internal/windows/windows.go:211-213。
	// 旧实现写的是 uintptr(0)（= HWND_TOP），所以实测 topmost=false。
	hwndTopmost = ^uintptr(0)
	hwndTop     = uintptr(0)

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
		w4, h4 := w/4, h/4
		x += (w - w4) / 2
		y += (h - h4) / 2
		w, h = w4, h4
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
//	SetWindowPos(insertAfter, rcMonitor, SWP_NOACTIVATE|SWP_FRAMECHANGED) →
//	ShowWindow(SW_SHOW)
//
// 理由：WS_EX_TOPMOST 必须经 SetWindowPos(HWND_TOPMOST) 才生效；
// SWP_FRAMECHANGED 让样式变更立即重算非客户区。
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
	procSetWindowPos.Call(hwnd, insert,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoActivate|swpFrameChanged)
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
		if time.Since(styleTick) >= 500*time.Millisecond {
			applySaverStyle(ownHwnd, scrDbgFlag)
			styleTick = time.Now()
		}
		if time.Since(topmostTick) >= 100*time.Millisecond && !scrDbgFlag {
			procSetWindowPos.Call(ownHwnd, hwndTopmost, 0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoActivate)
			topmostTick = time.Now()
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

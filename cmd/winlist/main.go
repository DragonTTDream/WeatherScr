//go:build windows

// 窗口枚举探针：把"屏保窗口到底在不在屏幕上"变成可直接读的数字。
//
// 为什么需要它：2026-10-16 实测，低性能 x86 小主机上屏保进程活着、SCR_FOCUS=true 也打了，
// 但肉眼可见"屏幕上什么都没有"，且 40 秒内没有一条 SCR_TICK（帧计数）——
// 说明窗口可能根本没被映射/显示。截图与日志都证明不了这件事，必须直接问 Win32。
//
// 用法：winlist_s.exe [类名子串]
// 输出每个顶层窗口的：类名 标题 可见性 矩形 Z序 是否前景
package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	pEnumWindows       = user32.NewProc("EnumWindows")
	pGetClassNameW     = user32.NewProc("GetClassNameW")
	pGetWindowTextW    = user32.NewProc("GetWindowTextW")
	pIsWindowVisible   = user32.NewProc("IsWindowVisible")
	pGetWindowRect     = user32.NewProc("GetWindowRect")
	pGetWindowLongW    = user32.NewProc("GetWindowLongW")
	pGetForegroundWnd  = user32.NewProc("GetForegroundWindow")
	pGetWindow         = user32.NewProc("GetWindow")
	pGetShellWindow    = user32.NewProc("GetShellWindow")
	pGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	pGetWindowThreadId = user32.NewProc("GetWindowThreadProcessId")
	pIsIconic          = user32.NewProc("IsIconic")
)

const (
	gwlpExStyle     = 0xFFFFFFEC // -20 扩展样式
	gwOwner         = 4          // GWLP_HWNDPARENT
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000008 // 与 TOPMOST 同值，靠类名区分
	wsExAppWindow   = 0x00040000
	swShowMinimized = 0x00000002
)

type rect struct {
	left, top, right, bottom int32
}

func utf16(buf []uint16) string {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return windows.UTF16PtrToString(&buf[0])
}

func className(hwnd uintptr) string {
	b := make([]uint16, 256)
	pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return utf16(b)
}

func title(hwnd uintptr) string {
	b := make([]uint16, 256)
	pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return utf16(b)
}

func visible(hwnd uintptr) bool {
	r, _, _ := pIsWindowVisible.Call(hwnd)
	return r != 0
}

func iconic(hwnd uintptr) bool {
	r, _, _ := pIsIconic.Call(hwnd)
	return r != 0
}

func rc(hwnd uintptr) rect {
	var r rect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func exStyle(hwnd uintptr) uintptr {
	r, _, _ := pGetWindowLongW.Call(hwnd, gwlpExStyle)
	return r
}

func pid(hwnd uintptr) uint32 {
	var id uint32
	pGetWindowThreadId.Call(hwnd, uintptr(unsafe.Pointer(&id)))
	return id
}

// zIndex 沿 Z 序从最顶往下数，返回该窗口的位次（0 = 最顶）。
// 找不到返回 -1。
func zIndex(target uintptr) int {
	top, _, _ := pGetWindow.Call(0, gwOwner) // 取桌面窗口作为链起点
	_ = top
	// EnumWindows 的顺序即 Z 序自顶向下
	cur := uintptr(0)
	idx := 0
	found := -1
	enumWindows(func(hwnd uintptr) bool {
		if hwnd == target {
			found = idx
			return false
		}
		idx++
		return true
	})
	_ = cur
	return found
}

// enumWindows 用 trampoline 回调遍历顶层窗口。
func enumWindows(fn func(hwnd uintptr) bool) {
	cb = fn
	pEnumWindows.Call(enumProcAddr, 0)
}

var cb func(hwnd uintptr) bool

//go:noinline
func enumTrampoline(hwnd, lparam uintptr) uintptr {
	if cb == nil {
		return 1
	}
	if cb(hwnd) {
		return 1
	}
	return 0
}

var enumProcAddr = windows.NewCallback(enumTrampoline)

func main() {
	needle := ""
	if len(os.Args) > 1 {
		needle = os.Args[1]
	}
	fmt.Printf("SCREEN=%dx%d\n", metric(0), metric(1)) // SM_CXSCREEN, SM_CYSCREEN
	fg, _, _ := pGetForegroundWnd.Call()
	shell, _, _ := pGetShellWindow.Call()
	fmt.Printf("FG=%x SHELL=%x\n", fg, shell)

	var all []uintptr
	enumWindows(func(hwnd uintptr) bool {
		all = append(all, hwnd)
		return true
	})
	fmt.Printf("TOPLEVEL=%d\n", len(all))

	for i, hwnd := range all {
		cn := className(hwnd)
		if needle != "" && !containsFold(cn, needle) && !containsFold(title(hwnd), needle) {
			continue
		}
		r := rc(hwnd)
		ex := exStyle(hwnd)
		st := style(hwnd)
		fmt.Printf("WIN#%d z=%d hwnd=%x class=%q title=%q vis=%v iconic=%v rect=(%d,%d,%d,%d) size=%dx%d topmost=%v appwindow=%v pid=%d fg=%v\n",
			i, i, hwnd, cn, title(hwnd), visible(hwnd), iconic(hwnd),
			r.left, r.top, r.right, r.bottom, r.right-r.left, r.bottom-r.top,
			ex&wsExTopmost != 0, ex&wsExAppWindow != 0, pid(hwnd), hwnd == fg)
		// 样式位直读（2026-10-16 排查）：vis=false 时唯一能定案的就是
		// WS_VISIBLE 到底有没有被置上，以及窗口是否处于最小化态。
		fmt.Printf("   STYLE=%08x WS_VISIBLE=%v WS_MINIMIZE=%v WS_MAXIMIZE=%v WS_CAPTION=%v WS_POPUP=%v EXSTYLE=%08x\n",
			st, st&wsVisible != 0, st&wsMinimize != 0, st&wsMaximize != 0,
			st&wsCaption != 0, st&wsPopup != 0, ex)
	}
	fmt.Println("WINLIST_DONE")
}

const (
	wsVisible    = 0x10000000
	wsMinimize   = 0x20000000
	wsMaximize   = 0x01000000
	wsCaption    = 0x00C00000
	wsPopup      = 0x00000000
	wsChild      = 0x40000000
	wsThickframe = 0x00040000
	wsSysmenu    = 0x00080000
)

func style(hwnd uintptr) uintptr {
	// GWL_STYLE = -16
	r, _, _ := pGetWindowLongW.Call(hwnd, uintptr(0xFFFFFFF0))
	return r
}

func metric(n uintptr) int32 {
	r, _, _ := pGetSystemMetrics.Call(n)
	return int32(r)
}

func containsFold(s, sub string) bool {
	if sub == "" {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		ca, cb2 := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb2 >= 'A' && cb2 <= 'Z' {
			cb2 += 32
		}
		if ca != cb2 {
			return false
		}
	}
	return true
}

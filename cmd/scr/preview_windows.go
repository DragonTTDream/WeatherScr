//go:build windows && !novulkan

// /p <hwnd> 预览模式。
// 规范要求：在给定 HWND 下建**同尺寸子窗口**，
// 在预览面板里显示。gioui 不能把它的顶层窗口改父成 WS_CHILD
// （`app/os_windows.go:190` 的 CreateWindowEx 是顶层窗口），
// 所以走已验证的离屏渲染路径（cmd/fxshot 跑通的同一条生产路径：
// ui.New + Screen.Layout → headless.Window.Frame → Screenshot：
// 1. CreateWindowEx(WS_CHILD|WS_VISIBLE) 在父 HWND 下建子窗口，
// 尺寸 = 父窗口客户区（GetClientRect）；
// 2. gioui.org/gpu/headless 在本进程内离屏渲染；
// 3. 每帧把 RGBA 经 SetDIBitsToDevice 贴到子窗口；
// 4. 父窗口销毁 ⇒ 子窗口 WM_DESTROY / IsWindow(parent)=false ⇒ 立即退出；
// 5. 不置顶、不隐藏任务栏、不因输入退出。
// novulkan 构建下本文件不参与编译，由 preview_novulkan_windows.go 打
// PREVIEW_UNSUPPORTED 并退出 0。
package main

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui"
	"github.com/DragonTTDream/WeatherScr/weather"
)

const previewClass = "WeatherScrPreview"

var (
	pvUser32 = windows.NewLazySystemDLL("user32.dll")
	pvGdi32  = windows.NewLazySystemDLL("gdi32.dll")
	// GetModuleHandleW 在 **kernel32.dll**，不在 user32.dll。
	// 此前挂在 pvUser32 上，低性能 x86 实机 panic：
	// `Failed to find GetModuleHandleW procedure in user32.dll:
	// The specified procedure could not be found.`（本文件调用点）
	pvKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pGetDC            = pvUser32.NewProc("GetDC")
	pReleaseDC        = pvUser32.NewProc("ReleaseDC")
	pGetClientRect    = pvUser32.NewProc("GetClientRect")
	pIsWindow         = pvUser32.NewProc("IsWindow")
	pRegisterClassExW = pvUser32.NewProc("RegisterClassExW")
	pCreateWindowExW  = pvUser32.NewProc("CreateWindowExW")
	pDefWindowProcW   = pvUser32.NewProc("DefWindowProcW")
	pPeekMessageW     = pvUser32.NewProc("PeekMessageW")
	pTranslateMsgW    = pvUser32.NewProc("TranslateMessage")
	pDispatchMsgW     = pvUser32.NewProc("DispatchMessageW")
	pGetModuleHandleW = pvKernel32.NewProc("GetModuleHandleW")

	pSetDIBitsToDevice = pvGdi32.NewProc("SetDIBitsToDevice")
)

const (
	// WS_CHILD = 0x40000000（winuser.h，Microsoft docs "Window Styles"）；
	// WS_VISIBLE = 0x10000000（同页；常量逐字取上游口径）。
	wsChild = 0x40000000

	// WM_CREATE=0x0001 / WM_DESTROY=0x0002 / WM_ERASEBKGND=0x0014 /
	// WM_CLOSE=0x0010：gioui app/internal/windows/windows.go:347-383。
	wmCreate     = 0x0001
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmEraseBkgnd = 0x0014

	// CS_HREDRAW=0x0002 / CS_VREDRAW=0x0001 / PM_REMOVE=0x0001：
	// gioui app/internal/windows/windows.go:189-192, 416。
	csHRedraw = 0x0002
	csVRedraw = 0x0001
	pmRemove  = 0x0001

	// DIB：32bpp BI_RGB，DIB_RGB_COLORS=0。
	biRGB        = 0
	dibRgbColors = 0

	// 离屏渲染尺寸上限（预览面板很小，这条只是兜底）
	maxPreviewDim = 2048
)

type rectW struct{ left, top, right, bottom int32 }

// wndClassExW 布局逐字照抄 gioui app/internal/windows/windows.go:34-47。
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cnClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// msgW 布局逐字照抄 gioui app/internal/windows/windows.go:56-64。
type msgW struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       [2]int32
	lPrivate uint32
}

type bitmapInfoHeader struct {
	biSize          int32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	header   bitmapInfoHeader
	bmiColor uint32
}

// previewWndProc 是预览子窗口的窗口过程。
// 实测：父窗口被销毁 ⇒ 子窗口收到 WM_DESTROY ⇒ 立即退出
// （规范：预览窗口销毁时 saver 必须终止）。
//
//go:noinline
func previewWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmDestroy:
		fmt.Println("PREVIEW_EXIT=wm_destroy 父窗口已销毁")
		os.Exit(0) // 立即退出
	case wmEraseBkgnd:
		return 1 // 背景每帧由本实现自身贴，不让系统擦（防闪烁）
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

var previewWndProcAddr = windows.NewCallback(previewWndProc)

// createPreviewChild 在 parent 下建 WS_CHILD|WS_VISIBLE 子窗口。
func createPreviewChild(parent uintptr, w, h int32) uintptr {
	cls, err := windows.UTF16PtrFromString(previewClass)
	if err != nil {
		return 0
	}
	hInst, _, _ := pGetModuleHandleW.Call(0)
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   previewWndProcAddr,
		hInstance:     hInst,
		lpszClassName: cls,
	}
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Println("PREVIEW_FAIL=RegisterClassExW 返回 0")
		return 0
	}
	hwnd, _, _ := pCreateWindowExW.Call(
		0, // 不置顶
		uintptr(unsafe.Pointer(cls)),
		0,
		uintptr(wsChild|wsVisible),
		0, 0, uintptr(w), uintptr(h),
		parent, 0, hInst, 0,
	)
	return hwnd
}

// pumpPreviewMessages 非阻塞将子窗口消息交给窗口过程（WM_DESTROY → 退出）。
func pumpPreviewMessages() {
	var m msgW
	for {
		if r, _, _ := pPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove); r == 0 {
			return
		}
		pTranslateMsgW.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMsgW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// windowAlive 父窗口是否还在（第二条判据）。
func windowAlive(hwnd uintptr) bool {
	r, _, _ := pIsWindow.Call(hwnd)
	return r != 0
}

// blitRGBA 把 image.RGBA（R,G,B,A）转成 32bpp BI_RGB 的 B,G,R,0 并用
// SetDIBitsToDevice 贴到子窗口客户区。
// biHeight 取负 = 自顶向下扫描，与 image.RGBA 的行序一致，无需翻转。
// 离屏尺寸已按子窗口客户区建立，故这里 1:1 贴，不缩放。
func blitRGBA(img *image.RGBA, dst uintptr, w, h int32) bool {
	buf := make([]byte, len(img.Pix))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		buf[i+0] = img.Pix[i+2]
		buf[i+1] = img.Pix[i+1]
		buf[i+2] = img.Pix[i+0]
		buf[i+3] = 0
	}
	var bmi bitmapInfo
	bmi.header.biSize = int32(unsafe.Sizeof(bitmapInfoHeader{}))
	bmi.header.biWidth = w
	bmi.header.biHeight = -h
	bmi.header.biPlanes = 1
	bmi.header.biBitCount = 32
	bmi.header.biCompression = biRGB

	hdc, _, _ := pGetDC.Call(dst)
	if hdc == 0 {
		return false
	}
	defer pReleaseDC.Call(dst, hdc)
	r, _, _ := pSetDIBitsToDevice.Call(
		hdc,
		0, 0, uintptr(w), uintptr(h), // 目标 = 子窗口客户区
		0, 0, 0, uintptr(h), // 源 = 整幅离屏图
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRgbColors,
	)
	return r != 0
}

// previewSnapshot 预览用的天气：优先上次成功的缓存，无缓存时用与
// cmd/fxshot 同口径的固定样本（让预览带文字层，不空屏）。
func previewSnapshot() snapshot {
	st := &weatherState{}
	// 预览没有配置里的位置键 ⇒ 传空键，lastGood 会退到"CachedAt 最新的一条"，
	// 仍然是确定性的，不会随机捞到旧时刻的记录。
	if s := lastGood(""); s != nil {
		st.set(s)
		return st.snapshot()
	}
	t, hu, pp, ws := 22.4, 84.0, 0.0, 4.6
	st.set(&weather.Sample{
		Condition:                "Mainly clear",
		Temperature:              &t,
		Humidity:                 &hu,
		PrecipitationProbability: &pp,
		WindSpeed:                &ws,
		WeatherCode:              1,
		IsDay:                    1,
	})
	return st.snapshot()
}

// drawPreview 是 /p 的实现体。
func drawPreview(parent uintptr) {
	// 子窗口的消息必须由**创建它的线程**泵 ⇒ 本线程 LockOSThread 到底。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var r rectW
	pGetClientRect.Call(parent, uintptr(unsafe.Pointer(&r)))
	w, h := r.right-r.left, r.bottom-r.top
	if w <= 0 || h <= 0 {
		fmt.Println("PREVIEW_FAIL=父窗口客户区尺寸为 0")
		return
	}
	if w > maxPreviewDim {
		h = h * maxPreviewDim / w
		w = maxPreviewDim
	}
	if h > maxPreviewDim {
		w = w * maxPreviewDim / h
		h = maxPreviewDim
	}
	if w <= 0 || h <= 0 {
		fmt.Println("PREVIEW_FAIL=缩放后尺寸为 0")
		return
	}

	child := createPreviewChild(parent, w, h)
	if child == 0 {
		fmt.Println("PREVIEW_FAIL=CreateWindowExW 建子窗口失败")
		return
	}
	fmt.Printf("PREVIEW_CHILD=hwnd=%x parent=%x %dx%d\n", child, parent, w, h)

	win, err := headless.NewWindow(int(w), int(h))
	if err != nil {
		fmt.Println("PREVIEW_UNSUPPORTED=headless 不可用: " + err.Error())
		return
	}
	defer win.Release()

	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	screen := ui.New()
	snap := previewSnapshot()
	field := ui.NewField(snap.Class, snap.IsDay, time.Now().UnixNano())
	// 闪现间隔按码分级：预览路径同样把码传进粒子场
	if snap.Sample != nil {
		field.SetWeatherCode(snap.Sample.WeatherCode)
	}
	cfg := config.Load()

	var ops op.Ops
	last := time.Now()
	n := 0
	for {
		// 实测：父窗口销毁 ⇒ 立即退出
		if !windowAlive(parent) {
			fmt.Println("PREVIEW_EXIT=parent_gone")
			return
		}
		pumpPreviewMessages() // WM_DESTROY → os.Exit(0)

		n++
		dt := time.Since(last)
		last = time.Now()
		field.Step(dt, int(w), int(h))

		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Point{X: int(w), Y: int(h)}),
			Now:         time.Now(),
		}
		// 生产渲染路径（与全屏同一套 ui.Screen.Layout）
		screen.Layout(gtx, ui.Frame{
			Now:      time.Now(),
			Clock:    ui.DefaultClockSpec(),
			Weather:  snap.Sample,
			Class:    snap.Class,
			IsDay:    snap.IsDay,
			Field:    field,
			Location: cfg.Location,
			Panel:    panelStyle(cfg),
		})

		if err := win.Frame(&ops); err != nil {
			fmt.Println("PREVIEW_FRAME_FAIL=" + err.Error())
			return
		}
		if err := win.Screenshot(img); err != nil {
			fmt.Println("PREVIEW_SHOT_FAIL=" + err.Error())
			return
		}
		if !blitRGBA(img, child, w, h) {
			fmt.Println("PREVIEW_FAIL=SetDIBitsToDevice 返回 0")
			return
		}
		if n == 1 {
			fmt.Printf("PREVIEW_OK=%dx%d class=%s\n", w, h, classString(snap.Class))
		}
		fmt.Printf("PREVIEW_FRAME n=%d %dx%d\n", n, w, h)
		time.Sleep(66 * time.Millisecond)
	}
}

// classString 把 effect.Class 打成名字（日志用）。
func classString(c effect.Class) string {
	switch c {
	case effect.Clear:
		return "clear"
	case effect.Cloudy:
		return "cloudy"
	case effect.Fog:
		return "fog"
	case effect.Rain:
		return "rain"
	case effect.Snow:
		return "snow"
	case effect.Thunder:
		return "thunder"
	case effect.Dust:
		return "dust"
	}
	return "clear"
}

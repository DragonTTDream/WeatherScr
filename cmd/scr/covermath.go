package main

// 铺满几何与 Win32 官方常量（可移植层：常量口径 + 纯函数几何，闸门单测在本文件测）。
// 为什么单独一个文件：常量与几何要在 Linux 侧的 `go test ./cmd/scr` 里钉死，
// 而 Win32 调用代码带 `//go:build windows` 标签、本机跑不了。
// WS_POPUP 口径改判（实机取证）：旧值 0x00000000（= WS_OVERLAPPED）
// 让窗口带边框且**服从最大化显示态** ⇒ 被钳到工作区 ⇒ 底部 40 px 任务栏露出
// （实测截图 y=1880–1919 全白，帧日志 size=1080x1880 共 400 帧）。
// 官方 winuser.h：WS_POPUP = 0x80000000；SW_SHOWNORMAL = 1。
// 闸门口径'WS_OVERLAPPEDWINDOW(0x00CF0000) 位为 0'在 WS_POPUP|WS_VISIBLE 下照样满足。
const (
	wsPopup   = 0x80000000 // winuser.h 官方值
	wsVisible = 0x10000000 // winuser.h 官方值

	swShowNormal = 1 // winuser.h SW_SHOWNORMAL：清除最大化/最小化显示态
	swShow       = 5
	swHide       = 0
	swShowNa     = 8

	swpShowWindow = 0x0040

	hwndTopmost = ^uintptr(0)
	hwndTop     = uintptr(0)
)

// dbgQuarterRect = DEBUG 小窗几何：整块矩形里的 1/4 尺寸居中盒。
// 纯函数（可单测）。奇数尺寸按整数除法向下取整（与 SetWindowPos 的取整口径一致）。
func dbgQuarterRect(x, y, w, h int32) (int32, int32, int32, int32) {
	if w <= 0 || h <= 0 {
		return x, y, 0, 0
	}
	w4, h4 := w/4, h/4
	return x + (w-w4)/2, y + (h-h4)/2, w4, h4
}

// coverStyleTarget = 铺满态目标样式位（WS_POPUP|WS_VISIBLE，无 OVERLAPPEDWINDOW 位）。
// 与 saverStyle 的口径一致；单测钉死"OVERLAPPEDWINDOW 位必须为 0"。
func coverStyleTarget() uintptr { return wsPopup | wsVisible }

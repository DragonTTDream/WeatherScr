//go:build !windows

package main

// 非 Windows 平台的桩：屏保只在 Windows 上运行，交叉编译与静态检查用。
// 签名（改动后）：
// showAndCover / ensureCover 带 dbg（SCR_DEBUG 开关），
// 显示与铺满在 Windows 侧一律经 app.Window.Run 在窗口线程上执行。

func showAndCover(hwnd uintptr, dbg bool)    {}
func ensureCover(dbg bool) bool              { return false }
func hideTaskbar()                           {}
func restoreTaskbar()                        {}
func raiseWindow(hwnd uintptr)               {}
func findOwnWindow(className string) uintptr { return 0 }
func foregroundWindow() uintptr              { return 0 }
func applyOwner(hwnd, parent uintptr)        {}

// normalizeDialog 空桩：设置窗口的风格夺回是 Win32 窗口调用，
// 非 Windows 平台无对应对象。签名与 Windows 侧一致。
func normalizeDialog(hwnd uintptr, wpx, hpx int, landscape bool) {}

// workAreaIsLandscape 空桩：非 Windows 没有 SPI_GETWORKAREA 可问，
// 恒返回 false ⇒ 设置窗口一律走竖屏原值（640×1100 / 320×480），零行为迁移。
func workAreaIsLandscape() bool { return false }

// coverLoop 空桩：run() 里 `go coverLoop()` 在双平台都要能编译。
// 非 Windows 无 Win32 窗口可显示/铺满，常驻检查无意义，直接返回。
func coverLoop() {}

// scrDbgFlag 空桩：调试态标志。Windows 侧定义在 foreground_windows.go:16
// （coverLoop 独立线程共用），main.go 在**无 build tag** 的行里写它
// （cmd/scr/main.go:640 `scrDbgFlag = dbg`）⇒ 非 Windows 也必须有个同名变量，
// 否则 `go build ./...` 在 Linux 上红（闸门 b1 第 1 步就是这条）。
var scrDbgFlag bool

//go:build !windows

// 非 Windows 平台的 /p 预览桩：交叉编译与本地静态检查用。
// 屏保只在 Windows 上运行，这里不实现任何绘制。
package main

import "fmt"

func drawPreview(hwnd uintptr) {
	fmt.Printf("PREVIEW_SKIP=非 Windows 平台不实现预览 (hwnd=%d)\n", hwnd)
}

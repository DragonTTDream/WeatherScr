//go:build windows && novulkan

// novulkan 构建下的 /p 预览桩：
// 构建标签 -tags novulkan 时不启用预览模式，打印 PREVIEW_UNSUPPORTED 并退出 0。
// （preview_windows.go 的构建标签是 `windows && !novulkan`，此时不参与编译。）
package main

import "fmt"

func drawPreview(hwnd uintptr) {
	fmt.Printf("PREVIEW_UNSUPPORTED=novulkan 构建不启用预览模式 hwnd=%d\n", hwnd)
}

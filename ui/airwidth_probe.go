//go:build !vulkan

package ui

import "fmt"

// airWidthProbe 逐云量最终屏宽（px）与云贴图下标清单（小云 / 白名单 的取证口径）。
// 本文件跨平台（仅排除 vulkan 标签），供 landscape_layout_test.go 等跨平台测试调用；
// 历史上它长在 linux-only 的 baseline_dump_test.go 里，导致 GOOS=windows 下
// 测试包 undefined 编译失败（全项目自检 fix）。
func airWidthProbe(f *Field) (float64, string) {
	minW := -1.0
	seen := ""
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud && p.kind != pkFog {
			continue
		}
		w := cloudDrawWidthPx(p)
		if w <= 0 {
			continue
		}
		if minW < 0 || w < minW {
			minW = w
		}
		if p.kind == pkCloud {
			seen += fmt.Sprintf("%d,", p.spr)
		}
	}
	if minW < 0 {
		minW = 0
	}
	return minW, seen
}

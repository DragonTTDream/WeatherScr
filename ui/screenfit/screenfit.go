// Package screenfit 负责屏幕基准的最低层算术：归一化基准宽、竖屏目标口径、零屏/负屏兜底。
// 纯算术模块：不依赖 gioui、不依赖 ui（；证据 H3）。
package screenfit

// Canvas 是屏幕基准（像素），Baseline 是建场（屏未知）时的竖屏目标口径。
type Canvas struct{ W, H int }

// Baseline 逐字 = 原 ui/layout.go:47-48（DISPLAY1 Bounds Width=1080 Height=1920 实测）。
func Baseline() Canvas { return Canvas{W: 1080, H: 1920} }

// Fallback 零屏/负屏兜底：w/h ≤ 0 时回到竖屏目标。
// 逐字 = 原 ui/effects.go:4022-4024（sizeCloudByClass）与 4470-4473（driftLayers）的判据。
func Fallback(w, h int) (int, int) {
	if w <= 0 {
		w = Baseline().W
	}
	if h <= 0 {
		h = Baseline().H
	}
	return w, h
}

// RefW 归一化基准宽（现云基准的整数口径，原 ui/effects.go:729 cloudDwRef 的分子）。
const RefW = 1178

// RefWidth 是 RefW 的 float64 视图，逐字 = 原 cloudDwRef = 1178.0 / 1080.0。
// 原为常量表达式，现为运行时 float64 变量：同一 IEEE754 双除舍入结果。
var RefWidth = 1178.0 / 1080.0

// PortraitW 竖屏目标宽的 float64 视图，逐字 = 1080.0。
var PortraitW = float64(Baseline().W)

// PortraitH 竖屏目标高的 float64 视图，逐字 = 1920.0。
var PortraitH = float64(Baseline().H)

// Package geometry 负责屏向与几何基准：竖/横两制的屏向常量对、px↔归一换算、云带宽分母
// （R5 拆分系列 卡 5，plans/active.md:209；纯迁移，公式/常量/取整夹取顺序逐字不变）。
//
// 与 ui/screenfit 的分工（spec 卡 5 括注）：screenfit = 屏幕基准的兜底与控件让位口径
// （Baseline 1080×1920、Fallback 零屏/负屏兜底、RefWidth 归一化基准宽）；
// geometry = 屏向基准（竖/横两制）与 px↔归一换算、带宽分母。两包不重叠：
// 本包不复制 screenfit 的任何数字，竖屏基准一律经 screenfit 取（见 PortraitW/PortraitH）。
package geometry

import (
	"math"

	"github.com/DragonTTDream/WeatherScr/ui/screenfit"
)

// ---- 屏向常量对（竖/横）----
//
// 竖屏逐字 = 原 ui/layout.go:47-48（DISPLAY1 Bounds Width=1080 Height=1920 实测；
// B2 起为 ui/layout.go:53-54 的薄壳，语义源在 ui/screenfit 的 Baseline / PortraitW / PortraitH）。
// 横屏 = 竖屏两制对调，逐字 = 出图矩阵 ui/baseline_dump_test.go:57 的 {PortraitH, PortraitW}
// 口径（1920×1080，spec/b4-landscape.md 的横屏取证尺寸）。
var (
	PortraitW  = int(screenfit.PortraitW) // 1080
	PortraitH  = int(screenfit.PortraitH) // 1920
	LandscapeW = PortraitH                // 1920
	LandscapeH = PortraitW                // 1080
)

// IsLandscape 屏向判定，逐字 = 原 ui/layout.go:67（证据 spec/b4-landscape.md:34「横屏判定 = w > h」）。
func IsLandscape(w, h int) bool { return w > h }

// ---- px ↔ 归一换算 ----

// NormToPx 归一（0–1）→ 像素（取整截断），逐字 = 原 ui/layout.go:26-31
// （Rect.Px 的四行 `int(r.X * float64(screenW))` 口径，逐式同形）。
func NormToPx(n float64, size int) int { return int(n * float64(size)) }

// NormPx 归一（0–1）→ 像素（浮点，不取整），逐字 = 原 ui/effects.go:2333
// （bandPlace 的带左上角 `y: float64(h) * (top + spread*frac)`）与 ui/effects.go:3884
// 的 minGap `0.10 * float64(w)`（乘式两侧同域，IEEE 乘法可交换 ⇒ 逐字同值）。
func NormPx(n float64, size int) float64 { return float64(size) * n }

// PxToNorm 像素 → 归一（除以屏幕尺寸），逐字 = 原 ui/effects.go:3380-3381
// （airHalfN 的 `... / float64(f.sw)`、`... / float64(f.sh)`）与 :3774、:3982-3983、:4235、:1408 同形。
func PxToNorm(px float64, size int) float64 { return px / float64(size) }

// ---- 带宽分母（P6 设计第 2 条"等宽"，定死不许自行调参）----

const (
	bandWidthTargetPx = 1000.0 // 1080 屏 ⇒ 1000 px
	bandWidthCapRatio = 0.95   // 硬上限 T ≤ 0.95·w
)

// BandWidthPx 云带宽度 T（px），逐字 = 原 ui/effects.go:2214-2227（bandWidthPx）：
//
//	T = round(w × 1000 / 1080)   （1080 屏 ⇒ 1000 px；分母 1080 = screenfit 的竖屏目标口径，复用不二造）
//	硬上限 T ≤ 0.95·w
//
// 带宽不再随 frac 变化（旧 sc = (1.5+0.8·frac)/bandNetDiv ⇒ 逐带 768…1178 px = 重叠根因，见 bandPlace 注释）。
// 粒子云 p.sc 口径**不动**（P6"不做什么"，只收云带）。
func BandWidthPx(w int) float64 {
	t := math.Round(float64(w) * bandWidthTargetPx / screenfit.PortraitW)
	if cap := bandWidthCapRatio * float64(w); t > cap {
		t = cap
	}
	return t
}

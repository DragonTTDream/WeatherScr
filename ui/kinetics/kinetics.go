// Package kinetics 是云运动的最低层算术：速度档、最终显示宽度下限、同带朵数、进场排队判据。
// 纯算术模块：不依赖 gioui、不依赖 ui（； 已定：B3 零依赖）。
// 数值出处= ：
//
//	宽度下限/间隙/朵数 :11-15
//	速度档（最快冻结）
//	贴图白名单 cloud1/2/4 :26-33（白名单常量在 ui 侧消费点 effects.go 的 cloudIdxAllowed）
package kinetics

import "math"

// Class 是运动侧的天气档标签。kinetics 不吃 effect/ui 包 ⇒ 档名在本地定义，
// 由消费点 ui/effects.go 的 cloudFormOf 做一对一映射（映射写在 cloudFormOf 里）。
type Class int

const (
	ClassClear Class = iota
	ClassPartlyCloudy
	ClassCloudy
	ClassOvercast
	ClassRainThunder
	ClassFogCloud
)

// 逐字钉的口径值。
const (
	// （17-23）：最快档冻结在现云速上界，
	// 拉开速度差只靠**调慢**，不调快 ⇒ VxMax 是硬上限（现值 0.014 来自 effects.go:729）。
	VxMax      = 0.0140
	VxTier0    = 0.0140 // 中档下限侧的快端（= VxMax，冻结）
	VxTier1    = 0.0085 // 中档 0.444–0.62 W
	VxTier2    = 0.0040 // 中大档 0.62–0.83 W
	VxTier3    = 0.0022 // 厚档 >0.83 W
	WidthFloor = 0.444  // 最终显示宽度下限 = 480 px @1080 / 853 px @1920
	GapRatio   = 0.0194 // 首尾相连最小间隙 = 21 px @1080 / 37 px @1920
	MaxPerBand = 2      // 同带最多 2 朵（3 朵需 1.37 W > 1 W）
)

// RefW 是 Rain/Thunder 档现口径（768–1178 px）的换算基准宽 = 竖屏目标 1080。
// 不 import screenfit（B2 产物，kinetics 只依赖标准库）。
const RefW = 1080

// rainLoPx / rainHiPx 逐字 = 现口径 effects.go:701 的 768–1178（@1080， 表内"不动"档）。
const (
	rainLoPx = 768
	rainHiPx = 1178
)

// GapPx 间隙 px（四舍五入）：1080 ⇒ 21、1920 ⇒ 37。
func GapPx(w int) int {
	return int(math.Round(GapRatio * float64(w)))
}

// FloorHi 同带 2 朵时的逐朵宽度上限 px：(w − gap)/2 ⇒ 1080 ⇒ 529（两朵 529 + 21 间隙 = 1080）。
// 41 把这条写作"(1 − gap)/2 = 0.529"，量纲是 px 不是屏宽占比
// （占比口径 = (1−0.0194)/2 = 0.4898 ⇒ 0.4898×1080 = 529 px，两者同值）。
func FloorHi(w int) int {
	return (w - GapPx(w)) / MaxPerBand // 整数除 = 向下取整
}

// BandCapacity 给定屏宽返回 (朵数上限, 最小逐朵宽 px, 间隙 px)。
// 钉死：BandCapacity(1080) == (2,480,21)、BandCapacity(1920) == (2,853,37)。
func BandCapacity(w int) (n int, minPx int, gapPx int) {
	return MaxPerBand, int(math.Ceil(WidthFloor * float64(w))), GapPx(w)
}

// WidthFor 逐类**最终显示宽度**区间 [lo,hi] px。
//
//	lo = ceil(0.444·w) ⇒ @1080 = 480（小于此宽的云不出，生成侧直接不产）
//	hi = FloorHi(w) = (w − gap)/2 ⇒ @1080 = 529（同带 2 朵首尾相连恰好铺满）
//	Rain/Thunder 档 = 现口径 768–1178 按屏宽等比，不套 529 封顶（43 逐字"不动"）。
//
// 与公式 "hi = min(现 hi, FloorHi)" 的自洽处：
// 现 hi 里 Clear 420 / Fog 500 低于 480 下限 ⇒ 与 lo 互斥，spec 的逐类新表（Clear 480–529、
// Fog 云档 480–529）取的是 FloorHi。这里以**逐类新表为准**：小云档的 hi 抬到 FloorHi，
// 只有现 hi 高于 FloorHi 的档才需要夹（Rain/Thunder 显式豁免）。
func WidthFor(cls Class, w int) (lo, hi int) {
	if cls == ClassRainThunder {
		lo = int(math.Round(float64(rainLoPx) * float64(w) / float64(RefW)))
		hi = int(math.Round(float64(rainHiPx) * float64(w) / float64(RefW)))
		return lo, hi
	}
	_, lo, _ = BandCapacity(w)
	hi = FloorHi(w)
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// EntryOK 进场排队判据（"决不能同出同进"，:24）：
// 同帧两朵进场，|Δx| 必须 ≥ 上一条云的半宽 ⇒ 不合格返回 false（消费点保持 parked，下一帧再试）。
// 单位 = px（与钉死用例一致：EntryOK(100,50,120,50,1080) ⇒ |Δx|=20 < 50 ⇒ false）。
// halfNew 在签名里保留（44 逐字签名），当前判据只吃上一条半宽；
// w 是屏宽口径的携带项，判据本身不用它 ⇒ 写在这里，不自行发明第二条件。
func EntryOK(xNew, halfNew, xPrev, halfPrev float64, w int) bool {
	_ = halfNew
	_ = w
	return math.Abs(xNew-xPrev) >= halfPrev
}

package sky

import (
	"image"
	"image/color"
	"math"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// 月亮盘面绘制（逐字迁自 ui/effects.go:2942-3024）。
// 生产路径已把月亮烘进天空层（bakeCelestial），这里保留离线路径与回退用的
// 逐帧画法：核心圆盘 + 64 环增量叠加光晕 + 5 处淡坑纹理。
// 唯一的签名变化 = drawMoon 的 (f *Field) 接收者（函数体内从未用到 f）去掉，
// 成导出函数 DrawMoon；公式、常量、颜色、绘制顺序一字未改。

// 月白：三层**同色，只变 alpha**。
var MoonColor = color.NRGBA{R: 245, G: 242, B: 230, A: 0xff}

// 月亮径向渐变参数。
// 2026-10-16 两轮视觉核对的实测教训：
// - 旧版三层实心盘 ⇒ "三个硬边同心圆，像灰色的靶子"；
// - 旧版第一版 26 环、每环画**绝对 alpha** ⇒ 读图仍是"一圈一圈的
// 硬边同心环"。根因：叠加后相邻环之间的 alpha 落差 = 该环自身 alpha，
// 近核处约 15/255 ≈ 6% 一跳 ⇒ 台阶肉眼可见。
// 正确做法：把目标**累积**曲线 A(r) = A0·exp(-decay·(r/rc−1)) 离散化，
// 每环只画**增量** `a_i = A(r_i) − A(r_{i+1})`，由外到内叠加。
// 叠加后中心累积 = A(rc) = A0、向外单调降到 A(外)，且相邻落差被压到
// 环数 N 的粒度上：N=64 时最大落差 ≈ 120×(1−e^(−3/63)) ≈ 6/255 ≈ 2.4%，
// 环间距 3.4/63 ≈ 0.054 rc ≈ 2.6 px ⇒ 台阶不可见。
const (
	MoonRingN      = 64    // 环数（越大台阶越细）
	MoonRingSpread = 3.4   // 最外环半径 = r_core × (1 + 3.4)
	MoonRingA0     = 120.0 // 核心处累积 alpha
	MoonRingDecay  = 3.0   // 累积曲线衰减率
	MoonDiscAlpha  = 235   // 核心圆盘 alpha
)

// drawMoon 月亮：核心圆盘 + 64 环增量叠加的径向衰减光晕 + 3 个淡坑纹理。
// 夜间才画（Draw 里的判据不变）；pkGlow 类保留给地平线光。
func DrawMoon(gtx layout.Context, w, h int, t float64) {
	cx := float32(w) * 0.72
	cy := float32(h) * 0.13
	// 半径口径沿用原 moonSz 96 px 贴图（1080 屏上半径 48 px），随屏宽走。
	r := float32(w) * 0.045
	pulse := float32(1.0 + 0.04*math.Sin(0.35*t)) // 原有的轻微呼吸，保留
	rc := r * pulse

	// 由外到内（i 从 N-1 递减到 0），每环画增量 alpha。
	for i := MoonRingN - 1; i >= 0; i-- {
		frac := float64(i) / float64(MoonRingN-1)
		next := float64(i+1) / float64(MoonRingN-1)
		ri := rc * (1 + MoonRingSpread*float32(frac))
		// 增量 = A(r_i) − A(r_{i+1})；最外环之外视为 0。
		aCur := MoonRingA0 * float32(math.Exp(-MoonRingDecay*frac))
		aNext := MoonRingA0 * float32(math.Exp(-MoonRingDecay*next))
		ai := aCur - aNext
		if ai < 1 {
			continue
		}
		Disc(gtx, cx, cy, ri, uint8(ai))
	}
	Disc(gtx, cx, cy, rc, MoonDiscAlpha)

	// 表面纹理：固定系数 ⇒ 跨帧稳定不闪。
	// 2026-10-16 视觉核对：alpha 26 的三坑"几乎没有"，提到 46 并补到 5 处，
	// 其中两处是月海（大片暗区），比小坑更容易被肉眼读到。
	craters := []struct{ dx, dy, rr float32 }{
		{-0.34, -0.21, 0.18},
		{0.26, 0.30, 0.26},
		{0.10, -0.40, 0.13},
		{-0.12, 0.22, 0.34}, // 月海
		{0.34, -0.14, 0.22}, // 月海
	}
	for _, c := range craters {
		DiscColor(gtx, cx+c.dx*rc, cy+c.dy*rc, c.rr*rc,
			color.NRGBA{R: 210, G: 208, B: 196, A: 46})
	}
}

// disc 铺一个半径 rad 的纯色圆盘：正方形 clip.RRect，四角半径 = rad ⇒ 正圆，
// 只填一种颜色（无变换、无渐变）。
func Disc(gtx layout.Context, cx, cy, rad float32, a uint8) {
	DiscColor(gtx, cx, cy, rad, color.NRGBA{MoonColor.R, MoonColor.G, MoonColor.B, a})
}

// discColor 是 disc 的通用版：任意颜色。
func DiscColor(gtx layout.Context, cx, cy, rad float32, c color.NRGBA) {
	if rad <= 0 {
		return
	}
	rr := gtx.Dp(unit.Dp(rad))
	rect := image.Rect(int(cx-rad), int(cy-rad), int(cx+rad), int(cy+rad))
	cl := clip.RRect{Rect: rect, SE: rr, SW: rr, NW: rr, NE: rr}.Push(gtx.Ops)
	paint.Fill(gtx.Ops, c)
	cl.Pop()
}

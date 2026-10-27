// Package sky 是天空层烘焙：天空底色、星月、日照渐变的绘制与烘焙（消费者 = ui）。
package sky

import (
	"image"
	"image/color"
	"math"
	"os"
	"strconv"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// ── 贴图口注入 ──────────────────────────────────────────
// 烘焙层需要查贴图（天空底图 / 太阳泛光 / 月亮 / 光线），但贴图目录与解码缓存
// 归 ui（ui/assetdir.go、ui/assets.go， 不点名 ⇒ 原地不动）。
// 处置 = 本包只声明一个只读查询口，由消费者 ui 在 init 注入实现（实现体逐字委托
// ui 现有的 assetAvailable / assetImageAny / assetSourceTag / spriteImg /
// sampleBilinear / blitCoverReplace / blitCoverOver）。出厂路径上注入前后行为逐字相同。
// 未注入时的默认实现 = "一切贴图都不存在"，即 ui 侧"内嵌也没有 ⇒ 程序化画法"
// 那一档既有语义（assetAvailable 返回 false 的同一条路），不新增行为。

// Assets 天空烘焙需要的贴图查询口（只读）。
type Assets interface {
	Available(name string) bool
	ImageAny(name string) (*image.NRGBA, paint.ImageOp, image.Point, bool)
	SourceTag(name string) string
	Sprite(name string) *image.NRGBA
	SampleBilinear(src *image.NRGBA, fx, fy float64) color.NRGBA
	BlitCoverReplace(dst, src *image.NRGBA)
	BlitCoverOver(dst, src *image.NRGBA, r image.Rectangle)
}

// TODO(未取证)：默认实现 assetsAbsent 只在"没有人注入贴图口"时生效。
// 出厂链路上 ui 在 init 注入 ⇒ 这条默认分支没有线上消费者，也没有取证过它单独的画面；
// 若将来出现第二个消费者（离屏工装等），记得它不注入时量到的是"无贴图"那一档。
var assets Assets = assetsAbsent{}

// SetAssets 注入贴图口（消费者 ui 在 init 调用一次；生产路径注入后行为与迁移前逐字相同）。
func SetAssets(a Assets) { assets = a }

type assetsAbsent struct{}

func (assetsAbsent) Available(string) bool { return false }
func (assetsAbsent) ImageAny(string) (*image.NRGBA, paint.ImageOp, image.Point, bool) {
	return nil, paint.ImageOp{}, image.Point{}, false
}
func (assetsAbsent) SourceTag(string) string    { return "" }
func (assetsAbsent) Sprite(string) *image.NRGBA { return nil }
func (assetsAbsent) SampleBilinear(*image.NRGBA, float64, float64) color.NRGBA {
	return color.NRGBA{}
}
func (assetsAbsent) BlitCoverReplace(dst, src *image.NRGBA)                 {}
func (assetsAbsent) BlitCoverOver(dst, src *image.NRGBA, r image.Rectangle) {}

// ── 与 ui 共用的逐字小工具（两份同体副本，公式一字未改）────────
// clamp8 / smoothstep 在 ui 侧还有别的消费者（ui/assetdir.go、ui/effects.go 粒子），
// 不点名那些文件 ⇒ 本包带一份**逐字副本**，不搬走原件、不改公式。

// clamp8 副本（逐字同 ui/render.go:543-552 原件，公式未改）
// clamp8 把任意浮点亮度收进 [0,255]。
func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// smoothstep 副本（逐字同 ui/effects.go:494-511 原件，公式未改）
func smoothstep(a, b, x float64) float64 {
	if b <= a {
		if x < a {
			return 0
		}
		return 1
	}
	t := (x - a) / (b - a)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

// ---- 粒子 ----

// SkyColor 按天气类与昼夜给不透明天空底色（无壁纸时天气层不透明铺满）。
func SkyColor(cls effect.Class, isDay bool) color.NRGBA {
	if !isDay {
		return NightSky[cls]
	}
	return DaySky[cls]
}

var (
	DaySky = map[effect.Class]color.NRGBA{
		effect.Clear:  {R: 0x2a, G: 0x6d, B: 0xb8, A: 0xff},
		effect.Cloudy: {R: 0x6b, G: 0x77, B: 0x84, A: 0xff},
		// PartlyCloudy = 多云见日那一档：天空基色 = 现 Cloudy 逐字不变
		// （它接管的正是"原 Cloudy 里太阳照得下来"的那一半场景）。
		effect.PartlyCloudy: {R: 0x6b, G: 0x77, B: 0x84, A: 0xff},
		// Overcast = 阴天无日那一档：基色比 PartlyCloudy **更暗更灰**。
		// 依据：现象是"多云看不到太阳一整天"⇒ 这一档没有直射光，
		// 天空只剩散射的灰；取 PartlyCloudy 基色 ×0.80（107,119,132 → 86,95,106），
		// 降幅 20% 是"同一色相、只降明度"的最小可辨档：
		// 与 Fog(154,162,168) 拉开 68 级、与 Rain(55,74,94) 拉开 31 级，
		// 不与新档以外的任何一类重合。
		effect.Overcast: {R: 0x56, G: 0x5f, B: 0x6a, A: 0xff},
		effect.Fog:      {R: 0x9a, G: 0xa2, B: 0xa8, A: 0xff},
		effect.Rain:     {R: 0x37, G: 0x4a, B: 0x5e, A: 0xff},
		effect.Snow:     {R: 0xa8, G: 0xb8, B: 0xc8, A: 0xff},
		effect.Thunder:  {R: 0x2b, G: 0x2f, B: 0x44, A: 0xff},
		effect.Dust:     {R: 0xa8, G: 0x8c, B: 0x5e, A: 0xff},
	}
	NightSky = map[effect.Class]color.NRGBA{
		effect.Clear:        {R: 0x0a, G: 0x14, B: 0x28, A: 0xff},
		effect.Cloudy:       {R: 0x1c, G: 0x24, B: 0x30, A: 0xff},
		effect.PartlyCloudy: {R: 0x1c, G: 0x24, B: 0x30, A: 0xff},
		// Overcast 夜间同口径：×0.80（28,36,48 → 22,29,38），更暗更灰。
		effect.Overcast: {R: 0x16, G: 0x1d, B: 0x26, A: 0xff},
		effect.Fog:      {R: 0x2a, G: 0x30, B: 0x36, A: 0xff},
		effect.Rain:     {R: 0x10, G: 0x1a, B: 0x26, A: 0xff},
		effect.Snow:     {R: 0x2e, G: 0x38, B: 0x46, A: 0xff},
		effect.Thunder:  {R: 0x0c, G: 0x0e, B: 0x1a, A: 0xff},
		effect.Dust:     {R: 0x3a, G: 0x30, B: 0x20, A: 0xff},
	}
)

// skyGradient 竖直方向的天色渐变：天顶深、地平线亮。
// 判读意见"背景过于纯黑，缺乏大气层的深蓝渐变" —— 单色平铺正是成因。
func SkyGradient(gtx layout.Context, size image.Point, cls effect.Class, isDay bool) {
	base := SkyColor(cls, isDay)
	top := color.NRGBA{
		R: uint8(float64(base.R) * 0.45),
		G: uint8(float64(base.G) * 0.45),
		B: uint8(float64(base.B) * 0.55),
		A: 0xff,
	}
	horizon := color.NRGBA{
		R: uint8(math.Min(255, float64(base.R)*1.35+18)),
		G: uint8(math.Min(255, float64(base.G)*1.35+18)),
		B: uint8(math.Min(255, float64(base.B)*1.25+14)),
		A: 0xff,
	}
	cl := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	paint.LinearGradientOp{
		Stop1:  f32.Pt(0, 0),
		Color1: top,
		Stop2:  f32.Pt(0, float32(size.Y)),
		Color2: horizon,
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
}

// skyDepth 天空纵深（两次读图判读"单调扁平"）：
// 1. 地平线辉光带：位于天空底部 22% 高度处（y ∈ [0.78h, h]），
// 颜色 = 当前基色提亮 base×1.25+14，alpha 沿高度 0 → 30 → 0
// 峰在带中点 0.89h；
// 2. 四角暗角 vignette：上下各一条高度 16% 的渐变（黑色 alpha 38 → 0 顶部向内、
// 0 → 38 底部向内，左右同理宽度 12%。
// 全部**同 RGB 只变 alpha**（铁律：gioui 的渐变按端点色插值，
// 两端 RGB 不同会把整屏插到白，实测过一次。颜色从 skyGradient 的基色派生，
// 不新增硬编码颜色表。
// 每段渐变都**独立裁剪**：gioui 的渐变在端点之外按端点色外插
// （gpu.go:1361 gradientSpaceTransform），不裁剪的三段渐变会把峰后的 alpha
// 平推成 30 铺满下半屏。
func SkyDepth(gtx layout.Context, size image.Point, cls effect.Class, isDay bool) {
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	base := SkyColor(cls, isDay)
	glow := color.NRGBA{
		R: clamp8(float64(base.R)*1.25 + 14),
		G: clamp8(float64(base.G)*1.25 + 14),
		B: clamp8(float64(base.B)*1.25 + 14),
		A: 0xff,
	}
	fy, fx := float32(size.Y), float32(size.X)

	// 1 地平线辉光带：底部 22% 高度，alpha 0 → 30 → 0
	y0, ym, y1 := int(fy*0.78), int(fy*0.89), size.Y
	GradAlpha(gtx, image.Rect(0, y0, size.X, ym), glow, 0, 30, true)
	GradAlpha(gtx, image.Rect(0, ym, size.X, y1), glow, 30, 0, true)

	// 2 四角暗角：上下 16% 高度、左右 12% 宽度。
	// 客观指标（bin/fxmetrics.py banding，斜率突变版）实测：
	// 单段线性 38→0 在暗角**边界处斜率从 -4.5/行 直接跳到 0**
	// ⇒ band_rows = 4–5、max_jump 3.2–4.2（肉眼是边界上的"肩部"）。
	// 改几何衰减分段，让斜率在边界处趋近 0 ⇒ 无肩部。
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}
	hv, hw := fy*0.16, fx*0.12
	_, _ = hv, hw
	vignetteAlpha := []uint8{38, 20, 9, 3, 0}
	vignetteFrac := []float32{0, 0.35, 0.65, 0.85, 1.0}
	for i := 0; i+1 < len(vignetteAlpha); i++ {
		f0, f1 := vignetteFrac[i], vignetteFrac[i+1]
		a0, a1 := vignetteAlpha[i], vignetteAlpha[i+1]
		// 上
		GradAlpha(gtx, image.Rect(0, int(float32(hv)*f0), size.X, int(float32(hv)*f1)), black, a0, a1, true)
		// 下（镜像）
		GradAlpha(gtx, image.Rect(0, size.Y-int(float32(hv)*f1), size.X, size.Y-int(float32(hv)*f0)), black, a1, a0, true)
		// 左
		GradAlpha(gtx, image.Rect(int(float32(hw)*f0), 0, int(float32(hw)*f1), size.Y), black, a0, a1, false)
		// 右（镜像）
		GradAlpha(gtx, image.Rect(size.X-int(float32(hw)*f1), 0, size.X-int(float32(hw)*f0), size.Y), black, a1, a0, false)
	}
}

// gradAlpha 在矩形 r 内铺一条线性渐变，**两端 RGB 完全相同、只变 alpha**
// （vertical = true 沿 y 方向，否则沿 x）。r 非法时不画。
func GradAlpha(gtx layout.Context, r image.Rectangle, c color.NRGBA, a1, a2 uint8, vertical bool) {
	if r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y {
		return
	}
	c1, c2 := c, c
	c1.A, c2.A = a1, a2
	p1 := f32.Pt(float32(r.Min.X), float32(r.Min.Y))
	p2 := p1
	if vertical {
		p2 = f32.Pt(float32(r.Min.X), float32(r.Max.Y))
	} else {
		p2 = f32.Pt(float32(r.Max.X), float32(r.Min.Y))
	}
	cl := clip.Rect(r).Push(gtx.Ops)
	paint.LinearGradientOp{Stop1: p1, Color1: c1, Stop2: p2, Color2: c2}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
}

// ── 日照渐变的预览工装系数（逐字迁自 ui/sunpreview.go:19-26）──────────
// dev-only：出厂二进制里 env 未设 ⇒ 取默认值，画面与迁移前逐字相同。
// 消费者只有本包的烘焙函数（SunSkyLift / bakeSunAnalytic / bakeSunGlow）。

var (
	SunLiftRFrac    = envFloat("WS_SUN_LIFT_R", 3.0)
	SunLiftLv       = envFloat("WS_SUN_LIFT_LV", 0.0)
	SunLiftFadeFrac = envFloat("WS_SUN_LIFT_FADE", 1.2667)
	SunSkyScale     = envFloat("WS_SUN_SKY", 1.0)
)

// envFloat 副本（逐字同 ui/sunpreview.go:40-50 原件）
func envFloat(name string, def float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

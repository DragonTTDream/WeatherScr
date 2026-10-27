// Package panel 是设置面板与屏保面板的版式层：面板底绘制、文本行布局、图标位与时钟布局，
// 含竖屏/横屏两档字号与行距（自 ui 纯迁移而来，行为与数值逐字不变）。
package panel

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// 面板专属常量：分隔线内缩比例、描边 alpha 系数、阴影色与偏移下限、圆角半径上限。
// 语义与取值逐字来自 ui/render.go（迁出，见各件注释里的证据行）。

// SeparatorLenFrac 分隔线的**内缩比例**：
// 线宽 = 该块内容宽度 × 0.62，水平居中，不得触及面板/屏幕全宽。
// 此前：0.62 → 0.42，让线段占比低于行中位数的采样过半线，
// 指标不再被单条线主导（也进一步降低"切割感"）。
const SeparatorLenFrac = 0.90 // 需求：上下的分割线拉长 ⇒ 0.42 拉到 0.90

// BorderAlphaScale 面板描边的实际 alpha 系数：
// 启用描边时 alpha = border_opacity × 0.35。默认 border_opacity = 0.0 = 不描边。
const BorderAlphaScale = 0.35

// ShadowInk 逐字照抄上游 rendering/quick/qml/ShadowedText.qml 的
// `shadowColor: "#96000000"`（ARGB，alpha 0x96=150 的暗色副本）。
// 证据：需求 2；本次不改。
var ShadowInk = color.NRGBA{R: 0, G: 0, B: 0, A: 0x96}

// ShadowOffsetPx = 2 是**下限**，语义来自上游 ShadowedText.qml
// `shadowOffsetY: 2.0` / `shadowOffsetX: 0.0`。
// 旧版把它当固定值照抄，实测结果 = 缺陷 2："文字
// 信息）看起来是纯白色的，没有明显的投影或阴影效果"（视觉判读
// 离屏图 lane_cloudy。根因：上游的 2.0 px 是给它的 25 pt 天气字号用的
// （default_settings.py 'weather' 块 font_size 25），本复刻时钟是 144 pt
// （声明值 ClockFontSize=144，低性能 x86 实机配置 font_size 144→174，
// 见 ui/layout.go:31-32，2 px 在 144 pt 上等于没有。
const ShadowOffsetPx = 2

// ShadowOffsetForPx 阴影偏移随字号走：max(2, round(字号px × 0.03))
// 下限 2 保持上游语义：
// 144 pt → 4 px、174 pt → 5 px、25 pt → 1 → 取下限 2（与上游一致）、
// 本复刻天气 40 pt → 1 → 取下限 2。
// 字号px 口径：gioui 的 unit.Sp 经 gtx.Sp 换成设备像素
// （unit/unit.go:48-51 `Metric.Sp = round(PxPerSp × v)`）；竖屏目标与 fxshot
// 的 Viewport.Scale=1、TextScale=1 → PxPerSp=1 → 字号 px 就是声明的 sp 值。
func ShadowOffsetForPx(px int) int {
	o := int(math.Round(float64(px) * 0.03))
	if o < ShadowOffsetPx {
		return ShadowOffsetPx
	}
	return o
}

// PanelRadiusMaxPx 面板圆角半径上限。上游 weather 块无此键（取证未覆盖），
// 取一个与上游面板视觉一致的常量作为**上限**。TODO(未取证)：上游圆角半径口径。
// 半径随面板高度自适应 =
// min(panelRadiusMaxPx, 面板高/6)，不再用固定 8 px。
const PanelRadiusMaxPx = 18

// PanelRadiusPx 面板圆角半径（像素）= min(18, 面板高/6)。
func PanelRadiusPx(panelH int) int {
	r := panelH / 6
	if r > PanelRadiusMaxPx {
		r = PanelRadiusMaxPx
	}
	if r < 0 {
		r = 0
	}
	return r
}

// InkSep 对应原版 Separator 的 separatorColor（细、半透明）。
// 降到 0x2A 后，客观指标（bin/fxmetrics.py panel_step）实测
// 分隔线处**落差仍是 68/255**，且直接量像素得到线色 (98,105,114)、
// 背景 (28,37,51) ⇒ **有效 alpha ≈ 0.37**，而声明值是 0x165。
// 根因（这才是"白条"没真正消失的机制）：gioui 的 layout.Flex 对子控件
// 先测量后绘制，1 px 线段被**叠加绘制 2–3 次**，
// 累积 alpha = 1−(1−a)^n：0x2A 时 n=3 → 0.418。
// 故按叠加次数反推：目标"线比背景亮约 15 级" ⇒ 累积 alpha ≈ 0.08
// ⇒ 单次 a ≈ 0.041 → **0x0A**。颜色 RGB 不动（同色只变 alpha 铁律）。
var InkSep = color.NRGBA{R: 0xd8, G: 0xe2, B: 0xee, A: 0x0A}

// Style 是面板底与文字样式，键名与取值逐字照抄上游
// core/settings/default_settings.py 的 'weather' 块 :1523-1540
// （'clock' 块 带同名键）。
// 证据：需求 2（实测）。
type Style struct {
	// Set=false 时 Layout 用 DefaultStyle()（fxshot 等未显式传值的调用方）。
	Set bool
	// Show = 上游 show_background（默认 True）。
	Show bool
	// BG = 上游 bg_color [35,35,35,255]；BGOpacity = 上游 bg_opacity 0.3。
	BG        color.NRGBA
	BGOpacity float64
	// ClockBGOpacity 时钟块面板底的不透明度，配置键 `clock_bg_opacity`，默认 0.26。
	// **这是有记录的偏离上游**：
	// 上游 'clock' 块（core/settings/default_settings.py:503-528）是
	// `bg_color [35,35,35,255]`、`bg_opacity 0.3`、**`show_background: False`**
	// —— 上游时钟默认**没有**面板底，因为它的背景是 shader，不忙。
	// 本复刻的背景是忙碌的 Kenney 云/雾精灵（ui/embed/*.png + effects.go），
	// 实测云直接压在时间数字上（视觉判读），故时钟区需要遮罩，
	// 此前把两个默认值一起减淡（0.45→0.26、0.3→0.22），遮罩仍强于天气块（0.26 > 0.22）。
	ClockBGOpacity float64
	// Border = 上游 border_color [255,255,255,255]；BorderOpacity 默认 0.0
	// = 不描边（上游是 1.0）。
	// 启用时实际 alpha = BorderOpacity × BorderAlphaScale(0.35)。
	Border        color.NRGBA
	BorderOpacity float64
	// Margin = 上游 margin 30：面板矩形 = 内容区 rect 外扩 margin。
	Margin int
	// TextColor = 上游 color [255,255,255,230]（alpha 230）。
	TextColor color.NRGBA
	// Shadow = 上游 ShadowedText.shadowEnabled（默认 true）。
	Shadow bool
}

// DefaultStyle 上游缺省值（default_settings.py:1523-1540 逐字），
// 外加 ClockBGOpacity = 本复刻的有记录偏离（见 Style.ClockBGOpacity）。
// 三个默认值按视觉读图整改：
//
//	BGOpacity 0.3 → 0.22 （天气面板底）
//	ClockBGOpacity 0.45 → 0.26 （时钟面板底）
//	BorderOpacity 1.0 → 0.0 （默认不描边）
//
// 描边默认关闭的判据 = 读图判读"白色细边框在夜间非常刺眼，像旧式 Windows
// 窗口残留"；面板底减淡的判据 = 底 0.45/0.3 把天空压成两块灰板。
// 键语义与键名不变（铁律：不改配置键语义），只改默认值。
// DefaultStyle 面板底默认值。
// 2026-10-16 修掉一个**双真相源**缺陷：config 的默认值与这里的默认值各写一份，
// 需求"全透明"落地时只改了 config，`cmd/fxshot` 走的是本函数
// ⇒ 预览图仍是不透明面板，看到的与生产路径不一致。
// 现在两处必须同口径：**全透明**（无底无边框）。
// 生产路径仍由 `cmd/scr/main.go: panelStyle()` 从 config 覆盖。
func DefaultStyle() Style {
	return Style{
		Set:            true,
		Show:           false, // 全透明
		BG:             color.NRGBA{35, 35, 35, 255},
		BGOpacity:      0.0,
		ClockBGOpacity: 0.0,
		Border:         color.NRGBA{255, 255, 255, 255},
		BorderOpacity:  0.0,
		Margin:         30,
		TextColor:      color.NRGBA{255, 255, 255, 230},
		Shadow:         true, // 透明底下的可读性全靠上游阴影字（ShadowedText.qml）
	}
}

// MulAlpha 按系数缩放 alpha：alpha = 色 alpha × opacity（上游 bg_color 自带
// alpha 255，bg_opacity 0.3 → 255×0.3 = 76.5 → 77，实测口径。
func MulAlpha(c color.NRGBA, k float64) color.NRGBA {
	if k < 0 {
		k = 0
	}
	if k > 1 {
		k = 1
	}
	c.A = uint8(math.Round(float64(c.A) * k))
	return c
}

// FillPanel 画面板底：
// 1. 圆角矩形内填 bg_color，alpha = bg_opacity × 255；
// 2. 沿同一圆角矩形描 1px 边，颜色 = border_color，
// alpha = border_opacity × BorderAlphaScale × 255。
// border_opacity = 0（默认）时**不描边**。
// 描边用 clip.Stroke（gioui v0.10.3 实测 API：clip.Stroke{Path, Width}.Op，
// Path 由 RRect.Path(ops) 给出。受 show_background 控制。
// bgOpacity 由调用方给：时钟块传 Style.ClockBGOpacity
// 默认 0.26，天气块传 Style.BGOpacity（默认 0.22）
// —— 上游口径。
// 圆角半径 = min(18, 面板高/6)，随面板高度自适应。
func FillPanel(gtx layout.Context, r image.Rectangle, st Style, bgOpacity float64) {
	if !st.Show {
		return
	}
	if r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y {
		return
	}
	rr := gtx.Dp(unit.Dp(PanelRadiusPx(r.Dy())))
	outer := clip.RRect{Rect: r, SE: rr, SW: rr, NW: rr, NE: rr}
	cl := outer.Push(gtx.Ops)
	// **平铺单色填充 = 上游原样**
	// "时钟底版和天气信息的底版要么跟原版一样，要么全透明也挺好看的，
	// 怎么现在是个四不像！"。
	// 此前曾把面板改成三段边缘渐隐（上下 alpha 归零），
	// 目的是消掉 bin/fxmetrics.py 测到的 panel_step 断层。
	// 结果：既不是原版的实底面板，也不是干净的全透明 —— 四不像。
	// **教训：指标驱动的改动不得越过"与上游一致"这条线。**
	// panel_step 那个"断层"本来就是上游面板的样子。
	base := MulAlpha(st.BG, bgOpacity)
	paint.Fill(gtx.Ops, base)
	cl.Pop()

	if st.BorderOpacity <= 0 {
		return // 默认不描边
	}
	border := clip.Stroke{Path: outer.Path(gtx.Ops), Width: 1}.Op().Push(gtx.Ops)
	paint.Fill(gtx.Ops, MulAlpha(st.Border, st.BorderOpacity*BorderAlphaScale))
	border.Pop()
}

// Separator 对应原版的 detailsSeparator / forecastSeparator（1px 细线）。
// **内缩短线** —— 线宽 = 该块内容宽度
// × 0.62、水平居中、1 px、alpha 0x2A。原实现画的是 gtx.Constraints.Max 全宽
// （= 天气块内容宽，1021 px @1080 屏），读图判为"像一道伤疤一样切断了天空背景"。
// 布局返回值仍是全宽，保证 Flex 的行距与原版一致（只有画出来的线段变短）。
func Separator() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		if size.X <= 0 {
			return layout.Dimensions{}
		}
		h := 1
		lw := int(float64(size.X) * SeparatorLenFrac)
		if lw < 1 {
			lw = 1
		}
		if lw > size.X {
			lw = size.X
		}
		x0 := (size.X - lw) / 2
		mac := op.Offset(image.Pt(0, 0)).Push(gtx.Ops)
		cl := clip.Rect(image.Rect(x0, 0, x0+lw, h)).Push(gtx.Ops)
		paint.Fill(gtx.Ops, InkSep)
		cl.Pop()
		mac.Pop()
		return layout.Dimensions{Size: image.Pt(size.X, h)}
	}
}

// DrawSprite 把精灵贴进绝对矩形 (x,y)–(x+w,y+h)：op.Offset 定位 + clip.Rect 限定 +
// 直接矩阵缩放。
// **不用 Affine2D 的 .Scale 方法**：ui/render.go:1036-1043 的 headless 逐像素探针
// （probe_blit）实测 `NewAffine2D(1,0,0,0,1,0).Scale(pt, origin)`
// 画不出东西（整屏全透明），而直接矩阵 `NewAffine2D(sx,0,0,0,sy,0)` 整屏覆盖可用。
// 变换与裁剪必须 Push/Pop 成对（gioui 的 Affine.Add 是累加语义，
// 见 ui/effects.go:1261-1264 的实测注释。
// 缩放系数直接算：250→30 就是 30.0/250.0，250→192 就是 192.0/250.0
// （需求 3）。
func DrawSprite(gtx layout.Context, x, y, w, h int, img paint.ImageOp, sz image.Point) {
	if w <= 0 || h <= 0 || sz.X <= 0 || sz.Y <= 0 {
		return
	}
	mac := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	cl := clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops)
	tr := op.Affine(f32.NewAffine2D(
		float32(w)/float32(sz.X), 0, 0,
		0, float32(h)/float32(sz.Y), 0,
	)).Push(gtx.Ops)
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
	mac.Pop()
}

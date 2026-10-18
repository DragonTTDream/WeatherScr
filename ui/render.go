package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"strconv"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// Screen 持有文字主题。material.NewTheme 用系统字体（Windows 上有），
// 不内嵌字体 → 不撑体积（产物 ≤15 MB）。
type Screen struct {
	Theme *material.Theme
}

func New() *Screen {
	th := material.NewTheme()
	th.TextSize = WeatherFontSize
	return &Screen{Theme: th}
}

// SkyColor 按天气类与昼夜给不透明天空底色（无壁纸时天气层不透明铺满）。
func SkyColor(cls effect.Class, isDay bool) color.NRGBA {
	if !isDay {
		return nightSky[cls]
	}
	return daySky[cls]
}

var (
	daySky = map[effect.Class]color.NRGBA{
		effect.Clear:   {R: 0x2a, G: 0x6d, B: 0xb8, A: 0xff},
		effect.Cloudy:  {R: 0x6b, G: 0x77, B: 0x84, A: 0xff},
		effect.Fog:     {R: 0x9a, G: 0xa2, B: 0xa8, A: 0xff},
		effect.Rain:    {R: 0x37, G: 0x4a, B: 0x5e, A: 0xff},
		effect.Snow:    {R: 0xa8, G: 0xb8, B: 0xc8, A: 0xff},
		effect.Thunder: {R: 0x2b, G: 0x2f, B: 0x44, A: 0xff},
		effect.Dust:    {R: 0xa8, G: 0x8c, B: 0x5e, A: 0xff},
	}
	nightSky = map[effect.Class]color.NRGBA{
		effect.Clear:   {R: 0x0a, G: 0x14, B: 0x28, A: 0xff},
		effect.Cloudy:  {R: 0x1c, G: 0x24, B: 0x30, A: 0xff},
		effect.Fog:     {R: 0x2a, G: 0x30, B: 0x36, A: 0xff},
		effect.Rain:    {R: 0x10, G: 0x1a, B: 0x26, A: 0xff},
		effect.Snow:    {R: 0x2e, G: 0x38, B: 0x46, A: 0xff},
		effect.Thunder: {R: 0x0c, G: 0x0e, B: 0x1a, A: 0xff},
		effect.Dust:    {R: 0x3a, G: 0x30, B: 0x20, A: 0xff},
	}
)

var (
	ink     = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	inkSoft = color.NRGBA{R: 0xe8, G: 0xee, B: 0xf6, A: 0xd8}
	// inkSep 对应原版 Separator 的 separatorColor（细、半透明）。
	//
	// 降到 0x2A 后，客观指标（bin/fxmetrics.py panel_step）实测
	// 分隔线处**落差仍是 68/255**，且直接量像素得到线色 (98,105,114)、
	// 背景 (28,37,51) ⇒ **有效 alpha ≈ 0.37**，而声明值是 0x165。
	// 根因（这才是"白条"没真正消失的机制）：gioui 的 layout.Flex 对子控件
	// 先测量后绘制，1 px 线段被**叠加绘制 2–3 次**，
	// 累积 alpha = 1−(1−a)^n：0x2A 时 n=3 → 0.418。
	// 故按叠加次数反推：目标"线比背景亮约 15 级" ⇒ 累积 alpha ≈ 0.08
	// ⇒ 单次 a ≈ 0.041 → **0x0A**。颜色 RGB 不动（同色只变 alpha 铁律）。
	inkSep = color.NRGBA{R: 0xd8, G: 0xe2, B: 0xee, A: 0x0A}
)

// separatorLenFrac 分隔线的**内缩比例**：
// 线宽 = 该块内容宽度 × 0.62，水平居中，不得触及面板/屏幕全宽。
// 此前：0.62 → 0.42，让线段占比低于行中位数的采样过半线，
// 指标不再被单条线主导（也进一步降低"切割感"）。
const separatorLenFrac = 0.90 // 需求：上下的分割线拉长 ⇒ 0.42 拉到 0.90

// borderAlphaScale 面板描边的实际 alpha 系数：
// 启用描边时 alpha = border_opacity × 0.35。默认 border_opacity = 0.0 = 不描边。
const borderAlphaScale = 0.35

// shadowInk 逐字照抄上游 rendering/quick/qml/ShadowedText.qml 的
// `shadowColor: "#96000000"`（ARGB，alpha 0x96=150 的暗色副本）。
// 证据：需求 2；本次不改。
var shadowInk = color.NRGBA{R: 0, G: 0, B: 0, A: 0x96}

// shadowOffsetPx = 2 是**下限**，语义来自上游 ShadowedText.qml
// `shadowOffsetY: 2.0` / `shadowOffsetX: 0.0`。
//
// 旧版把它当固定值照抄，实测结果 = 缺陷 2："文字
// 信息）看起来是纯白色的，没有明显的投影或阴影效果"（视觉判读
// 离屏图 lane_cloudy。根因：上游的 2.0 px 是给它的 25 pt 天气字号用的
// （default_settings.py 'weather' 块 font_size 25），本复刻时钟是 144 pt
// （声明值 ClockFontSize=144，低性能 x86 小主机配置 font_size 144→174，
// 见 ui/layout.go:31-32，2 px 在 144 pt 上等于没有。
const shadowOffsetPx = 2

// shadowOffsetForPx 阴影偏移随字号走：max(2, round(字号px × 0.03))
// 下限 2 保持上游语义：
// 144 pt → 4 px、174 pt → 5 px、25 pt → 1 → 取下限 2（与上游一致）、
// 本复刻天气 40 pt → 1 → 取下限 2。
//
// 字号px 口径：gioui 的 unit.Sp 经 gtx.Sp() 换成设备像素
// （unit/unit.go:48-51 `Metric.Sp = round(PxPerSp × v)`）；竖屏目标与 fxshot
// 的 Viewport.Scale=1、TextScale=1 → PxPerSp=1 → 字号 px 就是声明的 sp 值。
func shadowOffsetForPx(px int) int {
	o := int(math.Round(float64(px) * 0.03))
	if o < shadowOffsetPx {
		return shadowOffsetPx
	}
	return o
}

// panelRadiusMaxPx 面板圆角半径上限。上游 weather 块无此键（取证未覆盖），
// 取一个与上游面板视觉一致的常量作为**上限**。TODO(未取证)：上游圆角半径口径。
//
// 半径随面板高度自适应 =
// min(panelRadiusMaxPx, 面板高/6)，不再用固定 8 px。
const panelRadiusMaxPx = 18

// panelRadiusPx 面板圆角半径（像素）= min(18, 面板高/6)。
func panelRadiusPx(panelH int) int {
	r := panelH / 6
	if r > panelRadiusMaxPx {
		r = panelRadiusMaxPx
	}
	if r < 0 {
		r = 0
	}
	return r
}

// PanelStyle 是面板底与文字样式，键名与取值逐字照抄上游
// core/settings/default_settings.py 的 'weather' 块 :1523-1540
// （'clock' 块 :503-528 带同名键）。
// 证据：需求 2（实测）。
type PanelStyle struct {
	// Set=false 时 Layout 用 DefaultPanelStyle()（fxshot 等未显式传值的调用方）。
	Set bool
	// Show = 上游 show_background（默认 True）。
	Show bool
	// BG = 上游 bg_color [35,35,35,255]；BGOpacity = 上游 bg_opacity 0.3。
	BG        color.NRGBA
	BGOpacity float64
	// ClockBGOpacity 时钟块面板底的不透明度，配置键 `clock_bg_opacity`，默认 0.26。
	//
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
	// 启用时实际 alpha = BorderOpacity × borderAlphaScale(0.35)。
	Border        color.NRGBA
	BorderOpacity float64
	// Margin = 上游 margin 30：面板矩形 = 内容区 rect 外扩 margin。
	Margin int
	// TextColor = 上游 color [255,255,255,230]（alpha 230）。
	TextColor color.NRGBA
	// Shadow = 上游 ShadowedText.shadowEnabled（默认 true）。
	Shadow bool
}

// DefaultPanelStyle 上游缺省值（default_settings.py:1523-1540 逐字），
// 外加 ClockBGOpacity = 本复刻的有记录偏离（见 PanelStyle.ClockBGOpacity）。
//
// 三个默认值按视觉读图整改：
//
//	BGOpacity      0.3  → 0.22   （天气面板底）
//	ClockBGOpacity 0.45 → 0.26   （时钟面板底）
//	BorderOpacity  1.0  → 0.0    （默认不描边）
//
// 描边默认关闭的判据 = 读图判读"白色细边框在夜间非常刺眼，像旧式 Windows
// 窗口残留"；面板底减淡的判据 = 底 0.45/0.3 把天空压成两块灰板。
// 键语义与键名不变（铁律：不改配置键语义），只改默认值。
// DefaultPanelStyle 面板底默认值。
//
// 2026-10-16 修掉一个**双真相源**缺陷：config 的默认值与这里的默认值各写一份，
// 需求"全透明"落地时只改了 config，`cmd/fxshot` 走的是本函数
// ⇒ 预览图仍是不透明面板，看到的与生产路径不一致。
// 现在两处必须同口径：**全透明**（无底无边框）。
// 生产路径仍由 `cmd/scr/main.go: panelStyle()` 从 config 覆盖。
func DefaultPanelStyle() PanelStyle {
	return PanelStyle{
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

// Frame 一帧的输入。
type Frame struct {
	Now     time.Time
	Clock   ClockSpec
	Weather *weather.Sample // nil = 取不到；取不到时继续显示上一次，由调用方保证
	Class   effect.Class
	IsDay   bool
	Field   *Field // 背景粒子场；nil = 不画特效
	// PrevField 类别切换期间正在淡出的上一版粒子场；nil = 无切换。
	PrevField *Field
	// Location 地名行（原版 locationText，粗体）。空则不画。
	Location string
	// Panel 面板底样式；Set=false = 用上游缺省。
	Panel PanelStyle
	// ClockYOff / WeatherYOff 时钟与天气块的 y 偏移（像素，面板项）。
	ClockYOff   int
	WeatherYOff int
	// Alert 降雨提醒；nil 或概率低于阈值 = 不画。
	Alert *Alert
	// MoonPhase 今日月相（来自 Sample.MoonPhase）。
	// nil = 取不到 ⇒ 月亮回落到 bakeMoonCrisp 画法（硬边圆盘 + 临边昏暗）。
	// 非 nil ⇒ 按 8 等分相位表选月相素材烘进天空纹理（new moon 不画月亮）。
	MoonPhase *float64
	// Layers 图层掩码（性能定位用）：0 = 全开。
	// 低性能 x86 小主机稳态 CPU 15–17%（预算 ≤5%），`DBG=frame layout=0–1ms frame=8–12ms`
	// ⇒ 瓶颈在 GPU 填充不在 op 构建，必须能逐层量出各层占比。
	Layers LayerMask
}

// LayerMask 图层开关位；零值 = 全开。
type LayerMask uint32

const (
	LayerSky    LayerMask = 1 << 0
	LayerDepth  LayerMask = 1 << 1
	LayerField  LayerMask = 1 << 2
	LayerClock  LayerMask = 1 << 3
	LayerWeath  LayerMask = 1 << 4
	LayerShadow LayerMask = 1 << 5
	// LayerAlert 降雨提醒层。加进 LayerAll ⇒ 掩码为 0（全开）时默认画。
	LayerAlert LayerMask = 1 << 6
)

// LayerAll 全开（含"未设置"语义：掩码为 0 时按全开处理）。
const LayerAll = LayerSky | LayerDepth | LayerField | LayerClock | LayerWeath | LayerShadow | LayerAlert

func (m LayerMask) on(l LayerMask) bool {
	if m == 0 {
		return true // 未显式设置 = 全开
	}
	return m&l != 0
}

// Layout 绘制整帧：不透明天空 → 天空纵深 → 粒子特效 → 时钟 rect → 天气 rect。
func (s *Screen) Layout(gtx layout.Context, f Frame) layout.Dimensions {
	size := gtx.Constraints.Max
	skyOn := f.Layers.on(LayerSky)
	depthOn := f.Layers.on(LayerDepth)
	if skyOn && depthOn {
		// 天空 + 暗角一起：烘焙成一张纹理，每帧 1 次贴图
		skyBlit(gtx, size, f.Class, f.IsDay, f.MoonPhase)
	} else if skyOn {
		skyGradient(gtx, size, f.Class, f.IsDay)
	} else if depthOn {
		skyDepth(gtx, size, f.Class, f.IsDay) // 地平线辉光带 + 四角暗角
	}
	// 月亮独立层：画在天空之后、云/粒子之前
	// ⇒ 云真正飘在月亮上面（需求：多云时云飘在月亮上面，
	// 不是压在云层下面。全分辨率 ⇒ 边缘无整像素台阶。
	if f.Layers.on(LayerSky) {
		moonBlit(gtx, size, f.IsDay, f.MoonPhase)
	}
	// 类别切换期间先画旧场（淡出中），再画新场（淡入中）
	// ⇒ 云不会在切换那一帧整片凭空出现/消失。
	if f.PrevField != nil && f.Layers.on(LayerField) {
		f.PrevField.Draw(gtx, size.X, size.Y)
	}
	if f.Field != nil && f.Layers.on(LayerField) {
		f.Field.Draw(gtx, size.X, size.Y)
	}

	st := f.Panel
	if !st.Set {
		st = DefaultPanelStyle() // fxshot 等未显式传值 → 上游缺省
	}
	f.Panel = st

	if f.Layers.on(LayerClock) {
		s.clockBlock(gtx, size, f)
	}
	if f.Layers.on(LayerWeath) {
		s.weatherBlock(gtx, size, f)
	}
	// 提醒画在最上层（sky → field → clock → weather → alert）。
	// 出厂版式 panelrow：画在天气面板下沿之内 + 绝对屏幕坐标水平居中。
	// 走 flex 行盒时实测 chip 中心 766 vs 屏幕中心 540，右偏 226 px。
	if f.Layers.on(LayerAlert) {
		wpx := shiftY(WeatherRect.Px(size.X, size.Y), f.WeatherYOff)
		DrawAlertPanelRow(gtx, size, f.Alert, s, panelRect(wpx, f.Panel.Margin).Max.Y)
	}

	return layout.Dimensions{Size: size}
}

// mulAlpha 按系数缩放 alpha：alpha = 色 alpha × opacity（上游 bg_color 自带
// alpha 255，bg_opacity 0.3 → 255×0.3 = 76.5 → 77，实测口径。
func mulAlpha(c color.NRGBA, k float64) color.NRGBA {
	if k < 0 {
		k = 0
	}
	if k > 1 {
		k = 1
	}
	c.A = uint8(math.Round(float64(c.A) * k))
	return c
}

// fillPanel 画面板底：
// 1. 圆角矩形内填 bg_color，alpha = bg_opacity × 255；
// 2. 沿同一圆角矩形描 1px 边，颜色 = border_color，
// alpha = border_opacity × borderAlphaScale × 255。
// border_opacity = 0（默认）时**不描边**。
//
// 描边用 clip.Stroke（gioui v0.10.3 实测 API：clip.Stroke{Path, Width}.Op()，
// Path 由 RRect.Path(ops) 给出。受 show_background 控制。
//
// bgOpacity 由调用方给：时钟块传 PanelStyle.ClockBGOpacity
// 默认 0.26，天气块传 PanelStyle.BGOpacity（默认 0.22）
// —— 上游口径。
//
// 圆角半径 = min(18, 面板高/6)，随面板高度自适应。
func fillPanel(gtx layout.Context, r image.Rectangle, st PanelStyle, bgOpacity float64) {
	if !st.Show {
		return
	}
	if r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y {
		return
	}
	rr := gtx.Dp(unit.Dp(panelRadiusPx(r.Dy())))
	outer := clip.RRect{Rect: r, SE: rr, SW: rr, NW: rr, NE: rr}
	cl := outer.Push(gtx.Ops)
	// **平铺单色填充 = 上游原样**
	// "时钟底版和天气信息的底版要么跟原版一样，要么全透明也挺好看的，
	// 怎么现在是个四不像！"。
	//
	// 此前曾把面板改成三段边缘渐隐（上下 alpha 归零），
	// 目的是消掉 bin/fxmetrics.py 测到的 panel_step 断层。
	// 结果：既不是原版的实底面板，也不是干净的全透明 —— 四不像。
	// **教训：指标驱动的改动不得越过"与上游一致"这条线。**
	// panel_step 那个"断层"本来就是上游面板的样子。
	base := mulAlpha(st.BG, bgOpacity)
	paint.Fill(gtx.Ops, base)
	cl.Pop()

	if st.BorderOpacity <= 0 {
		return // 默认不描边
	}
	border := clip.Stroke{Path: outer.Path(gtx.Ops), Width: 1}.Op().Push(gtx.Ops)
	paint.Fill(gtx.Ops, mulAlpha(st.Border, st.BorderOpacity*borderAlphaScale))
	border.Pop()
}

// shadowed 照抄上游 ShadowedText.qml 的语义：同一字形先在偏移
// (0, +偏移) 画一份 alpha 0x96 的暗色副本（上游 z:0），再以主字色画在 (0,0)
// （上游 z:1）。**无模糊、无 MultiEffect、无 layer capture**（该文件注释明说）。
// row 是"按给定颜色画这一行"工厂，整行（含内联多段文字）一起复制两份。
//
// 偏移 = shadowOffsetForPx(gtx.Sp(sp))，随字号走
// （旧版固定 2 px 在 144 pt 大字上实测不可见 = 缺陷 2）。
func (s *Screen) shadowed(st PanelStyle, sp unit.Sp, row rowFn, layers LayerMask) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if st.Shadow && layers.on(LayerShadow) {
			off := shadowOffsetForPx(gtx.Sp(sp))
			mac := op.Offset(image.Pt(0, off)).Push(gtx.Ops)
			row(shadowInk)(gtx)
			mac.Pop()
		}
		return row(st.TextColor)(gtx)
	}
}

// skyGradient 竖直方向的天色渐变：天顶深、地平线亮。
// 判读意见"背景过于纯黑，缺乏大气层的深蓝渐变" —— 单色平铺正是成因。
func skyGradient(gtx layout.Context, size image.Point, cls effect.Class, isDay bool) {
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
//
// 1. 地平线辉光带：位于天空底部 22% 高度处（y ∈ [0.78h, h]），
// 颜色 = 当前基色提亮 base×1.25+14，alpha 沿高度 0 → 30 → 0
// 峰在带中点 0.89h；
// 2. 四角暗角 vignette：上下各一条高度 16% 的渐变（黑色 alpha 38 → 0 顶部向内、
// 0 → 38 底部向内，左右同理宽度 12%。
//
// 全部**同 RGB 只变 alpha**（铁律：gioui 的渐变按端点色插值，
// 两端 RGB 不同会把整屏插到白，实测过一次。颜色从 skyGradient 的基色派生，
// 不新增硬编码颜色表。
//
// 每段渐变都**独立裁剪**：gioui 的渐变在端点之外按端点色外插
// （gpu.go:1361 gradientSpaceTransform），不裁剪的三段渐变会把峰后的 alpha
// 平推成 30 铺满下半屏。
func skyDepth(gtx layout.Context, size image.Point, cls effect.Class, isDay bool) {
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
	gradAlpha(gtx, image.Rect(0, y0, size.X, ym), glow, 0, 30, true)
	gradAlpha(gtx, image.Rect(0, ym, size.X, y1), glow, 30, 0, true)

	// 2 四角暗角：上下 16% 高度、左右 12% 宽度。
	//
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
		gradAlpha(gtx, image.Rect(0, int(float32(hv)*f0), size.X, int(float32(hv)*f1)), black, a0, a1, true)
		// 下（镜像）
		gradAlpha(gtx, image.Rect(0, size.Y-int(float32(hv)*f1), size.X, size.Y-int(float32(hv)*f0)), black, a1, a0, true)
		// 左
		gradAlpha(gtx, image.Rect(int(float32(hw)*f0), 0, int(float32(hw)*f1), size.Y), black, a0, a1, false)
		// 右（镜像）
		gradAlpha(gtx, image.Rect(size.X-int(float32(hw)*f1), 0, size.X-int(float32(hw)*f0), size.Y), black, a1, a0, false)
	}
}

// gradAlpha 在矩形 r 内铺一条线性渐变，**两端 RGB 完全相同、只变 alpha**
// （vertical = true 沿 y 方向，否则沿 x）。r 非法时不画。
func gradAlpha(gtx layout.Context, r image.Rectangle, c color.NRGBA, a1, a2 uint8, vertical bool) {
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

// inRect 把内容画进归一化 rect 指定的像素区域。
// 裁剪矩形是必须的：不设裁剪时 paint.Fill 会填整个当前裁剪区（坑 1）。
func inRect(gtx layout.Context, r Rect, sw, sh int, content layout.Widget) {
	inPx(gtx, r.Px(sw, sh), content)
}

// inPx 同 inRect，但直接给像素矩形（时钟/天气块的 y 偏移在这里落地）。
func inPx(gtx layout.Context, px PxRect, content layout.Widget) {
	mac := op.Offset(image.Pt(px.X, px.Y)).Push(gtx.Ops)
	cl := clip.Rect(image.Rectangle{Max: image.Pt(px.W, px.H)}).Push(gtx.Ops)
	gtx2 := gtx
	gtx2.Constraints = layout.Exact(image.Point{X: px.W, Y: px.H})
	layout.Center.Layout(gtx2, content)
	cl.Pop()
	mac.Pop()
}

func (s *Screen) label(txt string, sp unit.Sp, col color.NRGBA, align text.Alignment) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if txt == "" {
			return layout.Dimensions{}
		}
		l := material.Label(s.Theme, sp, txt)
		l.Color = col
		l.Alignment = align
		return layout.Center.Layout(gtx, l.Layout)
	}
}

// shiftY 把像素矩形整体上下偏移 dy（布局偏移面板项）。
func shiftY(px PxRect, dy int) PxRect {
	px.Y += dy
	return px
}

// panelRect 面板矩形 = 内容区 rect 外扩 margin（上游 margin 30）。
func panelRect(px PxRect, margin int) image.Rectangle {
	return image.Rect(px.X-margin, px.Y-margin, px.X+px.W+margin, px.Y+px.H+margin)
}

// rowFn 是"按给定颜色画一行文字"工厂：shadowed 用它把整行画两遍
// （阴影副本 + 主字），内联多段文字的条件行也整体复制。
type rowFn = func(col color.NRGBA) layout.Widget

// rowText 单行文字。mod 用于粗体/斜体等字形修饰。
func (s *Screen) rowText(txt string, sp unit.Sp, mod func(*material.LabelStyle)) rowFn {
	return func(col color.NRGBA) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			if txt == "" {
				return layout.Dimensions{}
			}
			l := material.Label(s.Theme, sp, txt)
			l.Color = col
			l.Alignment = text.Middle
			if mod != nil {
				mod(&l)
			}
			return layout.Center.Layout(gtx, l.Layout)
		}
	}
}

// rowCondition 条件行：温度(0.81×) " - " + 条件
// rendering/quick/widgets/weather.py:758-772 的 0.81 口径。
func (s *Screen) rowCondition(temp, cond string) rowFn {
	return func(col color.NRGBA) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{
					Axis:      layout.Horizontal,
					Alignment: layout.Middle,
				}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(s.Theme, unit.Sp(WeatherFontSize*0.81), temp)
						l.Color = col
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(s.Theme, unit.Sp(WeatherFontSize), " - "+cond)
						l.Color = col
						return l.Layout(gtx)
					}),
				)
			})
		}
	}
}

func bold(l *material.LabelStyle)   { l.Font.Weight = font.Bold }
func italic(l *material.LabelStyle) { l.Font.Style = font.Italic }

func (s *Screen) clockBlock(gtx layout.Context, size image.Point, f Frame) {
	timeStr := f.Clock.TimeLine(f.Now)
	dateStr := f.Clock.DateLine(f.Now)

	px := shiftY(ClockRect.Px(size.X, size.Y), f.ClockYOff)
	// 面板底画在文字之前、块定位之后。
	// 时钟块用 clock_bg_opacity（默认 0.45，强于天气）—— 偏离上游
	// 'clock' 块 show_background: False，理由见 PanelStyle.ClockBGOpacity
	//
	fillPanel(gtx, panelRect(px, f.Panel.Margin), f.Panel, f.Panel.ClockBGOpacity)

	inPx(gtx, px, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis:      layout.Vertical,
			Alignment: layout.Middle,
			Spacing:   layout.SpaceEvenly,
		}.Layout(gtx,
			// 时间行、日期行都走阴影；偏移随字号走
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(ClockFontSizeShown), s.rowText(timeStr, unit.Sp(ClockFontSizeShown), nil), f.Layers)),
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(WeatherFontSize), s.rowText(dateStr, unit.Sp(WeatherFontSize), nil), f.Layers)),
		)
	})
}

// weatherTextColumn 天气块的纯文字列（地名/条件/分隔线/详情行/分隔线/预报）。
// 状况图标存在时它占 [块宽 − 192 − 12] 的那一段（需求 3）。
func (s *Screen) weatherTextColumn(f Frame, temp string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis:      layout.Vertical,
			Alignment: layout.Middle,
			Spacing:   layout.SpaceEvenly,
		}.Layout(gtx,
			// locationText：粗体（地名行，走阴影）
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(WeatherFontSize), s.rowText(f.Location, unit.Sp(WeatherFontSize), bold), f.Layers)),
			// conditionText：温度(0.81×) " - " + 条件（整行走阴影）
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(WeatherFontSize), s.rowCondition(temp, f.Weather.Condition), f.Layers)),
			layout.Rigid(s.separator()),
			// detailsRow：图标化的三项 rain/humidity/wind（文字部分走阴影）
			layout.Rigid(s.rowDetails(f)),
			layout.Rigid(s.separator()),
			// forecastText：斜体（走阴影）
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(WeatherFontSize), s.rowText(forecastOf(f.Weather), unit.Sp(WeatherFontSize), italic), f.Layers)),
		)
	}
}

// condIconGapPx 状况图标与文字列之间的间隙。
// 证据：上游 WeatherPresentation.qml:110 块宽含 `iconSize + 12.0`。
const condIconGapPx = 12

// weatherBlock 逐行对齐原版 WeatherPresentation.qml 的 readyColumn：
//
//	locationText   粗体，fontSize            （原版有地名）
//	conditionText  "<temp> - <condition>"，温度以 0.81× 条件字号内联
//	detailsSeparator 1px 分隔线
//	detailsRow     图标化三项 rain/humidity/wind
//	forecastSeparator 1px 分隔线
//	forecastText   斜体，detailFontSize，居中
//
// 字号口径来自 rendering/quick/widgets/weather.py:758-772
// （temperatureFontSize = conditionFontSize * 0.81）。
//
// 状况图标：低性能 x86 小主机配置 icon_size 192、
// show_condition_icon True、icon_alignment 'RIGHT'（低性能 x86 小主机 settings_v2.json）。
// ⇒ 块内布局 = [文字列][12 px][192×192 图标]，图标靠右、垂直居中于文字列
// （上游 WeatherPresentation.qml:328-338：anchors.right + anchors.verticalCenter）。
// 映射不到图标时退化为纯文字列，不预留图标位（需求 4）。
func (s *Screen) weatherBlock(gtx layout.Context, size image.Point, f Frame) {
	w := f.Weather
	if w == nil {
		return
	}
	temp := "--"
	if w.Temperature != nil {
		temp = formatTemp(*w.Temperature)
	}

	px := shiftY(WeatherRect.Px(size.X, size.Y), f.WeatherYOff)
	// 面板底画在文字之前、块定位之后。
	// 天气块沿用上游 bg_opacity 0.3。
	fillPanel(gtx, panelRect(px, f.Panel.Margin), f.Panel, f.Panel.BGOpacity)

	inPx(gtx, px, func(gtx layout.Context) layout.Dimensions {
		col := s.weatherTextColumn(f, temp)
		if !WeatherShowIcon {
			return col(gtx)
		}
		iop, isz, ok := condSprite(w.WeatherCode, f.IsDay)
		if !ok {
			return col(gtx)
		}
		return layout.Flex{
			Axis:      layout.Horizontal,
			Alignment: layout.Middle, // 图标垂直居中于文字列
		}.Layout(gtx,
			layout.Flexed(1, col), // 文字列宽 = 块宽 − 192 − 12
			layout.Rigid(layout.Spacer{Width: unit.Dp(condIconGapPx)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				drawSprite(gtx, 0, 0, WeatherIconSize, WeatherIconSize, iop, isz)
				return layout.Dimensions{Size: image.Pt(WeatherIconSize, WeatherIconSize)}
			}),
		)
	})
}

// separator 对应原版的 detailsSeparator / forecastSeparator（1px 细线）。
//
// **内缩短线** —— 线宽 = 该块内容宽度
// × 0.62、水平居中、1 px、alpha 0x2A。原实现画的是 gtx.Constraints.Max 全宽
// （= 天气块内容宽，1021 px @1080 屏），读图判为"像一道伤疤一样切断了天空背景"。
// 布局返回值仍是全宽，保证 Flex 的行距与原版一致（只有画出来的线段变短）。
func (s *Screen) separator() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		if size.X <= 0 {
			return layout.Dimensions{}
		}
		h := 1
		lw := int(float64(size.X) * separatorLenFrac)
		if lw < 1 {
			lw = 1
		}
		if lw > size.X {
			lw = size.X
		}
		x0 := (size.X - lw) / 2
		mac := op.Offset(image.Pt(0, 0)).Push(gtx.Ops)
		cl := clip.Rect(image.Rect(x0, 0, x0+lw, h)).Push(gtx.Ops)
		paint.Fill(gtx.Ops, inkSep)
		cl.Pop()
		mac.Pop()
		return layout.Dimensions{Size: image.Pt(size.X, h)}
	}
}

// drawSprite 把精灵贴进绝对矩形 (x,y)–(x+w,y+h)：op.Offset 定位 + clip.Rect 限定 +
// 直接矩阵缩放。
//
// **不用 Affine2D 的 .Scale 方法**：ui/render.go:1036-1043 的 headless 逐像素探针
// （probe_blit）实测 `NewAffine2D(1,0,0,0,1,0).Scale(pt, origin)`
// 画不出东西（整屏全透明），而直接矩阵 `NewAffine2D(sx,0,0,0,sy,0)` 整屏覆盖可用。
// 变换与裁剪必须 Push/Pop 成对（gioui 的 Affine.Add 是累加语义，
// 见 ui/effects.go:1261-1264 的实测注释。
//
// 缩放系数直接算：250→30 就是 30.0/250.0，250→192 就是 192.0/250.0
// （需求 3）。
func drawSprite(gtx layout.Context, x, y, w, h int, img paint.ImageOp, sz image.Point) {
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

// detailItem 详情行的一项（kind 对应上游 _DETAIL_ICON_FILES 的键）。
type detailItem struct {
	kind string
	text string
}

// detailItems 上游详情行三项的**顺序**与**文案口径**：
//
//	顺序 rain → humidity → wind（WeatherPresentation.qml:401-415 的 Repeater model）
//	文案 f"{precip:.0f}%" / f"{humidity:.0f}%" / f"{wind:.1f} km/h"（weather.py:725-727）
//
// 上游取不到值时是 nil（schema.go 的指针字段口径） ⇒ text 留空 ⇒ 该格整格不画，
// 但 3 等分几何不变（需求 3）。
//
// **降水项口径 = 当前时刻**（2026-10-16 逐字决定
// "伞图标口径：保持当前时刻"：
// 上游详情行取的是 current 的 precipitation_probability
// （upstream/rendering/quick/widgets/weather.py:700
// `precipitation = _optional_float(data.get("precipitation_probability"))`。
// 中间一版为了解决"伞 0% vs 预警 88%"自相矛盾，
// 把降水项改取 daily.precipitation_probability_max（当天最大值）；
// 本次预警的数据源换成**未来 N 小时滚动窗口**，
// "当天最大值"不再是屏幕上的预警量 ⇒ 伞项回到**当前时刻**：
// Sample.PrecipitationProbability（provider 已改取 current，
// current 为 nil 时回落 hourly[0]，需求 2。
// 湿度、风速两项**不变**（仍 current 口径，与上游一致，需求 4）。
func detailItems(w *weather.Sample) []detailItem {
	items := []detailItem{{kind: "rain"}, {kind: "humidity"}, {kind: "wind"}}
	if w == nil {
		return items
	}
	if w.PrecipitationProbability != nil {
		items[0].text = fmt.Sprintf("%.0f%%", *w.PrecipitationProbability)
	}
	if w.Humidity != nil {
		items[1].text = fmt.Sprintf("%.0f%%", *w.Humidity)
	}
	if w.WindSpeed != nil {
		items[2].text = fmt.Sprintf("%.1f km/h", *w.WindSpeed)
	}
	return items
}

// rowDetails 上游 details row 的图标化版本
// 取代原 detailsLine 的中文标签串。
//
// 上游布局原文（WeatherPresentation.qml:378-425）：
//
//	行 width = parent.width；height = max(68.0, detailIconSize+38.0, detailFontSize*3.2)
//	三项各占 detailsRow.width / 3.0
//	格内内容 width = metricIcon.width + 1.0 + metricText.implicitWidth
//	              height = Math.max(icon.height, text.implicitHeight)
//	格内水平垂直居中（anchors.centerIn: parent）
//
// 低性能 x86 小主机数字（低性能 x86 小主机 settings_v2.json：font_size 40、detail_icon_size 16）
// ⇒ 图标 30 px、文字 20 px、行高 max(68, 68, 64) = 68 px。
//
// 文字走现有 shadowed 的阴影口径（上游 ShadowedText：shadowColor "#96000000"、
// shadowOffsetY 2.0、无模糊；图标是位图，上游不给它阴影。
func (s *Screen) rowDetails(f Frame) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		rowW := gtx.Constraints.Max.X
		if rowW <= 0 {
			return layout.Dimensions{}
		}
		rowH := detailRowHeight()
		cellW := rowW / 3
		sp := unit.Sp(DetailFontPx)
		off := shadowOffsetForPx(gtx.Sp(DetailFontPx))
		shadowOn := f.Panel.Shadow && f.Layers.on(LayerShadow)
		for i, it := range detailItems(f.Weather) {
			if it.text == "" {
				continue // nil 项：整格不画，几何占位不变
			}
			cellX := i * cellW
			d := measureText(gtx, s.label(it.text, sp, f.Panel.TextColor, text.Middle))
			tw, th := d.Size.X, d.Size.Y
			if tw <= 0 || th <= 0 {
				continue
			}
			iw, ih := DetailIconPx, DetailIconPx
			if it.kind == "wind" {
				// 风图标可读性（方案 (a)）：
				// detail_wind.png 字形只占画布 0.78 ⇒ 40 px 下字形 ≈31 px 读成"-5"。
				// 按字形占画布比例放大绘制边长 = DetailIconPx/0.78 = 51 px（上限 56），
				// 使字形视觉尺寸与 DetailIconPx 一致。素材本身不动。
				sz := windIconDrawPx()
				iw, ih = sz, sz
			}
			iop, isz, ok := detailSprite(it.kind)
			if !ok {
				// 素材缺失时只画文字。上游没有这条路径。TODO(未取证)
				iw, ih = 0, 0
			}
			contentW := iw + detailGapPx + tw
			contentH := ih
			if th > contentH {
				contentH = th
			}
			cx := cellX + (cellW-contentW)/2
			cy := (rowH - contentH) / 2
			if iw > 0 {
				drawSprite(gtx, cx, cy+(contentH-ih)/2, iw, ih, iop, isz)
			}
			tx := cx + iw + detailGapPx
			ty := cy + (contentH-th)/2
			if shadowOn {
				inPx(gtx, PxRect{X: tx, Y: ty + off, W: tw, H: th},
					s.label(it.text, sp, shadowInk, text.Middle))
			}
			inPx(gtx, PxRect{X: tx, Y: ty, W: tw, H: th},
				s.label(it.text, sp, f.Panel.TextColor, text.Middle))
		}
		return layout.Dimensions{Size: image.Pt(rowW, rowH)}
	}
}

func forecastOf(w *weather.Sample) string {
	if w.Forecast == nil {
		return ""
	}
	return *w.Forecast
}

func formatTemp(v float64) string { return formatNum(v) + "°C" }

// formatNum 整数值去掉小数点，与上游 :.0f 呈现习惯一致。
func formatNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// ── 静态背景烘焙缓存（性能优化）──────────────────────────
//
// 实测：每帧发 19 个 paint op（天空 1 + 地平线辉光 2 + 暗角 4 边×4 段 16），
// 15 fps ⇒ 285 次 draw call/s。`DBG=frame layout=0–1ms frame=8–12ms`
// ⇒ 贵的是提交+GPU（填充与 draw call），不是 op 构建。
//
// 天空与暗角只随 (天气类, 昼夜, 尺寸) 变，逐帧完全重复 ⇒ 烘焙成一张
// 纹理，每帧 1 次贴图。烘焙在 1/2 分辨率做（生成成本 1/4），
// 贴图时放大 2×（GPU 双线性采样，顺带把渐变的台阶抹平）。
//
// 逐像素复现 GPU 语义：`gradAlpha` = 从 (c,a1) 到 (c,a2) 的线性渐变，
// src-over 合成 ⇒ CPU 侧按同一插值与同一合成公式逐像素算。

const skyCacheScale = 2 // 烘焙分辨率 = 屏幕 / 2

type skyCacheKey struct {
	cls   effect.Class
	isDay bool
	w, h  int
	moon  int // 量化后的月相（moonPhaseKey），需求 4
}

type skyCacheEntry struct {
	img *image.NRGBA
	ops paint.ImageOp // 按 image 指针缓存纹理（gioui 以 image 指针为纹理键）
}

type skyCacheType struct {
	key skyCacheKey
	ent *skyCacheEntry
}

var skyCache skyCacheType

// SkyCacheStats 烘焙缓存命中统计（性能定位用）。
type SkyCacheStats struct {
	Hits, Builds int
	LastKey      string
	LastMs       float64
}

var skyStats SkyCacheStats

// SkyCacheGet 读出统计快照。
func SkyCacheGet() SkyCacheStats { return skyStats }

// moonPhaseKey 把月相量化进烘焙缓存键（需求 4）：
// 键里放**量化后的相位** int(phase*800)（1/800 相位精度），避免每帧因浮点抖动重烘。
// nil（取不到月相）→ -1，与"相位 0 = 新月"区分开（两者画面不同：nil 画硬边圆盘，
// 0 落在 new 区间不画月亮。
func moonPhaseKey(phase *float64) int {
	if phase == nil {
		return -1
	}
	p := *phase
	p = p - math.Floor(p)
	if p < 0 {
		p += 1
	}
	return int(p * 800)
}

// skyCached 返回烘焙好的背景纹理与它的 ImageOp；尺寸/类别/月相变化时重建。
func skyCached(size image.Point, cls effect.Class, isDay bool, phase *float64) *skyCacheEntry {
	k := skyCacheKey{cls: cls, isDay: isDay, w: size.X, h: size.Y, moon: moonPhaseKey(phase)}
	if skyCache.key == k && skyCache.ent != nil {
		skyStats.Hits++
		return skyCache.ent
	}
	t0 := time.Now()
	img := renderSkyCPU(size.X/skyCacheScale, size.Y/skyCacheScale, cls, isDay, phase)
	ent := &skyCacheEntry{img: img, ops: paint.NewImageOp(img)}
	skyCache.key = k
	skyCache.ent = ent
	skyStats.Builds++
	skyStats.LastKey = fmt.Sprintf("%s/%v/%dx%d/moon=%d", cls, isDay, size.X, size.Y, k.moon)
	skyStats.LastMs = float64(time.Since(t0).Microseconds()) / 1000.0
	return ent
}

// renderSkyCPU 在 CPU 上逐像素画出与 skyGradient + skyDepth 相同的画面。
func renderSkyCPU(w, h int, cls effect.Class, isDay bool, phase *float64) *image.NRGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	base := SkyColor(cls, isDay)
	top := color.NRGBA{
		R: uint8(float64(base.R) * 0.45),
		G: uint8(float64(base.G) * 0.45),
		B: uint8(float64(base.B) * 0.55),
		A: 0xff,
	}
	horizon := color.NRGBA{
		R: clamp8(float64(base.R)*1.35 + 18),
		G: clamp8(float64(base.G)*1.35 + 18),
		B: clamp8(float64(base.B)*1.25 + 14),
		A: 0xff,
	}
	glow := color.NRGBA{
		R: clamp8(float64(base.R)*1.25 + 14),
		G: clamp8(float64(base.G)*1.25 + 14),
		B: clamp8(float64(base.B)*1.25 + 14),
		A: 0xff,
	}
	fw, fh := float64(w), float64(h)

	// 1 天空：top → horizon 竖向线性
	for y := 0; y < h; y++ {
		t := float64(y) / math.Max(1, fh-1)
		r := lerpF(float64(top.R), float64(horizon.R), t)
		g := lerpF(float64(top.G), float64(horizon.G), t)
		b := lerpF(float64(top.B), float64(horizon.B), t)
		for x := 0; x < w; x++ {
			dst.SetNRGBA(x, y, color.NRGBA{R: clamp8(r), G: clamp8(g), B: clamp8(b), A: 0xff})
		}
	}

	// 2 地平线辉光带：底部 22%，alpha 0 → 30 → 0
	y0, ym, y1 := int(fh*0.78), int(fh*0.89), h
	blendAlphaV(dst, image.Rect(0, y0, w, ym), glow, 0, 30)
	blendAlphaV(dst, image.Rect(0, ym, w, y1), glow, 30, 0)

	// 3 四角暗角：几何衰减分段（与 GPU 版同一张表）
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}
	vignetteAlpha := []uint8{38, 20, 9, 3, 0}
	vignetteFrac := []float32{0, 0.35, 0.65, 0.85, 1.0}
	hv, hw := fh*0.16, fw*0.12
	for i := 0; i+1 < len(vignetteAlpha); i++ {
		f0, f1 := float64(vignetteFrac[i]), float64(vignetteFrac[i+1])
		a0, a1 := vignetteAlpha[i], vignetteAlpha[i+1]
		blendAlphaV(dst, image.Rect(0, int(hv*f0), w, int(hv*f1)), black, a0, a1)
		blendAlphaV(dst, image.Rect(0, h-int(hv*f1), w, h-int(hv*f0)), black, a1, a0)
		blendAlphaH(dst, image.Rect(int(hw*f0), 0, int(hw*f1), h), black, a0, a1)
		blendAlphaH(dst, image.Rect(w-int(hw*f1), 0, w-int(hw*f0), h), black, a1, a0)
	}

	// 4 月亮 / 太阳：静止天体，烘进纹理 ⇒ 每帧零成本。
	//
	// 实测依据：`drawMoon` 每帧画 64 个圆盘，最外环半径 = rc×(1+3.4) = 4.4rc，
	// 单环 (2·4.4·48.6)² ≈ 183 Kpx ⇒ 64 环 ≈ 11.7 Mpx/帧 = **5.7 个整屏当量**
	// （实测口径 ≈ 25% CPU）。64 环叠加实现的就是
	// `A(frac) = A0·exp(−decay·frac)` 这条指数曲线本身，
	// 故 CPU 侧直接按该解析式逐像素积分，与 GPU 环叠加等价（同一 A0/decay/spread）。
	// 唯一取舍：月亮 ±4% 的"呼吸"脉动不再逐帧动（4% 幅度肉眼不可读）。
	// **所有类别都烘天体与星星**
	// "星星只是在云层后面不是多云就没星星了，月亮也一样，多云只是云飘在月亮上面"。
	// 原实现 `if cls == effect.Clear` ⇒ 云量一过 30% 类别从 Clear 变 Cloudy，
	// 星星与月亮整片消失，屏幕从"深夜蓝+星+月"突变成"灰云盖屏"
	// （低性能 x86 小主机实测：类别变化前后天空小窗 14.1 → 46~58）。
	// 烘进纹理天然在云的下层：云在粒子层后画 ⇒ 云飘在月亮上面。
	bakeCelestial(dst, w, h, isDay, phase)
	if !isDay {
		bakeStars(dst, w, h)
	}
	return dst
}

// bakeCelestial 把天体画进烘焙图：夜 = 月亮（有月相走素材 bakeMoon，
// 无月相回落 bakeMoonCrisp 硬边圆盘 + 临边昏暗；昼 = 解析衰减太阳（见 bakeSunAnalytic）。
func bakeCelestial(dst *image.NRGBA, w, h int, isDay bool, phase *float64) {
	if !isDay {
		// 夜间月亮**不再烘进天空纹理**：天空是半分辨率烘焙（skyCacheScale=2），
		// 月亮在里面只有 32 px 半径，放大 2 倍后外缘出现整像素台阶
		// （低性能 x86 小主机离屏图 shot66 量到相邻行最大跳变 7 px），且云（α 0.28–0.45）
		// 压不住 248 级月面 ⇒ 月亮读成在云层上面。
		// 月亮改由独立层 moonBlit 以全分辨率画在云之前（见该函数注释）。
		return
	}
	if isDay {
		// 白天：**只画解析衰减太阳，不画圆盘**。
		// 低性能 x86 小主机反馈"像个圆盘"+ 视觉模型判"有明确边界的圆盘"。
		// 实测定位：剖面 [229 227 221 177 167 …] 的平坦顶部止于 r≈16、
		// **r=24 处跌 44** —— 而 rc = 0.045w = 24 px，就是 cel 核的半径边缘。
		// 硬边圆盘 = 病根本身，白天不再画它。
		bakeSunAnalytic(dst, w, h)
		return
	}
	// 夜：照上游口径画月亮
	// "月亮的边缘太糊了很影响观感，从上游原版获取的那个月亮虽然是卡通的是不真实，
	// 但是看着就很顺眼一点也不糊"。依据与参照见 bakeMoonCrisp 注释。
	bakeMoon(dst, w, h, phase)
}

// bakeMoon 月亮烘焙：**本实现自身的解析画法**
// "去掉虚线只画亮面，需要光晕，原来那个就挺好的，让参考其显示方式让其不糊不粗糙，
//
//	不是让直接换成上游的了！"。
//
// 四条口径：
//  1. **只画亮面**，暗面不留任何轮廓 ⇒ 上游素材那圈 8 段虚线描边去掉
// （量 moon-waxing-crescent.png：半径带 1775 px 其中半透明 269 px = 那圈虚线）。
// 2. **边缘锐利用上游的 limb 口径**（neon_celestial.py:309-311
// `limb = smoothstep(0.015, 0.10, facing)`，`facing = √(1−(r/R)²)`
// ⇒ 过渡带只占半径 0.49%（R=65 px 时 0.32 px），不是原先 10% 半径的糊边。
// 借的是**显示方式**，不是换成上游素材。
// 3. **保留光晕**（需求：要光晕，原来那个就挺好）：指数衰减 + cos² 窗归一到 0，
// 消除外边界硬台阶（实测无窗时 alpha 6→0 的台阶就是可见边界）。
// 4. **放大**：rc 0.045w → 0.060w（需求：月亮要更大；
// 上游素材口径下有效月面只有画布的 56%，所以素材版看着小。
//
// 月相几何（θ=2πφ，a=|cosθ| 归一化半轴，E=(X/a)²+Y²<1，X/Y 为对圆心归一化坐标）：
//
//	盈（φ<0.5，亮面在右）：cosθ>0 ⇒ 亮 = 右半 ∧ ¬E（眉月）
//	                        cosθ<0 ⇒ 亮 = 右半 ∨ E（凸月）
//	                        cosθ=0 ⇒ 亮 = 右半（上弦）
//	亏（φ>0.5，亮面在左）：X 取反，同一套。
//
// 自检：φ=0 全暗 · 0.125 右眉 · 0.25 右半 · 0.375 右凸 · 0.5 全亮 · 0.75 左半 · 0.875 左眉。
// 照度 k=(1−cosθ)/2；k<0.02 视为新月，不画月也不画光晕。
// 光晕强度随 k 走（0.35+0.65k）：眉月的光晕比满月弱。
// ---- 月亮独立层（全分辨率，画在云之前）----

type moonLayerKey struct {
	w, h int
	moon int
}

type moonLayerEntry struct {
	img  *image.NRGBA
	ops  paint.ImageOp
	off  image.Point // 子矩形在整屏里的左上角
	size image.Point
}

var (
	moonLayerCache struct {
		key moonLayerKey
		ent *moonLayerEntry
	}
	moonLayerStats struct{ Builds, Hits int }
)

// moonLayerGet 以**整屏分辨率**把月亮烘进它的外接矩形（含光晕），缓存一份。
// 尺寸 = 2×maxR = 2×rc×(1+haloSpread) = 0.384w（1080 屏 ≈ 415 px）
// ⇒ 内存 ≈ 415×415×4 = 0.7 MB，远小于把整屏烘成全分辨率的 8.3 MB。
func moonLayerGet(w, h int, phase *float64) *moonLayerEntry {
	k := moonLayerKey{w: w, h: h, moon: moonPhaseKey(phase)}
	if e := moonLayerCache.ent; e != nil && moonLayerCache.key == k {
		moonLayerStats.Hits++
		return e
	}
	rc := moonRcFrac * float64(w)
	maxR := int(math.Ceil(rc*(1.0+moonHaloSpread))) + 1
	cx, cy := int(0.72*float64(w)), int(0.095*float64(h))
	ox, oy := cx-maxR, cy-maxR
	bw, bh := 2*maxR, 2*maxR
	// 夹进屏内
	if ox < 0 {
		bw += ox
		ox = 0
	}
	if oy < 0 {
		bh += oy
		oy = 0
	}
	if bw <= 0 || bh <= 0 {
		return nil
	}
	if ox+bw > w {
		bw = w - ox
	}
	if oy+bh > h {
		bh = h - oy
	}
	if bw <= 0 || bh <= 0 {
		return nil
	}
	dst := image.NewNRGBA(image.Rect(0, 0, bw, bh))
	bakeMoonAt(dst, bw, bh, ox, oy, w, h, phase)
	e := &moonLayerEntry{img: dst, ops: paint.NewImageOp(dst), off: image.Point{X: ox, Y: oy}, size: image.Point{X: bw, Y: bh}}
	moonLayerCache.key = k
	moonLayerCache.ent = e
	moonLayerStats.Builds++
	return e
}

// moonBlit 把月亮层贴到绝对屏幕位置：绘制顺序 天空 → **月亮** → 云/粒子 → 面板 → 文字。
// 云在粒子层后画 ⇒ 云真正飘在月亮上面（多云时云飘在月亮上面）。
// 填充率成本 = 415×415 / (1080×1920) = 0.166 整屏 ≈ 0.7% CPU @15 fps。
func moonBlit(gtx layout.Context, size image.Point, isDay bool, phase *float64) {
	if isDay || size.X <= 0 || size.Y <= 0 {
		return
	}
	e := moonLayerGet(size.X, size.Y, phase)
	if e == nil || e.size.X <= 0 || e.size.Y <= 0 {
		return
	}
	cl := clip.Rect(image.Rect(e.off.X, e.off.Y, e.off.X+e.size.X, e.off.Y+e.size.Y)).Push(gtx.Ops)
	// **变换必须带平移**：图像画在"当前变换原点"上，clip.Rect 只限制绘制区域、
	// 不移动图像。第一版用恒等变换 ⇒ 月亮被贴到窗口左上角，与裁剪框 (568,40)
	// 不相交 ⇒ 整层一个像素都没画出来（离屏探针 TestMoonBlitProbe 实测月核区亮像素 = 0）。
	// 1:1 不缩放 ⇒ 只平移 (off.X, off.Y)。skyBlit 用恒等+缩放是因为它铺满整屏、原点就是 (0,0)。
	tr := op.Affine(f32.NewAffine2D(1, 0, float32(e.off.X), 0, 1, float32(e.off.Y))).Push(gtx.Ops)
	e.ops.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
}

// 月亮几何（bakeMoonAt 与 moonLayerGet 共用，两处必须一致）
const (
	moonRcFrac     = 0.060 // 月核半径 = 0.060 × 屏宽（需求：月亮更大 → 0.045→0.060）
	moonHaloSpread = 2.2   // 光晕外沿 = rc×(1+2.2) = 3.2rc
)

// glowTab 指数衰减查表：dd ∈ [0, haloSpread（归一化半径）→ exp(−haloDecay·dd)。
// 辉光叠加的内层循环用它，避免每源一次 math.Exp（12 万像素 × 200 源）。
var glowTab = func() [glowTabN]float64 {
	var t [glowTabN]float64
	for i := 0; i < glowTabN; i++ {
		dd := float64(i) * 2.2 / glowTabN // haloSpread
		t[i] = math.Exp(-2.6 * dd)        // haloDecay
	}
	return t
}()

const glowTabN = 256

// bakeMoon 整屏口径的月亮烘焙（dst 覆盖整屏时使用）。
func bakeMoon(dst *image.NRGBA, w, h int, phase *float64) {
	bakeMoonAt(dst, w, h, 0, 0, w, h, phase)
}

// bakeMoonAt 把月亮画进 dst 的一块**子矩形**：dst 尺寸 boxW×boxH，
// 其在整屏里的左上角 = (ox, oy)，月心/半径按**整屏** w×h 计算。
// 用途 = 月亮独立层（全分辨率，画在云之前）：
// 天空纹理是半分辨率烘焙（skyCacheScale=2），月亮烘在里面只有 32 px 半径，
// 放大 2 倍后外缘出现整像素台阶（低性能 x86 小主机离屏图 shot66 量到相邻行最大跳变 7 px）；
// 且云盖在半分辨率月亮上只有 0.28–0.45 不透明度，压不住 248 级的月面
// ⇒ 月亮读成在云层上面。
// 拆成独立层后：全分辨率 ⇒ 边缘锐利；绘制顺序 天空→月亮→云 ⇒ 云飘在月亮上面。
func bakeMoonAt(dst *image.NRGBA, boxW, boxH, ox, oy int, w, h int, phase *float64) {
	p := 0.5 // 取不到月相 ⇒ 满月（夜里总该有个月亮）
	if phase != nil {
		p = *phase - math.Floor(*phase)
		if p < 0 {
			p += 1
		}
	}
	theta := 2.0 * math.Pi * p
	cosT := math.Cos(theta)
	k := (1.0 - cosT) / 2.0
	if k < 0.02 {
		return // 新月
	}
	// 月心按整屏算，再减去子矩形左上角 ⇒ 子矩形内的坐标
	cx, cy := 0.72*float64(w)-float64(ox), 0.095*float64(h)-float64(oy)
	rc := moonRcFrac * float64(w)
	a := math.Abs(cosT)
	waxing := p < 0.5

	core := color.NRGBA{R: 246, G: 244, B: 230, A: 0xff}
	halo := color.NRGBA{R: 214, G: 226, B: 238, A: 0xff}
	const (
		haloA0      = 95.0 // 光晕峰值 alpha（原 120 偏脏，按 95 收一档）
		haloDecay   = 2.6
		haloSpread  = moonHaloSpread // 光晕外沿 = rc×(1+2.2) = 3.2rc
		haloWin     = 0.7            // cos² 窗起点（先前口径：边界值 0、边界斜率≈0）
		halfFeather = 0.006          // 半侧边界羽化（归一化）：rc=65 px 时 0.4 px，亚像素防锯齿
		termFeather = 0.012          // 晨昏线（椭圆）羽化
	)
	glowMul := 0.35 + 0.65*k

	// litAt 归一化坐标 (X,Y) 处的亮面占比（月相几何，见函数头注释）。
	litAt := func(X, Y float64) float64 {
		sX := X
		if !waxing {
			sX = -X
		}
		half := smoothstep(-halfFeather, halfFeather, sX)
		if a < 1e-6 {
			return half
		}
		e := (sX*sX)/(a*a) + Y*Y
		// 注意：ui/effects.go:352 的 smoothstep 在 b<=a 时是**硬台阶**（x<a 返 0 否则 1），
		// 不是递减坡。第一版这里写的是 smoothstep(1+ε, 1−ε, e) ⇒ inE 恒 0
		// ⇒ 所有相位退化成"半月亮"（满月 0.5 也只剩右半，低性能 x86 小主机出图抓到）。
		// 递减必须写成"递增坡取反"。
		inE := 1.0 - smoothstep(1.0-termFeather, 1.0+termFeather, e)
		if cosT > 0 {
			return half * (1.0 - inE)
		}
		return half + (1.0-half)*inE
	}

	// 亮面点源网格（归一化坐标，16×16，步长 = 1/8 半径）：辉光的来源 = **当前可见的亮面**。
	type gsrc struct{ x, y, w float64 }
	const gN = 16
	srcs := make([]gsrc, 0, gN*gN)
	full := make([]gsrc, 0, gN*gN)
	for i := 0; i < gN; i++ {
		for j := 0; j < gN; j++ {
			Xc := -1.0 + 2.0*(float64(i)+0.5)/gN
			Yc := -1.0 + 2.0*(float64(j)+0.5)/gN
			rr := Xc*Xc + Yc*Yc
			if rr > 1.0 {
				continue
			}
			full = append(full, gsrc{Xc, Yc, 1.0})
			l := litAt(Xc, Yc)
			if l <= 0.01 {
				continue
			}
			facing := math.Sqrt(math.Max(0, 1.0-rr))
			srcs = append(srcs, gsrc{Xc, Yc, l * smoothstep(0.015, 0.10, facing) * (1.0 - 0.12*rr)})
		}
	}
	// 归一化：满月（整盘全亮）在月缘处的辉光和 = 1 ⇒ 各相位相对满月按比例走
	peak := 0.0
	for a := 0; a < 8; a++ {
		th := math.Pi * float64(a) / 4.0
		ux, uy := math.Cos(th)*1.05, math.Sin(th)*1.05
		sum := 0.0
		for _, s := range full {
			dx, dy := ux-s.x, uy-s.y
			dd := math.Sqrt(dx*dx + dy*dy)
			if dd >= haloSpread {
				continue
			}
			sum += glowTab[int(dd*glowTabN/haloSpread)]
		}
		if sum > peak {
			peak = sum
		}
	}
	glowNorm := 1.0
	if peak > 0 {
		glowNorm = 1.0 / peak
	}

	maxR := rc * (1.0 + haloSpread)
	// **必须钳到 0**：外扩 -1 在月亮贴顶/贴左时把起点变成 -1，
	// `dst.PixOffset(x, -1)` 直接负索引 panic
	// （有一版一启动就崩：窗口 810x1389、月亮在 0.095h、
	// oy 钳位后 cy-maxR ≤ 0 ⇒ `index out of range [-704]`，
	// 栈 bakeMoonAt render.go:1399 ← moonLayerGet ← moonBlit。
	x0 := int(math.Max(0, cx-maxR)) - 1
	y0 := int(math.Max(0, cy-maxR)) - 1
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	x1 := int(math.Min(float64(boxW), cx+maxR)) + 1
	y1 := int(math.Min(float64(boxH), cy+maxR)) + 1
	if x1 > boxW {
		x1 = boxW
	}
	if y1 > boxH {
		y1 = boxH
	}

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			X := (float64(x) - cx) / rc
			Y := (float64(y) - cy) / rc
			d := math.Sqrt(X*X + Y*Y)
			// 辉光场：对 spread 内的每个亮面源求 Σ w·exp(−decay·dist/rc)。
			// **盘内也铺**
			// 需求：用光晕把暗面补上 ⇒ 暗面被辉光填上，
			// 不再是被光晕围出来的发光的坑。亮面画在辉光之上。
			acc := 0.0
			for _, s := range srcs {
				dx, dy := X-s.x, Y-s.y
				dd := math.Sqrt(dx*dx + dy*dy)
				if dd >= haloSpread {
					continue
				}
				acc += s.w * glowTab[int(dd*glowTabN/haloSpread)]
			}
			g := haloA0 * acc * glowNorm * glowMul
			if d > 1.0 {
				// 外沿 cos² 窗归一到 0，消除硬边界
				frac := (d - 1.0) / haloSpread
				if frac >= 1.0 {
					continue
				}
				if frac > haloWin {
					u := (frac - haloWin) / (1.0 - haloWin)
					g *= math.Cos(u*math.Pi/2) * math.Cos(u*math.Pi/2)
				}
			}
			ag := 0.0
			if g > 0.4 {
				ag = g / 255.0
				if ag > 1 {
					ag = 1
				}
			}
			ac := 0.0
			if d <= 1.0 {
				// 月面：只画亮面，画在辉光之上
				facing := math.Sqrt(math.Max(0, 1.0-d*d))
				limb := smoothstep(0.015, 0.10, facing) // 上游口径：锐边
				shade := 1.0 - 0.12*(d*d)               // 临边昏暗 12%（实测上游素材降幅）
				ac = limb * litAt(X, Y) * shade
				if ac > 1 {
					ac = 1
				}
				if ac < 0.004 {
					ac = 0
				}
			}
			// **必须直接写非预乘 NRGBA**：月亮层是透明底，blendPix 走预乘混合后
			// 把预乘 RGB 当非预乘存进 NRGBA ⇒ α 小的像素颜色被存成近黑
			// ⇒ 整盘显成黑椭圆（离屏放大图实测）。烘进不透明天空时该错被底遮住，
			// 拆成独立透明层后暴露。
			a := ag + ac*(1.0-ag)
			if a <= 0.002 {
				continue
			}
			wg, wc := ag, ac*(1.0-ag)
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = clamp8((wg*float64(halo.R) + wc*float64(core.R)) / a)
			dst.Pix[i+1] = clamp8((wg*float64(halo.G) + wc*float64(core.G)) / a)
			dst.Pix[i+2] = clamp8((wg*float64(halo.B) + wc*float64(core.B)) / a)
			dst.Pix[i+3] = uint8(a * 255.0)
		}
	}
}

// bakeStars 把星星烘进天空纹理
// "星星只是在云层后面不是多云就没星星了，月亮也一样，多云只是云飘在月亮上面"。
//
// 烘进纹理 ⇒ ① 每帧零成本（星星原本是 Clear 夜间的 110 个粒子，别的类别一个没有）
// ② 天然在云的**下层**（云在粒子层绘制，后画在上）。
// 确定性种子：同一尺寸/类别烘出的星位逐帧一致，不闪跳；闪烁仍由 Clear 的粒子星承担。
func bakeStars(dst *image.NRGBA, w, h int) {
	const n = 150
	rng := rand.New(rand.NewSource(20261016))
	for i := 0; i < n; i++ {
		x := int(rng.Float64() * float64(w))
		y := int(rng.Float64() * float64(h) * 0.92)
		b := 0.18 + 0.62*rng.Float64()*rng.Float64()
		sz := 1
		if rng.Float64() < 0.12 {
			sz = 2
		}
		c := color.NRGBA{R: 244, G: 246, B: 252, A: 0xff}
		for dy := 0; dy < sz; dy++ {
			for dx := 0; dx < sz; dx++ {
				px, py := x+dx, y+dy
				if px >= w || py >= h {
					continue
				}
				blendPix(dst, px, py, c, b)
			}
		}
	}
}

// bakeMoonCrisp 月亮：硬边圆盘 + 临边昏暗，**无外发光、无月海纹理**。
//
// 参照 = 上游 GL 着色器 upstream/rendering/gl_programs/neon_celestial.py:309-311：
//
//	float facing = max(dot(normal, -ray), 0.0);
//	float limb   = smoothstep(0.015, 0.10, facing);
//	...
//	return vec4(moon, clamp(uMoonColor.a,0,1) * limb);   // 球外直接 vec4(0.0)
//
// 两条实测结论（2026-10-16）：
// 1. **边缘**：球体投影 facing = √(1−(r/R)²)，代入上游 smoothstep 参数 ⇒ 
// facing=0.10 → r/R=0.99499，facing=0.015 → r/R=0.99989
// ⇒ 过渡带只占**半径的 0.49%**（R=48.6 px 时 0.24 px，即亚像素抗锯齿）。
// 直接量上游素材 ui/assets/weather/source/moon-full.png 也印证：
// 沿水平剖面 r=68 处 α=255、r=72 处 α=0，**0..255 之间的过渡带 = 0 px**。
// 原先的 `feather = 0.10`（半径的 10%）**比上游宽 20 倍** ⇒ 就是"糊"。
// 2. **无外发光**：上游月亮的 alpha 只由 limb 决定，球外返回 vec4(0.0)，
// 素材里也没有任何光晕像素。原先挂着 haloA0=120 / spread=3.4 的光晕，
// 铺到 4.4 倍核半径 ⇒ 第二处"糊"。
//
// 临边昏暗照素材实测：填充色中心 (130,193,218) → 外缘 (114,185,213)，
// 即中心亮、外缘暗，降幅 12%（着色器里对应 earthshine/basinMoonSurfaceBrightness
// 那组项的净效果，这里按实测值做线性近似，不复制着色器的月海反照率。
//
// 这是 bakeMoon 在 phase==nil（取不到月相）时的回落画法，逐字不变
// （需求 3：作为回落）。
func bakeMoonCrisp(dst *image.NRGBA, w, h int) {
	cx, cy := 0.72*float64(w), 0.095*float64(h)
	rc := 0.045 * float64(w)
	col := color.NRGBA{R: 236, G: 236, B: 224, A: 0xff}
	core := 248.0 // 上游素材 α=255 全不透明；留 7 级余量避免与天空硬切

	maxR := rc * 1.02
	x0 := int(math.Max(0, cx-maxR))
	x1 := int(math.Min(float64(w), cx+maxR))
	y0 := int(math.Max(0, cy-maxR))
	y1 := int(math.Min(float64(h), cy+maxR))

	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			d := math.Hypot(dx, dy)
			rr := d / rc
			if rr > 1.02 {
				continue
			}
			facing := 0.0
			if rr < 1.0 {
				facing = math.Sqrt(1.0 - rr*rr)
			}
			// 上游逐字参数
			limb := smoothstep(0.015, 0.10, facing)
			if limb <= 0 {
				continue
			}
			// 临边昏暗：中心亮、外缘暗，降 12%（照素材实测）
			shade := 1.0 - 0.12*rr*rr
			c := color.NRGBA{
				R: clamp8(float64(col.R) * shade),
				G: clamp8(float64(col.G) * shade),
				B: clamp8(float64(col.B) * shade),
				A: 0xff,
			}
			blendPix(dst, x, y, c, core*limb/255.0)
		}
	}
}

// blendAlphaV/H：把 (c,a1)→(c,a2) 的线性渐变按 src-over 合成进 dst。
// 与 paint.LinearGradientOp + PaintOp 的 src-over 语义逐像素等价。
func blendAlphaV(dst *image.NRGBA, r image.Rectangle, c color.NRGBA, a1, a2 uint8) {
	n := r.Max.Y - r.Min.Y
	if n <= 0 || r.Max.X <= r.Min.X {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		t := 0.0
		if n > 1 {
			t = float64(y-r.Min.Y) / float64(n-1)
		}
		a := lerpF(float64(a1), float64(a2), t) / 255.0
		for x := r.Min.X; x < r.Max.X; x++ {
			blendPix(dst, x, y, c, a)
		}
	}
}

func blendAlphaH(dst *image.NRGBA, r image.Rectangle, c color.NRGBA, a1, a2 uint8) {
	n := r.Max.X - r.Min.X
	if n <= 0 || r.Max.Y <= r.Min.Y {
		return
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		t := 0.0
		if n > 1 {
			t = float64(x-r.Min.X) / float64(n-1)
		}
		a := lerpF(float64(a1), float64(a2), t) / 255.0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			blendPix(dst, x, y, c, a)
		}
	}
}

func blendPix(dst *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := dst.PixOffset(x, y)
	ia := 1.0 - a
	dst.Pix[i+0] = clamp8(float64(dst.Pix[i+0])*ia + float64(c.R)*a)
	dst.Pix[i+1] = clamp8(float64(dst.Pix[i+1])*ia + float64(c.G)*a)
	dst.Pix[i+2] = clamp8(float64(dst.Pix[i+2])*ia + float64(c.B)*a)
	// 背景恒不透明，alpha 通道保持 0xff
	dst.Pix[i+3] = 0xff
}

func lerpF(a, b, t float64) float64 { return a + (b-a)*t }

// skyBlit 把烘焙好的背景按整窗贴一次：1 个 clip + 1 个 ImageOp + 1 个 PaintOp。
// 取代 skyGradient(1) + skyDepth(18) = 19 个 op。
func skyBlit(gtx layout.Context, size image.Point, cls effect.Class, isDay bool, phase *float64) {
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	ent := skyCached(size, cls, isDay, phase)
	if ent == nil {
		skyGradient(gtx, size, cls, isDay)
		skyDepth(gtx, size, cls, isDay)
		return
	}
	cl := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	sz := ent.ops.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		cl.Pop()
		skyGradient(gtx, size, cls, isDay)
		skyDepth(gtx, size, cls, isDay)
		return
	}
	sx := float32(size.X) / float32(sz.X)
	sy := float32(size.Y) / float32(sz.Y)
	// op.Affine 是累加语义（已验证），必须 Push/Pop 成对。
	//
	// 变换写法实测（headless 逐像素探针 probe_blit）：
	// `f32.NewAffine2D(sx,0,0,0,sy,0)` 直接矩阵 ⇒ 整屏覆盖，(1070,1900) 有像素；
	// `f32.NewAffine2D(1,0,0,0,1,0).Scale(Point{sx,sy}, Point{})` ⇒ **全透明，
	// 什么都没画**。第一版用的就是后者，后果是"背景不画 ⇒ CPU 假降 11 个点"。
	// 故必须用直接矩阵，禁止 Scale(s, p) 形式。
	tr := op.Affine(f32.NewAffine2D(sx, 0, 0, 0, sy, 0)).Push(gtx.Ops)
	ent.ops.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
}

// bakeSunFromSprite 把 Kenney light_01 双线性放大贴成太阳：
// 心 (0.72w, 0.095h)，半径口径 0.11w（比月核 0.045w 大一倍多，做出"光源"分量），
// 暖色着色 (255,238,196)，并在核区叠一层近白热核做"刺眼感"。
// 返回 false = 素材缺失（调用方回退到程序圆盘）。
func bakeSunAnalytic(dst *image.NRGBA, w, h int) bool {
	// 太阳 = **单一连续衰减**，无核心/光晕两层叠加。
	//
	// 低性能 x86 小主机反馈"现在显示的太阳，读图发现也很奇怪，像个圆盘！"；
	// 视觉模型对原生裁切判"它看起来像一个有明确边界的圆盘"。
	// 实测剖面（skydump 原生导出，中心 (0.72w,0.095h)）：
	// [219 217 215 172 165 151 138 128 118 111 106 100 96 91 87 84 68 62]
	// ⇒ 顶部 16 px 平，**r=24 处跌 43**（核心高斯 σ 与光晕指数 L 的交界拐点）
	// ⇒ **r=128 处 84→68**（光晕绘制截止半径 = 外边界）
	// 两个边界都是"圆盘"读感来源。
	//
	// 病根：**两层不同尺度的衰减相加**（核心 σ=0.05w + 光晕 L=0.13w）
	// ⇒ 相加曲线在交界处必然出现拐点 ⇒ 拐点 = 边。
	// 改法：一个函数 A/(1+(r/L)²)（Lorentz 型）——单调、处处光滑、渐近到基线，
	// 没有拐点也没有截止；颜色随同一函数值从中心白插值到外缘暖，
	// 保证"中心更白"这件事不引入第二个尺度。
	cx, cy := 0.72*float64(w), 0.095*float64(h)
	// A/L 按"中心够亮 + 斜率 ≤25 亮度/8px"定：
	// 上一组 A=165,L=0.06w ⇒ 中心 184（偏暗，不像太阳）；
	// 再往前 A=150+96 双层 ⇒ 裁切 + 拐点 = 圆盘。
	A := 200.0
	L := 0.08 * float64(w)

	// 绘制半径 = **1.5 倍屏宽**（提的两个办法合起来用：
	// "直接拉到屏幕外面是不是就消除了，或者逐渐降低到0是不是也可以"：
	// 上一版画到 added<2 就停 ⇒ 截止处有一步亮度台阶（实测 70→67，3 级），
	// 那就是在更大范围看到的那条边界。
	// 做法：半径拉到屏幕外，并乘一个 cos² 窗 S(r/R)=cos²(πr/2R)——
	// 它在 r=R 处**函数值与导数同时为 0** ⇒ 边界处既无台阶也无折角；
	// 且窗只在 added 已 <1 的远尾起作用，不改变可见部分的形状。
	rad := 1.5 * float64(w)
	x0 := int(math.Max(0, cx-rad))
	x1 := int(math.Min(float64(w), cx+rad+1))
	y0 := int(math.Max(0, cy-rad))
	y1 := int(math.Min(float64(h), cy+rad+1))

	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			d := math.Hypot(dx, dy)
			if d > rad {
				continue
			}
			t := (d / L) * (d / L)
			added := A / (1.0 + t)
			// cos² 窗：r=R 处值与导数同时归零 ⇒ 无截止边
			u := d / rad
			if u >= 1.0 {
				continue
			}
			wl := math.Cos(u * math.Pi * 0.5)
			added *= wl * wl
			a := added / 255.0
			if a < 0.004 {
				continue
			}
			// 颜色随同一函数值插值：中心近白 → 外缘暖黄
			k := 1.0 / (1.0 + t) // 1 在中心，→0 在外缘
			col := color.NRGBA{
				R: clamp8(255.0),
				G: clamp8(232.0 + 20.0*k),
				B: clamp8(176.0 + 62.0*k),
				A: 0xff,
			}
			blendPix(dst, x, y, col, a)
		}
	}
	return true
}

// RenderSkyForProbe 导出天空烘焙层（含烘焙天体），供离线判读用。
// 与低性能 x86 小主机同源：走同一个 skyCacheScale 口径，保证判读对象就是上线画面。
// 月相传 nil ⇒ 月亮走 bakeMoonCrisp 回落画法（判读口径与无月相数据时一致）。
func RenderSkyForProbe(w, h int, cls effect.Class, isDay bool) *image.NRGBA {
	return renderSkyCPU(w/skyCacheScale, h/skyCacheScale, cls, isDay, nil)
}

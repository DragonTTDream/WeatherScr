package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
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
	"github.com/DragonTTDream/WeatherScr/ui/sky"
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
	// nil = 取不到 ⇒ **不画月亮**（与新月一致，规格 2.3）。
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

// frameCloudPct 从帧输入里取云量（0–100）。
//
// 数据源 = Sample.CloudCoverPct（weather 包逐时插值好的那个量，内部数据、
// 不进设置页）。Sample 为 nil ⇒ 0：按晴算 = 白天满亮太阳，
// 与"取不到天气时画面照常出"的既有口径一致。
// 越界钳位 [0,100]（sunDimOf 里还有一道钳位，这里先把可见的入口钳住）。
func frameCloudPct(f Frame) int {
	if f.Weather == nil {
		return 0
	}
	v := f.Weather.CloudCoverPct
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Layout 绘制整帧：不透明天空 → 天空纵深 → 粒子特效 → 时钟 rect → 天气 rect。
func (s *Screen) Layout(gtx layout.Context, f Frame) layout.Dimensions {
	size := gtx.Constraints.Max
	skyOn := f.Layers.on(LayerSky)
	depthOn := f.Layers.on(LayerDepth)
	// 云量（太阳可见度的输入）：取 Sample.CloudCoverPct（逐时插值好的量）。
	// 取不到天气 = 0 ⇒ 按晴算（白天满亮太阳），与"没有天气数据时照常画"的既有口径一致。
	cloudPct := frameCloudPct(f)
	if skyOn && depthOn {
		// 天空 + 暗角一起：烘焙成一张纹理，每帧 1 次贴图
		sky.SkyBlit(gtx, size, f.Class, f.IsDay, f.MoonPhase, cloudPct)
	} else if skyOn {
		sky.SkyGradient(gtx, size, f.Class, f.IsDay)
	} else if depthOn {
		sky.SkyDepth(gtx, size, f.Class, f.IsDay) // 地平线辉光带 + 四角暗角
	}
	// 月亮独立层：画在天空之后、云/粒子之前
	// ⇒ 云真正飘在月亮上面（需求：多云时云飘在月亮上面，
	// 不是压在云层下面。全分辨率 ⇒ 边缘无整像素台阶。
	if f.Layers.on(LayerSky) {
		sky.MoonBlit(gtx, size, f.IsDay, f.MoonPhase)
	}
	// 类别切换期间先画旧场（淡出中），再画新场（淡入中）
	// ⇒ 云不会在切换那一帧整片凭空出现/消失。
	//
	// z 序口径（缺陷 L1，spec/b4-landscape.md:19「先云带后信息层」）：
	// 云带/粒子场（Field.Draw）恒在时钟、天气文字**之前**提交 ⇒ gioui 立即模式下
	// 文字层永远画在云之上，云不得盖住日期/描述行。本批只确认次序，不改带数学。
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
		// TODO(未取证)：提醒行仍走竖屏 rect 口径 —— B4 spec 只点名面板/数据行/时钟
		// （spec/b4-landscape.md 映射表 L1–L5），横屏下的提醒行版式未取证 ⇒ 本批不动。
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

// clockBlock 时钟块：时间行 + 日期行。
//
// 文字部分走 ui/textcache.go 的绘制缓存：键 = 两行文字 + 落点/度量/字色/阴影，
// 键不变 ⇒ 不重排、不重 shape，直接重放录好的绘制宏。
// 时钟的可见输入每秒只变一次（TimeLine 带秒），15 fps 下约 15 帧里
// 有 14 帧是重放。面板底 fillPanel 仍在块外逐帧画（1–2 个 op，不在缓存范围）。
// clockLineWidthCache 横屏时钟行宽的单条记忆（键 = 文本|字号）。
// 时钟行的可见输入每秒才变一次（TimeLine 带秒）⇒ 单条记忆足够，15 fps 下约 15 帧里
// 只有 1 帧真正测量一次（其余走记忆），不引入新的每帧排版成本。
type clockLineWidthCache struct {
	key string
	w   int
}

var clockLineWidth clockLineWidthCache

// measureClockLineWidth 用现口径字号实测一行文字的实画宽（px）。
func (s *Screen) measureClockLineWidth(gtx layout.Context, txt string, sp int) int {
	key := txt + "|" + strconv.Itoa(sp)
	if clockLineWidth.key == key {
		return clockLineWidth.w
	}
	d := measureText(gtx, s.label(txt, unit.Sp(sp), color.NRGBA{}, text.Middle))
	clockLineWidth.key, clockLineWidth.w = key, d.Size.X
	return d.Size.X
}

// clockLineSp 时钟两行的字号（px）。
//
// 竖屏（w ≤ h）逐字返回现口径：时间行 ClockFontSizeShown=174、日期行 WeatherFontSize=40
// （ui/layout.go:33-34，竖屏版式测试钉死，不许变）。
// 横屏（w > h）走 L5 封顶：字号 = min(现口径, 由 0.30W 反解的字号) ⇒ 行宽 ≤ 0.30W
// ⇒ 1920 横屏时钟宽 ≤ 576 px（判据见 ui/landscape_layout_test.go）。
func (s *Screen) clockLineSp(gtx layout.Context, size image.Point, timeStr, dateStr string) (int, int) {
	if !IsLandscape(size.X, size.Y) {
		return ClockFontSizeShown, WeatherFontSize
	}
	return landscapeClockFontCapPx(ClockFontSizeShown, s.measureClockLineWidth(gtx, timeStr, ClockFontSizeShown), size.X),
		landscapeClockFontCapPx(WeatherFontSize, s.measureClockLineWidth(gtx, dateStr, WeatherFontSize), size.X)
}

func (s *Screen) clockBlock(gtx layout.Context, size image.Point, f Frame) {
	timeStr := f.Clock.TimeLine(f.Now)
	dateStr := f.Clock.DateLine(f.Now)

	px := shiftY(ClockRect.Px(size.X, size.Y), f.ClockYOff)
	// 横屏时钟字号封顶（缺陷 L5，spec/b4-landscape.md:20）：竖屏逐字 = 现口径 174/40。
	timeSp, dateSp := s.clockLineSp(gtx, size, timeStr, dateStr)
	// 面板底画在文字之前、块定位之后。
	// 时钟块用 clock_bg_opacity（默认 0.45，强于天气）—— 偏离上游
	// 'clock' 块 show_background: False，理由见 PanelStyle.ClockBGOpacity
	//
	fillPanel(gtx, panelRect(px, f.Panel.Margin), f.Panel, f.Panel.ClockBGOpacity)

	content := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis:      layout.Vertical,
			Alignment: layout.Middle,
			Spacing:   layout.SpaceEvenly,
		}.Layout(gtx,
			// 时间行、日期行都走阴影；偏移随字号走
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(timeSp), s.rowText(timeStr, unit.Sp(timeSp), nil), f.Layers)),
			layout.Rigid(s.shadowed(f.Panel, unit.Sp(dateSp), s.rowText(dateStr, unit.Sp(dateSp), nil), f.Layers)),
		)
	}
	key := textCacheKeyBase(gtx, px, f)
	key.timeStr, key.dateStr = timeStr, dateStr
	if ent := clockTextCache.get(key, gtx, px, content); ent != nil {
		inPxReplay(gtx, px, ent)
		return
	}
	inPx(gtx, px, content)
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
// 状况图标：上游配置是 icon_size 192、show_condition_icon True、
// icon_alignment 'RIGHT'（上游 settings_v2.json），块内布局 =
// [文字列][12 px][192×192 图标]（上游 WeatherPresentation.qml:328-338：
// anchors.right + anchors.verticalCenter）。本实现按布局判读撤掉了该图标，
// 块内只有纯文字列，宽度 = 块宽（不预留图标位）。
// 对应的状况图标素材与绘制路径已删除，配置键 show_condition_icon 仍照原样接受。
// 文字列（地名/条件/两条分隔线/详情行/预报，含详情行的图标位图）走
// ui/textcache.go 的绘制缓存：键 = 地名/条件/温度/预报/详情三项文本 +
// 落点/度量/字色/阴影。天气数据默认一分钟刷新一次 ⇒ 稳态下每帧都是重放。
// 面板底 fillPanel 仍在块外逐帧画，不在缓存范围。
// weatherPxRect 天气面板矩形（双朝向口径，缺陷 L2/L3）。
//
// 竖屏（w ≤ h）：逐字 = WeatherRect.Px + 布局偏移（现有竖屏版式测试钉死 29/1019/1021/558 @1080×1920）。
// 横屏（w > h）：landscapePanelPx —— 顶沿 = 云带下沿 + 间隙（云带下沿从 Field 的带区间
// 接口 BandRegionPx 取实际值），左右边距 = 0.0194W（= 37 px @1920），
// 三列间距 = 面板宽/3 ⇒ 与 W 同分制（rowDetails 的 cellW = rowW/3 直接跟着变）。
func (s *Screen) weatherPxRect(size image.Point, f Frame) PxRect {
	if !IsLandscape(size.X, size.Y) {
		return shiftY(WeatherRect.Px(size.X, size.Y), f.WeatherYOff)
	}
	bandBottom := 0
	if f.Field != nil {
		_, bandBottom = f.Field.BandRegionPx(size.X, size.Y)
	}
	px := landscapePanelPx(size.X, size.Y, bandBottom)
	px.Y += f.WeatherYOff
	return px
}

func (s *Screen) weatherBlock(gtx layout.Context, size image.Point, f Frame) {
	w := f.Weather
	if w == nil {
		return
	}
	temp := "--"
	if w.Temperature != nil {
		temp = formatTemp(*w.Temperature)
	}

	px := s.weatherPxRect(size, f)
	// 面板底画在文字之前、块定位之后。
	// 天气块沿用上游 bg_opacity 0.3。
	fillPanel(gtx, panelRect(px, f.Panel.Margin), f.Panel, f.Panel.BGOpacity)

	col := s.weatherTextColumn(f, temp)
	key := textCacheKeyBase(gtx, px, f)
	key.location, key.condition, key.temp = f.Location, f.Weather.Condition, temp
	key.forecast = forecastOf(f.Weather)
	for i, it := range detailItems(w) {
		key.details[i] = it.text
	}
	if ent := weatherTextCache.get(key, gtx, px, col); ent != nil {
		inPxReplay(gtx, px, ent)
		return
	}
	inPx(gtx, px, col)
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

// ── R5 卡 2（ui/sky 拆包）留在这条链路上的三件小东西 ──────────────
//
// 1) blendPix 的 ui 侧副本：函数本体迁进 ui/sky（烘焙层是它的主消费者），
//    ui/assetdir.go 的 blitCoverOver 还在用它 ⇒ 这里留一份逐字副本，
//    ui/assetdir.go 一个字没动（卡 2 不点名该文件）。
// 2) skyAssets：把 ui 的贴图目录/解码缓存接成 sky.Assets 查询口（逐字委托）。
// 3) SkyCacheGet：cmd/scr 的 SCR_SKYCACHE 日志出口签名不变，内部改道 ui/sky。

// blendPix 把颜色 c 以 alpha=a 合成进 dst 的 (x,y)（逐字副本，公式未改）。
func blendPix(dst *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := dst.PixOffset(x, y)
	ia := 1.0 - a
	dst.Pix[i+0] = clamp8(float64(dst.Pix[i+0])*ia + float64(c.R)*a)
	dst.Pix[i+1] = clamp8(float64(dst.Pix[i+1])*ia + float64(c.G)*a)
	dst.Pix[i+2] = clamp8(float64(dst.Pix[i+2])*ia + float64(c.B)*a)
	// 背景恒不透明，alpha 通道保持 0xff
	dst.Pix[i+3] = 0xff
}

// skyAssets 是 sky.Assets 的 ui 侧实现：每个方法逐字委托 ui 现有函数，不加判断、不加缓存。
type skyAssets struct{}

func (skyAssets) Available(name string) bool { return assetAvailable(name) }

func (skyAssets) ImageAny(name string) (*image.NRGBA, paint.ImageOp, image.Point, bool) {
	return assetImageAny(name)
}

func (skyAssets) SourceTag(name string) string { return assetSourceTag(name) }

func (skyAssets) Sprite(name string) *image.NRGBA { return spriteImg(name) }

func (skyAssets) SampleBilinear(src *image.NRGBA, fx, fy float64) color.NRGBA {
	return sampleBilinear(src, fx, fy)
}

func (skyAssets) BlitCoverReplace(dst, src *image.NRGBA) { blitCoverReplace(dst, src) }

func (skyAssets) BlitCoverOver(dst, src *image.NRGBA, r image.Rectangle) {
	blitCoverOver(dst, src, r)
}

func init() { sky.SetAssets(skyAssets{}) }

// SkyCacheGet 读出统计快照（cmd/scr 的 SCR_SKYCACHE 出口；口径不变）。
func SkyCacheGet() sky.SkyCacheStats { return sky.SkyCacheGet() }

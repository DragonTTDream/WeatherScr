package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/ui/panel"

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
	// inkSep 转发色：本体 = panel.InkSep（迁到 ui/panel/style.go，叠加口径注释随本体迁出）。
	inkSep = panel.InkSep
)

// separatorLenFrac 转发常量（本体 = panel.SeparatorLenFrac，注释随本体迁 ui/panel/style.go）。
const separatorLenFrac = panel.SeparatorLenFrac

// borderAlphaScale 转发常量（本体 = panel.BorderAlphaScale）。
const borderAlphaScale = panel.BorderAlphaScale

// shadowInk 转发色（本体 = panel.ShadowInk，逐字照抄上游 ShadowedText.qml 的 shadowColor）。
var shadowInk = panel.ShadowInk

// shadowOffsetPx 转发常量（本体 = panel.ShadowOffsetPx，上游 shadowOffsetY 的下限语义随本体迁出）。
const shadowOffsetPx = panel.ShadowOffsetPx

// shadowOffsetForPx 转发到 panel.ShadowOffsetForPx（纯迁移：本体见 ui/panel/style.go，
// 公式 max(2, round(字号px × 0.03)) 与字号 px 口径逐字不变）。
func shadowOffsetForPx(px int) int { return panel.ShadowOffsetForPx(px) }

// panelRadiusMaxPx 转发常量（本体 = panel.PanelRadiusMaxPx）。
const panelRadiusMaxPx = panel.PanelRadiusMaxPx

// panelRadiusPx 转发到 panel.PanelRadiusPx（= min(18, 面板高/6)，逐字同式）。
func panelRadiusPx(panelH int) int { return panel.PanelRadiusPx(panelH) }

// PanelStyle = panel.Style 的兼容别名（面板底样式本体迁 ui/panel/style.go，键名键序逐字不变）。
// ⇒ 调用点 ui/settings_ui.go、cmd/scr/main.go 一字不改。
type PanelStyle = panel.Style

// DefaultPanelStyle 转发到 panel.DefaultStyle（上游 default_settings.py 缺省值 + 本复刻的有记录偏离）。
func DefaultPanelStyle() PanelStyle { return panel.DefaultStyle() }

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
	// nil = 取不到 ⇒ **不画月亮**（与新月一致）。
	// 非 nil ⇒ 按 8 等分相位表选月相素材烘进天空纹理（new moon 不画月亮）。
	MoonPhase *float64
	// Layers 图层掩码（性能定位用）：0 = 全开。
	// 低性能 x86 实机稳态 CPU 15–17%（预算 ≤5%），`DBG=frame layout=0–1ms frame=8–12ms`
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
	// z 序口径（缺陷 L1，:19'先云带后信息层'）：
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
		// （映射表 L1–L5），横屏下的提醒行版式未取证 ⇒ 本批不动。
		wpx := shiftY(WeatherRect.Px(size.X, size.Y), f.WeatherYOff)
		DrawAlertPanelRow(gtx, size, f.Alert, s, panelRect(wpx, f.Panel.Margin).Max.Y)
	}

	return layout.Dimensions{Size: size}
}

// mulAlpha 转发到 panel.MulAlpha（alpha = 色 alpha × opacity，钳位 [0,1] 逐字同式）。
func mulAlpha(c color.NRGBA, k float64) color.NRGBA { return panel.MulAlpha(c, k) }

// fillPanel 转发到 panel.FillPanel（面板底 + 描边的绘制顺序与 alpha 口径逐字不变）。
func fillPanel(gtx layout.Context, r image.Rectangle, st PanelStyle, bgOpacity float64) {
	panel.FillPanel(gtx, r, st, bgOpacity)
}

// shadowed 转发到 panel.Shadowed（阴影副本 + 主字的两遍绘制逐字不变）。
// 图层掩码是 ui 帧调度侧的量（LayerMask 留在本文件）⇒ 这里把'要不要阴影'算成布尔传给 panel，
// 旧实现的判据 st.Shadow && layers.on(LayerShadow) 逐字保持。
func (s *Screen) shadowed(st PanelStyle, sp unit.Sp, row rowFn, layers LayerMask) layout.Widget {
	return panel.Shadowed(st, sp, row, layers.on(LayerShadow))
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

// label 转发到 panel.Label（字号/颜色/对齐口径逐字不变，主题取屏保侧 s.Theme）。
func (s *Screen) label(txt string, sp unit.Sp, col color.NRGBA, align text.Alignment) layout.Widget {
	return panel.Label(s.Theme, txt, sp, col, align)
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

// rowFn = panel.RowFn 的兼容别名（'按给定颜色画一行文字'工厂）。
type rowFn = panel.RowFn

// rowText 转发到 panel.RowText（单行文字 + 字形修饰，逐字不变）。
func (s *Screen) rowText(txt string, sp unit.Sp, mod func(*material.LabelStyle)) rowFn {
	return panel.RowText(s.Theme, txt, sp, mod)
}

// rowCondition 转发到 panel.RowCondition（温度 0.81× +' - '+ 条件，逐字不变）。
// 条件行字号 = ui 侧的 WeatherFontSize 档（layout.go 的上游实况常量， 不点名 ⇒ 留在这里传参）。
func (s *Screen) rowCondition(temp, cond string) rowFn {
	return panel.RowCondition(s.Theme, temp, cond, unit.Sp(WeatherFontSize))
}

// bold / italic 转发到 panel.Bold / panel.Italic（字形修饰逐字不变）。
func bold(l *material.LabelStyle)   { panel.Bold(l) }
func italic(l *material.LabelStyle) { panel.Italic(l) }

// 时钟行宽/字号档的本体在 ui/panel/clocklayout.go（纯迁移）：
// 单条记忆 clockLineWidthCache、量宽与横屏 0.30W 封顶的公式都在那边，逐字不变。
// 这里只留薄壳：量宽口径 measureText 与横屏封顶函数 landscapeClockFontCapPx 住在
// ui/alert.go、ui/layout.go（不点名 ⇒ 以参数注入，不复制公式）。

// measureClockLineWidth 用现口径字号实测一行文字的实画宽（px）。
func (s *Screen) measureClockLineWidth(gtx layout.Context, txt string, sp int) int {
	return panel.MeasureClockLineWidth(gtx, txt, sp, s.Theme, measureText)
}

// clockLineSp 时钟两行的字号（px）：竖屏逐字现口径，横屏走 L5 封顶。
func (s *Screen) clockLineSp(gtx layout.Context, size image.Point, timeStr, dateStr string) (int, int) {
	return panel.ClockLineSp(gtx, size, timeStr, dateStr,
		ClockFontSizeShown, WeatherFontSize, s.Theme, measureText, landscapeClockFontCapPx)
}
func (s *Screen) clockBlock(gtx layout.Context, size image.Point, f Frame) {
	timeStr := f.Clock.TimeLine(f.Now)
	dateStr := f.Clock.DateLine(f.Now)

	px := shiftY(ClockRect.Px(size.X, size.Y), f.ClockYOff)
	// 横屏时钟字号封顶（缺陷 L5，:20）：竖屏逐字 = 现口径 174/40。
	timeSp, dateSp := s.clockLineSp(gtx, size, timeStr, dateStr)
	// 面板底画在文字之前、块定位之后。
	// 时钟块用 clock_bg_opacity（默认 0.45，强于天气）—— 偏离上游
	// 'clock' 块 show_background: False，理由见 PanelStyle.ClockBGOpacity
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
//	locationText 粗体，fontSize （原版有地名）
//	conditionText "<temp> - <condition>"，温度以 0.81× 条件字号内联
//	detailsSeparator 1px 分隔线
//	detailsRow 图标化三项 rain/humidity/wind
//	forecastSeparator 1px 分隔线
//	forecastText 斜体，detailFontSize，居中
//
// 字号口径来自 rendering/quick/widgets/weather.py:758-772
// （temperatureFontSize = conditionFontSize * 0.81）。
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

// separator 转发到 panel.Separator（1px 内缩细线，居中落点与 alpha 逐字不变）。
func (s *Screen) separator() layout.Widget { return panel.Separator() }

// drawSprite 转发到 panel.DrawSprite（图标位：绝对矩形贴图的定位/裁剪/矩阵缩放逐字不变）。
func drawSprite(gtx layout.Context, x, y, w, h int, img paint.ImageOp, sz image.Point) {
	panel.DrawSprite(gtx, x, y, w, h, img, sz)
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
// 上游布局原文（WeatherPresentation.qml:378-425）：
//
//	行 width = parent.width；height = max(68.0, detailIconSize+38.0, detailFontSize*3.2)
//	三项各占 detailsRow.width / 3.0
//	格内内容 width = metricIcon.width + 1.0 + metricText.implicitWidth
//	 height = Math.max(icon.height, text.implicitHeight)
//	格内水平垂直居中（anchors.centerIn: parent）
//
// 低性能 x86 实机数字（低性能 x86 实机 settings_v2.json：font_size 40、detail_icon_size 16）
// ⇒ 图标 30 px、文字 20 px、行高 max(68, 68, 64) = 68 px。
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

// ── （ui/sky 拆包）留在这条链路上的三件小东西 ──────────────
// 1) blendPix 的 ui 侧副本：函数本体迁进 ui/sky（烘焙层是它的主消费者），
// ui/assetdir.go 的 blitCoverOver 还在用它 ⇒ 这里留一份逐字副本，
// ui/assetdir.go 一个字没动（不点名该文件）。
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

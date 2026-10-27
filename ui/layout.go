// Package ui 负责竖屏 1080×1920 的布局与绘制。
// 布局基准 = 上游 settings_v2.json 的 custom_layout 归一化 rect（实测取证）。
package ui

import (
	"github.com/DragonTTDream/WeatherScr/ui/geometry"
	"github.com/DragonTTDream/WeatherScr/ui/kinetics"
)

// Rect 是归一化矩形（0–1），与上游 custom_layout 同口径。
type Rect struct {
	X, Y, W, H float64
}

// 上游实况 rect。
var (
	ClockRect   = Rect{X: 0.013, Y: 0.244, W: 0.973, H: 0.210}
	WeatherRect = Rect{X: 0.027, Y: 0.531, W: 0.946, H: 0.291}
)

// Px 把归一化 rect 换算成像素矩形。
// 换算本体 起在 ui/geometry.NormToPx（逐字迁出，ui 侧调用点行为零变化）。
type PxRect struct{ X, Y, W, H int }

func (r Rect) Px(screenW, screenH int) PxRect {
	return PxRect{
		X: geometry.NormToPx(r.X, screenW),
		Y: geometry.NormToPx(r.Y, screenH),
		W: geometry.NormToPx(r.W, screenW),
		H: geometry.NormToPx(r.H, screenH),
	}
}

// 上游实况的时钟与天气呈现参数（实测自 settings_v2.json）。
const (
	ClockFontSize      = 144 // 声明值
	ClockFontSizeShown = 174 // 自定义缩放后的实际值
	ClockFormat24h     = true
	ClockShowSeconds   = true
	ClockShowDate      = true
	ClockShowWeekday   = true
	ClockShowTimezone  = false

	WeatherFontSize     = 40
	WeatherIconSize     = 192
	WeatherShowDetails  = true
	WeatherShowForecast = true
)

// 竖屏目标分辨率（低性能 x86 实机实测 DISPLAY1 Bounds Width=1080 Height=1920）。
// 薄壳：语义源在 ui/geometry 的屏向常量对（其语义源 = ui/screenfit 的 Baseline 口径，逐字 1080/1920）。
var (
	PortraitW = geometry.PortraitW // 1080
	PortraitH = geometry.PortraitH // 1920
)

// FontFamily 上游实况字体。gioui 走系统字体匹配，字体名仅作请求与记录用。
const FontFamily = "Inter"

// ---- B4/ 横屏版式（映射表 L2/L3/L5）----
// 横屏判定 = w > h（34'横屏判定 = w > h（运行时从 Step(dt, w, h) 已知）'）。
// 竖屏口径逐字不变：rect 常量与 Px 的竖屏结果、480/529/21 px @1080
// （38 钉死，ui/layout_test.go / ui/cloudscale_test.go 现有判据不动）。

// IsLandscape 横屏判定。
// 判定本体迁到 ui/geometry.IsLandscape（逐字同式 w > h），ui 侧只留出口薄壳。
func IsLandscape(w, h int) bool { return geometry.IsLandscape(w, h) }

// landscapeEdgeMarginPx 横屏左右边距（缺陷 L3）：与云首尾相连的最小间隙同口径
// 0.0194W（14，kinetics.GapRatio 逐字）
// ⇒ round(0.0194×1920) = 37 px、@1080 = 21 px。
func landscapeEdgeMarginPx(w int) int { return kinetics.GapPx(w) }

// landscapeClockWidthFrac 横屏时钟行实画宽上限占屏宽比（缺陷 L5 判据，
// 20'时钟宽 ≤ 0.30×1920 = 576 px'）。
const landscapeClockWidthFrac = 0.30

// landscapePanelMinH 横屏天气面板在云带避让后的最低高度（px）。
// TODO(未取证)：面板行高/行数在横屏下的上游口径未取证，这里只给"必须留在屏内"的下限。
const landscapePanelMinH = 120

// landscapePanelPx 横屏天气面板矩形（缺陷 L2 + L3）：
//
//	x = 边距 0.0194W、宽 = W − 2×边距（L3：边距按 W 比例，三列间距 = 宽/3 ⇒ 同 W 分制）
//	顶沿 = max(屏高比例 0.531H, 云带下沿 + 间隙)（L2：面板顶沿 = 云带下沿 + 带间隙）
//	高 = min(屏高比例 0.291H, H − 顶沿 − 边距)；连 landscapePanelMinH 都放不下时
//	 贴屏底保留最低高度（面板完整优先，冲突已记）
//
// bandBottomPx = 云带下沿（px），由调用方从 Field 的带区间接口取实际值
// （ui/effects.go 的 BandRegionPx；无云带时传 0 ⇒ 退化为屏高比例口径）。
func landscapePanelPx(w, h, bandBottomPx int) PxRect {
	margin := landscapeEdgeMarginPx(w)
	x := margin
	width := w - 2*margin
	if width <= 0 {
		width = w
		x = 0
	}
	y := int(WeatherRect.Y * float64(h))
	if top := bandBottomPx + kinetics.GapPx(w); top > y {
		y = top
	}
	height := int(WeatherRect.H * float64(h))
	if y+height > h-margin {
		height = h - margin - y
	}
	if height < landscapePanelMinH {
		height = landscapePanelMinH
		y = h - margin - height
	}
	if y < 0 {
		y = 0
	}
	return PxRect{X: x, Y: y, W: width, H: height}
}

// landscapeClockFontCapPx 横屏时钟字号封顶（缺陷 L5）：
// 字号 = min(现口径字号, 由 0.30W 反解出的字号) ⇒ 行宽 ≤ 0.30W。
// 现口径字号 base 下实画宽 w0（> 0.30W 时）⇒ 新字号 = floor(base × 0.30W / w0)。
// 实测依据：时钟占屏宽 40%。
func landscapeClockFontCapPx(base, textWPx, screenW int) int {
	capW := int(landscapeClockWidthFrac * float64(screenW))
	if textWPx <= 0 || textWPx <= capW {
		return base
	}
	size := int(float64(base) * float64(capW) / float64(textWPx))
	if size < 1 {
		size = 1
	}
	return size
}

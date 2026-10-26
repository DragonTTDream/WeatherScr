package panel

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/ui/geometry"
)

// MeasureFn 量一行文字的实画尺寸（口径由调用方注入：ui 侧 = ui/alert.go 的 measureText，
// 一次性 op 缓冲区 + 大有限约束，逐字同旧实现）。
type MeasureFn = func(gtx layout.Context, w layout.Widget) layout.Dimensions

// FontCapFn 横屏时钟字号封顶（旧实现 = ui/layout.go:landscapeClockFontCapPx，
// 住在 layout.go 的 B4/R10 横屏版式段，卡 7 不点名该文件 ⇒ 由调用方注入）。
type FontCapFn = func(base, textWPx, screenW int) int

// clockLineWidthCache 横屏时钟行宽的单条记忆（键 = 文本|字号）。
// 时钟行的可见输入每秒才变一次（TimeLine 带秒）⇒ 单条记忆足够，15 fps 下约 15 帧里
// 只有 1 帧真正测量一次（其余走记忆），不引入新的每帧排版成本。
type clockLineWidthCache struct {
	key string
	w   int
}

var clockLineWidth clockLineWidthCache

// MeasureClockLineWidth 用现口径字号实测一行文字的实画宽（px）。
func MeasureClockLineWidth(gtx layout.Context, txt string, sp int, th *material.Theme, measure MeasureFn) int {
	key := txt + "|" + strconv.Itoa(sp)
	if clockLineWidth.key == key {
		return clockLineWidth.w
	}
	d := measure(gtx, Label(th, txt, unit.Sp(sp), color.NRGBA{}, text.Middle))
	clockLineWidth.key, clockLineWidth.w = key, d.Size.X
	return d.Size.X
}

// ClockLineSp 时钟两行的字号（px）。
//
// 竖屏（w ≤ h）逐字返回现口径：时间行 ClockFontSizeShown=174、日期行 WeatherFontSize=40
// （ui/layout.go:33-34，竖屏版式测试钉死，不许变）。
// 横屏（w > h）走 L5 封顶：字号 = min(现口径, 由 0.30W 反解的字号) ⇒ 行宽 ≤ 0.30W
// ⇒ 1920 横屏时钟宽 ≤ 576 px（判据见 ui/landscape_layout_test.go）。
//
// 屏向判定走 ui/geometry.IsLandscape（R5 卡 5 的 w > h 口径，逐字同旧实现的 ui.IsLandscape 薄壳）。
func ClockLineSp(
	gtx layout.Context, size image.Point, timeStr, dateStr string,
	timeBase, dateBase int, th *material.Theme, measure MeasureFn, capFn FontCapFn,
) (int, int) {
	if !geometry.IsLandscape(size.X, size.Y) {
		return timeBase, dateBase
	}
	return capFn(timeBase, MeasureClockLineWidth(gtx, timeStr, timeBase, th, measure), size.X),
		capFn(dateBase, MeasureClockLineWidth(gtx, dateStr, dateBase, th, measure), size.X)
}

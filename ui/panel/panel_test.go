package panel

// TestPanelParity（钉死件）：期望数字 = **迁移前**在旧实现上跑出来的那一批
// （取数件 ui/zz_pre_panel_test.go 存档 ；迁移后同一件复跑存档
// ⇒ 两份 diff 逐字一致）。本件的期望值全部写死，不许改判据。

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func parityCtx(w, h int) layout.Context {
	var ops op.Ops
	return layout.Context{
		Ops:         &ops,
		Constraints: layout.Constraints{Max: image.Point{X: w, Y: h}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Unix(0, 0).UTC(),
	}
}

func eqPal(t *testing.T, name string, got material.Palette, fg, bg, cbg, cfg color.NRGBA) {
	t.Helper()
	if got.Fg != fg || got.Bg != bg || got.ContrastBg != cbg || got.ContrastFg != cfg {
		t.Errorf("%s = %v/%v/%v/%v，期望 %v/%v/%v/%v", name,
			got.Fg, got.Bg, got.ContrastBg, got.ContrastFg, fg, bg, cbg, cfg)
	}
}

// TestPanelParityTheme：NewTheme 竖/横两档字段逐字对拍（浅深双色 × 两制）。
func TestPanelParityTheme(t *testing.T) {
	// pre: panelFontFor(false)=15/17/12 inset=4/6；panelFontFor(true)=19/21/13 inset=3/5
	for _, tc := range []struct {
		land                 bool
		text, title, caption unit.Sp
		inset, titleInset    unit.Dp
	}{
		{false, 15, 17, 12, 4, 6},
		{true, 19, 21, 13, 3, 5},
	} {
		f := FontFor(tc.land)
		if f.Text != tc.text || f.Title != tc.title || f.Caption != tc.caption ||
			f.RowInset != tc.inset || f.TitleInset != tc.titleInset {
			t.Errorf("FontFor(%v) = %v/%v/%v inset=%v/%v，期望 %v/%v/%v inset=%v/%v",
				tc.land, f.Text, f.Title, f.Caption, f.RowInset, f.TitleInset,
				tc.text, tc.title, tc.caption, tc.inset, tc.titleInset)
		}
	}
	// pre: panelFontForTheme(nil)={15 17 12 4 6}（计数路径按竖屏档）
	if got := FontForTheme(nil); got != (Font{15, 17, 12, 4, 6}) {
		t.Errorf("FontForTheme(nil) = %+v，期望 {15 17 12 4 6}", got)
	}

	lightFg, lightBg := color.NRGBA{0, 0, 0, 255}, color.NRGBA{255, 255, 255, 255}
	lightCbg, lightCfg := color.NRGBA{63, 81, 181, 255}, color.NRGBA{255, 255, 255, 255}
	darkFg, darkBg := color.NRGBA{230, 230, 230, 255}, color.NRGBA{24, 24, 24, 255}
	darkCbg, darkCfg := color.NRGBA{40, 53, 147, 255}, color.NRGBA{245, 246, 255, 255}

	// pre: NewPanelTheme() TextSize=15、NewPanelTheme(true) TextSize=19，四色 = 浅色列
	for _, tc := range []struct {
		name             string
		dark             bool
		land             []bool
		textSize         unit.Sp
		fg, bg, cbg, cfg color.NRGBA
	}{
		{"竖屏浅色", false, nil, 15, lightFg, lightBg, lightCbg, lightCfg},
		{"横屏浅色", false, []bool{true}, 19, lightFg, lightBg, lightCbg, lightCfg},
		{"竖屏深色", true, nil, 15, darkFg, darkBg, darkCbg, darkCfg},
		{"横屏深色", true, []bool{true}, 19, darkFg, darkBg, darkCbg, darkCfg},
	} {
		th := NewTheme(tc.dark, tc.land...)
		if th.TextSize != tc.textSize {
			t.Errorf("%s TextSize = %v，期望 %v", tc.name, th.TextSize, tc.textSize)
		}
		eqPal(t, tc.name, th.Palette, tc.fg, tc.bg, tc.cbg, tc.cfg)
	}
	eqPal(t, "PaletteForMode(false)", PaletteForMode(false), lightFg, lightBg, lightCbg, lightCfg)
	eqPal(t, "PaletteForMode(true)", PaletteForMode(true), darkFg, darkBg, darkCbg, darkCfg)

	// pre: panelFontForTheme(portrait)={15 17 12 4 6}、panelFontForTheme(landscape)={19 21 13 3 5}
	if got := FontForTheme(NewTheme(false)); got != (Font{15, 17, 12, 4, 6}) {
		t.Errorf("FontForTheme(竖屏主题) = %+v，期望 {15 17 12 4 6}", got)
	}
	if got := FontForTheme(NewTheme(false, true)); got != (Font{19, 21, 13, 3, 5}) {
		t.Errorf("FontForTheme(横屏主题) = %+v，期望 {19 21 13 3 5}", got)
	}

	// pre: DefaultPanelStyle 全字段（全透明、无边框、margin 30、字色 alpha 230、阴影开）
	d := DefaultStyle()
	if !d.Set || d.Show || d.BGOpacity != 0 || d.ClockBGOpacity != 0 || d.BorderOpacity != 0 ||
		d.Margin != 30 || !d.Shadow ||
		d.BG != (color.NRGBA{35, 35, 35, 255}) || d.Border != (color.NRGBA{255, 255, 255, 255}) ||
		d.TextColor != (color.NRGBA{255, 255, 255, 230}) {
		t.Errorf("DefaultStyle = %+v，期望上游缺省（全透明、无边框、margin 30、字色 alpha 230、阴影开）", d)
	}
}

// TestPanelParityLayout：面板底/圆角/阴影/描边 alpha/分隔线的固定输入对拍。
func TestPanelParityLayout(t *testing.T) {
	// pre: panelRadiusPx(0)=0 (6)=1 (12)=2 (30)=5 (108..1920)=18，上限 18；负高钳到 0
	for _, tc := range []struct{ h, want int }{
		{-6, 0}, {0, 0}, {6, 1}, {12, 2}, {30, 5}, {108, 18}, {120, 18},
		{180, 18}, {200, 18}, {1080, 18}, {1920, 18},
	} {
		if got := PanelRadiusPx(tc.h); got != tc.want {
			t.Errorf("PanelRadiusPx(%d) = %d，期望 %d", tc.h, got, tc.want)
		}
	}
	if PanelRadiusMaxPx != 18 {
		t.Errorf("PanelRadiusMaxPx = %d，期望 18", PanelRadiusMaxPx)
	}

	// pre: shadowOffsetForPx(-10/0/1/25/40/60)=2、(144)=4、(174)=5、(200)=6
	for _, tc := range []struct{ px, want int }{
		{-10, 2}, {0, 2}, {1, 2}, {25, 2}, {40, 2}, {60, 2}, {144, 4}, {174, 5}, {200, 6},
	} {
		if got := ShadowOffsetForPx(tc.px); got != tc.want {
			t.Errorf("ShadowOffsetForPx(%d) = %d，期望 %d", tc.px, got, tc.want)
		}
	}
	if ShadowOffsetPx != 2 {
		t.Errorf("ShadowOffsetPx = %d，期望 2", ShadowOffsetPx)
	}
	if ShadowInk != (color.NRGBA{R: 0, G: 0, B: 0, A: 0x96}) {
		t.Errorf("ShadowInk = %v，期望 {0 0 0 150}", ShadowInk)
	}
	if InkSep != (color.NRGBA{R: 0xd8, G: 0xe2, B: 0xee, A: 0x0A}) {
		t.Errorf("InkSep = %v，期望 {216 226 238 10}", InkSep)
	}
	if BorderAlphaScale != 0.35 || SeparatorLenFrac != 0.90 {
		t.Errorf("BorderAlphaScale=%v SeparatorLenFrac=%v，期望 0.35 / 0.9", BorderAlphaScale, SeparatorLenFrac)
	}

	// pre: mulAlpha(BG,k) 的 alpha 列（RGB 恒不动）
	bg := color.NRGBA{35, 35, 35, 255}
	for _, tc := range []struct {
		k float64
		a uint8
	}{
		{-1, 0}, {0, 0}, {0.22, 56}, {0.26, 66}, {0.3, 77}, {0.45, 115}, {1, 255}, {2, 255},
	} {
		got := MulAlpha(bg, tc.k)
		if got.A != tc.a || got.R != 35 || got.G != 35 || got.B != 35 {
			t.Errorf("MulAlpha(BG,%v) = %v，期望 alpha=%d 且 RGB 不变", tc.k, got, tc.a)
		}
	}
	if got := MulAlpha(color.NRGBA{255, 255, 255, 255}, 1.0*BorderAlphaScale); got.A != 89 {
		t.Errorf("MulAlpha(Border,0.35) = %v，期望 alpha=89", got)
	}
	if got := MulAlpha(color.NRGBA{255, 255, 255, 230}, 1); got.A != 230 {
		t.Errorf("MulAlpha(TextColor,1) = %v，期望 alpha=230", got)
	}

	// pre: sep w/lw/x0/dims 全表（内缩 + 居中 + 布局返回全宽）
	for _, tc := range []struct{ w, lw, x0, dw, dh int }{
		{0, 0, 0, 0, 0}, {1, 1, 0, 1, 1}, {12, 10, 1, 12, 1}, {100, 90, 5, 100, 1},
		{300, 270, 15, 300, 1}, {1021, 918, 51, 1021, 1}, {1080, 972, 54, 1080, 1},
		{1920, 1728, 96, 1920, 1},
	} {
		lw := int(float64(tc.w) * SeparatorLenFrac)
		if lw < 1 {
			lw = 1
		}
		if lw > tc.w {
			lw = tc.w
		}
		if lw != tc.lw || (tc.w-lw)/2 != tc.x0 {
			t.Errorf("分隔线 w=%d ⇒ lw=%d x0=%d，期望 lw=%d x0=%d", tc.w, lw, (tc.w-lw)/2, tc.lw, tc.x0)
		}
		d := Separator()(parityCtx(tc.w, 40))
		if d.Size.X != tc.dw || d.Size.Y != tc.dh {
			t.Errorf("Separator()(w=%d) dims=%dx%d，期望 %dx%d", tc.w, d.Size.X, d.Size.Y, tc.dw, tc.dh)
		}
	}
}

// TestPanelParityClock：时钟两行的逐字串 + 竖/横两制字号档。
func TestPanelParityClock(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 10, 15, 15, 4, 5, 0, time.UTC),
		time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC),
		time.Date(2026, 10, 15, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC),
	}
	specs := []Clock{
		{Format24h: true, ShowSeconds: true, ShowDate: true, ShowWeekday: true, ShowTimezone: false},
		{Format24h: false, ShowSeconds: false, ShowDate: true, ShowWeekday: true},
		{Format24h: true, ShowSeconds: false, ShowDate: false, ShowWeekday: true},
		{Format24h: false, ShowSeconds: true, ShowDate: true, ShowWeekday: false},
		{},
	}
	// pre: clock s0..s4 × t0..t5 的 time/date 两行
	wantTime := [][]string{
		{"15:04:05", "00:00:00", "12:00:00", "12:30:00", "23:59:59", "13:00:00"},
		{"3:04", "12:00", "12:00", "12:30", "11:59", "1:00"},
		{"15:04", "00:00", "12:00", "12:30", "23:59", "13:00"},
		{"3:04:05", "12:00:00", "12:00:00", "12:30:00", "11:59:59", "1:00:00"},
		{"3:04", "12:00", "12:00", "12:30", "11:59", "1:00"},
	}
	wantDate := [][]string{
		{"Thursday, October 15 2026", "Thursday, October 15 2026", "Thursday, October 15 2026",
			"Thursday, October 15 2026", "Thursday, October 15 2026", "Thursday, January 1 2026"},
		{"Thursday, October 15 2026", "Thursday, October 15 2026", "Thursday, October 15 2026",
			"Thursday, October 15 2026", "Thursday, October 15 2026", "Thursday, January 1 2026"},
		{"Thursday", "Thursday", "Thursday", "Thursday", "Thursday", "Thursday"},
		{"October 15 2026", "October 15 2026", "October 15 2026", "October 15 2026",
			"October 15 2026", "January 1 2026"},
		{"", "", "", "", "", ""},
	}
	for si, sp := range specs {
		for ti, tm := range times {
			if got := sp.TimeLine(tm); got != wantTime[si][ti] {
				t.Errorf("clock s%d t%d TimeLine = %q，期望 %q", si, ti, got, wantTime[si][ti])
			}
			if got := sp.DateLine(tm); got != wantDate[si][ti] {
				t.Errorf("clock s%d t%d DateLine = %q，期望 %q", si, ti, got, wantDate[si][ti])
			}
		}
	}

	// 竖/横两制字号档。行宽 = 迁移前的实测值（pre: measureClockLineWidth=691 691 460）：
	// 横屏分支按调用次序（时间行 → 日期行）喂进这两个数，期望值 = 迁移前 clockLineSp 的返回。
	// pre: clockLineSp(1080x1920)=174/40、(1920x1080)=145/40、(1280x800)=96/33、(800x1280)=174/40
	th := material.NewTheme()
	const (
		timeStr = "15:04:05"
		dateStr = "Wednesday, Oct 15 2026"
	)
	seq := func(widths ...int) MeasureFn {
		i := 0
		return func(gtx layout.Context, w layout.Widget) layout.Dimensions {
			if i >= len(widths) {
				t.Errorf("量宽被调用 %d 次，期望 %d 次（横屏分支 = 时间行 + 日期行各一次）", i+1, len(widths))
				return layout.Dimensions{}
			}
			d := layout.Dimensions{Size: image.Point{X: widths[i], Y: 237}}
			i++
			return d
		}
	}
	capFn := func(base, textWPx, screenW int) int {
		capW := int(0.30 * float64(screenW))
		if textWPx <= 0 || textWPx <= capW {
			return base
		}
		size := int(float64(base) * float64(capW) / float64(textWPx))
		if size < 1 {
			size = 1
		}
		return size
	}
	for _, tc := range []struct{ w, h, timeSp, dateSp int }{
		{1080, 1920, 174, 40}, {1920, 1080, 145, 40}, {1280, 800, 96, 33}, {800, 1280, 174, 40},
	} {
		a, b := ClockLineSp(parityCtx(tc.w, tc.h), image.Pt(tc.w, tc.h), timeStr, dateStr,
			174, 40, th, seq(691, 460), capFn)
		if a != tc.timeSp || b != tc.dateSp {
			t.Errorf("ClockLineSp(%dx%d) = %d/%d，期望 %d/%d", tc.w, tc.h, a, b, tc.timeSp, tc.dateSp)
		}
	}

	// 单条记忆口径（迁移前的 clockLineWidthCache）：同一 key 第二次取数走记忆，不再量
	calls := 0
	counting := func(gtx layout.Context, w layout.Widget) layout.Dimensions {
		calls++
		return layout.Dimensions{Size: image.Point{X: 691, Y: 237}}
	}
	gtx := parityCtx(1920, 1080)
	w1 := MeasureClockLineWidth(gtx, timeStr, 174, th, counting)
	w2 := MeasureClockLineWidth(gtx, timeStr, 174, th, counting)
	if w1 != 691 || w2 != 691 || calls != 1 {
		t.Errorf("行宽记忆 = %d/%d（measure 调用 %d 次），期望 691/691 且只量一次", w1, w2, calls)
	}
	// 换 key（字号档 40 的日期行）⇒ 重新量一次，pre 值 460
	second := func(gtx layout.Context, w layout.Widget) layout.Dimensions {
		return layout.Dimensions{Size: image.Point{X: 460, Y: 55}}
	}
	if got := MeasureClockLineWidth(gtx, dateStr, 40, th, second); got != 460 {
		t.Errorf("MeasureClockLineWidth(日期行,40) = %d，期望 460", got)
	}
}

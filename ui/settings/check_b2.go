package settings

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// layoutAt 在给定像素上限下布局一个 widget，返回其**自然尺寸**。
// Min 必须置 0：layout.Exact 把 Min 抬到 Max，widget 会被撑满窗口，
// 量到的尺寸就是窗口本身（实测 langRow 报 h=1100），不是内容。
// 1 dp = 1 px = 1 sp：与设置窗口在 1080×1920 竖屏上的口径一致
// （cmd/setshot 的 density 注释：目标竖屏上 1 dp = 1 px）。
func layoutAt(t *testing.T, w layout.Widget, width, height int) layout.Dimensions {
	t.Helper()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Constraints{Max: image.Point{X: width, Y: height}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
	}
	return w(gtx)
}

// 本机（CI 字体）实测的单行高度，用作行高的上下界基准：
//
//	material.Body2(th, "英文 (en)") @ panelTextSp → h=22
//	material.RadioButton(th, langSel, "en", "英文 (en)") → h=34（图标 26 dp 占高）
//	langRow 竖排 → w=195 h=102 = 2*inset(4) + 22 + 2*(34 + gap 2)
//	同一行在 120 px 宽度下 → h=120（标签被挤成多行，即 的竖排症状）
const (
	b2LabelLinePx = 22
	b2RadioLinePx = 34
)

// TestPanelRadioRowsVerticalAt640 对应 的"640 px 宽度下实测不截断"。
// 判据口径：
// - 宽度 ≤ 640：整行不溢出窗口；
// - 宽度 < 400：标签与 radio 文本都落在**一行**里（标签自然宽 187 px，
// 被挤成逐字换行时自然宽会掉到几十字符宽、行高翻几倍）；
// - 行高 ≥ 3 个标签行：证明"标签 + 2 个 radio"是 3 行竖排；
// - 行高 ≤ 4 个 radio 行：证明没有任何一行被折成多行。
func CheckPanelRadioRowsVerticalAt640(t *testing.T) {
	th := NewPanelTheme()
	p := NewPanel(sampleModel())
	rows := []struct {
		name string
		w    layout.Widget
	}{
		{"langRow", p.langRow(th)},
	}
	for _, r := range rows {
		d := layoutAt(t, r.w, 640, 1100)
		t.Logf("%s: w=%d h=%d", r.name, d.Size.X, d.Size.Y)
		if d.Size.X > 640 {
			t.Errorf("%s 在 640 px 下宽度 %d 溢出窗口", r.name, d.Size.X)
		}
		if d.Size.X >= 400 {
			t.Errorf("%s 自然宽 %d px ⇒ 单行放不下（期望 < 400）", r.name, d.Size.X)
		}
		if min := 3 * b2LabelLinePx; d.Size.Y < min {
			t.Errorf("%s 行高 %d px < %d px ⇒ 未竖排成 3 行", r.name, d.Size.Y, min)
		}
		if max := 5 * b2RadioLinePx; d.Size.Y > max {
			// 上界从 4 个 radio 行放宽到 5 个：把
			// '窗口小时(3/6)'那组换成'视野小时(6/12/24)'⇒ 同一组多一个 radio
			// （标签 1 行 + 3 个 radio 行 = 4 行，实测 138 px）。
			// 本判据要挡的是"标签被挤成逐字换行"（行高翻几倍），不是 radio 个数。
			t.Errorf("%s 行高 %d px > %d px ⇒ 有行被折成多行（逐字换行）", r.name, d.Size.Y, max)
		}
	}
}

// TestPanelLabelSingleLineAt640 面板正文标签在 640 px 下逐行不折行
// （行距判据的基准行高）。
func CheckPanelLabelSingleLineAt640(t *testing.T) {
	th := NewPanelTheme()
	for _, s := range []string{
		"英文 (en)",
		"中文 (zh)",
		"3 小时",
		"6 小时",
		"display_language 地名显示",
		"alert_window_hours 预警窗口",
	} {
		l := material.Body2(th, s)
		l.TextSize = panelTextSp
		d := layoutAt(t, l.Layout, 640, 1100)
		if d.Size.Y > b2LabelLinePx+2 {
			t.Errorf("标签 %q 在 640 px 下高 %d px ⇒ 折成多行（单行基准 %d）", s, d.Size.Y, b2LabelLinePx)
		}
		if d.Size.X > 640 {
			t.Errorf("标签 %q 宽 %d px 溢出 640", s, d.Size.X)
		}
	}
}

// TestPanelLayoutAt640NoOverflow 整面板在设置窗口宽度（640）下不横向溢出。
func CheckPanelLayoutAt640NoOverflow(t *testing.T) {
	th := NewPanelTheme()
	p := NewPanel(sampleModel())
	d := layoutAt(t, func(gtx layout.Context) layout.Dimensions {
		return p.Layout(gtx, th)
	}, 640, 1100)
	t.Logf("panel: w=%d h=%d rows=%d", d.Size.X, d.Size.Y, p.RowCount())
	if d.Size.X > 640 {
		t.Errorf("面板在 640 px 下宽度 %d 溢出", d.Size.X)
	}
	if d.Size.Y <= 0 {
		t.Errorf("面板未占尺寸")
	}
}

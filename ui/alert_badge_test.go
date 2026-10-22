package ui

import (
	"image"
	"testing"
	"unicode"
)

// 态④ 左下角小信号的钉死单测（plan b-alert-v2e，2026-10-22 用户拍板：
// 「这既然不是预警，你给我信息也没有异议，最多在屏幕左下角用一个简易的标识提醒，
//  类似游戏中网络异常，ping值高等的那种小信号」）。

func TestAlertBadgeUsesPanelRow(t *testing.T) {
	for _, st := range []AlertState{AlertDaily, AlertDisaster, AlertOfficial} {
		if !usesPanelRow(&Alert{State: st}) {
			t.Errorf("态 %d 应走胶囊行（三态版式逐字不动）", st)
		}
	}
	if usesPanelRow(&Alert{State: AlertUnavailable}) {
		t.Errorf("态④ 不该走胶囊行（应走左下角小信号）")
	}
	if usesPanelRow(nil) {
		t.Errorf("nil 提醒不该走胶囊行")
	}
}

// 小信号文字逐字 + 语言口径（只有恰好 zh 走中文，其余一律英文且无汉字）。
func TestAlertBadgeTextVerbatim(t *testing.T) {
	if got, want := BadgeText("zh"), "无数据"; got != want {
		t.Errorf("BadgeText(zh) = %q，期望逐字 %q", got, want)
	}
	for _, lang := range []string{"en", "", "EN", "En", "zh-CN", "zh_CN", "zh-Hans"} {
		got := BadgeText(lang)
		if got != "No data" {
			t.Errorf("BadgeText(%q) = %q，期望 %q", lang, got, "No data")
		}
		for _, r := range got {
			if unicode.Is(unicode.Han, r) {
				t.Fatalf("BadgeText(%q) 出现汉字 %q：%q", lang, r, got)
			}
		}
	}
}

// 几何逐字钉死：左边距 28、底边距 28；退化入参给零矩形；盒不越出视口。
func TestAlertBadgeRectGeometry(t *testing.T) {
	r := badgeRect(image.Point{X: 1080, Y: 1920}, 120, 40)
	if r.Min.X != 28 || r.Max.Y != 1920-28 || r.Dx() != 120 || r.Dy() != 40 {
		t.Errorf("badgeRect(1080x1920,120x40) = %v，期望 Min.X=28 Max.Y=1892 120x40", r)
	}
	// 小窗口：右边界钳到视口内，底边距仍 28
	r2 := badgeRect(image.Point{X: 100, Y: 100}, 120, 40)
	if r2.Max.X != 100 || r2.Min.X != 28 || r2.Max.Y != 72 {
		t.Errorf("小窗口钳制 = %v，期望 Max.X=100 Min.X=28 Max.Y=72", r2)
	}
	// 超大盒不越到屏幕外（Min.Y 不为负、Max.Y 不越底）
	r3 := badgeRect(image.Point{X: 400, Y: 100}, 120, 400)
	if r3.Min.Y < 0 || r3.Max.Y > 100 {
		t.Errorf("超大盒钳制 = %v，期望 0 ≤ Min.Y 且 Max.Y ≤ 100", r3)
	}
	for _, tc := range []struct {
		size image.Point
		w, h int
	}{
		{image.Point{}, 120, 40},
		{image.Point{X: 0, Y: 900}, 120, 40},
		{image.Point{X: 900, Y: 0}, 120, 40},
		{image.Point{X: 1080, Y: 1920}, 0, 40},
		{image.Point{X: 1080, Y: 1920}, 120, 0},
	} {
		got := badgeRect(tc.size, tc.w, tc.h)
		if got.Dx() != 0 || got.Dy() != 0 {
			t.Errorf("退化入参 %v %dx%d 应给零矩形，实得 %v", tc.size, tc.w, tc.h, got)
		}
	}
}

// 常量口径钉死（版式改动要能被闸门逐字核对）。
func TestAlertBadgeConstants(t *testing.T) {
	if badgeMarginX != 28 || badgeMarginY != 28 {
		t.Errorf("边距 = %d/%d，期望 28/28", badgeMarginX, badgeMarginY)
	}
	if badgeSp != 18 {
		t.Errorf("字号 = %d，期望 18", badgeSp)
	}
	if len([]rune(BadgeText("zh"))) > 4 || len([]rune(BadgeText("en"))) > 8 {
		t.Errorf("小信号文字过长（zh %q / en %q）", BadgeText("zh"), BadgeText("en"))
	}
}

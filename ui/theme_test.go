package ui

import (
	"image/color"
	"testing"

	"gioui.org/unit"
)

// lum 是亮度（Rec. 601 luma，0–255）。
func lum(c color.NRGBA) int { return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000 }

func eq(c color.NRGBA, r, g, b, a uint8) bool {
	return c.R == r && c.G == g && c.B == b && c.A == a
}

// TestPaletteForModeExact 对应 T1：四色写死期望值，不许只比大小。
// 浅色 = material.NewTheme 的固定四色（widget/material/theme.go:58-63）。
// 深色 = 本批次自定并写死的深底浅字（ui/theme.go paletteForMode）。
func TestPaletteForModeExact(t *testing.T) {
	light := paletteForMode(false)
	dark := paletteForMode(true)

	wantLight := []color.NRGBA{
		{R: 0, G: 0, B: 0, A: 255},       // Fg
		{R: 255, G: 255, B: 255, A: 255}, // Bg
		{R: 63, G: 81, B: 181, A: 255},   // ContrastBg
		{R: 255, G: 255, B: 255, A: 255}, // ContrastFg
	}
	wantDark := []color.NRGBA{
		{R: 230, G: 230, B: 230, A: 255}, // Fg
		{R: 24, G: 24, B: 24, A: 255},    // Bg
		{R: 40, G: 53, B: 147, A: 255},   // ContrastBg
		{R: 245, G: 246, B: 255, A: 255}, // ContrastFg
	}
	names := []string{"Fg", "Bg", "ContrastBg", "ContrastFg"}
	gotLight := []color.NRGBA{light.Fg, light.Bg, light.ContrastBg, light.ContrastFg}
	gotDark := []color.NRGBA{dark.Fg, dark.Bg, dark.ContrastBg, dark.ContrastFg}

	for i, n := range names {
		if !eq(gotLight[i], wantLight[i].R, wantLight[i].G, wantLight[i].B, wantLight[i].A) {
			t.Errorf("浅色 %s = %v，期望 %v", n, gotLight[i], wantLight[i])
		}
		if !eq(gotDark[i], wantDark[i].R, wantDark[i].G, wantDark[i].B, wantDark[i].A) {
			t.Errorf("深色 %s = %v，期望 %v", n, gotDark[i], wantDark[i])
		}
		if gotLight[i].A != 255 || gotDark[i].A != 255 {
			t.Errorf("%s 的 A 必须是 255：浅色 %v 深色 %v", n, gotLight[i], gotDark[i])
		}
		if eq(gotLight[i], wantDark[i].R, wantDark[i].G, wantDark[i].B, wantDark[i].A) {
			t.Errorf("%s 深浅色同值 ⇒ 配色没有跟随（%v）", n, gotLight[i])
		}
	}
	// 深底必须比浅底暗、浅字必须比深字亮。
	if !(lum(dark.Bg) < lum(light.Bg)) {
		t.Errorf("深色 Bg 亮度 %d 应 < 浅色 Bg 亮度 %d", lum(dark.Bg), lum(light.Bg))
	}
	if !(lum(dark.Fg) > lum(light.Fg)) {
		t.Errorf("深色 Fg 亮度 %d 应 > 浅色 Fg 亮度 %d", lum(dark.Fg), lum(light.Fg))
	}
	// 深底上放浅字：Fg 与 Bg 的亮度差要够（中文细字在低对比下发飘）。
	if lum(dark.Fg)-lum(dark.Bg) < 150 {
		t.Errorf("深色前景/底色亮度差 %d 太小", lum(dark.Fg)-lum(dark.Bg))
	}
}

// TestPanelTextSp 对应 T3：panelTextSp 存在且 ≤ 20。
func TestPanelTextSp(t *testing.T) {
	if panelTextSp <= 0 || panelTextSp > 20 {
		t.Errorf("panelTextSp = %v，期望 (0, 20]", float32(panelTextSp))
	}
	if panelTitleSp <= panelTextSp {
		t.Errorf("标题 %v 应比正文 %v 大一号", float32(panelTitleSp), float32(panelTextSp))
	}
	if panelCaptionSp >= panelTextSp {
		t.Errorf("说明文字 %v 应比正文 %v 小", float32(panelCaptionSp), float32(panelTextSp))
	}
	// 面板主题的 TextSize 必须就是 panelTextSp（不再继承 WeatherFontSize）。
	th := NewPanelTheme()
	if th.TextSize != unit.Sp(panelTextSp) {
		t.Errorf("面板主题 TextSize = %v，期望 %v", float32(th.TextSize), float32(panelTextSp))
	}
	if th.TextSize >= unit.Sp(WeatherFontSize) {
		t.Errorf("面板字号没有脱离屏保的 WeatherFontSize=%d 量级", WeatherFontSize)
	}
	// 面板主题的配色必须来自 paletteForMode（跟随系统的那一支）。
	want := paletteForMode(currentThemeMode().dark())
	if th.Palette != want {
		t.Errorf("面板主题配色 = %+v，期望 %+v", th.Palette, want)
	}
}

package main

// 职责（app/scr）：设置面板模型换算：config.Config 与 ui.PanelModel 互转、面板样式装配
// 搬移来源：cmd/scr/main.go:372-414,430-468,1096-1126（逐字，含注释）
// 规格：—— app/scr = 屏保生命周期；本卡零行为变化。

import (
	"image/color"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/ui"
)

// toModel / saveModel：config.Config ↔ 面板模型。
// ui 包不 import config（config 已 import ui，会成环），转换放在这里。
func toModel(c config.Config) ui.PanelModel {
	return ui.PanelModel{
		Location:            c.Location,
		FontSize:            c.FontSize,
		IconSize:            c.IconSize,
		Margin:              c.Margin,
		Units:               c.Units,
		RefreshMinutes:      c.RefreshMinutes,
		ShowBackground:      c.ShowBackground,
		BgOpacity:           c.BgOpacity,
		ClockBgOpacity:      c.ClockBgOpacity,
		ShowConditionIcon:   c.ShowConditionIcon,
		ShowDetailsRow:      c.ShowDetailsRow,
		ShowForecast:        c.ShowForecast,
		ShowFiveDayForecast: c.ShowFiveDayForecast,
		EffectsOn:           len(c.EffectEnabled) > 0,
		WallpaperDir:        c.WallpaperDir,
		WallpaperIntervalS:  c.WallpaperIntS,
		ClockYOffset:        c.ClockYOffset,
		WeatherYOffset:      c.WeatherYOffset,
		InteractionMode:     c.InteractionMode,
		AlertEnabled:        c.AlertEnabled == nil || *c.AlertEnabled,
		AlertThresholdPct:   alertThreshold(c),
		AlertYOffset:        c.AlertYOffset,
		AlertPulse:          c.AlertPulse == nil || *c.AlertPulse,
		AlertMinMM:          alertMinMM(c),
		LocationLat:         c.LocationLat,
		LocationLon:         c.LocationLon,
		LocationNameZh:      c.LocationNameZh,
		LocationNameEn:      c.LocationNameEn,
		DisplayLanguage:     displayLang(c),
	}
}

// displayLang 显示语言，未设时按默认 "en"（= 当前样式，不改）。
func displayLang(c config.Config) string {
	if c.DisplayLanguage == "zh" {
		return "zh"
	}
	return "en"
}

// saveModel 写回 config 并复用 config 现有保存函数 Config.Save。
func saveModel(c config.Config, m ui.PanelModel) error {
	c.Location = m.Location
	c.FontSize = m.FontSize
	c.IconSize = m.IconSize
	c.Margin = m.Margin
	c.Units = m.Units
	c.RefreshMinutes = m.RefreshMinutes
	c.AlertEnabled = &m.AlertEnabled
	c.AlertThresholdPct = m.AlertThresholdPct
	c.AlertYOffset = m.AlertYOffset
	c.AlertPulse = &m.AlertPulse
	// 淋湿门槛：面板是自由文本输入，写回前收口到合法范围（越界回落 0.05）
	c.AlertMinMM = alertMinMM(config.Config{AlertMinMM: m.AlertMinMM})
	c.LocationLat = m.LocationLat
	c.LocationLon = m.LocationLon
	c.LocationNameZh = m.LocationNameZh
	c.LocationNameEn = m.LocationNameEn
	c.DisplayLanguage = m.DisplayLanguage
	c.ShowBackground = m.ShowBackground
	c.BgOpacity = m.BgOpacity
	c.ClockBgOpacity = m.ClockBgOpacity
	c.ShowConditionIcon = m.ShowConditionIcon
	c.ShowDetailsRow = m.ShowDetailsRow
	c.ShowForecast = m.ShowForecast
	c.ShowFiveDayForecast = m.ShowFiveDayForecast
	// 特效开关 = effect.enabled 列表：开 = 全 7 类（同 Defaults），关 = 空
	if m.EffectsOn {
		c.EffectEnabled = []string{"clear", "cloudy", "fog", "rain", "snow", "thunder", "dust"}
	} else {
		c.EffectEnabled = []string{}
	}
	c.WallpaperDir = m.WallpaperDir
	c.WallpaperIntS = m.WallpaperIntervalS
	c.ClockYOffset = m.ClockYOffset
	c.WeatherYOffset = m.WeatherYOffset
	c.InteractionMode = m.InteractionMode
	return c.Save()
}

// panelStyle 把 config 的面板键映射成 ui.PanelStyle
// （键名与取值口径 = 上游 default_settings.py 'weather' 块）。
func panelStyle(c config.Config) ui.PanelStyle {
	st := ui.DefaultPanelStyle()
	st.Set = true
	st.Show = c.ShowBackground
	st.BG = nrgba(c.BgColor)
	st.BGOpacity = c.BgOpacity
	st.ClockBGOpacity = c.ClockBgOpacity // 时钟块遮罩
	st.Border = nrgba(c.BorderColor)
	st.BorderOpacity = c.BorderOpacity
	if c.Margin >= 0 {
		st.Margin = c.Margin
	}
	st.TextColor = nrgba(c.TextColor)
	return st
}

func nrgba(a [4]int) color.NRGBA {
	return color.NRGBA{R: u8(a[0]), G: u8(a[1]), B: u8(a[2]), A: u8(a[3])}
}

func u8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

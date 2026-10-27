package ui

import (
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/ui/panel"
	"github.com/DragonTTDream/WeatherScr/ui/settings"
)

// （ui/panel 拆包）：面板主题的**本体**整体迁到 ui/panel/theme.go
// （字号/行距/配色/选档/构主题，逐字不动）。本文件只留兼容转发：
// 名字、签名与取值逐字不变 ⇒ 调用点 ui/settings_ui.go、cmd/scr/main.go、
// cmd/setshot/main.go 与 ui/*_test.go 一字不改。
// 证据（ui/panel = 面板/文本/图标布局，含横竖两制）。
// 深浅色的**读取**留在 ui：currentThemeMode 按 //go:build 分档实现在
// ui/theme_windows.go（注册表）与 ui/theme_other.go（非 Windows = 浅色），
// 不点名那两文件 ⇒ themeMode 与它的两个取值继续住在 ui。

// themeMode 是系统深浅色模式。
type themeMode int

const (
	themeLight themeMode = iota
	themeDark
)

func (m themeMode) dark() bool { return m == themeDark }

// panelFont 是面板一档字号 + 行距。字段名与键序逐字保持旧 ui 版
// （判据文件 ui/panel_landscape_test.go 与 ui/settings_ui.go 按下划线字段名取档）；
// 数值本体在 panel.Font（导出字段），这里做逐字段搬运，公式零改动。
type panelFont struct {
	text, title, caption unit.Sp
	rowInset, titleInset unit.Dp
}

func toPanelFont(f panel.Font) panelFont {
	return panelFont{
		text: f.Text, title: f.Title, caption: f.Caption,
		rowInset: f.RowInset, titleInset: f.TitleInset,
	}
}

// 面板字号（sp）与行内边距（dp）的兼容常量：本体 = panel 的同名导出件。
const (
	panelTextSp    = panel.TextSp
	panelTitleSp   = panel.TitleSp
	panelCaptionSp = panel.CaptionSp

	panelRowInsetDp   = panel.RowInsetDp
	panelTitleInsetDp = panel.TitleInsetDp
	panelRadioIndent  = panel.RadioIndent
	panelRadioGapDp   = panel.RadioGapDp
)

// 面板字号的**横屏档**兼容常量（本体 = panel 的同名导出件）。
const (
	panelTextSpLand    = panel.TextSpLand
	panelTitleSpLand   = panel.TitleSpLand
	panelCaptionSpLand = panel.CaptionSpLand

	panelRowInsetLandDp   = panel.RowInsetLandDp
	panelTitleInsetLandDp = panel.TitleInsetLandDp
)

// panelFontFor 按朝向选档（转发到 panel.FontFor，竖屏/横屏两档逐字不变）。
func panelFontFor(landscape bool) panelFont { return toPanelFont(panel.FontFor(landscape)) }

// panelFontForTheme 从主题取档（转发到 panel.FontForTheme；th=nil 走竖屏档的
// 判据随本体迁出，ui/settings_diag.go 的 p.rows(nil) 计数路径行为不变）。
func panelFontForTheme(th *material.Theme) panelFont { return toPanelFont(panel.FontForTheme(th)) }

// paletteForMode 返回面板四色（转发到 panel.PaletteForMode，深浅色各四色逐字不变）。
func paletteForMode(dark bool) material.Palette { return panel.PaletteForMode(dark) }

// NewPanelTheme 构建设置面板专用主题（转发到 panel.NewTheme）：
// 深浅色 = 本文件的 currentThemeMode().dark()（Windows 注册表 / 非 Windows 浅色），
// 朝向 = 变参（不传 = 竖屏档 15sp，传 true = 横屏档 19sp）⇒ 零参调用点逐字不变。
func NewPanelTheme(landscape ...bool) *material.Theme {
	return panel.NewTheme(currentThemeMode().dark(), landscape...)
}

// （ui/settings 拆包）：深浅色读取的**注入点**。
// 本体 currentThemeMode 按 //go:build 分档住在 ui/theme_windows.go / ui/theme_other.go，
// 而 ui/settings 不得 import ui（ui 已 import ui/settings ⇒ 成环）⇒ 把读取以函数指针
// 注入过去：公式与取值仍是单一真相源（上面 NewPanelTheme 与本注入走同一条 currentThemeMode）。
func init() { settings.SystemDark = func() bool { return currentThemeMode().dark() } }

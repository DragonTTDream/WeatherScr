package settings

// 随实现迁来的**兼容 shim**：本体全部在已结案的 ui/panel。
// 分层口径 = ui/settings（设置页 UI，横矩形 + 新交互）与
// ui/panel（面板/文本/图标布局，含横竖两制，已结案）。
// 纪律：本文件不产生任何新数值 ⇒ 字号/行距/配色/选档公式一律取 panel 的同名导出件，
// 逐字段搬运的 shim 与 ui/theme.go:30-82（留下的同型 shim）**逐字同型**，
// 目的是让迁来的行构造代码（panel.go）与迁来的判据文件按下划线字段名取档、
// 一字不改（判据文件 ui/panel_landscape_test.go 与 ui/settings_ui_test.go 的写法）。
// 深浅色的**读取**不在本包：本体 currentThemeMode 按 //go:build 分档住在
// ui/theme_windows.go（注册表）与 ui/theme_other.go（非 Windows = 浅色），
// ui/settings 不得 import ui（ui 已 import 本包 ⇒ 会成环）⇒ 以 SystemDark
// 函数指针注入，注入点在 ui/theme.go 的 init（单一真相源，本包默认值 = 非 Windows 浅色）。

import (
	"strconv"

	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/ui/panel"
)

// panelFont 是面板一档字号 + 行距。字段名与键序逐字照 ui/theme.go:33-43
// （判据文件按下划线字段名取档）；数值本体在 panel.Font（导出字段），逐字段搬运。
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
// 判据随本体迁出，diag.go 的 p.rows(nil) 计数路径行为不变）。
func panelFontForTheme(th *material.Theme) panelFont { return toPanelFont(panel.FontForTheme(th)) }

// paletteForMode 返回面板四色（转发到 panel.PaletteForMode，深浅色各四色逐字不变）。
func paletteForMode(dark bool) material.Palette { return panel.PaletteForMode(dark) }

// SystemDark 是系统深浅色的注入点（本体在 ui，带 //go:build 分档，见本文件头注）。
var SystemDark func() bool = func() bool { return false }

// NewPanelTheme 构建设置面板专用主题（转发到 panel.NewTheme，签名与 ui 侧
// ui.NewPanelTheme 逐字一致）：深浅色 = SystemDark()，朝向 = 变参
// （不传 = 竖屏档 15sp，传 true = 横屏档 19sp）。
func NewPanelTheme(landscape ...bool) *material.Theme {
	return panel.NewTheme(SystemDark(), landscape...)
}

// formatNum 整数值去掉小数点，与上游 :.0f 呈现习惯一致。
// 逐字副本（公式零改动），本体 = ui/render.go:559-564（判为"天气数据计算
// 留在原处"的那件，ui/settings 不得 import ui ⇒ 这里留一份逐字副本，
// 同 在 ui 留 blendPix 逐字副本的先例，见）。
func formatNum(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

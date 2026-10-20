package ui

import (
	"image/color"

	"gioui.org/unit"
	"gioui.org/widget/material"
)

// 设置面板的主题：配色跟随 Windows 深浅色 + 面板局部字号。
//
// 取证依据（spec/publish-b2-panel-theme.md §0）：
//   - gioui v0.10.3 没有 theme.SystemTheme（grep -rn "SystemTheme"
//     gioui.org@v0.10.3 = 0 命中），material.NewTheme() 固定浅色
//     （widget/material/theme.go:56-75）⇒ "跟随系统"只能自己读注册表 +
//     自己设 material.Palette，不升 gioui。
//   - 浅色四色照抄 NewTheme 的固定值（widget/material/theme.go:58-63）。
//   - 面板字号过大的根因链：ui/layout.go:39 WeatherFontSize = 40 →
//     ui/render.go:35 th.TextSize = WeatherFontSize → material 控件按
//     th.TextSize 派生字号（material/label.go:78,92 H5=×20/16、Body2=×14/16、
//     material/editor.go:46 Editor=×1、material/radiobutton.go:24 与
//     material/checkbox.go:23 与 material/button.go:59 = ×14/16）。
//     ⇒ 面板换一份自己的主题即可，屏保侧 Screen.Theme 与 WeatherFontSize 不动
//     （Screen.Theme 的消费点只有 cmd/scr/main.go 的设置面板与 cmd/setshot）。
//
// 取舍（spec §1.1 点名要写的注释）：深浅色只在**构建面板主题时读一次**，
// 面板开着期间系统切主题不会跟着变。理由：设置窗口是短生命周期的模态窗口
// （打开→改→保存/取消就关），为它挂 WM_SETTINGCHANGE / 注册表监听
// （gioui 没有系统事件源，得自己开 RegNotifyChangeKeyValue + 窗口线程轮询）
// 换来的收益远小于成本；重启设置窗口即拿到新配色。

// 面板字号（sp）。屏保侧的 WeatherFontSize 与所有显式字号一律不动。
//
// panelTextSp = 15 的理由：
//   - gioui material 的控件字号全部从 th.TextSize 派生：Editor = ×1，
//     Button / CheckBox / RadioButton / Body2 = ×14/16，H5 = ×20/16，
//     Caption = ×12/16（见上面 material 源码行号）。th.TextSize = 15 时
//     正文 13.1sp、输入框 15sp、标题 18.75sp、标签说明 11.25sp。
//   - gioui 自己的表单基准是 16sp（theme.go:64），派生出 14sp 正文，
//     即 gioui 官方表单的观感；面板是 640 dp 宽的密集表单
//     （cmd/scr/main.go:459 app.Size(640, 1100)），观看距离是窗口距离，
//     不是屏保 1080×1920 的 3 m 观看距离 ⇒ 继承来的 40sp 是错的量级
//     （行距过大、语言行 radio 标签被挤成逐字竖排）。
//   - 取 15 而不是 16：15 让两列字段（row2 每列 ≈ 300 px）与 radio 标签
//     在 640 px 内都不截断，同时比 gioui 基准小半号，给中文标签留宽度
//     （中文按全角字宽 ≈ 1em，比拉丁文更吃宽度）。
const (
	panelTextSp    unit.Sp = 15
	panelTitleSp   unit.Sp = 17 // 标题行比正文大一号（spec §1.2）
	panelCaptionSp unit.Sp = 12 // 字段说明 / 坐标副行

	// 行内边距按 panelTextSp 重排：正文 13.1sp 的行高 ≈ 17 px，
	// 上下各 4 dp ⇒ 单行 ≈ 25 px，26 行 ≈ 650 px，落在 1100 px 窗口内
	// 且首屏可见行数明显多于 40sp 时代（cmd/setshot 实测口径）。
	panelRowInsetDp   unit.Dp = 4
	panelTitleInsetDp unit.Dp = 6
	panelRadioIndent  unit.Dp = 6 // 竖排 radio 相对标签的缩进
	panelRadioGapDp   unit.Dp = 2 // 竖排 radio 行间距
)

// themeMode 是系统深浅色模式。
type themeMode int

const (
	themeLight themeMode = iota
	themeDark
)

func (m themeMode) dark() bool { return m == themeDark }

// paletteForMode 返回面板四色。
//
// 浅色 = material.NewTheme() 的固定值（widget/material/theme.go:58-63，
// rgb() 把 A 置 0xff）；深色 = 深底浅字，数值自定并写死（spec §1.1）。
// 深色取值口径：
//   - Fg 230 = 近白但不刺眼（纯 255 在 24 的底上对比度过高，中文细字发飘）；
//   - Bg 24 = Windows 深色设置页的底色量级（#202020 附近）；
//   - ContrastBg 沿用浅色那支 indigo 的暗版（63,81,181 → 40,53,147），
//     保持"选中态是同一支蓝"的观感连续，同时与浅色 ContrastBg 不同值；
//   - ContrastFg 245,246,255 = 带一点蓝的白，与浅色 ContrastFg（纯白）区分，
//     保证深浅色四色逐位互不相同（spec §3 T1）。
func paletteForMode(dark bool) material.Palette {
	if dark {
		return material.Palette{
			Fg:         color.NRGBA{R: 230, G: 230, B: 230, A: 255},
			Bg:         color.NRGBA{R: 24, G: 24, B: 24, A: 255},
			ContrastBg: color.NRGBA{R: 40, G: 53, B: 147, A: 255},
			ContrastFg: color.NRGBA{R: 245, G: 246, B: 255, A: 255},
		}
	}
	return material.Palette{
		Fg:         color.NRGBA{R: 0, G: 0, B: 0, A: 255},
		Bg:         color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		ContrastBg: color.NRGBA{R: 63, G: 81, B: 181, A: 255},
		ContrastFg: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	}
}

// NewPanelTheme 构建设置面板专用主题：配色按系统深浅色，字号按 panelTextSp。
//
// 与 ui.New()（屏保侧 Screen.Theme）是两份互不相干的 Theme：
// 本函数不碰 WeatherFontSize，屏保画面零影响。
// 每次构建面板时调一次（取舍见本文件顶部注释）。
func NewPanelTheme() *material.Theme {
	th := material.NewTheme()
	th.Palette = paletteForMode(currentThemeMode().dark())
	th.TextSize = panelTextSp
	return th
}

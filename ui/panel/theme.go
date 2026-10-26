package panel

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
//
// 系统深浅色的**读取**住在调用方（ui/theme_windows.go / ui/theme_other.go 的
// currentThemeMode，带 //go:build 分档）：本包只收一个 dark 布尔，不碰注册表、
// 不新增依赖（R5 卡 7 边界，见 out/notes-R5-panel.md）。

// 面板字号（sp）。屏保侧的 WeatherFontSize 与所有显式字号一律不动。
//
// TextSp = 15 的理由：
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
	TextSp    unit.Sp = 15
	TitleSp   unit.Sp = 17 // 标题行比正文大一号（spec §1.2）
	CaptionSp unit.Sp = 12 // 字段说明 / 坐标副行

	// 行内边距按 TextSp 重排：正文 13.1sp 的行高 ≈ 17 px，
	// 上下各 4 dp ⇒ 单行 ≈ 25 px，26 行 ≈ 650 px，落在 1100 px 窗口内
	// 且首屏可见行数明显多于 40sp 时代（cmd/setshot 实测口径）。
	RowInsetDp   unit.Dp = 4
	TitleInsetDp unit.Dp = 6 // 标题行内边距
	RadioIndent  unit.Dp = 6 // 竖排 radio 相对标签的缩进
	RadioGapDp   unit.Dp = 2 // 竖排 radio 行间距
)

// 面板字号的**横屏档**（spec/b5-settings-landscape.md 点位「ui/theme.go:48-58：
// 横屏字号档（19/21/13sp）」；判据 3 钉「横屏字号 ≥17sp」⇒ 正文取 19）。
//
// 竖屏档常量（上面 15/17/12）**逐字不动**：判据 4「竖屏零迁移」要求
// ui/panel_theme_b2_test.go 三条 640 判据原样保持，那三条量的就是 15/17/12。
//
// 横屏为什么加大字号：横屏几何下面板是 1920×1080 的双栏矩形（每栏 ≈ 960 px 宽），
// 观看距离与竖屏 640 px 密集表单同级 ⇒ 15sp 在 960 px 宽的栏里偏小；
// 取 19 满足判据 3 的「≥17sp」，同时 19 派生的正文 16.6sp、标题 21sp 在
// 960 px 栏内仍放得下最长标签（实测见 ui/panel_landscape_test.go）。
const (
	TextSpLand    unit.Sp = 19
	TitleSpLand   unit.Sp = 21 // 标题比正文大一号（与竖屏同口径）
	CaptionSpLand unit.Sp = 13 // 字段说明 / 坐标副行

	// 横屏行距（spec 点位「+ 行距」）：判据 3 钉「行高 ≤40 px」。
	// 竖屏的 4 dp 行内边距在 19sp 下把单行推到 40 px 以上 ⇒ 横屏收紧到 3 dp，
	// 让 17 行的右栏在 1080 px 内整列可见（实测行高见 panel_landscape_test.go）。
	RowInsetLandDp   unit.Dp = 3
	TitleInsetLandDp unit.Dp = 5
)

// Font 是面板一档字号 + 行距。竖屏/横屏各一档（spec 判据 3「panelTextSp 横屏档 19sp」）。
type Font struct {
	Text, Title, Caption unit.Sp
	RowInset, TitleInset unit.Dp
}

// FontFor 按朝向选档：横屏 = 19/21/13 + 收紧行距，竖屏 = 现档逐字不变。
func FontFor(landscape bool) Font {
	if landscape {
		return Font{
			Text: TextSpLand, Title: TitleSpLand, Caption: CaptionSpLand,
			RowInset: RowInsetLandDp, TitleInset: TitleInsetLandDp,
		}
	}
	return Font{
		Text: TextSp, Title: TitleSp, Caption: CaptionSp,
		RowInset: RowInsetDp, TitleInset: TitleInsetDp,
	}
}

// FontForTheme 从主题取档：NewTheme 把档位写进 th.TextSize
// （竖屏 15 / 横屏 19），行构造闭包只拿得到 th，故以 th.TextSize 选档。
// 判据 3 的「横屏字号 ≥17sp」使两档在 th.TextSize 上可判且无重叠。
//
// th == nil 时按竖屏档返回：ui/settings_diag.go:13 的 RowCount 走
// p.rows(nil) 计数（那里注明"th 在闭包内部才被解引用 ⇒ th=nil 计数安全"），
// 选档发生在行构造的函数体里 ⇒ 这里必须容忍 nil，否则计数路径 panic。
func FontForTheme(th *material.Theme) Font {
	if th == nil {
		return FontFor(false)
	}
	return FontFor(th.TextSize >= TextSpLand)
}

// PaletteForMode 返回面板四色。
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
func PaletteForMode(dark bool) material.Palette {
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

// NewTheme 构建设置面板专用主题：配色按系统深浅色，字号按朝向选档。
//
// 与 ui.New()（屏保侧 Screen.Theme）是两份互不相干的 Theme：
// 本函数不碰 WeatherFontSize，屏保画面零影响。
// 每次构建面板时调一次（取舍见本文件顶部注释）。
//
// 参数 = 深浅色（由调用方读系统配色后传入，取证口径见文件顶部）+ 朝向
// （spec/b5-settings-landscape.md 点位「NewPanelTheme 按参数选档
// （调用方按朝向传参）」）：**不传朝向 = 竖屏档**（15sp，逐像素零迁移），
// 传 true = 横屏档（19sp）。做成变参是为了既有零参调用点
// （ui/panel_theme_b2_test.go:53/86/109、ui/theme_test.go:80、
// ui/theme_other_test.go:13 —— 全是竖屏判据，硬纪律不许改）继续编译。
func NewTheme(dark bool, landscape ...bool) *material.Theme {
	th := material.NewTheme()
	th.Palette = PaletteForMode(dark)
	th.TextSize = FontFor(len(landscape) > 0 && landscape[0]).Text
	return th
}

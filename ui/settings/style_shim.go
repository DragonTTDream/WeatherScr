package settings

// 随实现迁来的第二块 shim：面板底/分隔线/圆角/阴影的**风格常量与助手**。
// 本体全部在已结案的 ui/panel，名字与取值一字不动，
// 这里只做同名转发 ⇒ 迁来的判据文件 ui/settings_ui_test.go 的断言体
// （TestPanelStyleDefaults / TestSeparatorInnerLength / TestPanelRadiusAdaptive /
// TestShadowOffsetScalesWithFont）里的标识符逐字不变（只有 s.separator() 一处按事实改）。
// 证据 = + 。转发式与 ui/render.go:44-59/:208-213/
// 425-426 的 ui 侧同型。

import (
	"image/color"

	"gioui.org/layout"

	"github.com/DragonTTDream/WeatherScr/ui/panel"
)

var (
	shadowInk = panel.ShadowInk
	inkSep    = panel.InkSep
)

const (
	shadowOffsetPx   = panel.ShadowOffsetPx
	borderAlphaScale = panel.BorderAlphaScale
	separatorLenFrac = panel.SeparatorLenFrac
)

func shadowOffsetForPx(px int) int                  { return panel.ShadowOffsetForPx(px) }
func panelRadiusPx(panelH int) int                  { return panel.PanelRadiusPx(panelH) }
func mulAlpha(c color.NRGBA, k float64) color.NRGBA { return panel.MulAlpha(c, k) }

// DefaultPanelStyle 面板底默认值（转发到 panel.DefaultStyle，十键取值逐字不变）。
func DefaultPanelStyle() panel.Style { return panel.DefaultStyle() }

// separator 分隔线（转发到 panel.Separator；判据文件里旧写法是 ui.Screen 的方法
// s.separator()，本体同一条 ⇒ 判据里改成包级调用，见 settings_ui_test.go 的行注）。
func separator() layout.Widget { return panel.Separator() }

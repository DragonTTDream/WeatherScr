package ui

// （ui/settings 拆包）·闸门薄壳（GATE-B5 判据 1 与 2 的测试名）。
// 设置页判据的本体随实现迁进 ui/settings（package settings，
// 判据文件旧 ui/panel_landscape_test.go → ui/settings/check_landscape.go、
// 旧 ui/panel_theme_b2_test.go → ui/settings/check_b2.go，断言逐字未改）。
// 的判据 1/2 是 `go test ./ui -run 'TestPanelLandscape…'`
// 与竖屏三条 ⇒ 闸门要在 **./ui** 路径下按名字命中。
// 这里按"一行调用 settings 包同名判据函数"的口径留薄壳 ⇒ 闸门命令零改动。
// 薄壳不含任何判据内容（数字/断言都在 settings 侧），只做名字可见性；
// 红了即 settings 侧本体红，定位不歧义（两处同名，同一函数）。

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/ui/settings"
)

func TestPanelLandscapeRect(t *testing.T)          { settings.CheckPanelLandscapeRect(t) }
func TestPanelLandscapeDualColumn(t *testing.T)    { settings.CheckPanelLandscapeDualColumn(t) }
func TestPanelLandscapeFontReadable(t *testing.T)  { settings.CheckPanelLandscapeFontReadable(t) }
func TestPanelLandscapeLabels(t *testing.T)        { settings.CheckPanelLandscapeLabels(t) }
func TestPanelRadioRowsVerticalAt640(t *testing.T) { settings.CheckPanelRadioRowsVerticalAt640(t) }
func TestPanelLabelSingleLineAt640(t *testing.T)   { settings.CheckPanelLabelSingleLineAt640(t) }
func TestPanelLayoutAt640NoOverflow(t *testing.T)  { settings.CheckPanelLayoutAt640NoOverflow(t) }

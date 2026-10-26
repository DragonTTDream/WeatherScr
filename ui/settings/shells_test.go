package settings

// R5 卡 8（ui/settings 拆包）·判据名薄壳。
//
// B5 闸门（out/gate-settings-landscape.sh 判据 1/2）按测试名在 **./ui 路径**里跑
// TestPanelLandscapeRect / DualColumn / FontReadable / Labels 与竖屏三条。
// 判据本体随实现迁进本包（check_landscape.go / check_b2.go，断言逐字未改），
// 这里按同名挂上 ⇒ ./ui/settings 与 ./ui（ui/settings_gate_shells_test.go 的薄壳）
// 两条路径的闸门 -run 正则都命中，闸门命令零改动。

import "testing"

func TestPanelLandscapeRect(t *testing.T)          { CheckPanelLandscapeRect(t) }
func TestPanelLandscapeDualColumn(t *testing.T)    { CheckPanelLandscapeDualColumn(t) }
func TestPanelLandscapeFontReadable(t *testing.T)  { CheckPanelLandscapeFontReadable(t) }
func TestPanelLandscapeLabels(t *testing.T)        { CheckPanelLandscapeLabels(t) }
func TestPanelRadioRowsVerticalAt640(t *testing.T) { CheckPanelRadioRowsVerticalAt640(t) }
func TestPanelLabelSingleLineAt640(t *testing.T)   { CheckPanelLabelSingleLineAt640(t) }
func TestPanelLayoutAt640NoOverflow(t *testing.T)  { CheckPanelLayoutAt640NoOverflow(t) }

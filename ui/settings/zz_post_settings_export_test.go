package settings

// （ui/settings 拆包）取数件 A：**只用导出面**，迁移前后一字不改复跑。
// 口径照 ui/zz_pre_panel_test.go：本件只取证不改行为。
// 迁移前跑 ⇒ ；迁移后同一件一字不改复跑 ⇒ 。
// 覆盖：RowCount、640×1100（竖屏主题）与 1920×1080（横屏主题）两制下
// SettingsPanel.Layout 的宽高、ListOffset / ListVisibleCount、IsLandscape 判定表。
// 本件自带模型字面量（不借 ui/settings_ui_test.go 的 sampleModel，
// 那份随判据文件迁进 ui/settings ⇒ 本件在迁移后必须仍能编译）。

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func zzModel() Model {
	return Model{
		Location:            "Hangzhou, China",
		FontSize:            25,
		IconSize:            96,
		Margin:              30,
		Units:               "c",
		RefreshMinutes:      30,
		ShowBackground:      true,
		BgOpacity:           0.3,
		ClockBgOpacity:      0.45,
		ShowConditionIcon:   true,
		ShowDetailsRow:      true,
		ShowForecast:        true,
		ShowFiveDayForecast: false,
		EffectsOn:           true,
		WallpaperDir:        `C:\Users\Public\Pictures`,
		WallpaperIntervalS:  40,
		ClockYOffset:        0,
		WeatherYOffset:      0,
		InteractionMode:     false,
		AlertEnabled:        false,
		AlertThresholdPct:   0,
		AlertYOffset:        0,
		AlertPulse:          false,
		AlertMinMM:          0.05,
		LocationLat:         0,
		LocationLon:         0,
		LocationNameZh:      "",
		LocationNameEn:      "",
		DisplayLanguage:     "en",
	}
}

func zzSizeNatA(w layout.Widget, wd, hd int) image.Point {
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Constraints{Max: image.Point{X: wd, Y: hd}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
	}
	return w(gtx).Size
}

func TestZZPostSettingsExport(t *testing.T) {
	thP := NewPanelTheme()
	thL := NewPanelTheme(true)
	p := NewPanel(zzModel())

	fmt.Printf("A rowcount=%d\n", p.RowCount())

	dP := zzSizeNatA(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thP) }, 640, 1100)
	fmt.Printf("A panel 640x1100 w=%d h=%d offset=%d visible=%d\n", dP.X, dP.Y, p.ListOffset(), p.ListVisibleCount())

	dL := zzSizeNatA(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thL) }, 1920, 1080)
	fmt.Printf("A panel 1920x1080 w=%d h=%d offset=%d visible=%d\n", dL.X, dL.Y, p.ListOffset(), p.ListVisibleCount())

	for _, tc := range [][2]int{{640, 1100}, {1080, 1920}, {1200, 800}, {1199, 1080}, {1920, 1080}, {900, 1600}} {
		fmt.Printf("A land %dx%d = %v\n", tc[0], tc[1], IsLandscape(tc[0], tc[1]))
	}
}

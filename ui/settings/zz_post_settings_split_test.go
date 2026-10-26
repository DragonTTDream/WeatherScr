package settings

// R5 卡 8（ui/settings 拆包）取数件 B：**内部面**取数（rowsSplit 点名表 + 双栏宽高）。
//
// 迁移前在 ui 跑（旧实现）⇒ /tmp/settings.pre.txt；迁移后同一件搬进
// ui/settings（只改包名行与被迁类型的构造前缀）复跑 ⇒ /tmp/settings.post.txt。
// 两件数字逐字对拍；TestSettingsParity（ui/settings/parity_test.go）把这里的
// 数字写死成断言。
//
// 指纹与点名口径照 ui/panel_landscape_test.go:32-56 / :83-124（同一支七档宽度指纹），
// 本件自带副本，避免与判据文件的迁移互相牵制。

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/geo"
)

const zzProbeN = 7

var zzProbeWidths = []int{4000, 500, 260, 200, 150, 360, 120}

type zzFP [zzProbeN]image.Point

// zzSizeNat 口径 = ui/panel_theme_b2_test.go 的 layoutAt（Min 置 0 量自然尺寸，1 dp=1 px=1 sp）。
func zzSizeNat(w layout.Widget, wd, hd int) image.Point {
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Constraints{Max: image.Point{X: wd, Y: hd}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
	}
	return w(gtx).Size
}

func zzFpOf(w layout.Widget) zzFP {
	var fp zzFP
	for i, wd := range zzProbeWidths {
		fp[i] = zzSizeNat(w, wd, 4000)
	}
	return fp
}

func zzRowRefs(p *Panel, th *material.Theme) map[string]layout.Widget {
	return map[string]layout.Widget{
		"locSearch": p.locSearchRow(th),
		"refresh": p.row2(th,
			fieldSpec{label: "margin", ed: &p.edMargin},
			fieldSpec{label: "refresh_minutes 天气刷新(分钟，最小1)", ed: &p.edRefresh}),
		"units": p.unitsRow(th),

		"check:show_background":        p.checkRow(th, "show_background 面板底", &p.cbShowBg),
		"check:show_condition_icon":    p.checkRow(th, "show_condition_icon 天气图标", &p.cbConditionIcon),
		"check:show_details_row":       p.checkRow(th, "show_details_row 明细行", &p.cbDetailsRow),
		"check:show_forecast":          p.checkRow(th, "show_forecast 预报行", &p.cbForecast),
		"check:show_five_day_forecast": p.checkRow(th, "show_five_day_forecast 五日预报", &p.cbFiveDay),
		"check:effects":                p.checkRow(th, "背景动效（effect.enabled）", &p.cbEffects),
		"check:interaction_mode":       p.checkRow(th, "interaction_mode（false=一动就退出）", &p.cbInteractionMode),
		"check:alert_enabled":          p.checkRow(th, "alert_enabled 降雨提醒开关", &p.cbAlert),
		"check:alert_pulse":            p.checkRow(th, "alert_pulse 提醒缓慢呼吸", &p.cbAlertPulse),

		"buttons": p.buttonRow(th),
	}
}

const zzUnnamed = "(未点名)"

func zzRefMap(refs map[string]layout.Widget) map[zzFP]string {
	fp := make(map[zzFP]string, len(refs))
	for name, w := range refs {
		fp[zzFpOf(w)] = name
	}
	return fp
}

func zzNameOf(fp map[zzFP]string, k zzFP) string {
	if n, ok := fp[k]; ok {
		return n
	}
	return zzUnnamed
}

func zzNames(t *testing.T, rows []layout.Widget, fp map[zzFP]string) []string {
	t.Helper()
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, zzNameOf(fp, zzFpOf(r)))
	}
	return out
}

func zzCol(rows []layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		items := make([]layout.FlexChild, 0, len(rows))
		for _, r := range rows {
			items = append(items, layout.Rigid(r))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
	}
}

func zzPlaces() []geo.Place {
	return []geo.Place{
		{Lat: 25.55, Lon: 113.38, Name: "自流井区", Admin1: "四川省", Country: "China",
			FullPath: "China/Sichuan/Zigong/Ziliujing", CountryCode: "CN"},
		{Lat: 31.87, Lon: 117.23, Name: "包河区", Admin1: "安徽省", Country: "China",
			FullPath: "China/Anhui/Hefei/Baohe", CountryCode: "CN"},
	}
}

func TestZZPostSettingsSplit(t *testing.T) {
	thP := NewPanelTheme()
	thL := NewPanelTheme(true)
	p := NewPanel(sampleModel())

	fmt.Printf("B rowcount=%d rows_th_land=%d rows_th_port=%d\n", p.RowCount(), len(p.rows(thL)), len(p.rows(thP)))

	lL, rL := p.rowsSplit(thL)
	fmt.Printf("B split land left=%d right=%d\n", len(lL), len(rL))
	fpL := zzRefMap(zzRowRefs(p, thL))
	fmt.Printf("B land left_names=[%s]\n", strings.Join(zzNames(t, lL, fpL), ","))
	fmt.Printf("B land right_names=[%s]\n", strings.Join(zzNames(t, rL, fpL), ","))

	lP, rP := p.rowsSplit(thP)
	fmt.Printf("B split port left=%d right=%d\n", len(lP), len(rP))
	fpP := zzRefMap(zzRowRefs(p, thP))
	fmt.Printf("B port left_names=[%s]\n", strings.Join(zzNames(t, lP, fpP), ","))
	fmt.Printf("B port right_names=[%s]\n", strings.Join(zzNames(t, rP, fpP), ","))

	dl := zzSizeNat(zzCol(lL), 960, 1080)
	dr := zzSizeNat(zzCol(rL), 960, 1080)
	fmt.Printf("B col land left w=%d h=%d right w=%d h=%d\n", dl.X, dl.Y, dr.X, dr.Y)

	dpc := zzSizeNat(zzCol(p.rows(thP)), 640, 1100)
	fmt.Printf("B col port rows w=%d h=%d\n", dpc.X, dpc.Y)

	dp := zzSizeNat(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thP) }, 640, 1100)
	dld := zzSizeNat(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thL) }, 1920, 1080)
	fmt.Printf("B panel port w=%d h=%d land w=%d h=%d\n", dp.X, dp.Y, dld.X, dld.Y)

	cands := p.locCandidateRows(thL)
	fmt.Printf("B cand empty=%d\n", len(cands))
	p.SetLocationResults(zzPlaces(), "2 个候选")
	l2, r2 := p.rowsSplit(thL)
	fmt.Printf("B cand rows=%d left_after=%d right_after=%d rowcount_after=%d\n",
		len(p.locCandidateRows(thL)), len(l2), len(r2), p.RowCount())
	n2 := zzNames(t, l2, fpL)
	fmt.Printf("B land left_names_after=[%s]\n", strings.Join(n2, ","))
	fmt.Printf("B axis list=%v listRight=%v\n", p.list.Axis, p.listRight.Axis)
	fmt.Printf("B cand_at=%d\n", panelLandCandAt)
}

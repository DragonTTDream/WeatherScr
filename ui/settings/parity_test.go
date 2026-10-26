package settings

// TestSettingsParity R5 卡 8 钉死件（新包，数字写死）。
//
// 证据 = plans/active.md:212（ui/settings = 设置页 UI（横矩形 + 新交互））。
// 基准数字 = **迁移前旧实现**的实测打印：旧 ui/settings_ui.go 改名后的逐字副本编译在
// 一次性取数件包 ui/zzpre8 里（本卡迁移时创建，跑完即删），2026-10-26 本机
// 6 轮打印 ⇒ /tmp/pre6.txt；迁移后由本包 zz_post_settings_split_test.go 与
// zz_post_settings_export_test.go 以同一支探件复跑 ⇒ /tmp/post6.txt，
// 24 行数字逐字相同（唯一浮动项 = 横屏左栏自然高，pre/post 分布同为 {460,461}，
// 系 gioui 文本测量字形缓存的 ±1 抖动，非本卡引入 —— 见下④的集合断言）。
//
// 口径 = ui/panel_theme_b2_test.go:21-34 的 layoutAt（Min 置 0 量**自然尺寸**、
// 1 dp = 1 px = 1 sp、Now 固定 2026-10-26 12:00:00Z）；
// 行指纹 = ui/panel_landscape_test.go:36-44 的七档采样宽度；
// 点名表 = ui/panel_landscape_test.go:83-106 的 b5RowRefs 同名参考行指纹表。

import (
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/widget/material"
)

const parityUnnamed = "(未点名)"

// 迁移前旧实现实测的点名表（栏内顺序逐字）。
var (
	parityLandLeft = []string{
		parityUnnamed, parityUnnamed, parityUnnamed, "locSearch", parityUnnamed,
		parityUnnamed, parityUnnamed, "refresh", parityUnnamed, "units",
	}
	parityLandRight = []string{
		parityUnnamed, parityUnnamed, parityUnnamed, parityUnnamed, parityUnnamed,
		parityUnnamed, parityUnnamed,
		"check:show_background", "check:show_condition_icon", "check:show_details_row",
		"check:show_forecast", "check:show_five_day_forecast", "check:effects",
		"check:interaction_mode", "check:alert_enabled", "check:alert_pulse", "buttons",
	}
	parityPortLeft = []string{
		parityUnnamed, parityUnnamed, parityUnnamed, "locSearch", parityUnnamed,
		parityUnnamed, parityUnnamed, "refresh", parityUnnamed, "units",
	}
	parityPortRight = []string{
		parityUnnamed, parityUnnamed, parityUnnamed, "refresh", parityUnnamed,
		parityUnnamed, parityUnnamed,
		"check:show_background", "check:show_condition_icon", "check:show_details_row",
		"check:show_forecast", "check:show_five_day_forecast", "check:effects",
		"check:interaction_mode", "check:alert_enabled", "check:alert_pulse", "buttons",
	}
	parityLandLeftAfterCand = []string{
		parityUnnamed, parityUnnamed, parityUnnamed, "locSearch", parityUnnamed,
		parityUnnamed, parityUnnamed, parityUnnamed, parityUnnamed, "refresh",
		parityUnnamed, "units",
	}
)

// parityPanelNames 按指纹给一栏逐行点名，与迁移前表逐字对拍。
func parityPanelNames(t *testing.T, label string, p *Panel, rows []layout.Widget, th *material.Theme, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("%s 行数 = %d，要求 %d", label, len(rows), len(want))
	}
	fp := zzRefMap(zzRowRefs(p, th))
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, zzNameOf(fp, zzFpOf(r)))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s 点名表 ≠ 迁移前实测\ngot  %s\nwant %s", label, strings.Join(got, ","), strings.Join(want, ","))
	}
	t.Logf("%s 点名表逐字一致（%d 行）", label, len(got))
}

func TestSettingsParity(t *testing.T) {
	thP := NewPanelTheme()
	thL := NewPanelTheme(true)
	p := NewPanel(sampleModel())

	// ① 行数（竖屏单栏 / 横屏双栏 / th=nil 计数口）
	if got := p.RowCount(); got != 27 {
		t.Fatalf("RowCount = %d，要求 27（迁移前旧实现实测）", got)
	}
	if l, r := len(p.rows(thL)), len(p.rows(thP)); l != 27 || r != 27 {
		t.Errorf("rows() 行数 横=%d 竖=%d，要求 27/27", l, r)
	}

	// ② 双栏分组表（panelLandLeftIdx：左 10 / 右 17）+ 点名表逐行逐字
	lL, rL := p.rowsSplit(thL)
	if len(lL) != 10 || len(rL) != 17 {
		t.Fatalf("横屏双栏 = %d/%d，要求 10/17（分组表逐字未动）", len(lL), len(rL))
	}
	parityPanelNames(t, "横屏左栏", p, lL, thL, parityLandLeft)
	parityPanelNames(t, "横屏右栏", p, rL, thL, parityLandRight)

	lP, rP := p.rowsSplit(thP)
	if len(lP) != 10 || len(rP) != 17 {
		t.Fatalf("竖屏 th 下双栏 = %d/%d，要求 10/17", len(lP), len(rP))
	}
	parityPanelNames(t, "竖屏左栏", p, lP, thP, parityPortLeft)
	parityPanelNames(t, "竖屏右栏", p, rP, thP, parityPortRight)

	// ③ 整面板两制宽高（640×1100 竖屏窗口 / 1920×1080 横屏窗口，自然尺寸口）
	if got := zzSizeNat(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thP) }, 640, 1100); got.X != 640 || got.Y != 1100 {
		t.Errorf("竖屏面板 640×1100 宽高 = %d×%d，要求 640×1100", got.X, got.Y)
	}
	if off, vis := p.ListOffset(), p.ListVisibleCount(); off != 0 || vis != 25 {
		t.Errorf("竖屏 offset=%d visible=%d，要求 0/25", off, vis)
	}
	if got := zzSizeNat(func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, thL) }, 1920, 1080); got.X != 1920 || got.Y != 779 {
		t.Errorf("横屏面板 1920×1080 宽高 = %d×%d，要求 1920×779", got.X, got.Y)
	}
	if off, vis := p.ListOffset(), p.ListVisibleCount(); off != 0 || vis != 10 {
		t.Errorf("横屏 offset=%d visible=%d，要求 0/10", off, vis)
	}

	// ④ 两栏自然宽高（每栏 960 px 宽 = 1920/2）
	dl := zzSizeNat(zzCol(lL), 960, 1080)
	dr := zzSizeNat(zzCol(rL), 960, 1080)
	if dl.X != 960 || dr.X != 960 || dr.Y != 779 {
		t.Errorf("横屏栏宽/右栏高 = %d×%d / %d×%d，要求 960 宽、右栏高 779", dl.X, dl.Y, dr.X, dr.Y)
	}
	if dl.Y != 460 && dl.Y != 461 {
		t.Errorf("横屏左栏自然高 = %d，要求落在迁移前实测集合 {460,461}", dl.Y)
	}
	if got := zzSizeNat(zzCol(p.rows(thP)), 640, 1100); got.X != 640 || got.Y != 1100 {
		t.Errorf("竖屏单栏 640 宽 = %d×%d，要求 640×1100", got.X, got.Y)
	}

	// ⑤ 候选地名插在左栏地名组 5 行之后
	if n := len(p.locCandidateRows(thL)); n != 0 {
		t.Errorf("无候选时候选行 = %d，要求 0", n)
	}
	p.SetLocationResults(zzPlaces(), "2 个候选")
	if n := len(p.locCandidateRows(thL)); n != 2 {
		t.Fatalf("候选行 = %d，要求 2", n)
	}
	l2, r2 := p.rowsSplit(thL)
	if len(l2) != 12 || len(r2) != 17 || p.RowCount() != 29 {
		t.Errorf("灌入候选后 左=%d 右=%d 总=%d，要求 12/17/29", len(l2), len(r2), p.RowCount())
	}
	parityPanelNames(t, "横屏左栏(含候选)", p, l2, thL, parityLandLeftAfterCand)
	if p.list.Axis != layout.Vertical || p.listRight.Axis != layout.Vertical {
		t.Errorf("两栏主轴 = %v/%v，要求 layout.Vertical", p.list.Axis, p.listRight.Axis)
	}
	if panelLandCandAt != 5 {
		t.Errorf("候选插入位 panelLandCandAt = %d，要求 5", panelLandCandAt)
	}

	// ⑥ 横屏判定（宽>高 且 宽≥panelLandMinWpx=1200）
	for _, tc := range []struct {
		w, h int
		want bool
	}{
		{640, 1100, false}, {1080, 1920, false}, {900, 1600, false},
		{1199, 1080, false}, {1200, 800, true}, {1920, 1080, true},
	} {
		if got := IsLandscape(tc.w, tc.h); got != tc.want {
			t.Errorf("IsLandscape(%d,%d) = %v，要求 %v", tc.w, tc.h, got, tc.want)
		}
	}
	if panelLandMinWpx != 1200 {
		t.Errorf("panelLandMinWpx = %d，要求 1200", panelLandMinWpx)
	}
}

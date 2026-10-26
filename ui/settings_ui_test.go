package ui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
)

func sampleModel() PanelModel {
	return PanelModel{
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
		// 提醒与位置。**必须写规范值**：面板 setModel 会把
		// display_language 规范成 "en"（非 zh 一律回落 en），夹具写空串会让
		// model() 读出 "en" ≠ 夹具的 "" ⇒ 无事件帧也被判成"有改动"（实测两条测试就是这么红的）。
		AlertEnabled:      false,
		AlertThresholdPct: 0,
		AlertYOffset:      0,
		AlertPulse:        false,
		AlertMinMM:        0.05,
		LocationLat:       0,
		LocationLon:       0,
		LocationNameZh:    "",
		LocationNameEn:    "",
		DisplayLanguage:   "en",
	}
}

// TestSettingsPanelRoundTrip 面板项 → 控件 → 模型 的往返必须无损
// （面板项清单；键名口径 = 上游 'weather' 块）。
func TestSettingsPanelRoundTrip(t *testing.T) {
	m := sampleModel()
	p := NewSettingsPanel(m)
	if got := p.Model(); got != m {
		t.Errorf("初始化未往返: got %+v want %+v", got, m)
	}
	if got := p.model(); got != m {
		t.Errorf("从控件读回未往返: got %+v want %+v", got, m)
	}
}

// TestSettingsPanelUndoLock 对应 Ctrl+Z 撤销与 L 锁定
// （上游 rendering/quick/input_controller.py:207-221 的 undo/lock 语义）。
func TestSettingsPanelUndoLock(t *testing.T) {
	m := sampleModel()
	p := NewSettingsPanel(m)

	// 模拟一次改动入栈（Layout 里入栈的逻辑：before → history）
	changed := m
	changed.Margin = 12
	changed.Units = "i"
	// R12：数据结构换成 stack.Stack[PanelModel] 后，这里的压栈口径与生产入栈点
	// 一致（证据 R12 / settings_ui.go 的 p.history.Push(before)）。
	p.history.Push(m)
	p.setModel(changed)
	if p.Model().Margin != 12 {
		t.Fatalf("改动未生效: %+v", p.Model())
	}
	if !p.Undo() {
		t.Fatalf("Undo 应成功")
	}
	if got := p.Model(); got != m {
		t.Errorf("Undo 未回到上一态: got %+v want %+v", got, m)
	}
	if p.Undo() {
		t.Errorf("空撤销栈应返回 false")
	}

	if p.Locked() {
		t.Errorf("初始不应为锁定态")
	}
	p.ToggleLock()
	if !p.Locked() {
		t.Errorf("L 应进入锁定态")
	}
	p.ToggleLock()
	if p.Locked() {
		t.Errorf("再按 L 应解锁")
	}
}

// TestSettingsPanelLayoutNoEvents 用零 Source 的 layout.Context 走一遍
// 全部控件（material.List/Editor/CheckBox/Slider/RadioButton/Button）。
// 目的：把 gioui v0.10.3 的控件 API 调用在 CI 里真跑一遍
// 照实测 API 写，不得凭记忆。零 Source 不投递事件（gioui 文档）。
func TestSettingsPanelLayoutNoEvents(t *testing.T) {
	th := New().Theme
	p := NewSettingsPanel(sampleModel())
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Point{X: 900, Y: 1600}),
	}
	dims := p.Layout(gtx, th)
	if dims.Size.X <= 0 || dims.Size.Y <= 0 {
		t.Errorf("面板未占尺寸: %+v", dims.Size)
	}
	if p.Changed() {
		t.Errorf("无事件帧不应报改动（改完即写盘的触发条件）")
	}
	if act := p.HandleKeys(gtx); act != "" {
		t.Errorf("无事件帧不应有热键动作: %q", act)
	}
	if p.SaveClicked(gtx) || p.CancelClicked(gtx) || p.UndoClicked(gtx) || p.LockClicked(gtx) {
		t.Errorf("无事件帧不应有按钮点击")
	}
}

// TestSettingsPanelAlertMinMM 对应批次 b-alert-v1 第 10 步的「最小雨量(mm/15min)」行：
// 文本编辑器往返稳定；解析失败保留旧值（与其它数值行同口径）。
func TestSettingsPanelAlertMinMM(t *testing.T) {
	m := sampleModel()
	if m.AlertMinMM != 0.05 {
		t.Fatalf("夹具应含 AlertMinMM=0.05，实得 %v", m.AlertMinMM)
	}
	p := NewSettingsPanel(m)
	if got := p.edAlertMin.Text(); got != "0.05" {
		t.Errorf("编辑器初值 = %q，期望 \"0.05\"", got)
	}
	p.edAlertMin.SetText("0.2")
	if got := p.model().AlertMinMM; got != 0.2 {
		t.Errorf("填 0.2 后模型值 = %v，期望 0.2", got)
	}
	// 解析失败保留**面板模型里的现值**（model() 以 p.m 为基，与其它数值行同口径）
	p.setModel(p.model())
	p.edAlertMin.SetText("abc")
	if got := p.model().AlertMinMM; got != 0.2 {
		t.Errorf("解析失败应保留模型现值 0.2，实得 %v", got)
	}
}

// TestPanelStyleDefaults 面板底缺省值。
//
// 旧版判据是"逐字对上上游 core/settings/default_settings.py:1523-1540
// 的 'weather' 块"。
//
// 按三次视觉读图**有意偏离**上游，
// 改了三个默认值（键名与键语义不变）：
//
//	bg_opacity      0.3  → 0.22
//	clock_bg_opacity 0.45 → 0.26
//	border_opacity  1.0  → 0.0（默认不描边；启用时 alpha = opacity × 0.35）
//
// 故本自检的期望值随之更新；上游口径的键名/键序仍逐字不变。
//
//	面板底默认 = **全透明**
//	需求：面板底取全透明。
//	上游原值 bg_opacity 0.3 / border_opacity 1.0 留档在 config.go 注释。
//	键名/键序仍逐字不变；show_background 是上游可调项（'clock' 块就是 False）。
//
// 2026-10-16 修掉双真相源：config 默认值与 DefaultPanelStyle 各写一份，
// 只改 config 时 cmd/fxshot 的预览图不跟着变 ⇒ 预览与生产不一致。
// 两处必须同口径，本自检就是钉这个口径的。
func TestPanelStyleDefaults(t *testing.T) {
	st := DefaultPanelStyle()
	if st.Show {
		t.Errorf("show_background 默认应为 false = 全透明（定案）")
	}
	if st.BG != (color.NRGBA{35, 35, 35, 255}) {
		t.Errorf("bg_color 上游 [35,35,35,255]，got %+v", st.BG)
	}
	if st.BGOpacity != 0.0 {
		t.Errorf("bg_opacity 默认应为 0.0 = 全透明，got %v", st.BGOpacity)
	}
	if st.ClockBGOpacity != 0.0 {
		t.Errorf("clock_bg_opacity 默认应为 0.0 = 全透明，got %v", st.ClockBGOpacity)
	}
	if st.Border != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("border_color 上游 [255,255,255,255]，got %+v", st.Border)
	}
	if st.BorderOpacity != 0.0 {
		t.Errorf("border_opacity 默认应为 0.0 = 无边框，got %v", st.BorderOpacity)
	}
	if !st.Shadow {
		t.Errorf("全透明底板的可读性全靠上游阴影字，Shadow 必须为 true")
	}
	if st.Margin != 30 {
		t.Errorf("margin 上游 30，got %d", st.Margin)
	}
	if st.TextColor != (color.NRGBA{255, 255, 255, 230}) {
		t.Errorf("color 上游 [255,255,255,230]，got %+v", st.TextColor)
	}
	// 阴影：ShadowedText.qml shadowColor "#96000000" / shadowOffsetY 2.0
	if shadowInk.A != 0x96 || shadowInk.R != 0 || shadowInk.G != 0 || shadowInk.B != 0 {
		t.Errorf("shadowColor 上游 #96000000，got %+v", shadowInk)
	}
	if shadowOffsetPx != 2 {
		t.Errorf("shadowOffsetY 上游 2.0（下限），got %d", shadowOffsetPx)
	}
	// 全透明 ⇒ 面板底 alpha 必须为 0
	if got := mulAlpha(st.BG, st.BGOpacity); got.A != 0 {
		t.Errorf("全透明时 bg alpha 应为 0，got %d", got.A)
	}
	// 描边默认关：alpha = 0.0 × 0.35 × 255 = 0
	if got := mulAlpha(st.Border, st.BorderOpacity*borderAlphaScale); got.A != 0 {
		t.Errorf("默认 border alpha 应为 0，got %d", got.A)
	}
	// 启用描边时的系数：1.0 × 0.35 × 255 = 89.25 → 89
	if got := mulAlpha(st.Border, 1.0*borderAlphaScale); got.A != 89 {
		t.Errorf("border_opacity=1 时 border alpha 应为 89（0.35×255），got %d", got.A)
	}
	// 全透明 ⇒ 时钟遮罩 alpha 必须为 0
	if got := mulAlpha(st.BG, st.ClockBGOpacity); got.A != 0 {
		t.Errorf("全透明时 clock bg alpha 应为 0，got %d", got.A)
	}
}

// TestSeparatorInnerLength 分隔线内缩
// 量化修正 bin/fxmetrics.py。
//
// 旧版的 0x2A / 0.62 被客观指标推翻：实测分隔线处**落差 68/255**，
// 直接量像素得线色 (98,105,114)、背景 (28,37,51) ⇒ 有效 alpha ≈ 0.37，
// 远大于声明的 0x165。根因：gioui layout.Flex 对子控件先测量后绘制，
// 1 px 线段被叠加绘制 2–3 次，累积 alpha = 1−(1−a)^n。
// 故 alpha 按叠加次数反推取 0x0A（目标"线比背景亮约 15 级"），
// 内缩比例 0.62 → 0.42。
func TestSeparatorInnerLength(t *testing.T) {
	if inkSep.A != 0x0A {
		t.Errorf("分隔线 alpha 应为 0x0A（叠加 2–3 次后约 0.08–0.12），got 0x%02X", inkSep.A)
	}
	// 上线口径 0.90：需求是分割线拉长 ⇒ 从 0.42 拉长。
	if separatorLenFrac != 0.90 {
		t.Errorf("内缩比例应为 0.90，got %v", separatorLenFrac)
	}
	s := New()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Point{X: 1021, Y: 40}),
	}
	d := s.separator()(gtx)
	if d.Size.X != 1021 {
		t.Errorf("分隔线行宽应占满内容宽（布局不变），got %d", d.Size.X)
	}
	lw := int(float64(gtx.Constraints.Max.X) * separatorLenFrac)
	if lw != 918 {
		t.Errorf("1021 px 内容宽的线宽应为 918（1021×0.90），got %d", lw)
	}
	if lw >= gtx.Constraints.Max.X {
		t.Errorf("内短线宽必须小于全宽")
	}
}

// TestPanelRadiusAdaptive 圆角随面板高自适应 = min(18, 面板高/6)
func TestPanelRadiusAdaptive(t *testing.T) {
	for _, tc := range []struct{ h, want int }{
		{463, 18}, // 时钟面板（403+2×30）→ 77 → 上限 18
		{618, 18}, // 天气面板（558+2×30）→ 103 → 上限 18
		{90, 15},  // 矮面板 → 15
		{60, 10},  // 10
		{12, 2},   // 2
		{0, 0},    // 非法 → 0
	} {
		if got := panelRadiusPx(tc.h); got != tc.want {
			t.Errorf("panelRadiusPx(%d) = %d，want %d", tc.h, got, tc.want)
		}
	}
}

// TestShadowOffsetScalesWithFont 阴影偏移随字号走：max(2, round(字号px × 0.03))
// 下限 2 = 上游 ShadowedText.qml shadowOffsetY 2.0。
// 144 pt 时钟 → 4 px、174 pt（低性能 x86 小主机 font_size 144→174）→ 5 px、
// 25 pt（上游天气字号）→ 取下限 2、本复刻天气 40 pt → 取下限 2。
func TestShadowOffsetScalesWithFont(t *testing.T) {
	for _, tc := range []struct {
		px, want int
	}{
		{144, 4}, // ClockFontSize 声明值
		{174, 5}, // ClockFontSizeShown 低性能 x86 小主机值
		{40, 2},  // WeatherFontSize（本复刻）
		{25, 2},  // 上游 weather font_size 25 → 0.75 → 下限 2（与上游一致）
		{2, 2},   // 下限
		{0, 2},   // 非法字号 → 下限
		{100, 3}, // 3.0 → 3
		{84, 3},  // 2.52 → 3
	} {
		if got := shadowOffsetForPx(tc.px); got != tc.want {
			t.Errorf("shadowOffsetForPx(%d) = %d，want %d", tc.px, got, tc.want)
		}
	}
}

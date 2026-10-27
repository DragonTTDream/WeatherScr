package ui

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

// 的三态文案：
//	日常 zh `22:15 起 有雨 · 有机会 45% · 0.6 mm/h` en `Rain from 22:15 · 45% likely · 0.6 mm/h`
//	灾害 zh `暴雨风险 蓝 · 12h 内 62 mm` en `Rainstorm risk Blue · 62 mm within 12 h`
//	不可用 zh `预警数据不可用 · 截至 21:11` en `Alert data unavailable · as of 21:11`
// 语言口径不变：**只有恰好 "zh" 走中文，其余（含空串）一律英文**。

func at(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, time.UTC) }

func TestAlertTextDailyVerbatim(t *testing.T) {
	a := &Alert{State: AlertDaily, Pct: 45, MMPerH: 0.6, Start: at(22, 15)}
	if got, want := a.Text("zh"), "22:15 起 有雨 · 有机会 45% · 0.6 mm/h"; got != want {
		t.Errorf("日常 zh = %q，期望逐字 %q", got, want)
	}
	if got, want := a.Text("en"), "Rain from 22:15 · 45% likely · 0.6 mm/h"; got != want {
		t.Errorf("日常 en = %q，期望逐字 %q", got, want)
	}
	if got := a.Text(""); strings.Contains(got, "起") {
		t.Errorf(`语言口径破了：lang="" 应走英文，实得 %q`, got)
	}
}

func TestAlertTextDisasterVerbatim(t *testing.T) {
	a := &Alert{State: AlertDisaster, TierVal: 1, MM: 62, DisasterHours: 12}
	if got, want := a.Text("zh"), "暴雨风险 蓝 · 12h 内 62 mm"; got != want {
		t.Errorf("灾害 zh = %q，期望逐字 %q", got, want)
	}
	if got, want := a.Text("en"), "Rainstorm risk Blue · 62 mm within 12 h"; got != want {
		t.Errorf("灾害 en = %q，期望逐字 %q", got, want)
	}
}

func TestAlertTextUnavailableVerbatim(t *testing.T) {
	a := &Alert{State: AlertUnavailable, AsOf: at(21, 11)}
	if got, want := a.Text("zh"), "预警数据不可用 · 截至 21:11"; got != want {
		t.Errorf("不可用 zh = %q，期望逐字 %q", got, want)
	}
	if got, want := a.Text("en"), "Alert data unavailable · as of 21:11"; got != want {
		t.Errorf("不可用 en = %q，期望逐字 %q", got, want)
	}
}

// 灾害层四档逐字（蓝/黄/橙/红 → Blue/Yellow/Orange/Red），档位由灾害层给。
func TestAlertTierFromDisasterLayer(t *testing.T) {
	want := map[int][2]string{
		1: {"蓝", "Blue"},
		2: {"黄", "Yellow"},
		3: {"橙", "Orange"},
		4: {"红", "Red"},
	}
	for tier, names := range want {
		a := &Alert{State: AlertDisaster, TierVal: tier, MM: 120, DisasterHours: 3}
		if got := a.Tier(); got != tier {
			t.Errorf("Tier() = %d，期望灾害层给的 %d", got, tier)
		}
		if got := a.TierName("zh"); got != names[0] {
			t.Errorf("TierName(zh) = %q，期望 %q", got, names[0])
		}
		if got := a.TierName("en"); got != names[1] {
			t.Errorf("TierName(en) = %q，期望 %q", got, names[1])
		}
		if !strings.Contains(a.Text("zh"), names[0]) {
			t.Errorf("灾害 zh 文案应含档位名 %q，实得 %q", names[0], a.Text("zh"))
		}
	}
	// 日常层 = 态③ 的蓝色小色块
	d := &Alert{State: AlertDaily, Pct: 95, MMPerH: 0.6, Start: at(22, 15)}
	if d.Tier() != 1 {
		t.Errorf("日常层档位 = %d，期望 1（蓝，恒定不闪）", d.Tier())
	}
}

// 官方降水概率分级词逐字（1663-1665）；
// 官方未定义的 20–30 与 50–60 两档本批自定为'可能性很小'/'可能性中等'。
func TestProbWordOfficialBands(t *testing.T) {
	cases := []struct {
		pct  int
		want string
	}{
		{0, "基本不会"}, {19, "基本不会"},
		{20, "可能性很小"}, {29, "可能性很小"}, // 自定档（官方未定义 20–30）
		{30, "有机会"}, {45, "有机会"}, {50, "有机会"},
		{51, "可能性中等"}, {59, "可能性中等"}, // 自定档（官方未定义 50–60）
		{60, "可能性较大"}, {70, "可能性较大"},
		{71, "认为有降水"}, {79, "认为有降水"},
		{80, "可能性非常大"}, {90, "可能性非常大"},
		{91, "认为有降水"},
	}
	for _, tc := range cases {
		if got := ProbWord(tc.pct); got != tc.want {
			t.Errorf("ProbWord(%d) = %q，期望 %q", tc.pct, got, tc.want)
		}
	}
	// 分级词逐字进日常层文案
	a := &Alert{State: AlertDaily, Pct: 45, MMPerH: 0.6, Start: at(22, 15)}
	if !strings.Contains(a.Text("zh"), "有机会 45%") {
		t.Errorf("日常 zh 文案应含'有机会 45%%'，实得 %q", a.Text("zh"))
	}
}

// 显隐口径：旧 `Pct < AlertThresholdPct ⇒ 不画` 作废（概率不单独决定亮灭），
// 改由 State 决定。
func TestAlertVisibilityByState(t *testing.T) {
	if (&Alert{State: AlertNone, Pct: 99}).Visible() {
		t.Errorf("AlertNone 不该画（旧的概率触发线口径已作废）")
	}
	for _, st := range []AlertState{AlertDaily, AlertDisaster, AlertUnavailable} {
		if !(&Alert{State: st}).Visible() {
			t.Errorf("State=%d 应画", st)
		}
	}
	// 低概率的日常层照样画（判据 + 滞回决定亮灭，不是概率阈值）
	if !(&Alert{State: AlertDaily, Pct: 5}).Visible() {
		t.Errorf("Pct=5 的日常层应画（概率只进措辞）")
	}
	var nilAlert *Alert
	if nilAlert.Visible() {
		t.Errorf("nil 提醒不该画")
	}
}

// 不可用态配色 = 灰（态④'灰，恒定'）。
func TestAlertUnavailableColor(t *testing.T) {
	c := (&Alert{State: AlertUnavailable, AsOf: at(21, 11)}).TierColor()
	if !(c.R == 128 && c.G == 128 && c.B == 132) {
		t.Errorf("不可用态色块 = %v，期望灰 {128,128,132}", c)
	}
	red := (&Alert{State: AlertDisaster, TierVal: 4}).TierColor()
	if !(red.R == 217 && red.G == 38 && red.B == 38) {
		t.Errorf("红档色块 = %v，期望 {217,38,38}", red)
	}
}

// 态①（中国区官方信号）文案逐字钉死（第 6 步）：
//
//	zh `暴雨 黄色预警 · 22:44 发布` en `Rainstorm Yellow · issued 22:44`
//
// 起因：界面选的是英文、预警却出中文 ⇒ 预警文案必须跟随所选界面语言。
// —— 四态里只有态① 此前**没有任何单测**，语言口径全靠代码注释兜。
func TestAlertTextOfficialVerbatim(t *testing.T) {
	a := &Alert{State: AlertOfficial, OfficialTypeZh: "暴雨", OfficialTypeEn: "Rainstorm",
		OfficialLevel: 2, OfficialIssued: at(22, 44)}
	if got, want := a.Text("zh"), "暴雨 黄色预警 · 22:44 发布"; got != want {
		t.Errorf("官方 zh = %q，期望逐字 %q", got, want)
	}
	if got, want := a.Text("en"), "Rainstorm Yellow · issued 22:44"; got != want {
		t.Errorf("官方 en = %q，期望逐字 %q", got, want)
	}
	// 等级双语言逐字（1=蓝 2=黄 3=橙 4=红）
	wantZh := map[int]string{1: "蓝色", 2: "黄色", 3: "橙色", 4: "红色"}
	wantEn := map[int]string{1: "Blue", 2: "Yellow", 3: "Orange", 4: "Red"}
	for lv := 1; lv <= 4; lv++ {
		if got := officialLevelZh(lv); got != wantZh[lv] {
			t.Errorf("officialLevelZh(%d) = %q，期望 %q", lv, got, wantZh[lv])
		}
		if got := officialLevelEn(lv); got != wantEn[lv] {
			t.Errorf("officialLevelEn(%d) = %q，期望 %q", lv, got, wantEn[lv])
		}
	}
	// 官方英文名缺失（实网标题解析不出英文类型）时，en 行**不得回落成中文**：
	// 只能是缺名的英文行，不能出现汉字。
	e := &Alert{State: AlertOfficial, OfficialTypeZh: "雷电", OfficialTypeEn: "",
		OfficialLevel: 3, OfficialIssued: at(9, 5)}
	line := e.Text("en")
	for _, r := range line {
		if unicode.Is(unicode.Han, r) {
			t.Fatalf("官方英文名缺失时 en 行出现汉字 %q：整行 %q", r, line)
		}
	}
}

// 语言口径总闸：**任何态在 lang=en / 空串 / 大小写变体 / zh-CN 下都不得出现汉字**。
// 口径逐字（alert.go:129-130）：只有恰好 "zh" 走中文，其余一律英文。
func TestAlertTextEnHasNoHan(t *testing.T) {
	states := []*Alert{
		{State: AlertDaily, Pct: 45, MMPerH: 0.6, Start: at(22, 15)},
		{State: AlertDisaster, TierVal: 3, MM: 62, DisasterHours: 12},
		{State: AlertUnavailable, AsOf: at(21, 11)},
		{State: AlertOfficial, OfficialTypeZh: "暴雨", OfficialTypeEn: "Rainstorm",
			OfficialLevel: 4, OfficialIssued: at(22, 44)},
	}
	for _, a := range states {
		for _, lang := range []string{"en", "", "EN", "En", "zh-CN", "zh_CN", "zh-Hans"} {
			got := a.Text(lang)
			if got == "" {
				t.Errorf("态 %d 在 lang=%q 下文案为空", a.State, lang)
			}
			for _, r := range got {
				if unicode.Is(unicode.Han, r) {
					t.Fatalf("态 %d 在 lang=%q 下出现汉字 %q：整行 %q", a.State, lang, r, got)
				}
			}
		}
		// 反向：恰好 "zh" 必须出中文（防把中文全删掉的"假绿"）
		if !strings.ContainsFunc(a.Text("zh"), func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			t.Errorf("态 %d 在 lang=zh 下没有汉字：%q", a.State, a.Text("zh"))
		}
	}
}

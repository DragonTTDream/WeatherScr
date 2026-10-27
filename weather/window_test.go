package weather

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

// 预警判据 fixture（重写 weather/window.go 之后）。
// 逐小时 fixture 沿用批次"需求 1/2"的实测序列（2026-10-16，坐标 30.2495,120.1583，
// 每 3 小时抽样：00:00 16%/0.0 · 03:00 56%/0.5 · 06:00 62%/0.2 · 09:00 57%/0.1
// · 12:00 79%/0.1 · 15:00 77%/0.1 · 18:00 28%/0.0 · 21:00 0%/0.0）；
// 中间小时实测未取证 ⇒ 步进取下一个实测点（补齐规则已记工程记录，本批不改 fixture）。

var day1Prob = []float64{16, 56, 62, 57, 79, 77, 28, 0} // 00,03,06,09,12,15,18,21 实测
var day1MM = []float64{0.0, 0.5, 0.2, 0.1, 0.1, 0.1, 0.0, 0.0}

// fill24 把 3 小时抽样补齐成 24 格（步进取下一个实测点）。
func fill24(probs, mms []float64) ([]float64, []float64) {
	p := make([]float64, 24)
	m := make([]float64, 24)
	for h := 0; h < 24; h++ {
		k := h / 3
		if h%3 != 0 {
			k++ // 步进：下一个实测点
		}
		if k > 7 {
			k = 7 // 22/23 点无下一个实测点 ⇒ 用最后一个实测点（21:00）
		}
		p[h] = probs[k]
		m[h] = mms[k]
	}
	return p, m
}

// day1Sample 造 2026-10-16 的 24 条逐小时 fixture（当地墙钟，无时区标记）。
// ProbN/MMN 填满 = 数据闸门放行（闸门口径见 Evaluate）。
func day1Sample() *Sample {
	p, m := fill24(day1Prob, day1MM)
	s := &Sample{UTCOffsetSeconds: 28800, ProbN: 24, MMN: 24}
	for h := 0; h < 24; h++ {
		s.HourlyTime = append(s.HourlyTime, "2026-10-16T"+hourStr(h)+":00")
		s.HourlyProb = append(s.HourlyProb, p[h])
		s.HourlyMM = append(s.HourlyMM, m[h])
	}
	return s
}

// min15Sample 造 n 小时的 15 分钟格（每格 mmPerCell 毫米），prob 固定。
func min15Sample(start time.Time, cells int, mmPerCell float64, prob float64) *Sample {
	s := &Sample{UTCOffsetSeconds: 28800, ProbN: cells, MMN: cells, Min15N: cells}
	for i := 0; i < cells; i++ {
		s.Min15Time = append(s.Min15Time, start.Add(time.Duration(i*15)*time.Minute).Format(hourlySlotLayout))
		s.Min15MM = append(s.Min15MM, mmPerCell)
		s.Min15Prob = append(s.Min15Prob, prob)
	}
	s.ProbN, s.MMN, s.Min15N = 24*24, 24*24, cells
	return s
}

func hourStr(h int) string { return fmt.Sprintf("%02d", h) }

func localAt(y, mo, d, hh, mi int) time.Time {
	// 条目串无时区 ⇒ 解析成 UTC 基的墙钟；nowLocal 同基即可直接比较。
	return time.Date(y, time.Month(mo), d, hh, mi, 0, 0, time.UTC)
}

// ---- 钉死：官方四级阈值常量（；
// 官方逐字口径见 引北京市丰台区政府科普页）----

func TestPinnedOfficialTierConstants(t *testing.T) {
	if tierRedMM != 100 {
		t.Errorf("tierRedMM = %v，期望 100（官方：红 = 3h 内 100 mm 以上）", tierRedMM)
	}
	if tierOrangeMM != 50 {
		t.Errorf("tierOrangeMM = %v，期望 50（官方：橙 = 3h 内 50 mm 以上）", tierOrangeMM)
	}
	if tierYellowMM != 50 {
		t.Errorf("tierYellowMM = %v，期望 50（官方：黄 = 6h 内 50 mm 以上）", tierYellowMM)
	}
	if tierBlueMM != 50 {
		t.Errorf("tierBlueMM = %v，期望 50（官方：蓝 = 12h 内 50 mm 以上）", tierBlueMM)
	}
	if tierRedHours != 3 || tierOrangeHours != 3 {
		t.Errorf("红/橙窗口 = %d/%d，期望 3/3", tierRedHours, tierOrangeHours)
	}
	if tierYellowHours != 6 {
		t.Errorf("tierYellowHours = %d，期望 6", tierYellowHours)
	}
	if tierBlueHours != 12 {
		t.Errorf("tierBlueHours = %d，期望 12", tierBlueHours)
	}
	if defaultMinMMPer15min != 0.05 {
		t.Errorf("defaultMinMMPer15min = %v，期望 0.05", defaultMinMMPer15min)
	}
	if hysteresisFactor != 0.7 {
		t.Errorf("hysteresisFactor = %v，期望 0.7", hysteresisFactor)
	}
	if hysteresisOffCycles != 2 {
		t.Errorf("hysteresisOffCycles = %d，期望 2", hysteresisOffCycles)
	}
}

// ---- 钉死：逐模型键名----
// 钉死 p_probability_<model>；本机 直连实测到的是
// precipitation_probability_<model>（`hourly=p_probability` 被 API 拒 400）
// ⇒ 两套键名在解析端并存，两套都钉（差异记）。

func TestPinnedPerModelKeyNames(t *testing.T) {
	tag := func(path ...string) string {
		typ := reflect.TypeOf(forecastResponse{})
		var f reflect.StructField
		for _, name := range path {
			var ok bool
			f, ok = typ.FieldByName(name)
			if !ok {
				t.Fatalf("forecastResponse 里没有字段 %s（路径 %v）", name, path)
			}
			typ = f.Type
		}
		return f.Tag.Get("json")
	}
	want := map[string]string{
		// 钉死的三套
		"Hourly.ProbGFS":  "p_probability_gfs_seamless",
		"Hourly.ProbIcon": "p_probability_icon_seamless",
		"Hourly.ProbECM":  "p_probability_ecmwf_ifs025",
		"Hourly.MMGFS":    "precipitation_gfs_seamless",
		"Hourly.MMIcon":   "precipitation_icon_seamless",
		"Hourly.MMECM":    "precipitation_ecmwf_ifs025",
		// 云量
		"Hourly.CloudCoverGFS": "cloud_cover_gfs_seamless",
		// 15 分钟层同规则（第 3 步）
		"Minutely15.ProbGFS": "p_probability_gfs_seamless",
		"Minutely15.MMGFS":   "precipitation_gfs_seamless",
	}
	for path, w := range want {
		parts := splitPath(path)
		if got := tag(parts[0], parts[1]); got != w {
			t.Errorf("键名 %s = %q，期望逐字 %q", path, got, w)
		}
	}
	// 实测那套拼法（同一家的别名，不许被改名）
	alias := map[string]string{
		"Hourly.ProbGFSApi":  "precipitation_probability_gfs_seamless",
		"Hourly.ProbIconApi": "precipitation_probability_icon_seamless",
		"Hourly.ProbECMApi":  "precipitation_probability_ecmwf_ifs025",
	}
	for path, w := range alias {
		parts := splitPath(path)
		if got := tag(parts[0], parts[1]); got != w {
			t.Errorf("别名键名 %s = %q，期望逐字 %q", path, got, w)
		}
	}
}

func splitPath(p string) []string {
	var out []string
	cur := ""
	for _, r := range p {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

// ---- 第 5 步：R(Δ) 与官方配对判据 ----

func TestRainWithinPrefersMin15(t *testing.T) {
	// 15 分钟格每格 1.0 mm ⇒ 3 h = 12 格 = 12 mm；逐小时每格 5 mm ⇒ 3 h = 15 mm。
	// 口径"优先 15 分钟格求和，缺则逐小时"⇒ 应取 12。
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 48, 1.0, 40)
	for h := 0; h < 12; h++ {
		s.HourlyTime = append(s.HourlyTime, "2026-10-16T"+hourStr(h)+":00")
		s.HourlyMM = append(s.HourlyMM, 5)
	}
	mm, ok := RainWithin(s, localAt(2026, 10, 16, 0, 0), 3)
	if !ok {
		t.Fatalf("R(3h) 应有数据")
	}
	if math.Abs(mm-12) > 1e-9 {
		t.Errorf("R(3h) = %v，期望 12（15 分钟格优先）", mm)
	}
}

func TestRainWithinFallsBackToHourly(t *testing.T) {
	s := day1Sample() // 无 15 分钟层
	mm, ok := RainWithin(s, localAt(2026, 10, 16, 0, 0), 3)
	if !ok {
		t.Fatalf("R(3h) 应有数据")
	}
	// 00,01,02 三格 = 0.0 + 0.5 + 0.5（fill24 的步进补齐口径）
	if math.Abs(mm-1.0) > 1e-9 {
		t.Errorf("R(3h) = %v，期望 1.0", mm)
	}
}

func TestDisasterTierOfficialPairing(t *testing.T) {
	cases := []struct {
		name      string
		mmPerCell float64 // 每 15 分钟格的毫米数
		wantTier  int
		wantHours int
	}{
		// 3 h = 12 个 15 分钟格、6 h = 24 格、12 h = 48 格
		// 每格 25 mm ⇒ R(3h)=300 ≥100 ⇒ 红（同时满足橙/黄/蓝，取满足的最高级）
		{"red", 25, TierRed, 3},
		// 每格 5 mm ⇒ R(3h)=60 ⇒ 橙（红不满足）
		{"orange", 5, TierOrange, 3},
		// 每格 1.1 mm ⇒ R(3h)=13.2、R(6h)=26.4 不满足；R(12h)=52.8 ⇒ 蓝
		{"blue", 1.1, TierBlue, 12},
		// 每格 0.5 mm ⇒ R(12h)=24 ⇒ 全不满足 ⇒ 灾害层不亮
		{"none", 0.5, TierNone, 0},
	}
	for _, tc := range cases {
		s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, tc.mmPerCell, 40)
		tier, mm, hrs := DisasterTier(s, localAt(2026, 10, 16, 0, 0))
		if tier != tc.wantTier || hrs != tc.wantHours {
			t.Errorf("%s: tier=%d hours=%d (mm=%.1f)，期望 tier=%d hours=%d",
				tc.name, tier, hrs, mm, tc.wantTier, tc.wantHours)
		}
	}
}

// 黄档（6h 内 50 mm）在 3h 内不足 50 mm 时的命中口径：
// 每格 2.5 mm ⇒ R(3h)=30（橙不满足）、R(6h)=60 ⇒ 黄。
func TestDisasterTierYellow(t *testing.T) {
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 2.5, 40)
	tier, mm, hrs := DisasterTier(s, localAt(2026, 10, 16, 0, 0))
	if tier != TierYellow || hrs != 6 {
		t.Errorf("tier=%d hours=%d (mm=%.2f)，期望 黄(2)/6", tier, hrs, mm)
	}
}

// ---- 第 6 步：日常层锚定（视野内第一个达门槛格）----

func TestFirstWetCellAnchorsFirstCell(t *testing.T) {
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 0.06, 45)
	at, prob, mmPerH, ok := FirstWetCell(s, localAt(2026, 10, 16, 0, 0), 12, 0.05)
	if !ok {
		t.Fatalf("视野内应有达门槛格")
	}
	if !at.Equal(localAt(2026, 10, 16, 0, 0)) {
		t.Errorf("起始时刻 = %v，期望 00:00（第一个达门槛格）", at)
	}
	if prob != 45 {
		t.Errorf("该格概率 = %d，期望 45", prob)
	}
	if math.Abs(mmPerH-0.24) > 1e-9 {
		t.Errorf("折算强度 = %v mm/h，期望 0.24（0.06×4）", mmPerH)
	}
}

func TestFirstWetCellBelowThreshold(t *testing.T) {
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 0.04, 45)
	if _, _, _, ok := FirstWetCell(s, localAt(2026, 10, 16, 0, 0), 12, 0.05); ok {
		t.Errorf("0.04 mm/15min < 门槛 0.05 ⇒ 不应有达门槛格")
	}
}

func TestFirstWetCellRespectsHorizon(t *testing.T) {
	// 达门槛的格在 13 h 之后 ⇒ 12 h 视野内找不到。
	s := &Sample{UTCOffsetSeconds: 28800, ProbN: 24 * 24, MMN: 24 * 24, Min15N: 96}
	for i := 0; i < 96; i++ {
		at := localAt(2026, 10, 16, 0, 0).Add(time.Duration(i*15) * time.Minute)
		s.Min15Time = append(s.Min15Time, at.Format(hourlySlotLayout))
		mm := 0.0
		if i >= 52 { // 13:00 起
			mm = 0.6
		}
		s.Min15MM = append(s.Min15MM, mm)
		s.Min15Prob = append(s.Min15Prob, 45)
	}
	if _, _, _, ok := FirstWetCell(s, localAt(2026, 10, 16, 0, 0), 12, 0.05); ok {
		t.Errorf("达门槛格在 13:00，12 h 视野内不应命中")
	}
	at, _, _, ok := FirstWetCell(s, localAt(2026, 10, 16, 0, 0), 24, 0.05)
	if !ok || !at.Equal(localAt(2026, 10, 16, 13, 0)) {
		t.Errorf("24 h 视野应命中 13:00，实得 %v ok=%v", at, ok)
	}
}

// ---- 第 7 步：滞回 ----

func TestHysteresisTwoCycleOff(t *testing.T) {
	h := &Hysteresis{}
	if h.Step(0.06, 0.05) != true {
		t.Errorf("达门槛应亮")
	}
	if h.Step(0.03, 0.05) != true {
		t.Errorf("第 1 个低于门槛七成 的周期不应灭（灭 = 连续 2 个）")
	}
	if h.Step(0.03, 0.05) != false {
		t.Errorf("连续 2 个低于门槛七成 的周期应灭")
	}
}

func TestHysteresisBandKeepsState(t *testing.T) {
	// 0.7×门槛 ≤ v < 门槛 的带内：状态保持、连击清零
	h := &Hysteresis{}
	h.Step(0.06, 0.05) // 亮
	h.Step(0.03, 0.05) // 低 1
	h.Step(0.04, 0.05) // 带内（0.035 ≤ v < 0.05）⇒ 清零
	if h.Step(0.03, 0.05) != true {
		t.Errorf("带内清零后，单个低周期不应灭")
	}
}

// ---- 第 4 步：数据闸门 ----

func TestEvaluateDataGate(t *testing.T) {
	s := day1Sample()
	s.ProbN = 3 // < 视野 12 ⇒ 数据不可用
	e := Evaluate(s, localAt(2026, 10, 16, 0, 0), 0.05, &Hysteresis{})
	if e.Available {
		t.Errorf("ProbN=%d < 视野 12 ⇒ 应置数据不可用", s.ProbN)
	}
	if e.Disaster || e.Daily {
		t.Errorf("数据不可用时不评灾害/日常，实得 disaster=%v daily=%v", e.Disaster, e.Daily)
	}
}

// ---- ：钉死视野常量（旋钮已拆，值来自 fixture 标定）----

func TestUmbrellaViewConstants(t *testing.T) {
	if umbrellaViewHours != 2 {
		t.Errorf("umbrellaViewHours = %d，期望 2（标定表：门槛 0.05 下 "+
			"ON_RATIO=0.1397、COVERAGE=1.00、LEAD 105–345 min）", umbrellaViewHours)
	}
	if officialMaxWindowHours != 12 {
		t.Errorf("officialMaxWindowHours = %d，期望 12（官方配对最长窗口 = 蓝 12 h 内 50 mm）",
			officialMaxWindowHours)
	}
	// 数据闸门 = ProbN < officialMaxWindowHours（12）：边界两侧各钉一次。
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 0.15, 45)
	s.ProbN = officialMaxWindowHours - 1
	if e := Evaluate(s, localAt(2026, 10, 16, 0, 0), 0.05, &Hysteresis{}); e.Available {
		t.Errorf("ProbN=%d < 12 ⇒ 应判'数据不可用'", s.ProbN)
	}
	s.ProbN = officialMaxWindowHours
	if e := Evaluate(s, localAt(2026, 10, 16, 0, 0), 0.05, &Hysteresis{}); !e.Available {
		t.Errorf("ProbN=%d = 12 ⇒ 应放行（闸门是 <，不是 <=）", s.ProbN)
	}
	// 日常层视野 = 常量 2 h：达门槛格在 3 h 后 ⇒ 不命中（视野不再是调用方可传的参数）。
	s2 := &Sample{UTCOffsetSeconds: 28800, ProbN: 24 * 24, MMN: 24 * 24, Min15N: 96}
	base := localAt(2026, 10, 16, 0, 0)
	for i := 0; i < 96; i++ {
		s2.Min15Time = append(s2.Min15Time, base.Add(time.Duration(i*15)*time.Minute).Format(hourlySlotLayout))
		mm := 0.0
		if i == 12 { // 3 h 后那格
			mm = 0.2
		}
		s2.Min15MM = append(s2.Min15MM, mm)
	}
	if _, _, _, ok := FirstWetCell(s2, base, umbrellaViewHours, 0.05); ok {
		t.Errorf("达门槛格在 3:00，2 h 视野内不应命中")
	}
	if _, _, _, ok := FirstWetCell(s2, base, umbrellaViewHours+2, 0.05); !ok {
		t.Errorf("夹具自检：4 h 视野应命中 3:00 那格")
	}
}

func TestEvaluateDisasterBeatsDaily(t *testing.T) {
	// 规格'待定点'第 2 条本批定案：灾害层优先，同一行。
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 25, 45)
	e := Evaluate(s, localAt(2026, 10, 16, 0, 0), 0.05, &Hysteresis{})
	if !e.Disaster || !e.On {
		t.Fatalf("应命中灾害层且亮，实得 disaster=%v on=%v", e.Disaster, e.On)
	}
	if e.Tier != TierRed || e.Hours != 3 {
		t.Errorf("tier=%d hours=%d，期望 红(4)/3", e.Tier, e.Hours)
	}
}

func TestEvaluateDailyBlue(t *testing.T) {
	s := min15Sample(localAt(2026, 10, 16, 0, 0), 96, 0.15, 45)
	e := Evaluate(s, localAt(2026, 10, 16, 0, 0), 0.05, &Hysteresis{})
	if !e.Daily || !e.On || e.Disaster {
		t.Fatalf("应命中日常层且亮，实得 %+v", e)
	}
	if e.Tier != TierBlue {
		t.Errorf("日常层档位 = %d，期望 蓝(1)（态③ 蓝色小色块）", e.Tier)
	}
}

// ---- 第 2 步：逐模型中位数 ----

func TestPerSlotMedianRules(t *testing.T) {
	p := func(v float64) *float64 { x := v; return &x }
	n := 4
	gfs := []*float64{p(10), p(20), nil, nil}
	icon := []*float64{p(30), p(40), p(50), nil}
	ecm := []*float64{p(50), p(60), p(70), nil}
	vals, nOK := perSlotMedian(n, nil, gfs, icon, ecm)
	// 格 0：10,30,50 ⇒ 中位 30；格 1：20,40,60 ⇒ (40+40)/2? 偶数取中间两数平均 = 40
	// 格 2：50,70 ⇒ 偶数 ⇒ 60；格 3：全缺 ⇒ 0 且不计入
	if vals[0] != 30 {
		t.Errorf("格0 中位 = %v，期望 30", vals[0])
	}
	if vals[1] != 40 {
		t.Errorf("格1 中位 = %v，期望 40", vals[1])
	}
	if vals[2] != 60 {
		t.Errorf("格2 两家 50/70 ⇒ 偶数取中间两数平均 = %v，期望 60", vals[2])
	}
	if vals[3] != 0 {
		t.Errorf("格3 三模型全缺 ⇒ 期望 0，实得 %v", vals[3])
	}
	if nOK != 3 {
		t.Errorf("nOK = %d，期望 3（全缺那格计入缺数据）", nOK)
	}
}

func TestPerSlotMedianBareFallback(t *testing.T) {
	p := func(v float64) *float64 { x := v; return &x }
	vals, nOK := perSlotMedian(2, []*float64{p(11), p(12)}, nil, nil, nil)
	if vals[0] != 11 || vals[1] != 12 || nOK != 2 {
		t.Errorf("三家全缺 + 裸键有值 ⇒ 应回落裸键，实得 %v nOK=%d", vals, nOK)
	}
}

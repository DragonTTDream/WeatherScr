package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/DragonTTDream/WeatherScr/weather"
)

// alertreplay = 回放标定工具。
// 输入（Open-Meteo 形态：hourly 240 格 +
// minutely_15 960 格，2026-10-14T00:00 → 10-23T23:00，utc_offset_seconds=28800）。
// 口径（写死在本文件，规格'判据（对拍）'一节逐字）：
//
//	以 fixture 的 15 分钟格为时间轴，逐格模拟"现在 = 该格起点"，
//	按第 5 步（灾害层官方配对）+ 第 6 步（日常层锚定）判据 + 第 7 步滞回算亮灭
//	（回放里一个采样周期 = 一格）。
//	ON_RATIO = 亮格数 / 总格数（只算未来 12 h 视野内的格）
//	COVERAGE = 在实际发生降水（该格 mm ≥ 0.05）的格中，当时处于亮态的比例
//	LEAD = 每个降水段起点 − 该段第一次亮起的时刻（分钟，打印中位/最小/最大）
//
// 三个数一起看，单看任何一个都能被"常亮"作弊。
func main() {
	// -horizon 只决定回放的"视野内格"分母口径（ON_RATIO/COVERAGE 的统计窗）；
	// 判据侧的视野已是常量 2（weather.umbrellaViewHours），默认值与之对齐。
	horizon := flag.Int("horizon", 2, "回放统计窗小时数（b-alert-v1b 标定口径 2）")
	minMM := flag.Float64("minmm", 0.05, "淋湿门槛 mm/15min（第 8 步默认 0.05）")
	flag.Parse()
	path := flag.Arg(0)
	if path == "" {
		path = "out/alert-fixture-7d.json"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("ALERTREPLAY_FAIL=%v\n", err)
		os.Exit(1)
	}
	var raw struct {
		UTCOffsetSeconds int `json:"utc_offset_seconds"`
		Hourly           struct {
			Time                     []string  `json:"time"`
			Precipitation            []float64 `json:"precipitation"`
			PrecipitationProbability []float64 `json:"precipitation_probability"`
		} `json:"hourly"`
		Minutely15 struct {
			Time                     []string  `json:"time"`
			Precipitation            []float64 `json:"precipitation"`
			PrecipitationProbability []float64 `json:"precipitation_probability"`
		} `json:"minutely_15"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		fmt.Printf("ALERTREPLAY_FAIL=%v\n", err)
		os.Exit(1)
	}

	s := &weather.Sample{
		HourlyTime:       raw.Hourly.Time,
		HourlyProb:       raw.Hourly.PrecipitationProbability,
		HourlyMM:         raw.Hourly.Precipitation,
		ProbN:            len(raw.Hourly.Time),
		MMN:              len(raw.Hourly.Time),
		Min15Time:        raw.Minutely15.Time,
		Min15Prob:        raw.Minutely15.PrecipitationProbability,
		Min15MM:          raw.Minutely15.Precipitation,
		Min15N:           len(raw.Minutely15.Time),
		UTCOffsetSeconds: raw.UTCOffsetSeconds,
	}
	n := len(s.Min15Time)
	if n == 0 {
		fmt.Println("ALERTREPLAY_FAIL=fixture 无 minutely_15 格")
		os.Exit(1)
	}
	times := make([]time.Time, n)
	for i, ts := range s.Min15Time {
		t, ok := parseSlot(ts)
		if !ok {
			fmt.Printf("ALERTREPLAY_FAIL=时间格解析失败 %q\n", ts)
			os.Exit(1)
		}
		times[i] = t
	}
	last := times[n-1]

	// 逐格模拟"现在 = 该格起点"，滞回按一格一个采样周期推进（进程内存，不落盘）。
	hyst := &weather.Hysteresis{}
	on := make([]bool, n)
	for i := 0; i < n; i++ {
		e := weather.Evaluate(s, times[i], *minMM, hyst)
		on[i] = e.Disaster || (e.Daily && e.On)
	}

	// 分母口径："只算未来 12 h 视野内的格" = 该格起点 + 视野 仍落在 fixture 覆盖内。
	inView := make([]bool, n)
	for i := 0; i < n; i++ {
		inView[i] = !times[i].Add(time.Duration(*horizon) * time.Hour).After(last)
	}
	var onN, viewN, wetN, wetOnN int
	wet := make([]bool, n)
	for i := 0; i < n; i++ {
		if !inView[i] {
			continue
		}
		viewN++
		if on[i] {
			onN++
		}
		if s.Min15MM[i] >= *minMM {
			wet[i] = true
			wetN++
			if on[i] {
				wetOnN++
			}
		}
	}

	// 降水段 = 连续 wet 格。"该段第一次亮起的时刻" = 从段起点往回能连通的连续亮段里
	// 最早的那一格（提前点亮才算提前量）；段起点没亮 ⇒ 该段记 MISSED，不进 LEAD。
	var leads []float64
	segments, missed := 0, 0
	for i := 0; i < n; {
		if !wet[i] {
			i++
			continue
		}
		start := i
		for i < n && wet[i] {
			i++
		}
		end := i - 1
		_ = end
		segments++
		if !on[start] {
			missed++
			continue
		}
		j := start
		// 提前量往回最多量 24 小时（96 个 15 分钟格），**与 horizon 解耦**。
		// 理由（实测）：horizon 耦合的写法在 horizon=12 时将提前量截在 12 h，
		// 于是 8 个降水段的 LEAD 全等于视野上限（705/713/720），量不出真实提前量；
		// 而 horizon=2 时 limit=start ⇒ 提前量恒 0，同样量不到。
		limit := start - 96
		if limit < 0 {
			limit = 0
		}
		for j-1 >= limit && on[j-1] {
			j--
		}
		leads = append(leads, times[start].Sub(times[j]).Minutes())
	}

	// 判据侧视野 = 常量 2 h（weather.umbrellaViewHours，旋钮已拆）
	fmt.Printf("UMBRELLA_VIEW=2\n")
	fmt.Printf("CELLS=%d VIEW_CELLS=%d ON_CELLS=%d WET_CELLS=%d WET_ON=%d SEGMENTS=%d MISSED=%d\n",
		n, viewN, onN, wetN, wetOnN, segments, missed)
	fmt.Printf("ON_RATIO=%s\n", ratio(onN, viewN))
	fmt.Printf("COVERAGE=%s\n", ratio(wetOnN, wetN))
	fmt.Printf("LEAD_P50=%s\n", leadStat(leads, "p50"))
	fmt.Printf("LEAD_MIN=%s\n", leadStat(leads, "min"))
	fmt.Printf("LEAD_MAX=%s\n", leadStat(leads, "max"))
}

func ratio(a, b int) string {
	if b == 0 {
		return "NA"
	}
	return fmt.Sprintf("%.4f", float64(a)/float64(b))
}

// leadStat 提前量统计（分钟）。偶数个取中间两数平均 —— 与 weather.perSlotMedian 同口径。
func leadStat(v []float64, what string) string {
	if len(v) == 0 {
		return "NA"
	}
	sort.Float64s(v)
	switch what {
	case "min":
		return fmt.Sprintf("%.0f", v[0])
	case "max":
		return fmt.Sprintf("%.0f", v[len(v)-1])
	}
	m := len(v) / 2
	if len(v)%2 == 0 {
		return fmt.Sprintf("%.0f", math.Round((v[m-1]+v[m])/2))
	}
	return fmt.Sprintf("%.0f", v[m])
}

const slotLayout = "2006-01-02T15:04"

// parseSlot 与 weather.parseSlot 同口径（fixture 的 time 是当地墙钟，无时区标记）。
func parseSlot(ts string) (time.Time, bool) {
	if len(ts) < 16 {
		return time.Time{}, false
	}
	t, err := time.Parse(slotLayout, ts[:16])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

package weather

import (
	"sort"
	"time"
)

// 预警判据（第 5–7 步）。
// 本文件替换的是旧的"未来 N 小时滚动窗口取最大概率"口径（原 WindowProb，
// 点名 `weather/window.go:43-92` 重写）。
// 换掉的根因（证据 E-P1/P2 +）：
// 1. 数据通路断（forecast_hours=1 + models= ⇒ hourly 只剩 1 条、无 prob/mm）；
// 2. 判据是"窗口内最大概率"，窗口还是可调旋钮（alert_window_hours=3/6）
// ⇒ 与官方口径无关，这就是"不科学"的准确位置。
// 新口径两层：
// 灾害层 = 官方四级**配对**判据（窗口由量级决定，不是配置项）；
// 日常层 = 锚定型：视野内**第一个** ≥ alert_min_mm 的 15 分钟格；
// 滞回 = 亮 = 达门槛；灭 = 连续 2 个采样周期低于门槛的 70%；
// 数据闸门 = ProbN < officialMaxWindowHours(12) ⇒ '数据不可用'态（第 4 步）。

// hourlySlotLayout 是响应 hourly.time[] / minutely_15.time[] 的格式：
// iso8601 且**无时区标记**（请求带 timezone=auto ⇒ 上游按当地时区给墙钟时间），
// 故解析结果按当地墙钟理解，与 Sample.UTCOffsetSeconds 换算出的 local 直接比较。
const hourlySlotLayout = "2006-01-02T15:04"

// ---- 钉死常量（；官方四级出处见
// 逐字引文：
// 蓝 = 12 h 内 50 mm、黄 = 6 h 内 50 mm、橙 = 3 h 内 50 mm、红 = 3 h 内 100 mm）----
const (
	tierRedMM    = 100.0
	tierOrangeMM = 50.0
	tierYellowMM = 50.0
	tierBlueMM   = 50.0

	tierRedHours    = 3
	tierOrangeHours = 3
	tierYellowHours = 6
	tierBlueHours   = 12

	defaultMinMMPer15min = 0.05
	hysteresisFactor     = 0.7
	hysteresisOffCycles  = 2
)

// 视野不再是可调旋钮。
//
//	umbrellaViewHours = 2 日常层视野（"接下来出门会不会淋到"，fixture 标定：
//	 门槛 0.05 下 ON_RATIO=0.1397、COVERAGE=1.00、LEAD 105–345 min）
//	officialMaxWindowHours = 12 灾害层官方配对里最长的窗口（蓝 = 12 h 内 50 mm），
//	 数据闸门按它判"数据够不够评"
const umbrellaViewHours = 2
const officialMaxWindowHours = 12

// 灾害层档位（与 ui 的 1=蓝 2=黄 3=橙 4=红 同序）。
const (
	TierNone   = 0
	TierBlue   = 1
	TierYellow = 2
	TierOrange = 3
	TierRed    = 4
)

// ptrsFromFloats 把无后缀键解出的 []float64 转成逐格指针，
// 好让"无后缀键"与"逐模型后缀键"走同一个中位数函数（0 与"没有值"要分得开）。
func ptrsFromFloats(a []float64) []*float64 {
	if len(a) == 0 {
		return nil
	}
	out := make([]*float64, len(a))
	for i, v := range a {
		v := v
		out[i] = &v
	}
	return out
}

// perSlotMedian 逐下标对各家取中位数（口径，逐字）：
//
//	奇数取中、偶数取中间两数平均；缺值跳过；
//	三模型全缺 ⇒ 该格 0，且**不计入 nOK**（= 缺数据计数由调用方按 n - nOK 算）。
//
// bare = 无后缀键数组（请求不带 models= 时的旧口径）：三家全缺时若 bare 在该格有值 ⇒
// 用 bare（并计入 nOK）。n = 时间轴长度；数组比 n 短 ⇒ 越界那几家按缺失处理，不 panic。
// models = 逐模型后缀键数组，**同一家的两种键名拼法都传进来**（钉死的
// p_probability_<model> 与本机实测的 precipitation_probability_<model>，
// 见 provider.go 的 Hourly 段注释）⇒ 同一格不会被算成两家。
func perSlotMedian(n int, bare []*float64, models ...[]*float64) (vals []float64, nOK int) {
	vals = make([]float64, n)
	for i := 0; i < n; i++ {
		var v []float64
		for _, a := range models {
			if i < len(a) && a[i] != nil {
				v = append(v, *a[i])
			}
		}
		if len(v) == 0 {
			if i < len(bare) && bare[i] != nil {
				vals[i] = *bare[i]
				nOK++
				continue
			}
			continue // 三模型全缺且无裸键 ⇒ 该格 0，不计入 nOK
		}
		sort.Float64s(v)
		if len(v)%2 == 1 {
			vals[i] = v[len(v)/2]
		} else {
			vals[i] = (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		nOK++
	}
	return vals, nOK
}

// parseSlot 解析一条时间格（"T14:15"，偶带 ":00" 秒位 ⇒ 截 16 字符）。
func parseSlot(ts string) (time.Time, bool) {
	if len(ts) < 16 {
		return time.Time{}, false
	}
	at, err := time.Parse(hourlySlotLayout, ts[:16])
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// RainWithin 第 5 步的 R(Δ)：从 nowLocal 起 Δ 小时内累计预报雨量。
// 口径：**优先 15 分钟格求和，缺则逐小时**。
// 15 分钟格可用 = Sample.Min15Time 非空；逐小时 = Sample.HourlyTime。
// 入选 = 格起点 >= nowLocal 且 < nowLocal+Δ（当前那格还没走完 ⇒ 算在"接下来"里，
// 与旧 WindowProb 的 `slot >= nowLocal` 口径一致）。
// 返回 ok=false = 该 Δ 窗口内一条可用条目都没有。
func RainWithin(s *Sample, nowLocal time.Time, hours int) (mm float64, ok bool) {
	if s == nil || hours < 1 {
		return 0, false
	}
	end := nowLocal.Add(time.Duration(hours) * time.Hour)
	if len(s.Min15Time) > 0 {
		var sum float64
		var n int
		for i, ts := range s.Min15Time {
			at, ok := parseSlot(ts)
			if !ok {
				continue
			}
			if at.Before(nowLocal) || !at.Before(end) {
				continue
			}
			if i < len(s.Min15MM) {
				sum += s.Min15MM[i]
				n++
			}
		}
		if n > 0 {
			return sum, true
		}
	}
	var sum float64
	var n int
	for i, ts := range s.HourlyTime {
		at, ok := parseSlot(ts)
		if !ok {
			continue
		}
		if at.Before(nowLocal) || !at.Before(end) {
			continue
		}
		if i < len(s.HourlyMM) {
			sum += s.HourlyMM[i]
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum, true
}

// DisasterTier 第 5 步：官方四级配对判据，取满足条件的**最高级**。
//
//	红 R(3h) ≥ 100 mm
//	橙 R(3h) ≥ 50 mm
//	黄 R(6h) ≥ 50 mm
//	蓝 R(12h) ≥ 50 mm
//
// 都不满足 ⇒ TierNone（灾害层不亮）。返回命中那一级对应的窗口小时数与累计 mm，
// 文案逐字用它（"暴雨风险 蓝 · 12h 内 62 mm"）。
func DisasterTier(s *Sample, nowLocal time.Time) (tier int, mm float64, hours int) {
	type rule struct {
		tier  int
		hours int
		minMM float64
	}
	rules := []rule{
		{TierRed, tierRedHours, tierRedMM},
		{TierOrange, tierOrangeHours, tierOrangeMM},
		{TierYellow, tierYellowHours, tierYellowMM},
		{TierBlue, tierBlueHours, tierBlueMM},
	}
	for _, r := range rules {
		v, ok := RainWithin(s, nowLocal, r.hours)
		if ok && v >= r.minMM {
			return r.tier, v, r.hours
		}
	}
	return TierNone, 0, 0
}

// FirstWetCell 第 6 步（日常层，锚定型）：视野内**第一个** ≥ minMM 的 15 分钟格。
// 返回该格起始时刻、该格概率、该格折算强度（mm/h = mm×4）。
// 视野 = [nowLocal, nowLocal+horizonHours)；15 分钟层缺失 ⇒ 回落逐小时
// （门槛按同格长换算：逐小时门槛 = minMM×4，强度 = 该格 mm）。
// 找不到 ⇒ ok=false。
func FirstWetCell(s *Sample, nowLocal time.Time, horizonHours int, minMM float64) (at time.Time, prob int, mmPerH float64, ok bool) {
	if s == nil || horizonHours < 1 {
		return time.Time{}, 0, 0, false
	}
	end := nowLocal.Add(time.Duration(horizonHours) * time.Hour)
	if len(s.Min15Time) > 0 {
		for i, ts := range s.Min15Time {
			t, ok := parseSlot(ts)
			if !ok {
				continue
			}
			if t.Before(nowLocal) || !t.Before(end) {
				continue
			}
			if i >= len(s.Min15MM) {
				continue
			}
			v := s.Min15MM[i]
			if v < minMM {
				continue
			}
			p := 0
			if i < len(s.Min15Prob) {
				p = int(s.Min15Prob[i] + 0.5)
			}
			return t, p, v * 4, true
		}
		return time.Time{}, 0, 0, false
	}
	hourlyMin := minMM * 4
	for i, ts := range s.HourlyTime {
		t, ok := parseSlot(ts)
		if !ok {
			continue
		}
		if t.Before(nowLocal) || !t.Before(end) {
			continue
		}
		if i >= len(s.HourlyMM) {
			continue
		}
		v := s.HourlyMM[i]
		if v < hourlyMin {
			continue
		}
		p := 0
		if i < len(s.HourlyProb) {
			p = int(s.HourlyProb[i] + 0.5)
		}
		return t, p, v, true
	}
	return time.Time{}, 0, 0, false
}

// Hysteresis 第 7 步的滞回状态：**进程内存，不持久化**。
// 亮 = 达门槛；灭 = 连续 hysteresisOffCycles 个采样周期低于门槛的 70%。
// 采样周期 = 一次取数（refresh_minutes）—— 规格'待定点'第 3 条本批取此口径，已记未决。
// 门槛与 70% 之间的那段（0.7×门槛 ≤ v < 门槛）：既不算"达门槛"也不算"低于 70%"
// ⇒ 状态保持、连击计数清零。规格未定义该带内的行为，已记未决。
type Hysteresis struct {
	on        bool
	lowStreak int
}

// Step 走一个采样周期，返回滞回后的亮/灭。
func (h *Hysteresis) Step(value, threshold float64) bool {
	if h == nil {
		return value >= threshold
	}
	switch {
	case value >= threshold:
		h.on = true
		h.lowStreak = 0
	case value < threshold*hysteresisFactor:
		h.lowStreak++
		if h.lowStreak >= hysteresisOffCycles {
			h.on = false
		}
	default:
		h.lowStreak = 0
	}
	return h.on
}

// On 报告当前滞回状态（不推进周期）。
func (h *Hysteresis) On() bool {
	if h == nil {
		return false
	}
	return h.on
}

// AlertEval 一次判据的产物（灾害层优先，同一行 ——）。
type AlertEval struct {
	Available bool // 第 4 步数据闸门：ProbN < officialMaxWindowHours(12) ⇒ false（'数据不可用'态）
	Tier      int  // 灾害层档位（0=不亮）；日常层命中时由调用方按蓝给
	Disaster  bool // 灾害层命中
	MM        float64
	Hours     int

	Daily  bool
	Start  time.Time
	Prob   int
	MMPerH float64

	On bool // 滞回后的亮/灭（日常层用它决定显隐；灾害层命中即亮，不受滞回压制）
}

// Evaluate 把第 4/5/6/7 步串起来算一次。
// h = 滞回状态载体（调用方持有，进程内存）；nil = 无状态（回放工具逐格自带状态）。
// 视野不是参数（拆掉可调旋钮）——
// 日常层用 umbrellaViewHours(2)，数据闸门用 officialMaxWindowHours(12)。
// 数据闸门（第 4 步）：Sample.ProbN < officialMaxWindowHours ⇒ Available=false，
// 此时不评灾害/日常，画面出'预警数据不可用 · 截至 HH:MM'。
func Evaluate(s *Sample, nowLocal time.Time, minMM float64, h *Hysteresis) AlertEval {
	e := AlertEval{Available: true}
	if s == nil {
		e.Available = false
		return e
	}
	if minMM <= 0 {
		minMM = defaultMinMMPer15min
	}
	if s.ProbN < officialMaxWindowHours {
		e.Available = false
		return e
	}
	if tier, mm, hrs := DisasterTier(s, nowLocal); tier != TierNone {
		e.Disaster = true
		e.Tier = tier
		e.MM = mm
		e.Hours = hrs
		e.On = true
		if h != nil {
			// 灾害层命中 = 达门槛（滞回按"亮"推进，避免灾害亮着时把状态清成灭）
			h.Step(1e9, 1)
		}
		return e
	}
	at, prob, mmPerH, ok := FirstWetCell(s, nowLocal, umbrellaViewHours, minMM)
	if ok {
		e.Daily = true
		e.Start = at
		e.Prob = prob
		e.MMPerH = mmPerH
		e.Tier = TierBlue // 态③：蓝色小色块，恒定不闪
	}
	// 滞回（第 7 步）：进滞回的量 = 日常层那格的 mm（15 分钟格原始毫米，
	// 与门槛 alert_min_mm 同单位；逐小时回落路径下 mmPerH 已是该格毫米）。
	// 视野内没有达门槛的格 ⇒ 进 0（低于门槛的 70%，计入"灭"的连击）。
	value := 0.0
	if ok {
		if len(s.Min15Time) > 0 {
			value = mmPerH / 4
		} else {
			value = mmPerH
		}
	}
	e.On = h.Step(value, minMM)
	return e
}

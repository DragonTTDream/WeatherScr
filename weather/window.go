package weather

import (
	"math"
	"sort"
	"time"
)

// 未来 N 小时滚动窗口。
//
// 逐字定为：
//
//	"预警应该是如果接下来要出门有雨的概率，比如早上出门那显示全天没问题，
//	  比如晚上出门那就应该是晚上是否会下雨了，**它应该是动态的，而不是一天的最大值**"
//	"**默认3小时，设置项可以改成6小时**"
//
// ⇒ 预警不再是 daily.precipitation_probability_max（一天的最大值），
// 而是"从现在起未来 N 个逐小时条目"里的最大概率与毫米和。

// hourlySlotLayout 是响应 hourly.time[] 的格式：
// iso8601 且**无时区标记**（请求带 timezone=auto ⇒ 上游按当地时区给墙钟时间），
// 故解析结果按当地墙钟理解，与 Sample.UTCOffsetSeconds 换算出的 local 直接比较。
const hourlySlotLayout = "2006-01-02T15:04"

// WindowProb 算"从现在起未来 hours 个逐小时条目"降雨概率与毫米和。
//
// nowLocal = **当地**时间（调用方按 Sample.UTCOffsetSeconds 换算：
// local := time.Now().UTC().Add(time.Duration(off) * time.Second)。
//
// 口径（逐条）：
// - 取"当前小时尚未走完"条目，按时间升序取前 hours 个；
// - prob = 这些条目 precipitation_probability 的最大值，四舍五入取整；
// - mm  = 这些条目 precipitation 的求和，保留 1 位小数；
// - 可用条目不足 hours 个 ⇒ 用全部可用条目（末日截断，不报错）；
// - 无可用条目 ⇒ ok=false（预警不显示）。
//
// **条目入选口径 = slot >= nowLocal**（测试 1 第 2 条给的
// 硬数字是 now=07:00/hours=3 ⇒ prob=57，只有把 07:00 那一格算进窗口才成立：
// 07,08,09 = 57,57,57；若按算法原文的 `slot > nowLocal` 取 08,09,10 = 57,57,79 ⇒ 79。
// 两处口径互相冲突，这里取**能满足实测数字**的那一条，
// 冲突已记入工程记录。逐小时条目代表"该小时"，
// 整点时刻当前小时还没走完 ⇒ 算在"接下来"里；已过整点（如 20:45） ⇒ 20:00 那格不算。
func WindowProb(s *Sample, nowLocal time.Time, hours int) (prob int, mm float64, ok bool) {
	if s == nil || hours < 1 {
		return 0, 0, false
	}
	type slot struct {
		at   time.Time
		prob float64
		mm   float64
	}
	slots := make([]slot, 0, len(s.HourlyTime))
	for i, ts := range s.HourlyTime {
		// 条目串按 16 字符（"2006-01-02T15:04"）截断再解析：
		// 上游偶尔带 ":00" 秒位，截断后两种形态都能解析。
		if len(ts) < 16 {
			continue
		}
		at, err := time.Parse(hourlySlotLayout, ts[:16])
		if err != nil {
			continue
		}
		if at.Before(nowLocal) {
			continue // 该小时已走完 ⇒ 不进窗口
		}
		var p, m float64
		if i < len(s.HourlyProb) {
			p = s.HourlyProb[i]
		}
		if i < len(s.HourlyMM) {
			m = s.HourlyMM[i]
		}
		slots = append(slots, slot{at: at, prob: p, mm: m})
	}
	if len(slots) == 0 {
		return 0, 0, false
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].at.Before(slots[j].at) })
	n := hours
	if n > len(slots) {
		n = len(slots) // 末日截断：用全部可用条目
	}
	maxProb := slots[0].prob
	sum := 0.0
	for _, sl := range slots[:n] {
		if sl.prob > maxProb {
			maxProb = sl.prob
		}
		sum += sl.mm
	}
	return int(math.Round(maxProb)), math.Round(sum*10) / 10, true
}

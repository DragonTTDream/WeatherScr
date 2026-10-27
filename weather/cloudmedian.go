package weather

import (
	"sort"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// 三模型云量仲裁。
// 实测形态（本机直连 Open-Meteo，12:45 本地时刻）：请求带
// `&models=gfs_seamless,icon_seamless,ecmwf_ifs025` 时，hourly 里出现**逐模型键**
// cloud_cover_gfs_seamless / cloud_cover_icon_seamless / cloud_cover_ecmwf_ifs025
// （各自一条数组，与 time 对齐）；current 只返回一个合并对象，不能拿它做多模型。
// 实测数值（同一时刻、同一坐标）：
//	best_match = 天气码 3 / 云量 100%
//	gfs_seamless = 3 / 100%
//	icon_seamless = 2 / 62%
//	ecmwf_ifs025 = 2 / 56%
// ⇒ 码映射没有错，是默认源取到了最阴的那个模型：单源口径下 100% 云量判成阴，
// 而三家的中位数 62% 判成多云见日。本文件就是把档位从"单源"换成"三家中位数"。

// 逐模型云量在 hourly 响应里的键名。
const (
	CloudKeyGFS   = "cloud_cover_gfs_seamless"
	CloudKeyIcon  = "cloud_cover_icon_seamless"
	CloudKeyECMWF = "cloud_cover_ecmwf_ifs025"

	// ModelsParam = 请求里 models= 的取值（逐字三家）。
	ModelsParam = "gfs_seamless,icon_seamless,ecmwf_ifs025"
)

// MedianCloud 若干家云量的中位数。
// 口径：nil 的剔除（缺一个模型就用剩下两家），全缺 ⇒ nil（调用方退回天气码）。
// 偶数个值取中间两个的平均 —— 与 ui 包 medianF 的口径一致。
func MedianCloud(vals ...*int) *int {
	v := make([]int, 0, len(vals))
	for _, p := range vals {
		if p != nil {
			v = append(v, *p)
		}
	}
	if len(v) == 0 {
		return nil
	}
	sort.Ints(v)
	var m float64
	if len(v)%2 == 1 {
		m = float64(v[len(v)/2])
	} else {
		m = float64(v[len(v)/2-1]+v[len(v)/2]) / 2.0
	}
	r := int(m + 0.5)
	return &r
}

// MedianFromModels 从三条逐模型数组取当前下标处的三家云量，再取中位数。
// 下标越界 / 数组长度不齐 ⇒ 该家按缺失处理（不 panic、不猜）。
func MedianFromModels(gfs, icon, ecmwf []int, idx int) *int {
	pick := func(a []int) *int {
		if idx < 0 || idx >= len(a) {
			return nil
		}
		v := a[idx]
		return &v
	}
	return MedianCloud(pick(gfs), pick(icon), pick(ecmwf))
}

// CurrentHourIndex 在 hourly 的 time 数组里找"当前时刻"的下标。
// hourly.time 是 iso8601 **当地墙钟**（请求带 timezone=auto ⇒ 响应无时区标记），
// 形如 "T12:00"。当前时刻 = UTC 现在 + utc_offset_seconds，
// 截到小时，再按同一格式比对。取不到 ⇒ -1（调用方按缺失处理）。
func CurrentHourIndex(times []string, offsetSeconds int, now time.Time) int {
	if len(times) == 0 {
		return -1
	}
	local := now.UTC().Add(time.Duration(offsetSeconds) * time.Second)
	key := local.Format("2006-01-02T15:00")
	for i, s := range times {
		if s == key {
			return i
		}
	}
	return -1
}

// ClassFromCloud 天空档位由**三家中位数云量**决定：
//
//	<30 → Clear
//	30–70 → PartlyCloudy
//	>70 → Overcast
//
// 两条不许越界：
//
//	1 降水类（雨/雪/雷/雾/尘）仍由天气码决定，云量不得覆盖它
//	 （95% 的云量不改变"在下雨"这件事）；
//	2 中位数缺失（三家都没返回 / 下标取不到）⇒ 退回现有天气码判定，
//	 不 panic、不让画面变空白。
func ClassFromCloud(code int, median *int) (effect.Class, effect.Intensity) {
	c, i := effect.Classify(code)
	switch c {
	case effect.Clear, effect.Cloudy, effect.PartlyCloudy, effect.Overcast:
		// 非降水区：交给中位数
	default:
		return c, i
	}
	if median == nil {
		return c, i
	}
	switch v := *median; {
	case v < 30:
		return effect.Clear, i
	case v <= 70:
		return effect.PartlyCloudy, i
	default:
		return effect.Overcast, i
	}
}

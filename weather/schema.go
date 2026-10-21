package weather

import (
	"encoding/json"
	"os"
	"strings"
	"unicode"
)

// Sample 是落盘 cache/weather.json 的逐字段复刻（10 个字段，golden 口径）。
//
// 指针字段表示上游可为 null 的量（humidity / precipitation_probability 在上游
// 初始化即 None，取不到时保持 null，不得填 0 冒充。
type Sample struct {
	Temperature              *float64 `json:"temperature"`
	Condition                string   `json:"condition"`
	Location                 string   `json:"location"`
	Humidity                 *float64 `json:"humidity"`
	PrecipitationProbability *float64 `json:"precipitation_probability"`
	WindSpeed                *float64 `json:"windspeed"`
	Forecast                 *string  `json:"forecast"`
	IsDay                    int      `json:"is_day"`
	WeatherCode              int      `json:"weather_code"`
	// CloudCover = Open-Meteo current.cloud_cover（百分比）。
	// 定案的动画分档口径用它，WMO 码只作降水/雾的判据。
	CloudCover *int `json:"cloud_cover"`
	// CloudCoverPct = 云量百分比（0–100），**逐时线性插值到"现在"这一时刻**。
	// 口径（D1 第 3 条）：hourly.cloud_cover 是整点阶梯，整点交界处会跳变，
	// 而太阳可见度是逐帧画出来的量 ⇒ 主源取插值（InterpCloudPct）。
	// current.cloud_cover 只用于交叉校验与日志，不当主源。
	// 回落：响应里没有 cloud_cover ⇒ 按码推（cloudPctFromCode）并打 CLOUD_FALLBACK。
	// 越界一律钳位 [0,100]。
	CloudCoverPct int `json:"cloud_cover_pct,omitempty"`
	// CloudMedian = 当前时刻三家模型云量（gfs_seamless / icon_seamless /
	// ecmwf_ifs025）的**中位数**（规格 1.6）。
	// 天空档位由它判：&lt;30 晴、30–70 多云见日、&gt;70 阴；降水类仍由天气码判。
	// nil = 三家都没给 / 当前小时取不到 ⇒ 档位退回 CloudCover 与天气码。
	CloudMedian *int `json:"cloud_median,omitempty"`
	// CloudHourIndex = 取中位数时用的 hourly 下标（诊断用，-1 = 取不到）。
	CloudHourIndex int `json:"cloud_hour_index,omitempty"`
	// TodayPrecipProbMax = 今日最大降雨概率（%，daily.precipitation_probability_max[0]）。
	// 降雨提醒的触发线数据源（官方"有机会出现降水"下限 30%）。
	TodayPrecipProbMax *int `json:"today_precip_prob_max"`
	// TodayPrecipMM = 今日累计降雨量（mm，daily.precipitation_sum[0]）；<=0 时不填。
	TodayPrecipMM *float64 `json:"today_precip_mm"`
	// MoonPhase = 今日月相（daily.moon_phase[0]，单位 fraction，0–1）。
	// 实测（2026-10-16 逐字段实测）：
	// daily=moon_phase 接受，实测值 [0.174, 0.205]（2026-10-16 / 17）；
	// moon_illumination / lunar_illumination / lunar_phase / illumination /
	// moon_altitude / moon_azimuth 全部拒绝；/v1/astronomy 端点 404 不存在
	// ⇒ 照度只能自己算（见 ui.moonIllumination）。
	// 取 daily 数组第一个值 = 今天；带 models= 的响应里 daily 只有带模型后缀的
	// moon_phase_* 键（见 provider.go 的 Daily 段），填值顺序 = 裸键 → 三家后缀键。
	// nil = 取不到 ⇒ **不画月亮**（与新月一致，规格 2.3）。
	MoonPhase *float64 `json:"moon_phase"`
	// ---- 未来 N 小时滚动窗口的原料（逐小时数据 + 时基）----
	// 三条**平行数组**，从响应 hourly 原样存（不排序、不去空、不补齐）：
	// HourlyTime = iso8601 **当地墙钟**（请求带 timezone=auto ⇒ 响应无时区标记），
	// HourlyProb = precipitation_probability（%），
	// HourlyMM   = precipitation（mm）。
	// 实测 forecast_days=1 → 24 条、=2 → 48 条（需求 1）。
	HourlyTime []string  `json:"hourly_time,omitempty"`
	HourlyProb []float64 `json:"hourly_prob,omitempty"`
	HourlyMM   []float64 `json:"hourly_mm,omitempty"`
	// ---- b-alert-v1 第 2/3/4 步：逐模型中位数的产物与数据闸门计数 ----
	// ProbN = hourly 里"至少一家模型给出概率值"的格数（三模型全缺的格不计）。
	// MMN   = 同口径的毫米格数。Min15N = 15 分钟层解析出的格数。
	// 第 4 步的数据闸门：ProbN < 视野小时数 ⇒ 置「数据不可用」态；
	// alertdiag 自检打印 HOURLY_SLOTS / PROB_N / MM_N / MIN15_N 用的就是这三条。
	ProbN  int `json:"prob_n,omitempty"`
	MMN    int `json:"mm_n,omitempty"`
	Min15N int `json:"min15_n,omitempty"`
	// 15 分钟层（第 3 步）：三条平行数组，键 time / precipitation / precipitation_probability
	// 的逐模型中位数产物，与 Min15Time 对齐（每格 15 分钟）。
	Min15Time []string  `json:"min15_time,omitempty"`
	Min15Prob []float64 `json:"min15_prob,omitempty"`
	Min15MM   []float64 `json:"min15_mm,omitempty"`
	// UTCOffsetSeconds = 响应顶层 utc_offset_seconds（秒，当地相对 UTC 的偏移）。
	// "现在"要按当地时间算：local := time.Now().UTC().Add(off 秒)。
	UTCOffsetSeconds int     `json:"utc_offset_seconds,omitempty"`
	CachedAt         float64 `json:"_cached_at"`
}

// Cache 是落盘文件的整体形状：位置 key → Sample。
type Cache map[string]Sample

// NormalizeLocationKey 复刻上游 normalize_weather_location_key：折叠连续空白为单空格
// + 首尾去空白 + casefold。
func NormalizeLocationKey(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}

// LoadCache 读落盘缓存；文件缺失/损坏返回空缓存而非报错
// （上游语义：缓存是 last-good evidence，读不到就当作没有，不得因读失败而崩）。
func LoadCache(path string) Cache {
	c := Cache{}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Cache{}
	}
	return c
}

// SaveCache 写落盘缓存（缩进 4 空格，与上游实况文件一致）。
func SaveCache(path string, c Cache) error {
	b, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

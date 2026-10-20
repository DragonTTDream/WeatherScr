package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	GeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	WeatherURL   = "https://api.open-meteo.com/v1/forecast"

	// CacheTTLSeconds = 上游 _WEATHER_CACHE_TTL_SECONDS = 1800（30 分钟）
	CacheTTLSeconds = 1800

	// 上游 params 逐字（open_meteo_provider.py get_current_weather）
	// + **cloud_cover**：本项目独有的一个字段。
	// 依据：上游天气码只喂图标与文字、背景动画由设置里选场景；
	// 本项目的"背景随天气变"是产品需求，口径 = **按逐小时云量分档**
	// （WMO 码在低云量区判"主要晴"，与国内天气应用同刻的"多云"观感落差大：
	// 杭州实测 code=1 而云量 26%。
	//
	// + **precipitation_probability**（需求 1，2026-10-16
	// "保持当前时刻"：上游 open_meteo_provider.py:366-368 的注释说 current 块里
	// 没有这个字段、只能取 hourly 第一个值；**本次实测推翻该注释**
	// （坐标 30.2495,120.1583 实测
	// `current.precipitation_probability = 0` 且字段被接受 ⇒ 伞图标改取 current。
	currentFields = "temperature_2m,weather_code,wind_speed_10m,is_day,relative_humidity_2m,precipitation,rain,cloud_cover,precipitation_probability"
	// + **precipitation**（mm）：预警改"未来 N 小时
	// 滚动窗口"需要逐小时毫米数求和；上游 hourly 只有 precipitation_probability
	// （open_meteo_provider.py:331），实测 `hourly=precipitation_probability,precipitation`
	// 接受，units `%` 与 `mm`（需求 1）。
	hourlyFields = "precipitation_probability,precipitation,cloud_cover"
	// cloud_cover 是规格 1.6 加的第三项：三模型云量仲裁的原料。
	// 实测：与 `models=gfs_seamless,icon_seamless,ecmwf_ifs025` 同时给出时，hourly 里
	// 出现逐模型键 cloud_cover_gfs_seamless / cloud_cover_icon_seamless /
	// cloud_cover_ecmwf_ifs025（各自一条数组，与 time 对齐）；
	// current 只返回一个合并对象，不能拿它做多模型。
	// dailyFields 在上游三键之外多取三键（与 cloud_cover 同类偏离）：
	// precipitation_probability_max = 今日最大降雨概率（提醒触发线用它），
	// precipitation_sum = 今日预计降雨量 mm（提醒正文用它），
	// moon_phase = 今日月相（月相背景用它）。
	// 依据：上游无任何预警功能（grep 到的 warning 全是 Steam/音频日志），
	// 上游 daily 只有 temperature_2m_max/min + weathercode（open_meteo_provider.py:332）。
	// moon_phase 实测接受（daily=moon_phase 接受，
	// 单位 fraction，实测 [0.174, 0.205]；moon_illumination 等全部拒绝。
	dailyFields = "temperature_2m_max,temperature_2m_min,weathercode,precipitation_probability_max,precipitation_sum,moon_phase"
	// forecastDays ≥2 是"未来 N 小时滚动窗口"硬要求：
	// 23:xx 的 3/6 小时窗口必然跨午夜，只有 1 天数据时窗口在当日 23:00 之后无条目。
	// 实测 forecast_days=1 → 24 条、=2 → 48 条（2026-10-16T00:00 … 2026-10-17T23:00），
	// 跨午夜窗口有覆盖（需求 1）。现值 6 已满足 ≥2。
	forecastDays  = "6"
	forecastHours = "1"
)

// RequestParams 构造 forecast 请求的参数集，逐键与上游一致（RequestShape 判据）。
func RequestParams(lat, lon float64) url.Values {
	v := url.Values{}
	v.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	v.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	v.Set("current", currentFields)
	v.Set("hourly", hourlyFields)
	// 三模型云量（规格 1.6）：models= 逐字三家，与 hourly= 里的 cloud_cover 配对。
	v.Set("models", ModelsParam)
	v.Set("daily", dailyFields)
	v.Set("forecast_days", forecastDays)
	v.Set("forecast_hours", forecastHours)
	v.Set("timezone", "auto")
	return v
}

// GeocodeParams 构造 geocoding 请求的参数集，逐键与上游一致。
func GeocodeParams(city string) url.Values {
	v := url.Values{}
	v.Set("name", city)
	v.Set("count", "1")
	v.Set("language", "en")
	v.Set("format", "json")
	return v
}

type geoResponse struct {
	Results []struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
		Country   string  `json:"country"`
	} `json:"results"`
}

type forecastResponse struct {
	Current struct {
		Temperature2m      *float64 `json:"temperature_2m"`
		WeatherCode        int      `json:"weather_code"`
		CloudCover         *int     `json:"cloud_cover"`
		WindSpeed10m       *float64 `json:"wind_speed_10m"`
		IsDay              int      `json:"is_day"`
		RelativeHumidity2m *float64 `json:"relative_humidity_2m"`
		Precipitation      *float64 `json:"precipitation"`
		Rain               *float64 `json:"rain"`
		// 伞图标的数据源（需求 1）。
		// 指针 = 上游不给该字段时保持 nil ⇒ Fetch 回落 hourly[0]（旧行为，不空）。
		// 实测该字段被接受（需求 4）。
		PrecipitationProbability *float64 `json:"precipitation_probability"`
	} `json:"current"`
	Hourly struct {
		PrecipitationProbability []float64 `json:"precipitation_probability"`
		// 逐小时条目：
		// time 是 iso8601 **当地墙钟**（请求带 timezone=auto ⇒ 响应不带时区标记），
		// precipitation 单位 mm。平行数组，原样存进 Sample 供 WindowProb 用。
		Time          []string  `json:"time"`
		Precipitation []float64 `json:"precipitation"`
		// 逐模型云量（规格 1.6，%）：请求带 models= 时 Open-Meteo 才给的三条数组，
		// 与 Time 对齐。键名逐字照抄响应（CloudKeyGFS 等三个常量）。
		// 请求没带 models=、或该模型不覆盖本地 ⇒ 键不存在 ⇒ 解成 nil ⇒ 中位数按缺失算。
		CloudCover      []int `json:"cloud_cover"`
		CloudCoverGFS   []int `json:"cloud_cover_gfs_seamless"`
		CloudCoverIcon  []int `json:"cloud_cover_icon_seamless"`
		CloudCoverECMWF []int `json:"cloud_cover_ecmwf_ifs025"`
	} `json:"hourly"`
	Daily struct {
		Time             []string  `json:"time"`
		Temperature2mMax []float64 `json:"temperature_2m_max"`
		Temperature2mMin []float64 `json:"temperature_2m_min"`
		Weathercode      []int     `json:"weathercode"`
		// 今日最大降雨概率（%）与今日累计降雨量（mm），实测
		PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
		PrecipitationSum            []float64 `json:"precipitation_sum"`
		// 今日月相（fraction，0–1），实测
		//
		// 请求带 models=（三模型云量，规格 1.6）时，Open-Meteo 把 daily 里
		// **每个逐模型字段都按模型后缀各给一条数组**，实测该请求的 daily 键 =
		// [time, temperature_2m_max_gfs_seamless, …, moon_phase_gfs_seamless,
		//  moon_phase_icon_seamless, moon_phase_ecmwf_ifs025, …]，
		// **没有裸 moon_phase 键**（证据 E-MOON-KEYS：weather/moonprobe2_test.go
		// 打印 DAILY_KEYS 与 DAILY_MOON=<nil>）。
		// 只解裸键 ⇒ 数组空 ⇒ Sample.MoonPhase = nil（证据 E-MOON-NIL）。
		// 三条后缀键 = 本批补的解析口径；裸键保留 = 请求不带 models= 时的旧口径。
		MoonPhase []float64 `json:"moon_phase"`
		// 带模型后缀的三条月相数组（键名逐字照抄响应，与 CloudCoverGFS 同一族）。
		MoonPhaseGFS   []float64 `json:"moon_phase_gfs_seamless"`
		MoonPhaseIcon  []float64 `json:"moon_phase_icon_seamless"`
		MoonPhaseECMWF []float64 `json:"moon_phase_ecmwf_ifs025"`
	} `json:"daily"`
	// 响应顶层 utc_offset_seconds：
	// 滚动窗口的"现在"要按**当地**时间算，必须拿到这个偏移。
	UTCOffsetSeconds int `json:"utc_offset_seconds"`
}

// Client 是 Open-Meteo 客户端。零值可用（用 10 秒超时，对齐上游 timeout=10）。
//
// BaseURL 非空时改走自建代理（低性能 x86 小主机无 DNS/无外网，只能走 IP 字面量
// 的本机代理。代理镜像 Open-Meteo 的路径：/v1/search 与 /v1/forecast。
type Client struct {
	HTTP    *http.Client
	BaseURL string

	// coordsMu/coords = 城市 → (lat, lon) 缓存。
	// 对齐上游 weather/open_meteo_provider.py:112
	// `self._cached_coords: Dict[str, Tuple[float, float]]  # City → (lat, lon)`。
	// 原先没有 ⇒ 每次刷新 2 个 HTTPS 请求（地理编码 + 预报），上游只有 1 个。
	coordsMu sync.Mutex
	coords   map[string]geoCoord

	// HasCoords/Lat/Lon = 设置里"搜索选点"存下来的坐标
	// "存坐标 + 搜索选点"。非零时 Fetch **直接用它们、零地理编码请求**：
	// 实测一次地理编码平均 1212 ms（最大 3791 ms），预报只要 476 ms
	// ⇒ 存坐标把每次刷新的请求数从 2 降到 1、延迟砍掉一大截。
	// 未存坐标时回落到 Geocode(city)（上游口径，兼容旧配置）。
	HasCoords bool
	Lat       float64
	Lon       float64
}

type geoCoord struct{ lat, lon float64 }

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// geoURL / wxURL：直连或代理，取决于 BaseURL。
func (c *Client) geoURL() string {
	if c.BaseURL == "" {
		return GeocodingURL
	}
	return strings.TrimRight(c.BaseURL, "/") + "/v1/search"
}

func (c *Client) wxURL() string {
	if c.BaseURL == "" {
		return WeatherURL
	}
	return strings.TrimRight(c.BaseURL, "/") + "/v1/forecast"
}

func (c *Client) get(u string, params url.Values) ([]byte, error) {
	full := u + "?" + params.Encode()
	resp, err := c.client().Get(full)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo: %s 返回 %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 响应失败: %w", u, err)
	}
	return body, nil
}

// Geocode 城市 → 经纬度（上游 geocode：count=1 取第一条）。
func (c *Client) Geocode(city string) (lat, lon float64, err error) {
	c.coordsMu.Lock()
	if g, ok := c.coords[city]; ok {
		c.coordsMu.Unlock()
		return g.lat, g.lon, nil
	}
	c.coordsMu.Unlock()
	body, err := c.get(c.geoURL(), GeocodeParams(city))
	if err != nil {
		return 0, 0, err
	}
	var g geoResponse
	if err := json.Unmarshal(body, &g); err != nil {
		return 0, 0, fmt.Errorf("解析 geocoding 响应失败: %w", err)
	}
	if len(g.Results) == 0 {
		return 0, 0, fmt.Errorf("城市无 geocoding 结果: %s", city)
	}
	lat, lon = g.Results[0].Latitude, g.Results[0].Longitude
	c.coordsMu.Lock()
	if c.coords == nil {
		c.coords = make(map[string]geoCoord)
	}
	c.coords[city] = geoCoord{lat, lon}
	c.coordsMu.Unlock()
	return lat, lon, nil
}

// Fetch 取一次当前天气，产出落盘 schema 的 Sample（十字段）。
//
// 降水概率口径（需求 2、3：保持当前时刻）：
// - 上游 open_meteo_provider.py:366-371 取的是 **hourly 第一个值**（当天 00:00 那一格），
// 其注释称"precipitation_probability 只存在于 hourly，current 块里没有该字段"。
// 上游取 hourly[0] 是**上游行为**，本复刻不再照抄。
// - 实测（需求 4，坐标 30.2495,120.1583）：
// `current=precipitation_probability` **是被接受的字段**，实测
// `current.precipitation_probability = 0` ⇒ 上游那句注释不成立。
// - 故本复刻取 current（= 当前时刻）；**仅当 current 为 nil** 时回落 hourly[0]
// （保持旧行为、不空）。
func (c *Client) Fetch(city string) (*Sample, error) {
	var lat, lon float64
	if c.HasCoords {
		// 已存坐标 ⇒ 零地理编码请求
		lat, lon = c.Lat, c.Lon
	} else {
		var err error
		lat, lon, err = c.Geocode(city)
		if err != nil {
			return nil, err
		}
	}
	body, err := c.get(c.wxURL(), RequestParams(lat, lon))
	if err != nil {
		return nil, err
	}
	var f forecastResponse
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("解析 forecast 响应失败: %w", err)
	}

	s := &Sample{
		Location:    city,
		Temperature: f.Current.Temperature2m,
		Condition:   Condition(f.Current.WeatherCode),
		WeatherCode: f.Current.WeatherCode,
		CloudCover:  f.Current.CloudCover,
		IsDay:       f.Current.IsDay,
		WindSpeed:   f.Current.WindSpeed10m,
		Humidity:    f.Current.RelativeHumidity2m,
		Forecast:    tomorrowText(f.Daily.Temperature2mMin, f.Daily.Temperature2mMax, f.Daily.Weathercode),
		CachedAt:    float64(time.Now().UnixNano()) / 1e9,
	}
	// 伞图标 / 详情行降水项的数据源 = **当前时刻**（需求 2）。
	// current 为 nil（上游不给该字段） ⇒ 回落 hourly[0]，即上游 open_meteo_provider.py:369-371
	// 的旧行为，保证不空。
	if f.Current.PrecipitationProbability != nil {
		v := *f.Current.PrecipitationProbability
		s.PrecipitationProbability = &v
	} else if len(f.Hourly.PrecipitationProbability) > 0 {
		v := f.Hourly.PrecipitationProbability[0]
		s.PrecipitationProbability = &v
	}
	// 逐小时平行数组原样存：
	// WindowProb 靠这三条数组 + utc_offset_seconds 算"未来 N 小时滚动窗口"。
	s.HourlyTime = f.Hourly.Time
	s.HourlyProb = f.Hourly.PrecipitationProbability
	s.HourlyMM = f.Hourly.Precipitation
	s.UTCOffsetSeconds = f.UTCOffsetSeconds
	// 今日（daily 下标 0）最大降雨概率与累计降雨量。
	// 概率取整存 int；降雨量 <=0 视为"无降雨"，不填。
	// **不再是预警的数据源**：把预警数据源换成
	// WindowProb（未来 N 小时滚动窗口）；这两项保留 = 旧缓存/预览图（cmd/fxshot）
	// 仍在用，删掉会让那些路径变空。
	if len(f.Daily.PrecipitationProbabilityMax) > 0 {
		v := int(f.Daily.PrecipitationProbabilityMax[0] + 0.5)
		s.TodayPrecipProbMax = &v
	}
	if len(f.Daily.PrecipitationSum) > 0 {
		if v := f.Daily.PrecipitationSum[0]; v > 0 {
			mm := v
			s.TodayPrecipMM = &mm
		}
	}
	// 今日月相（daily 下标 0 = 今天）。
	// 填值顺序 = 裸键 → 三家后缀键里第一个非空数组的 [0]（证据 E-MOON-KEYS：
	// 带 models= 的响应里只有后缀键、没有裸键）；值域外（<0 或 >1）视为无效。
	// 取不到 ⇒ 保持 nil ⇒ 绘制端不画月亮（规格 D5/D7 第 2.3 条）。
	if v, ok := pickMoonPhase(f.Daily.MoonPhase, f.Daily.MoonPhaseGFS,
		f.Daily.MoonPhaseIcon, f.Daily.MoonPhaseECMWF); ok {
		val := v
		s.MoonPhase = &val
		fmt.Printf("MOONPHASE=%.4f\n", val)
	}
	// 三模型云量的中位数（规格 1.6）：当前时刻那一条上三家各取一个值。
	// 下标取不到（hourly.time 里没有当前小时）/ 三家全缺 ⇒ nil ⇒
	// 档位退回 current.cloud_cover 与天气码的既有判定，不空不崩。
	idx := CurrentHourIndex(f.Hourly.Time, f.UTCOffsetSeconds, time.Now())
	s.CloudHourIndex = idx
	if m := MedianFromModels(f.Hourly.CloudCoverGFS, f.Hourly.CloudCoverIcon,
		f.Hourly.CloudCoverECMWF, idx); m != nil {
		s.CloudMedian = m
	}
	// 云量（D1 第 1–5 条）：主源 = 逐时 cloud_cover 在"现在"的**线性插值**，
	// current.cloud_cover 只用于交叉校验与日志；响应里没有该字段 ⇒ 按码推回落，
	// 并打 CLOUD_FALLBACK=code=N pct=M。三条路径出来的值一律钳位 [0,100]。
	pct, ok := InterpCloudPct(f.Hourly.Time, f.Hourly.CloudCover, f.UTCOffsetSeconds, time.Now())
	curStr := "none"
	if f.Current.CloudCover != nil {
		curStr = strconv.Itoa(*f.Current.CloudCover)
	}
	src := "hourly_interp"
	switch {
	case ok:
	case f.Current.CloudCover != nil:
		// 逐时数组缺失/解不开，current 有值 ⇒ 用 current（旧口径，不空）
		pct, src = *f.Current.CloudCover, "current"
	default:
		// 两边都没有 ⇒ 按码推
		pct, src = cloudPctFromCode(f.Current.WeatherCode), "code_fallback"
		fmt.Printf("CLOUD_FALLBACK=code=%d pct=%d\n", f.Current.WeatherCode, clampCloudPct(pct))
	}
	s.CloudCoverPct = clampCloudPct(pct)
	CloudPctLog(s.CloudCoverPct, src, f.Current.WeatherCode, curStr)
	return s, nil
}

// pickMoonPhase 按填值顺序取今日月相：裸键 → 三家后缀键里第一个非空数组的 [0]。
//
// 值域外（<0 或 >1）视为无效，继续往下一家找；全都没有 ⇒ false（调用方保持 nil）。
// 顺序逐字照规格 2.2：裸键优先（请求不带 models= 时的旧口径），
// 后缀键按 gfs_seamless → icon_seamless → ecmwf_ifs025（与请求里 models= 的次序一致）。
func pickMoonPhase(bare, gfs, icon, ecmwf []float64) (float64, bool) {
	for _, arr := range [][]float64{bare, gfs, icon, ecmwf} {
		if len(arr) == 0 {
			continue
		}
		v := arr[0]
		if v < 0 || v > 1 {
			continue
		}
		return v, true
	}
	return 0, false
}

// tomorrowText 复刻上游明日行文案：
// f"Tomorrow: {min:.0f}°-{max:.0f}°C/{WEATHER_CODES[codes[1]].title()}"
// 注意 `°C/` 之间无空格（逐日行是 `°C / ` 有空格，两者格式不同，不得统一）。
func tomorrowText(mins, maxs []float64, codes []int) *string {
	avail := len(mins)
	if len(maxs) < avail {
		avail = len(maxs)
	}
	if len(codes) < avail {
		avail = len(codes)
	}
	if avail <= 1 {
		return nil
	}
	t := fmt.Sprintf("Tomorrow: %.0f°-%.0f°C/%s", mins[1], maxs[1], titleCase(Condition(codes[1])))
	return &t
}

// titleCase 对齐 Python str.title() 在本用途下的行为：词首字母大写、余下小写，
// 词边界为空格 / '-' / '/'。
func titleCase(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		if r == ' ' || r == '-' || r == '/' {
			b.WriteRune(r)
			start = true
			continue
		}
		if start {
			b.WriteRune(unicode.ToUpper(r))
			start = false
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

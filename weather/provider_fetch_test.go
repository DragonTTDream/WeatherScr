package weather

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Fetch 的伞图标口径测试（需求 5）：
//
//	Current.PrecipitationProbability=42 且 Hourly[0]=16 ⇒ 取 42（当前时刻）
//	Current 为 nil 且 Hourly[0]=16 ⇒ 回落 16（旧行为，不空）
//
// 用 httptest 起一个本地 forecast 端点（Client.BaseURL 走代理口径，
// 见 provider.go 的 wxURL，零外网依赖。

func forecastBody(currentBlock string) string {
	return `{
  "latitude": 30.2495, "longitude": 120.1583,
  "utc_offset_seconds": 28800,
  "timezone": "Asia/Shanghai",
  "current": ` + currentBlock + `,
  "hourly": {
    "time": ["2026-10-16T00:00", "2026-10-16T01:00", "2026-10-16T02:00"],
    "precipitation_probability": [16, 56, 56],
    "precipitation": [0.0, 0.5, 0.5]
  },
  "daily": {
    "time": ["2026-10-16", "2026-10-17"],
    "temperature_2m_max": [20.0, 19.0],
    "temperature_2m_min": [15.0, 14.0],
    "weathercode": [61, 63],
    "precipitation_probability_max": [88, 70],
    "precipitation_sum": [2.3, 1.1],
    "moon_phase": [0.174, 0.205]
  }
}`
}

func fetchFrom(t *testing.T, currentBlock string, query *url.Values) *Sample {
	t.Helper()
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/search") {
			w.Write([]byte(`{"results":[]}`))
			return
		}
		got = r.URL.Query()
		w.Write([]byte(forecastBody(currentBlock)))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HasCoords: true, Lat: 30.2495, Lon: 120.1583}
	s, err := c.Fetch("Hangzhou, China")
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	if query != nil {
		*query = got
	}
	return s
}

// TestFetchUmbrellaUsesCurrent 断言伞图标口径 = 当前时刻（需求 2、5）。
func TestFetchUmbrellaUsesCurrent(t *testing.T) {
	var q url.Values
	s := fetchFrom(t, `{"temperature_2m":18.3,"weather_code":61,"wind_speed_10m":4.2,
		"is_day":1,"relative_humidity_2m":77,"precipitation":0.0,"rain":0.0,
		"cloud_cover":80,"precipitation_probability":42}`, &q)
	if s.PrecipitationProbability == nil {
		t.Fatalf("降水概率不应为 nil")
	}
	if v := *s.PrecipitationProbability; v != 42 {
		t.Errorf("降水概率 = %v，期望 current 的 42（当前时刻），不是 hourly[0] 的 16", v)
	}
	// 请求里 current 与 hourly 都带上了新字段（需求 1）
	if c := q.Get("current"); !strings.HasSuffix(c, "precipitation_probability") {
		t.Errorf("current 参数未追加 precipitation_probability：%q", c)
	}
	if h := q.Get("hourly"); h != "precipitation_probability,precipitation,cloud_cover" {
		t.Errorf("hourly 参数 = %q，期望 precipitation_probability,precipitation,cloud_cover", h)
	}
	if m := q.Get("models"); m != ModelsParam {
		t.Errorf("models 参数 = %q，期望 %q（三模型云量的开关）", m, ModelsParam)
	}
	// 逐小时平行数组 + 时基
	if len(s.HourlyTime) != 3 || len(s.HourlyProb) != 3 || len(s.HourlyMM) != 3 {
		t.Errorf("逐小时平行数组长度 = %d/%d/%d，期望 3/3/3",
			len(s.HourlyTime), len(s.HourlyProb), len(s.HourlyMM))
	}
	if s.UTCOffsetSeconds != 28800 {
		t.Errorf("UTCOffsetSeconds = %d，期望 28800", s.UTCOffsetSeconds)
	}
	if s.HourlyMM[1] != 0.5 {
		t.Errorf("HourlyMM[1] = %v，期望 0.5", s.HourlyMM[1])
	}
}

// TestFetchUmbrellaFallsBackToHourly0 断言 current 为 nil 时回落 hourly[0]
// （需求 2：保持旧行为，不空）。
func TestFetchUmbrellaFallsBackToHourly0(t *testing.T) {
	s := fetchFrom(t, `{"temperature_2m":18.3,"weather_code":61,"wind_speed_10m":4.2,
		"is_day":1,"relative_humidity_2m":77,"precipitation":0.0,"rain":0.0,
		"cloud_cover":80}`, nil)
	if s.PrecipitationProbability == nil {
		t.Fatalf("回落时降水概率不应为 nil")
	}
	if v := *s.PrecipitationProbability; v != 16 {
		t.Errorf("降水概率 = %v，期望回落 hourly[0] 的 16", v)
	}
}

// TestWindowProbUsesFetchedSample 把 Fetch 的产物直接喂进 WindowProb，
// 实测"时基 = 当地"这条链路（local := UTC + utc_offset_seconds）。
func TestWindowProbUsesFetchedSample(t *testing.T) {
	s := fetchFrom(t, `{"temperature_2m":18.3,"weather_code":61,"wind_speed_10m":4.2,
		"is_day":1,"relative_humidity_2m":77,"precipitation":0.0,"rain":0.0,
		"cloud_cover":80,"precipitation_probability":42}`, nil)
	// "现在" = 当地 2026-10-16T00:30（= UTC 2026-10-15T16:30 + 28800 s）
	nowUTC := time.Date(2026, 10, 15, 16, 30, 0, 0, time.UTC)
	local := nowUTC.UTC().Add(time.Duration(s.UTCOffsetSeconds) * time.Second)
	prob, mm, ok := WindowProb(s, local, 2)
	if !ok {
		t.Fatalf("当地 00:30 应有未来条目")
	}
	if prob != 56 {
		t.Errorf("prob = %d，期望 56（01:00,02:00 两格最大值）", prob)
	}
	if math.Abs(mm-1.0) > 1e-9 {
		t.Errorf("mm = %v，期望 1.0（0.5+0.5）", mm)
	}
}

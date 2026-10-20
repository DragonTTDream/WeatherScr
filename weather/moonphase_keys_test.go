package weather

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 月相数据修复（规格 D5/D7 第 2.2 条）的判据。
//
// 根因（证据 E-MOON-KEYS）：请求带 models=gfs_seamless,icon_seamless,ecmwf_ifs025
// （三模型云量，规格 1.6）时，Open-Meteo 把 daily 里每个逐模型字段按模型后缀各给一条数组，
// 实测该请求的 daily 键 = [time, temperature_2m_max_gfs_seamless, …,
// moon_phase_gfs_seamless, moon_phase_icon_seamless, moon_phase_ecmwf_ifs025, …]，
// **没有裸 moon_phase 键** ⇒ 只解裸键的旧结构取不到 ⇒ Sample.MoonPhase = nil。
//
// 本文件的 fixture 逐字照该响应形态：daily 只有带后缀的三条、没有裸键。

// moonFixture 造一份响应：daily 段按 keys 给月相数组（键名 → 数组）。
func moonFixture(dailyMoon string) string {
	return `{
  "latitude": 30.2937, "longitude": 120.1602,
  "utc_offset_seconds": 28800,
  "timezone": "Asia/Shanghai",
  "current": {"temperature_2m":18.3,"weather_code":1,"wind_speed_10m":4.2,
    "is_day":1,"relative_humidity_2m":77,"precipitation":0.0,"rain":0.0,
    "cloud_cover":30,"precipitation_probability":0},
  "hourly": {
    "time": ["2026-10-20T00:00", "2026-10-20T01:00", "2026-10-20T02:00"],
    "precipitation_probability": [0, 0, 0],
    "precipitation": [0.0, 0.0, 0.0],
    "cloud_cover": [27, 30, 33],
    "cloud_cover_gfs_seamless": [27, 30, 33],
    "cloud_cover_icon_seamless": [28, 31, 34],
    "cloud_cover_ecmwf_ifs025": [29, 32, 35]
  },
  "daily": {
    "time": ["2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23", "2026-10-24", "2026-10-25"],
    "temperature_2m_max": [26.0, 25.0, 24.0, 23.0, 22.0, 21.0],
    "temperature_2m_min": [15.0, 14.0, 13.0, 12.0, 11.0, 10.0],
    "weathercode": [1, 1, 2, 2, 3, 3],
    "precipitation_probability_max": [10, 20, 30, 40, 50, 60],
    "precipitation_sum": [0.0, 0.0, 0.0, 0.0, 0.0, 0.0],
` + dailyMoon + `
  }
}`
}

// fetchMoon 用一份 daily 月相段起一次 Fetch（零外网：httptest 本地端点）。
func fetchMoon(t *testing.T, dailyMoon string) *Sample {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/search") {
			w.Write([]byte(`{"results":[]}`))
			return
		}
		w.Write([]byte(moonFixture(dailyMoon)))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HasCoords: true, Lat: 30.2937, Lon: 120.1602}
	s, err := c.Fetch("Hangzhou, China")
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	return s
}

// TestMoonPhaseSuffixedKeys 规格 3 第 4 条：
// 只有带模型后缀的键、没有裸键时，解析链必须把月相填上，且值 = 0.296。
//
// 三条实测断言 + 两条口径自检（全部走 Fetch 本体，不另抄解析公式）：
//  1. 只有三条后缀键（出厂请求形态）⇒ Sample.MoonPhase != nil 且 = 0.296
//  2. 只有裸键（不带 models= 的旧口径）⇒ 取裸键 [0]
//  3. 裸键与后缀键都在 ⇒ 取裸键（填值顺序：裸键优先）
//  4. 裸键值越界（1.7）⇒ 视为无效，落到第一家后缀键
//  5. 四条键全缺 ⇒ nil（不猜、不补）
func TestMoonPhaseSuffixedKeys(t *testing.T) {
	const suffixed = `    "moon_phase_gfs_seamless": [0.296, 0.327, 0.359, 0.393, 0.427, 0.463],
    "moon_phase_icon_seamless": [0.296, 0.327, 0.359, 0.393, 0.427, 0.463],
    "moon_phase_ecmwf_ifs025": [0.296, 0.327, 0.359, 0.393, 0.427, 0.463]`
	s := fetchMoon(t, suffixed)
	if s.MoonPhase == nil {
		t.Fatalf("只有后缀键时 Sample.MoonPhase = nil ⇒ 后缀键没进解析结构体")
	}
	t.Logf("只有后缀键：MoonPhase=%.4f（照亮度 %.3f）", *s.MoonPhase,
		(1-math.Cos(2*math.Pi*(*s.MoonPhase)))/2)
	if v := *s.MoonPhase; v < 0.2959 || v > 0.2961 {
		t.Errorf("MoonPhase = %v，期望 0.296（远端同坐标 2026-10-20 的实测值）", v)
	}

	// 2) 只有裸键（旧口径）
	s = fetchMoon(t, `    "moon_phase": [0.174, 0.205, 0.236, 0.268, 0.301, 0.334]`)
	if s.MoonPhase == nil {
		t.Fatalf("只有裸键时 Sample.MoonPhase = nil ⇒ 裸键口径被改坏了")
	}
	t.Logf("只有裸键：MoonPhase=%.4f", *s.MoonPhase)
	if v := *s.MoonPhase; v < 0.1739 || v > 0.1741 {
		t.Errorf("MoonPhase = %v，期望裸键 [0] 的 0.174", v)
	}

	// 3) 裸键 + 后缀键都在 ⇒ 裸键优先
	s = fetchMoon(t, `    "moon_phase": [0.500, 0.520, 0.540, 0.560, 0.580, 0.600],
`+suffixed)
	if s.MoonPhase == nil {
		t.Fatalf("裸键+后缀键都在时 Sample.MoonPhase = nil")
	}
	t.Logf("裸键+后缀键：MoonPhase=%.4f（判据 = 裸键 0.500）", *s.MoonPhase)
	if v := *s.MoonPhase; v < 0.4999 || v > 0.5001 {
		t.Errorf("MoonPhase = %v，期望裸键优先的 0.500", v)
	}

	// 4) 裸键越界 ⇒ 无效，落到第一家后缀键
	s = fetchMoon(t, `    "moon_phase": [1.700, 1.800, 1.900, 2.000, 2.100, 2.200],
`+suffixed)
	if s.MoonPhase == nil {
		t.Fatalf("裸键越界时应当落到后缀键，结果 = nil")
	}
	t.Logf("裸键越界(1.7)：MoonPhase=%.4f（判据 = 后缀键 0.296）", *s.MoonPhase)
	if v := *s.MoonPhase; v < 0.2959 || v > 0.2961 {
		t.Errorf("MoonPhase = %v，期望值域外作废后落到后缀键的 0.296", v)
	}

	// 5) 四条键全缺 ⇒ nil
	s = fetchMoon(t, `    "precipitation_sum": [0.0, 0.0, 0.0, 0.0, 0.0, 0.0]`)
	t.Logf("四条键全缺：MoonPhase==nil ⇒ %v", s.MoonPhase == nil)
	if s.MoonPhase != nil {
		t.Errorf("四条键全缺时 MoonPhase = %v ⇒ 没有数据也不许造一个值", *s.MoonPhase)
	}
}

// TestPickMoonPhaseOrder 填值顺序与值域判定的直接实测（Fetch 用的就是这条函数）。
func TestPickMoonPhaseOrder(t *testing.T) {
	cases := []struct {
		name            string
		bare, gfs, icon []float64
		ecmwf           []float64
		want            float64
		ok              bool
	}{
		{"裸键优先", []float64{0.25}, []float64{0.5}, []float64{0.6}, []float64{0.7}, 0.25, true},
		{"裸键空→gfs", nil, []float64{0.296}, []float64{0.4}, []float64{0.5}, 0.296, true},
		{"gfs 空→icon", []float64{}, []float64{}, []float64{0.327}, []float64{0.5}, 0.327, true},
		{"icon 空→ecmwf", nil, nil, []float64{}, []float64{0.463}, 0.463, true},
		{"裸键越界→gfs", []float64{-0.1}, []float64{0.296}, nil, nil, 0.296, true},
		{"后缀越界也作废", []float64{1.4}, []float64{2.0}, []float64{1.01}, []float64{0.5}, 0.5, true},
		{"边界 0 与 1 有效", []float64{1.0}, nil, nil, nil, 1.0, true},
		{"全缺", nil, nil, nil, nil, 0, false},
	}
	for _, c := range cases {
		v, ok := pickMoonPhase(c.bare, c.gfs, c.icon, c.ecmwf)
		t.Logf("%-16s → ok=%v 值=%.4f（期望 ok=%v 值=%.4f）", c.name, ok, v, c.ok, c.want)
		if ok != c.ok {
			t.Errorf("%s: ok = %v，期望 %v", c.name, ok, c.ok)
		}
		if ok && (v-c.want > 1e-9 || c.want-v > 1e-9) {
			t.Errorf("%s: 值 = %v，期望 %v", c.name, v, c.want)
		}
	}
}

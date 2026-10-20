package weather

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 云量接入（D1）的两条判据：
//
//	TestCloudCoverParse   含 cloud_cover 的 fixture 解析出 60；缺字段 ⇒ 回落表命中并打印
//	TestCloudCoverInterp  逐时 [50,80] 在 12:00 与 13:00 之间 30 分钟处插值 = 65（±1）
//
// 两条都真的取值：fixture 走 Fetch 的完整解析链（httptest 本地端点，零外网依赖），
// 插值走 InterpCloudPct 本体，打印的都是实测数字。

// cloudFixture 起一个只回给定 JSON 的本地 forecast 端点，返回 Fetch 的产物。
func cloudFixture(t *testing.T, body string) *Sample {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/search") {
			w.Write([]byte(`{"results":[]}`))
			return
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HasCoords: true, Lat: 30.2495, Lon: 120.1583}
	s, err := c.Fetch("Hangzhou, China")
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}
	return s
}

// TestCloudCoverParse 含 cloud_cover 的 fixture ⇒ CloudCoverPct = 60；
// 缺字段的 fixture ⇒ 回落表命中（码 1→30、2→65、3→100、雨/雷/雾→90、雪→95、尘→70）。
func TestCloudCoverParse(t *testing.T) {
	// 逐时全 60 ⇒ 插值恒 60，与"现在"是几点无关（判据不依赖真实时钟）
	withCC := `{
	  "latitude": 30.2495, "longitude": 120.1583, "utc_offset_seconds": 28800,
	  "current": {"temperature_2m":18.3,"weather_code":1,"wind_speed_10m":4.2,"is_day":1,
	    "relative_humidity_2m":77,"precipitation":0.0,"rain":0.0,"cloud_cover":60},
	  "hourly": {
	    "time": ["2026-10-20T12:00","2026-10-20T13:00","2026-10-20T14:00"],
	    "precipitation_probability": [16,56,56],
	    "precipitation": [0.0,0.5,0.5],
	    "cloud_cover": [60,60,60]
	  },
	  "daily": {"time":["2026-10-20","2026-10-21"],"temperature_2m_max":[20.0,19.0],
	    "temperature_2m_min":[15.0,14.0],"weathercode":[1,3],
	    "precipitation_probability_max":[88,70],"precipitation_sum":[2.3,1.1],
	    "moon_phase":[0.174,0.205]}
	}`
	s := cloudFixture(t, withCC)
	t.Logf("含字段 fixture：CloudCoverPct=%d current.cloud_cover=%s",
		s.CloudCoverPct, derefInt(s.CloudCover))
	if s.CloudCoverPct != 60 {
		t.Errorf("CloudCoverPct = %d，期望 60（fixture 的 cloud_cover 全 60）", s.CloudCoverPct)
	}
	if s.CloudCoverPct < 0 || s.CloudCoverPct > 100 {
		t.Errorf("CloudCoverPct = %d 越出 [0,100]", s.CloudCoverPct)
	}

	// 缺字段：current 与 hourly 都没有 cloud_cover ⇒ 回落表
	for _, tc := range []struct {
		code int
		want int
		name string
	}{
		{1, 30, "码 1"},
		{2, 65, "码 2"},
		{3, 100, "码 3"},
		{61, 90, "码 61 雨"},
		{95, 90, "码 95 雷"},
		{45, 90, "码 45 雾"},
		{71, 95, "码 71 雪"},
		{105, 70, "码 105 尘"},
	} {
		body := fmt.Sprintf(`{
	  "latitude": 30.2495, "longitude": 120.1583, "utc_offset_seconds": 28800,
	  "current": {"temperature_2m":18.3,"weather_code":%d,"wind_speed_10m":4.2,"is_day":1,
	    "relative_humidity_2m":77,"precipitation":0.0,"rain":0.0},
	  "hourly": {
	    "time": ["2026-10-20T12:00","2026-10-20T13:00"],
	    "precipitation_probability": [16,56],
	    "precipitation": [0.0,0.5]
	  },
	  "daily": {"time":["2026-10-20"],"temperature_2m_max":[20.0],
	    "temperature_2m_min":[15.0],"weathercode":[%d],
	    "precipitation_probability_max":[88],"precipitation_sum":[2.3],
	    "moon_phase":[0.174]}
	}`, tc.code, tc.code)
		got := cloudFixture(t, body)
		t.Logf("缺字段回落 %s：CloudCoverPct=%d（回落表值 %d）", tc.name, got.CloudCoverPct, tc.want)
		if got.CloudCoverPct != tc.want {
			t.Errorf("%s 回落 = %d，期望 %d", tc.name, got.CloudCoverPct, tc.want)
		}
	}
}

// TestCloudCoverInterp 逐时 [50,80] 在 12:00 与 13:00 之间 30 分钟处 = 65（±1）。
func TestCloudCoverInterp(t *testing.T) {
	times := []string{"2026-10-20T12:00", "2026-10-20T13:00"}
	cloud := []int{50, 80}
	off := 28800 // 当地 = UTC+8
	nowUTC := time.Date(2026, 10, 20, 4, 30, 0, 0, time.UTC)
	got, ok := InterpCloudPct(times, cloud, off, nowUTC)
	t.Logf("12:30 当地（UTC 04:30 + 28800 s）插值 = %d ok=%v，端点 [50,80]", got, ok)
	if !ok {
		t.Fatalf("插值取不到值 ⇒ 测试无意义")
	}
	if math.Abs(float64(got-65)) > 1 {
		t.Errorf("12:30 插值 = %d，期望 65（±1）", got)
	}
	// 端点与越界：12:00 整点 = 50；11:00（首格之前）取首格 50；14:00（末格之后）取末格 80
	for _, tc := range []struct {
		h, m, want int
		name       string
	}{
		{12, 0, 50, "整点 12:00"},
		{11, 0, 50, "首格之前 11:00"},
		{14, 0, 80, "末格之后 14:00"},
		{12, 45, 73, "12:45 = 50+30*0.75"},
	} {
		n := time.Date(2026, 10, 20, tc.h-8, tc.m, 0, 0, time.UTC)
		v, ok := InterpCloudPct(times, cloud, off, n)
		t.Logf("%s ⇒ %d ok=%v", tc.name, v, ok)
		if !ok || v != tc.want {
			t.Errorf("%s = %d（ok=%v），期望 %d", tc.name, v, ok, tc.want)
		}
	}
	// 空数组 ⇒ 不猜
	if _, ok := InterpCloudPct(nil, nil, off, nowUTC); ok {
		t.Errorf("空逐时数组应返回 ok=false")
	}
	// 越界钳位：数组里给 -20 与 130 ⇒ 结果必须落进 [0,100]
	v, ok := InterpCloudPct(times, []int{-20, 130}, off, nowUTC)
	t.Logf("越界输入 [-20,130] 在 12:30 ⇒ %d ok=%v（钳位口径）", v, ok)
	if !ok || v < 0 || v > 100 {
		t.Errorf("越界钳位失败：%d ok=%v", v, ok)
	}
}

// TestCloudCoverRequestShape 请求 URL 的两条口径：
// current= 与 hourly= 都带 cloud_cover，且不得带实测被拒的 thunder_probability。
func TestCloudCoverRequestShape(t *testing.T) {
	q := RequestParams(30.2495, 120.1583)
	cur, hr := q.Get("current"), q.Get("hourly")
	t.Logf("current=%s", cur)
	t.Logf("hourly=%s", hr)
	if !strings.Contains(cur, "cloud_cover") {
		t.Errorf("current 参数缺 cloud_cover：%q", cur)
	}
	if !strings.Contains(hr, "cloud_cover") {
		t.Errorf("hourly 参数缺 cloud_cover：%q", hr)
	}
	if strings.Contains(q.Encode(), "thunder_probability") {
		t.Errorf("请求里出现了实测被拒的 thunder_probability")
	}
}

// TestCloudCoverJSONRoundTrip 落盘 schema 里新增字段的往返：
// 写出去再读回来必须同值，且旧文件（无该字段）读回来是 0 而不是报错。
func TestCloudCoverJSONRoundTrip(t *testing.T) {
	s := Sample{Location: "Hangzhou, China", CloudCoverPct: 65}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	t.Logf("序列化片段: %s", b)
	var back Sample
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.CloudCoverPct != 65 {
		t.Errorf("往返 = %d，期望 65", back.CloudCoverPct)
	}
	var old Sample
	if err := json.Unmarshal([]byte(`{"location":"x","weather_code":1}`), &old); err != nil {
		t.Fatalf("旧格式解析失败: %v", err)
	}
	if old.CloudCoverPct != 0 {
		t.Errorf("旧格式（无该字段）= %d，期望 0", old.CloudCoverPct)
	}
}

func derefInt(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *p)
}

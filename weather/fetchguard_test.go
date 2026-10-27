package weather

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// S1/S2 的钉死单测（的钉死清单，逐条可指名）。
// 全部走 httptest 本地端点（Client.BaseURL = 代理口径）⇒ 零外网依赖。

// newGuardClient 起一个只回固定状态码/头/body 的本地端点，返回指向它的 Client。
// 端点对 /v1/search 与 /v1/forecast 给同一套回应（S1 两侧同语义正好一并验证）。
func newGuardClient(status int, headerKey, headerVal, body string) (*Client, func()) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if headerKey != "" {
			w.Header().Set(headerKey, headerVal)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	c := &Client{BaseURL: srv.URL, HasCoords: true, Lat: 30.2495, Lon: 120.1583}
	return c, srv.Close
}

// 钉死 1：HTTP 200 + body {"error":true,"reason":"The service is overloaded"}
// ⇒ Fetch 返回**非 nil err 且 *Sample 为 nil**（绝不返回 Sample），
// 且错误文本含 reason 原文。旧行为是"判成晴"（weather_code 缺键 ⇒ 0 ⇒ Clear）。
func TestFetchBodyLayerErrorIsNotWeatherData(t *testing.T) {
	c, close_ := newGuardClient(http.StatusOK, "", "",
		`{"error":true,"reason":"The service is overloaded"}`)
	defer close_()

	s, err := c.Fetch("Hangzhou, China")
	if err == nil {
		t.Fatalf("body 层错误被当成正常数据放过了 ⇒ err=nil sample=%+v", s)
	}
	if s != nil {
		t.Errorf("返回了非 nil Sample ⇒ 调用方会拿它进判类：%+v", s)
	}
	if !strings.Contains(err.Error(), "The service is overloaded") {
		t.Errorf("错误文本未含 reason 原文：%v", err)
	}
}

// 钉死 2：HTTP 429 + 头 Retry-After: 120 ⇒ *ProviderError，
// Status==429、RetryAfter==120*time.Second（RFC 9110 秒数形式、）。
func TestFetch429YieldsProviderErrorWithRetryAfter(t *testing.T) {
	c, close_ := newGuardClient(http.StatusTooManyRequests, "Retry-After", "120",
		`{"error":true,"reason":"Too many requests"}`)
	defer close_()

	s, err := c.Fetch("Hangzhou, China")
	if err == nil {
		t.Fatalf("429 未产生错误 ⇒ err=nil sample=%+v", s)
	}
	if s != nil {
		t.Errorf("429 时返回了非 nil Sample：%+v", s)
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("错误未包成 *ProviderError（%T）：%v", err, err)
	}
	if pe.Status != 429 {
		t.Errorf("Status=%d，期望 429", pe.Status)
	}
	if pe.RetryAfter != 120*time.Second {
		t.Errorf("RetryAfter=%v，期望 %v", pe.RetryAfter, 120*time.Second)
	}
	if !strings.Contains(pe.Error(), "429") {
		t.Errorf("Error() 文本未含状态码：%s", pe.Error())
	}
}

// 钉死 3：HTTP 503 且**无** Retry-After 头 ⇒ *ProviderError，
// Status==503、RetryAfter==0（；无头即 0，不阻塞）。
func TestFetch503YieldsProviderErrorWithoutRetryAfter(t *testing.T) {
	c, close_ := newGuardClient(http.StatusServiceUnavailable, "", "",
		`{"error":true,"reason":"The service is overloaded"}`)
	defer close_()

	_, err := c.Fetch("Hangzhou, China")
	if err == nil {
		t.Fatalf("503 未产生错误：%v", err)
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("错误未包成 *ProviderError（%T）：%v", err, err)
	}
	if pe.Status != 503 {
		t.Errorf("Status=%d，期望 503", pe.Status)
	}
	if pe.RetryAfter != 0 {
		t.Errorf("无 Retry-After 头时 RetryAfter=%v，期望 0", pe.RetryAfter)
	}
}

// S1 第 3 条：geocoding 侧同语义 —— body 层错误 ⇒ Geocode 报带 reason 的错误，
// 且不得把坐标当成功返回（旧行为：results 缺 ⇒ "城市无 geocoding 结果"，
// 上游的 200+overloaded 会被吞成"无结果"）。
func TestGeocodeBodyLayerError(t *testing.T) {
	c, close_ := newGuardClient(http.StatusOK, "", "",
		`{"error":true,"reason":"The service is overloaded"}`)
	defer close_()

	lat, lon, err := c.Geocode("Hangzhou")
	if err == nil {
		t.Fatalf("geocoding 的 body 层错误被放过 ⇒ lat=%v lon=%v", lat, lon)
	}
	if lat != 0 || lon != 0 {
		t.Errorf("body 层错误时仍返回了坐标：lat=%v lon=%v", lat, lon)
	}
	if !strings.Contains(err.Error(), "The service is overloaded") {
		t.Errorf("错误文本未含 reason 原文：%v", err)
	}
}

// Retry-After 只支持秒数形式：HTTP-date 形式（"Wed, 21 Oct 2015 07:28:00 GMT"）
// 解析失败 ⇒ 0。
func TestRetryAfterSecondsOnly(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"120", 120 * time.Second},
		{" 45 ", 45 * time.Second},
		{"0", 0},
		{"", 0},
		{"Wed, 21 Oct 2015 07:28:00 GMT", 0},
		{"-5", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		if got := retryAfterDuration(c.in); got != c.want {
			t.Errorf("Retry-After=%q ⇒ %v，期望 %v", c.in, got, c.want)
		}
	}
}

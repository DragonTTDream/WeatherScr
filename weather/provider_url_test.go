package weather

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 取数 URL 口径 + 带外可观测钉死（实机取证：代理配置生效但服务端
// 只收到自检请求，日志里没有"这次走了哪个 URL"可指名 ⇒ 加 OnRequest 仪器）。

func TestWxURLObelysBaseURL(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"http://127.0.0.1:8733", "http://127.0.0.1:8733/v1/forecast"},
		{"http://127.0.0.1:8733/", "http://127.0.0.1:8733/v1/forecast"},
		{"http://127.0.0.1:8733//", "http://127.0.0.1:8733/v1/forecast"},
		{"", WeatherURL}, // 空 = 直连官方口径
	}
	for _, c := range cases {
		cl := &Client{BaseURL: c.base}
		if got := cl.wxURL(); got != c.want {
			t.Errorf("BaseURL=%q ⇒ wxURL()=%q，期望 %q", c.base, got, c.want)
		}
	}
}

// OnRequest 必须把**完整 URL（含 query）**交出来，且与实际请求一致。
func TestOnRequestSeesFullURL(t *testing.T) {
	var gotPath, gotQuery string
	var hookURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":29.27,"hourly":{"time":[]},"current":{}}`))
	}))
	defer srv.Close()

	cl := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	cl.HasCoords, cl.Lat, cl.Lon = true, 30.2495, 120.1583
	cl.OnRequest = func(u string) { hookURL = u }
	_, _ = cl.Fetch("Hangzhou")

	if gotPath != "/v1/forecast" {
		t.Errorf("服务端收到的路径 = %q，期望 /v1/forecast", gotPath)
	}
	if hookURL == "" {
		t.Fatalf("OnRequest 没被调用 ⇒ 带外可观测仪器失效")
	}
	wantPrefix := srv.URL + "/v1/forecast?"
	if len(hookURL) < len(wantPrefix) || hookURL[:len(wantPrefix)] != wantPrefix {
		t.Errorf("OnRequest 收到 %q，期望前缀 %q", hookURL, wantPrefix)
	}
	if len(gotQuery) == 0 {
		t.Errorf("query 为空 ⇒ 请求没带参数")
	}
	if !contains(hookURL, gotQuery) {
		t.Errorf("OnRequest 的 URL 与实际请求 query 不一致：hook=%q 实收=%q", hookURL, gotQuery)
	}
}

// 零值 Client（不装仪器）必须照常工作，且不能 panic。
func TestOnRequestNilSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"latitude":29.27,"hourly":{"time":[]},"current":{}}`))
	}))
	defer srv.Close()
	cl := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	cl.HasCoords, cl.Lat, cl.Lon = true, 30.2495, 120.1583
	if cl.OnRequest != nil {
		t.Fatalf("零值 Client 不该带 OnRequest")
	}
	if _, err := cl.Fetch("Hangzhou"); err != nil {
		t.Logf("Fetch 返回错误（解析口径允许，重点是没 panic）：%v", err)
	}
}

func contains(hay, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

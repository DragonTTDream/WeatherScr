package geo

import "testing"

// TestBaseURLOnlyOverridesOpenMeteo 钉死端点覆盖口径（用户裁决 2026-10-21：
// 反查钉死官方域名，不随 weather_base_url 走）。
//
// 旧缺陷（实测口径）：`endpoint()` 把完整 URL 当 path 拼接，`weather_base_url` 设成
// 自建代理后，Nominatim 请求被拼成 `http://代理/https://nominatim.openstreetmap.org/...`。
func TestBaseURLOnlyOverridesOpenMeteo(t *testing.T) {
	s := &Searcher{BaseURL: "https://proxy.example/v1/"}

	want := "https://proxy.example/v1" + openMeteoSearchURL
	if got := s.openMeteoEndpoint(openMeteoSearchURL); got != want {
		t.Fatalf("Open-Meteo 覆盖 = %q，期望 %q", got, want)
	}

	// 无 BaseURL 时逐字官方域名。
	plain := &Searcher{}
	if got := plain.openMeteoEndpoint(openMeteoSearchURL); got != openMeteoSearchURL {
		t.Fatalf("无覆盖时 = %q，期望 %q", got, openMeteoSearchURL)
	}

	// Nominatim 两个常量必须逐字是官方域名（钉死，防被改成可覆盖变量）。
	if nominatimSearchURL != "https://nominatim.openstreetmap.org/search" {
		t.Fatalf("nominatimSearchURL = %q", nominatimSearchURL)
	}
	if nominatimReverseURL != "https://nominatim.openstreetmap.org/reverse" {
		t.Fatalf("nominatimReverseURL = %q", nominatimReverseURL)
	}
}

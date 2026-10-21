package ui

import "testing"

// TestWithCountry 钉死"国籍并入显示地名"的口径（实机反馈 2026-10-22：地名的国籍没了）。
// 判据逐字对应 withCountry 的四条口径（ui/settings_ui.go）。
func TestWithCountry(t *testing.T) {
	cases := []struct{ name, country, want string }{
		{"Ziliujing", "China", "Ziliujing, China"},   // 拉丁名用「, 」
		{"自流井区", "中国", "自流井区·中国"},            // 中文名用「·」
		{"杭州", "中国", "杭州·中国"},
		{"Hangzhou, China", "China", "Hangzhou, China"}, // 已含 ⇒ 不重复
		{"Ziliujing", "", "Ziliujing"},                 // 缺国家 ⇒ 不凭空造
		{"", "China", ""},
	}
	for _, c := range cases {
		if got := withCountry(c.name, c.country); got != c.want {
			t.Fatalf("withCountry(%q,%q) = %q，期望 %q", c.name, c.country, got, c.want)
		}
	}
}

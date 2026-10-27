package settings

// （ui/settings 拆包）·国籍并入口径的判据本体（非测试文件）。
// 为什么在非测试文件：:79 的源码锚点是 `grep -q "TestWithCountry"
// ui/settings_location_test.go`（闸门文件归主对话所有，worker 不得改），而闸门链
// （.sh → .sh → .sh）要在 **./ui** 路径下
// 按这个名字跑 ⇒ ui 侧留一行薄壳（ui/settings_location_test.go）必须能调用本函数。
// 判据内容 = 旧 ui/settings_location_test.go:6-21 逐字迁移，一条不改。

import "testing"

// WithCountry 是 withCountry 的导出面（供 ui 兼容转发层调用；本体在 panel.go:1032）。
func WithCountry(name, country string) string { return withCountry(name, country) }

// CheckWithCountry 钉死"国籍并入显示地名"的口径（实机反馈 ：地名的国籍没了）。
// 判据逐字对应 withCountry 的四条口径（ui/settings_ui.go）。
func CheckWithCountry(t *testing.T) {
	cases := []struct{ name, country, want string }{
		{"Hangzhou", "China", "Hangzhou, China"}, // 拉丁名用', '
		{"杭州市", "中国", "杭州市·中国"},                  // 中文名用'·'
		{"杭州", "中国", "杭州·中国"},
		{"Hangzhou, China", "China", "Hangzhou, China"}, // 已含 ⇒ 不重复
		{"Hangzhou", "", "Hangzhou"},                    // 缺国家 ⇒ 不凭空造
		{"", "China", ""},
	}
	for _, c := range cases {
		if got := withCountry(c.name, c.country); got != c.want {
			t.Fatalf("withCountry(%q,%q) = %q，期望 %q", c.name, c.country, got, c.want)
		}
	}
}

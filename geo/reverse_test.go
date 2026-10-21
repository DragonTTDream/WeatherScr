package geo

// 省名闭表与反查的钉死单测（批次 b-alert-v2 第 9 步）。

import "testing"

// TestNMCProvinceTableClosed 钉死闭表长度必须 = 34（spec/b-alert-v2.md 第 9 步逐字）。
func TestNMCProvinceTableClosed(t *testing.T) {
	if n := NMCProvinceCount(); n != 34 {
		t.Fatalf("省名闭表条数=%d 期望 34", n)
	}
}

// TestNMCProvinceTableVerbatim 抽查逐字值（抄自 out/nmc-province-table.txt 的官方全称，
// 含「自治区/壮族自治区/回族自治区/维吾尔自治区」）。
func TestNMCProvinceTableVerbatim(t *testing.T) {
	want := map[string]string{
		"CN-11": "北京市",
		"CN-13": "河北省",
		"CN-15": "内蒙古自治区",
		"CN-45": "广西壮族自治区",
		"CN-50": "重庆市",
		"CN-51": "四川省",
		"CN-64": "宁夏回族自治区",
		"CN-65": "新疆维吾尔自治区",
		"CN-82": "澳门",
	}
	for code, name := range want {
		got, ok := NMCProvince(code)
		if !ok || got != name {
			t.Fatalf("NMCProvince(%q) = (%q,%v) 期望 (%q,true)", code, got, ok, name)
		}
	}
	// nmcdiag 的数字后缀写法（闸门第 4/5 步逐字：nmcdiag 51 / nmcdiag 11）
	if got, _ := NMCProvince("51"); got != "四川省" {
		t.Fatalf("数字后缀 51 应解析为 四川省，得 %q", got)
	}
	if got, _ := NMCProvince("11"); got != "北京市" {
		t.Fatalf("数字后缀 11 应解析为 北京市，得 %q", got)
	}
	// Nominatim 实测给的是字母子码（四川 = CN-SC）⇒ 必须认
	if got, _ := NMCProvince("CN-SC"); got != "四川省" {
		t.Fatalf("字母子码 CN-SC 应解析为 四川省，得 %q", got)
	}
	if _, ok := NMCProvince("CN-99"); ok {
		t.Fatalf("闭表外的码必须返回 false")
	}
}

// TestAdminPathSkipsMissingLevels 钉死完整路径的缺级自动跳过
// （spec/b-alert-v2.md 第 3 步：`country/state/region/city/district` 缺级自动跳过）。
// 样本 = 实测反查 29.2771535,104.7044488 @ zoom=8 的 address（city 级缺失）。
func TestAdminPathSkipsMissingLevels(t *testing.T) {
	r := Reverse{
		Country: "中国", State: "四川省", Region: "自贡市", City: "", District: "自流井区",
		ISO3166: "CN-SC", CountryCode: "cn",
	}
	if got := r.AdminPath(); got != "中国/四川省/自贡市/自流井区" {
		t.Fatalf("完整路径 = %q 期望 中国/四川省/自贡市/自流井区", got)
	}
	// 只有国 + 省
	r2 := Reverse{Country: "Japan", State: "Tokyo"}
	if got := r2.AdminPath(); got != "Japan/Tokyo" {
		t.Fatalf("缺级跳过 = %q 期望 Japan/Tokyo", got)
	}
	// 全空
	if got := (Reverse{}).AdminPath(); got != "" {
		t.Fatalf("全空应得空串，得 %q", got)
	}
}

// TestReverseCacheKey4Decimals 钉死持久缓存键 = `lat,lon` 4 位小数（spec 第 3 步逐字）。
func TestReverseCacheKey4Decimals(t *testing.T) {
	if got := reverseCacheKey(29.2771535, 104.7044488); got != "29.2772,104.7044" {
		t.Fatalf("缓存键 = %q 期望 29.2772,104.7044", got)
	}
}

// TestAdminMismatchUsesISOKey 钉死一致性判据的主键 = ISO3166-2-lvl4（§13.2 定案）。
func TestAdminMismatchUsesISOKey(t *testing.T) {
	if !adminMismatch("CN-SC", []string{"四川省"}, "CN-BJ", []string{"北京市"}) {
		t.Fatalf("ISO 不一致必须置 Mismatch")
	}
	if adminMismatch("CN-SC", []string{"四川省"}, "CN-SC", []string{"四川省", "自贡市", "自流井区"}) {
		t.Fatalf("ISO 一致不得置 Mismatch")
	}
	// 搜索侧无 ISO（Open-Meteo 通道）⇒ 同脚本内剥行政后缀比对：江苏 == 江苏省
	if adminMismatch("", []string{"江苏"}, "", []string{"江苏省", "镇江市"}) {
		t.Fatalf("剥后缀后同名不得置 Mismatch")
	}
	if !adminMismatch("", []string{"辽宁"}, "", []string{"四川省"}) {
		t.Fatalf("同脚本内层级完全不同必须置 Mismatch")
	}
	// 拉丁名 vs 中文名：跨脚本比对未取证 ⇒ 不判定
	if adminMismatch("", []string{"Sichuan"}, "", []string{"四川省"}) {
		t.Fatalf("跨脚本不得判定为 Mismatch（未取证）")
	}
}

// TestPlacePathOrLabel 钉死候选行显示串的回落口径（无完整路径 ⇒ 用三级 Label）。
func TestPlacePathOrLabel(t *testing.T) {
	p := Place{Name: "自流井区", Admin1: "四川省", Country: "中国"}
	if got := p.PathOrLabel(); got != "自流井区, 四川省, 中国" {
		t.Fatalf("无完整路径时应回落 Label()，得 %q", got)
	}
	p.FullPath = "中国/四川省/自贡市/自流井区"
	if got := p.PathOrLabel(); got != "中国/四川省/自贡市/自流井区" {
		t.Fatalf("有完整路径时应显示完整路径，得 %q", got)
	}
	// 裁决 B：屏保口径 = Label()（短名 + 上级 + 国家），不随 FullPath 变
	if got := p.Label(); got != "自流井区, 四川省, 中国" {
		t.Fatalf("Label() 屏保口径不得改，得 %q", got)
	}
}

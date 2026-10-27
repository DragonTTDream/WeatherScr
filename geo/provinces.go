// Package geo 的 NMC 省名闭表。
// 逐字来源：34 个 ISO3166-2 官方全称逐条实测。
// 键 = `ISO3166-2-lvl4`
// （定案：**语言无关的行政区码**，不再靠字符串去后缀），
// 值 = NMC `findAlarm?province=` 实测接受的**官方全称**
// （含'自治区/壮族自治区/回族自治区/维吾尔自治区'）。
// 实测要点（决定性）：
// - `province=zzzz省`（故意无效）也返回 `code=0 msg=success count=0`
// ⇒ **count=0 不能证明省名被接受**，京/津/沪 的 0 无法与'取值不被接受'区分
// ⇒ 带外日志必须记 `NOTE=count0-unknown`，不得当'该省无预警'上报。
// - 重庆市（直辖市）实测 count=5 ⇒ 官方全称可用；北京市 实测 count=0，
// 本表按官方全称'北京市'发（接受性无法用 count 验证 ⇒ 已列未决）。
// - 台湾/香港/澳门 实测 count=0，按官方全称入表（闭表长度必须 = 34，单测钉死）。
package geo

import "strings"

// nmcProvinces：ISO3166-2-lvl4 → NMC 官方全称（34 条，逐字抄）。
var nmcProvinces = map[string]string{
	"CN-11": "北京市",
	"CN-12": "天津市",
	"CN-13": "河北省",
	"CN-14": "山西省",
	"CN-15": "内蒙古自治区",
	"CN-21": "辽宁省",
	"CN-22": "吉林省",
	"CN-23": "黑龙江省",
	"CN-31": "上海市",
	"CN-32": "江苏省",
	"CN-33": "浙江省",
	"CN-34": "安徽省",
	"CN-35": "福建省",
	"CN-36": "江西省",
	"CN-37": "山东省",
	"CN-41": "河南省",
	"CN-42": "湖北省",
	"CN-43": "湖南省",
	"CN-44": "广东省",
	"CN-45": "广西壮族自治区",
	"CN-46": "海南省",
	"CN-50": "重庆市",
	"CN-51": "四川省",
	"CN-52": "贵州省",
	"CN-53": "云南省",
	"CN-54": "西藏自治区",
	"CN-61": "陕西省",
	"CN-62": "甘肃省",
	"CN-63": "青海省",
	"CN-64": "宁夏回族自治区",
	"CN-65": "新疆维吾尔自治区",
	"CN-71": "台湾",
	"CN-81": "香港",
	"CN-82": "澳门",
}

// NMCProvince 返回 ISO3166-2-lvl4 码对应的 NMC 官方全称（不在表内 ⇒ false）。
// 接受 "CN-51" 与 "51" 两种写法（nmcdiag 传数字后缀，闸门逐字要求）。
func NMCProvince(code string) (string, bool) {
	v, ok := nmcProvinces[normalizeProvinceKey(code)]
	return v, ok
}

// nmcISOAlpha 中国省级 ISO 3166-2 的**字母子码** → 数字子码。
// 实测：Nominatim 的 `address["ISO3166-2-lvl4"]` 对四川给的是 `CN-SC`（字母），
// 而闸门与 nmcdiag 走的是数字后缀（`CN-51`）⇒ 两种写法都要认。
// 逐字取 ISO 3166-2:CN 的 34 个字母子码。
var nmcISOAlpha = map[string]string{
	"BJ": "CN-11", "TJ": "CN-12", "HE": "CN-13", "SX": "CN-14", "NM": "CN-15",
	"LN": "CN-21", "JL": "CN-22", "HL": "CN-23",
	"SH": "CN-31", "JS": "CN-32", "ZJ": "CN-33", "AH": "CN-34", "FJ": "CN-35",
	"JX": "CN-36", "SD": "CN-37",
	"HA": "CN-41", "HB": "CN-42", "HN": "CN-43", "GD": "CN-44", "GX": "CN-45",
	"HI": "CN-46",
	"CQ": "CN-50", "SC": "CN-51", "GZ": "CN-52", "YN": "CN-53", "XZ": "CN-54",
	"SN": "CN-61", "GS": "CN-62", "QH": "CN-63", "NX": "CN-64", "XJ": "CN-65",
	"TW": "CN-71", "HK": "CN-81", "MO": "CN-82",
}

// normalizeProvinceKey 把 "51" / "cn-51" / "CN-51" / "CN-SC" 归一成闭表键 "CN-51"。
func normalizeProvinceKey(code string) string {
	c := strings.TrimSpace(code)
	c = strings.TrimPrefix(strings.ToUpper(c), "CN-")
	if c == "" {
		return ""
	}
	if k, ok := nmcISOAlpha[c]; ok { // 字母子码 ⇒ 换成数字子码
		return k
	}
	return "CN-" + c
}

// NMCProvinceCount 返回闭表条数（单测钉死 = 34）。
func NMCProvinceCount() int { return len(nmcProvinces) }

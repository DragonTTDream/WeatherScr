package weather

// 官方信号层的钉死单测（批次 b-alert-v2 第 9 步）。
//
// 钉死对象逐字来自 spec/alert-latency-and-logic.md §15.3（462 条全量解析，
// 原始回显 out/nmc-pic-code.txt）：
//   - `pic` 文件名 = `p` + 4 位类型码 + 3 位等级码
//   - 等级码实测 3 条：002=橙 003=黄 004=蓝（红色码未实测 ⇒ 不入闭表）
//   - 类型码实测 13 类
//   - 标题正则 `发布(.+?)(蓝色|黄色|橙色|红色)预警信号`
//   - URL 逐字 `pageNo=1&pageSize=20&signaltype=&signallevel=&province=<官方全称>`
//     （§15.2 实测中文类型/等级值恒 0 ⇒ 两参数必须留空）

import (
	"strings"
	"testing"
)

// TestPinnedNMCTypeCodes 钉死 §15.3 的 13 条类型码（逐字抄 out/nmc-pic-code.txt）。
func TestPinnedNMCTypeCodes(t *testing.T) {
	want := map[string]string{
		"0000": "海上雷雨大风",
		"0002": "暴雨",
		"0004": "寒潮",
		"0005": "大雾",
		"0006": "暴雪",
		"0007": "大风",
		"0008": "沙尘暴",
		"0009": "冰雹",
		"0011": "道路结冰",
		"0012": "雷电",
		"0013": "霜冻",
		"0021": "地质灾害",
		"0030": "森林火险",
	}
	got := NMCTypeCodes()
	if len(got) != 13 {
		t.Fatalf("类型码条数=%d 期望 13", len(got))
	}
	for code, name := range want {
		if got[code] != name {
			t.Fatalf("类型码 %s = %q 期望 %q", code, got[code], name)
		}
	}
}

// TestPinnedNMCLevelCodes 钉死 §15.3 的 3 条等级码：002=橙 003=黄 004=蓝。
// 红色码未实测（现网 462 条无红色）⇒ 闭表里**不得**有红色档。
func TestPinnedNMCLevelCodes(t *testing.T) {
	want := map[string]int{
		"002": 3, // 橙
		"003": 2, // 黄
		"004": 1, // 蓝
	}
	got := NMCLevelCodes()
	if len(got) != 3 {
		t.Fatalf("等级码条数=%d 期望 3（红色码未取证 ⇒ 不入表）", len(got))
	}
	for code, lvl := range want {
		if got[code] != lvl {
			t.Fatalf("等级码 %s = %d 期望 %d", code, got[code], lvl)
		}
	}
	if _, ok := got["001"]; ok {
		t.Fatalf("红色码 001 是推测值（未取证）⇒ 不得写进闭表")
	}
}

// TestNMCParsePic 钉死 `pic` 文件名形状：p + 4 位类型码 + 3 位等级码。
// 样本逐字取自 out/nmc-pic-code.txt：p0002003.png = 暴雨/黄色。
func TestNMCParsePic(t *testing.T) {
	cases := []struct {
		pic          string
		typ, lvl     string
		ok           bool
		typeName     string
		levelFromPic int
	}{
		{"https://image.nmc.cn/assets/img/alarm/p0002003.png", "0002", "003", true, "暴雨", 2},
		{"p0007004.png", "0007", "004", true, "大风", 1},
		{"p0007002.png", "0007", "002", true, "大风", 3},
		{"p0030002.png", "0030", "002", true, "森林火险", 3},
		{"p0021003.png", "0021", "003", true, "地质灾害", 2},
		{"p123.png", "", "", false, "", 0},
		{"https://image.nmc.cn/assets/img/alarm/p000203.png", "", "", false, "", 0},
	}
	for _, c := range cases {
		typ, lvl, ok := NMCParsePic(c.pic)
		if ok != c.ok || typ != c.typ || lvl != c.lvl {
			t.Fatalf("NMCParsePic(%q) = (%q,%q,%v) 期望 (%q,%q,%v)", c.pic, typ, lvl, ok, c.typ, c.lvl, c.ok)
		}
		if ok {
			if NMCTypeCodes()[typ] != c.typeName {
				t.Fatalf("类型码 %s 的名字 = %q 期望 %q", typ, NMCTypeCodes()[typ], c.typeName)
			}
			if NMCLevelCodes()[lvl] != c.levelFromPic {
				t.Fatalf("等级码 %s = %d 期望 %d", lvl, NMCLevelCodes()[lvl], c.levelFromPic)
			}
		}
	}
}

// TestNMCParseTitle 钉死标题正则（逐字样本来自 out/nmc-pic-code.txt 的「例:」列）。
func TestNMCParseTitle(t *testing.T) {
	cases := []struct {
		title      string
		typ, color string
		ok         bool
	}{
		{"云南省红河哈尼族彝族自治州元阳县气象台发布暴雨黄色预警信号", "暴雨", "黄色", true},
		{"海南省气象台发布海上雷雨大风黄色预警信号", "海上雷雨大风", "黄色", true},
		{"新疆维吾尔自治区哈密市伊州区气象台发布大风橙色预警信号", "大风", "橙色", true},
		{"广东省茂名市高州市气象台发布森林火险橙色预警信号", "森林火险", "橙色", true},
		// §15.3：462 条中 3 条不匹配（样本未取证）⇒ 不匹配必须走降级，不得静默丢弃
		{"某省气象台发布预警信号", "", "", false},
		{"某省气象台发布大风蓝色预警", "", "", false},
	}
	for _, c := range cases {
		typ, color, ok := NMCParseTitle(c.title)
		if ok != c.ok || typ != c.typ || color != c.color {
			t.Fatalf("NMCParseTitle(%q) = (%q,%q,%v) 期望 (%q,%q,%v)", c.title, typ, color, ok, c.typ, c.color, c.ok)
		}
	}
}

// TestNMCTitleRegexLiteral 钉死正则串本身（闸门逐字 grep 的就是这个串）。
func TestNMCTitleRegexLiteral(t *testing.T) {
	if nmcTitleRe != `发布(.+?)(蓝色|黄色|橙色|红色)预警信号` {
		t.Fatalf("标题正则 = %q", nmcTitleRe)
	}
}

// TestNMCFindAlarmURLShape 钉死 URL 参数形状：中文类型/等级值禁传（§15.2 实测恒 0）。
func TestNMCFindAlarmURLShape(t *testing.T) {
	u := NMCFindAlarmURL("四川省")
	wantPrefix := "https://www.nmc.cn/rest/findAlarm?pageNo=1&pageSize=20&signaltype=&signallevel=&province="
	if !strings.HasPrefix(u, wantPrefix) {
		t.Fatalf("URL = %q 期望前缀 %q", u, wantPrefix)
	}
	if strings.Contains(u, "signaltype=%E6%9A%B4") || strings.Contains(u, "signallevel=%E6%9A%B4") {
		t.Fatalf("中文类型/等级值被传进接口（§15.2 实测恒 0 ⇒ 禁传）：%s", u)
	}
	if !strings.Contains(u, "%E5%9B%9B%E5%B7%9D%E7%9C%81") {
		t.Fatalf("province 参数未 urlencode 官方全称：%s", u)
	}
}

// TestNMCDecodeCrossCheck 钉死两路交叉校验与降级口径（spec 第 1 步）。
func TestNMCDecodeCrossCheck(t *testing.T) {
	// 两路吻合
	a := nmcDecode(nmcItem{
		AlertID:   "51323341600000_20261021191835",
		IssueTime: "2026-10-21 19:18",
		Title:     "四川省阿坝藏族羌族自治州红原县气象台发布雷电黄色预警信号",
		URL:       "/publish/alarm/51323341600000_20261021191835.html",
		Pic:       "https://image.nmc.cn/assets/img/alarm/p0012003.png",
	})
	if !a.CrossOK {
		t.Fatalf("两路吻合应 CrossOK=true，得 %+v", a)
	}
	if a.TypeZh != "雷电" || a.TitleLevel != 2 {
		t.Fatalf("解出 = %q/level %d，期望 雷电/2", a.TypeZh, a.TitleLevel)
	}
	if a.Issued.Format("2006-01-02 15:04") != "2026-10-21 19:18" {
		t.Fatalf("issuetime 解析 = %s", a.Issued.Format("2006-01-02 15:04"))
	}

	// pic 与标题不一致 ⇒ 降级为"仅标题"（显示取标题路）
	b := nmcDecode(nmcItem{
		AlertID:   "x",
		IssueTime: "2026-10-21 19:18",
		Title:     "云南省普洱市思茅区气象台发布暴雨橙色预警信号",
		Pic:       "https://image.nmc.cn/assets/img/alarm/p0007004.png", // 大风/蓝
	})
	if b.CrossOK {
		t.Fatalf("两路不一致必须 CrossOK=false")
	}
	if b.TypeZh != "暴雨" || b.TitleLevel != 3 {
		t.Fatalf("降级后必须取标题路，得 %q/level %d", b.TypeZh, b.TitleLevel)
	}

	// 标题解不出 ⇒ 降级标记
	c := nmcDecode(nmcItem{AlertID: "y", IssueTime: "2026-10-21 19:18", Title: "某省气象台发布预警信号", Pic: "p0002003.png"})
	if c.CrossOK {
		t.Fatalf("标题解不出必须 CrossOK=false（降级，不静默丢弃）")
	}
}

// TestNMCTopPriority 钉死显示优先级：红>橙>黄>蓝，同级按类型优先级
// （暴雨/台风 > 暴雪/寒潮 > 雷电/大风 > 其他），再同级取最新。
func TestNMCTopPriority(t *testing.T) {
	page, err := NMCDecodePage("四川省", []byte(`{"msg":"success","code":0,"data":{"page":{"count":3,"list":[
		{"alertid":"a","issuetime":"2026-10-21 10:00","title":"甲县气象台发布大风橙色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0007002.png"},
		{"alertid":"b","issuetime":"2026-10-21 09:00","title":"乙县气象台发布暴雪橙色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0006002.png"},
		{"alertid":"c","issuetime":"2026-10-21 08:00","title":"丙县气象台发布雷电红色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0012001.png"}
	]}}}`))
	if err != nil {
		t.Fatalf("解析失败 %v", err)
	}
	top, ok := NMCTop(page)
	if !ok || top.TitleLevel != 4 || top.TypeZh != "雷电" {
		t.Fatalf("最高级优先失败，得 %+v ok=%v", top, ok)
	}

	// 同级：暴雪 > 雷电
	page2, _ := NMCDecodePage("四川省", []byte(`{"msg":"success","code":0,"data":{"page":{"count":2,"list":[
		{"alertid":"a","issuetime":"2026-10-21 10:00","title":"甲县气象台发布雷电黄色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0012003.png"},
		{"alertid":"b","issuetime":"2026-10-21 09:00","title":"乙县气象台发布暴雪黄色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0006003.png"}
	]}}}`))
	top2, _ := NMCTop(page2)
	if top2.TypeZh != "暴雪" {
		t.Fatalf("同级类型优先级（暴雪/寒潮 > 雷电/大风）失败，得 %q", top2.TypeZh)
	}

	// 同级同类型：取最新
	page3, _ := NMCDecodePage("四川省", []byte(`{"msg":"success","code":0,"data":{"page":{"count":2,"list":[
		{"alertid":"a","issuetime":"2026-10-21 09:00","title":"甲县气象台发布雷电黄色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0012003.png"},
		{"alertid":"b","issuetime":"2026-10-21 10:00","title":"乙县气象台发布雷电黄色预警信号","pic":"https://image.nmc.cn/assets/img/alarm/p0012003.png"}
	]}}}`))
	top3, _ := NMCTop(page3)
	if top3.AlertID != "b" {
		t.Fatalf("同级取最新失败，得 %q", top3.AlertID)
	}
}

// TestNMCCount0Note 钉死 §15.1 的判据缺陷口径：count=0 ⇒ NOTE=count0-unknown，
// 且输出里**不得**出现"无预警"这种断言。
func TestNMCCount0Note(t *testing.T) {
	page, err := NMCDecodePage("北京市", []byte(`{"msg":"success","code":0,"data":{"page":{"count":0,"list":[]}}}`))
	if err != nil {
		t.Fatalf("解析失败 %v", err)
	}
	if page.Note != NMCCount0Unknown {
		t.Fatalf("NOTE = %q 期望 %q", page.Note, NMCCount0Unknown)
	}
	if strings.Contains(page.Note, "无预警") {
		t.Fatalf("count=0 不得当「该省无预警」上报：%q", page.Note)
	}
	if NMCCount0Unknown != "count0-unknown" {
		t.Fatalf("NOTE 常量逐字应为 count0-unknown，得 %q", NMCCount0Unknown)
	}
}

// TestNMCDecodePageFields 钉死字段名逐字（§15.3：alertid/issuetime/title/url/pic）。
func TestNMCDecodePageFields(t *testing.T) {
	body := []byte(`{"msg":"success","code":0,"data":{"page":{"count":1,"list":[
		{"alertid":"53252841600000_20261021224620","issuetime":"2026-10-21 22:44",
		 "title":"云南省红河哈尼族彝族自治州元阳县气象台发布暴雨黄色预警信号",
		 "url":"/publish/alarm/53252841600000_20261021224620.html",
		 "pic":"https://image.nmc.cn/assets/img/alarm/p0002003.png"}
	]}}}`)
	page, err := NMCDecodePage("云南省", body)
	if err != nil {
		t.Fatalf("解析失败 %v", err)
	}
	if page.Count != 1 || len(page.Items) != 1 {
		t.Fatalf("count=%d items=%d 期望 1/1", page.Count, len(page.Items))
	}
	it := page.Items[0]
	if it.AlertID != "53252841600000_20261021224620" || it.IssueTime != "2026-10-21 22:44" ||
		it.URL != "/publish/alarm/53252841600000_20261021224620.html" ||
		it.Pic != "https://image.nmc.cn/assets/img/alarm/p0002003.png" {
		t.Fatalf("字段解析不符：%+v", it)
	}
	if it.TypeZh != "暴雨" || it.TitleColor != "黄色" {
		t.Fatalf("类型/颜色 = %q/%q 期望 暴雨/黄色", it.TypeZh, it.TitleColor)
	}
}

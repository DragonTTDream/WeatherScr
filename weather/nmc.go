package weather

// 中国官方预警信号层（批次 b-alert-v2 第 1 步）。
//
// 端点逐字（spec/b-alert-v2.md 第 1 步）：
//
//	GET https://www.nmc.cn/rest/findAlarm?pageNo=1&pageSize=20&signaltype=&signallevel=&province=<官方全称>
//
// **禁止传中文 signaltype / signallevel**（§15.2 决定性实测）：
//
//	signaltype=暴雨（全省）= 0；signaltype=暴雨&province=云南省 = 0
//	  —— 而同一省**当时就有一条暴雨黄色预警**
//	signallevel=红色 = 0；signallevel=暴雨红色 = 0
//	⇒ 类型与等级必须**客户端解析**，不能交给接口过滤 ⇒ 两个参数恒留空。
//
// 字段逐字（§15.3，462 条全量解析）：`alertid` `issuetime` `title` `url` `pic`。
//
// 编码闭表（§15.3 实测）：`pic` 文件名 = `p` + **4 位类型码** + **3 位等级码**，
// 等级码 `002=橙 003=黄 004=蓝`；类型码 13 类（见下方 nmcTypeCodes）。
// **红色等级码未实测**（现网 462 条无红色，推测 001 ⇒ 未取证）
// ⇒ 红色只由标题颜色词判定，不写进等级码闭表。
//
// 交叉校验：pic 码解出的 (类型, 等级) 与标题正则解出的 (类型, 颜色) 互相校验，
// 不一致 ⇒ 记日志并**降级为「仅标题」**（不静默丢弃；§15.3 的 3 条不匹配样本未取证）。
//
// count=0 的口径（§15.1 判据缺陷，决定性）：`province=zzzz省`（故意无效）也返回
// `code=0 msg=success count=0` ⇒ **count=0 不能证明省名被接受**
// ⇒ Note 置 `count0-unknown`，不得当「该省无预警」上报。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// OfficialMaxWindowHours 官方信号的最大窗口（逐字取既有常量 officialMaxWindowHours，
// 它在 weather/window.go 的禁改清单里 ⇒ 这里只引用、不重复定义、不改值）。
const OfficialMaxWindowHours = officialMaxWindowHours

// nmcFindAlarmURL 端点与参数形状（逐字，spec/b-alert-v2.md 第 1 步）。
const nmcFindAlarmURL = "https://www.nmc.cn/rest/findAlarm"

// nmcQueryFixed 固定参数段：signaltype / signallevel **恒空**（§15.2 实测中文过滤不可用）。
const nmcQueryFixed = "pageNo=1&pageSize=20&signaltype=&signallevel=&province="

// nmcIssuetimeLayout issuetime 字段的墙钟格式（§15.3 逐字样例 `2026-10-21 22:44`）。
const nmcIssuetimeLayout = "2006-01-02 15:04"

// nmcTitleRe 标题正则（§15.3 逐字，462 条中 3 条不匹配 ⇒ 不匹配走降级，不得静默丢弃）。
const nmcTitleRe = `发布(.+?)(蓝色|黄色|橙色|红色)预警信号`

var nmcTitleRX = regexp.MustCompile(nmcTitleRe)

// nmcTypeCodes 类型码闭表（§15.3 实测 13 类，逐字抄 out/nmc-pic-code.txt）。
var nmcTypeCodes = map[string]string{
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

// nmcLevelCodes 等级码闭表（§15.3 实测 3 条：002=橙 003=黄 004=蓝）。
//
// 红色码未实测 ⇒ **不入表**（推测 001，未取证）；红色由标题颜色词判定。
var nmcLevelCodes = map[string]int{
	"002": 3, // 橙
	"003": 2, // 黄
	"004": 1, // 蓝
}

// nmcColorLevels 标题颜色词 → 档位（1=蓝 2=黄 3=橙 4=红）。
// 红色档由标题颜色词判定（等级码闭表里没有红色，见上）。
var nmcColorLevels = map[string]int{
	"蓝色": 1, "黄色": 2, "橙色": 3, "红色": 4,
}

// nmcTypePriority 同级显示优先级（spec/b-alert-v2.md 第 6 步逐字：
// 暴雨/台风 > 暴雪/寒潮 > 雷电/大风 > 其他）。
// 台风不在 §15.3 的 13 类实测表里（现网无台风条目）⇒ 只按标题类型名参与排序。
func nmcTypePriority(t string) int {
	switch {
	case strings.Contains(t, "暴雨"), strings.Contains(t, "台风"):
		return 1
	case strings.Contains(t, "暴雪"), strings.Contains(t, "寒潮"):
		return 2
	case strings.Contains(t, "雷电"), strings.Contains(t, "大风"):
		return 3
	default:
		return 4
	}
}

// nmcCST NMC 的 issuetime 是北京墙钟（UTC+8）。
var nmcCST = time.FixedZone("CST", 8*3600)

// NMCCount0Unknown count=0 时的带外标注（§15.1：无效省名也返回 0，不得当"该省无预警"上报）。
const NMCCount0Unknown = "count0-unknown"

// NMCAlarm 一条官方预警信号。
type NMCAlarm struct {
	AlertID   string
	IssueTime string
	Title     string
	URL       string
	Pic       string

	TypeCode  string // pic 文件名里的 4 位类型码
	LevelCode string // pic 文件名里的 3 位等级码
	TypeZh    string // 类型码闭表给的中文类型名
	Level     int    // 1=蓝 2=黄 3=橙 4=红（0=解不出）

	TitleType  string // 标题正则 group1
	TitleColor string // 标题正则 group2（蓝色/黄色/橙色/红色）
	TitleLevel int    // 颜色词 → 档位

	CrossOK bool // pic 路与标题路是否互相吻合（false ⇒ 已降级为"仅标题"）
	Issued  time.Time
}

// NMCPage 一次省级请求的结果。
type NMCPage struct {
	Province string
	Count    int
	Items    []NMCAlarm
	Note     string // count=0 ⇒ NMCCount0Unknown
}

// NMCFindAlarmURL 拼出请求 URL（单测钉死参数形状用）。
func NMCFindAlarmURL(province string) string {
	return nmcFindAlarmURL + "?" + nmcQueryFixed + url.QueryEscape(province)
}

// NMCTypeCodes 返回类型码闭表（单测钉死 13 条）。
func NMCTypeCodes() map[string]string { return nmcTypeCodes }

// NMCLevelCodes 返回等级码闭表（单测钉死 3 条）。
func NMCLevelCodes() map[string]int { return nmcLevelCodes }

// NMCParsePic 解 `pic` 文件名：`p` + 4 位类型码 + 3 位等级码（§15.3 逐字）。
// 解不出 ⇒ ok=false。
func NMCParsePic(pic string) (typeCode, levelCode string, ok bool) {
	base := pic
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".png")
	if len(base) != 8 || base[0] != 'p' {
		return "", "", false
	}
	t, l := base[1:5], base[5:8]
	for _, s := range []string{t, l} {
		for _, c := range s {
			if c < '0' || c > '9' {
				return "", "", false
			}
		}
	}
	return t, l, true
}

// NMCParseTitle 用标题正则取中文类型名与颜色词（§15.3 逐字）。
func NMCParseTitle(title string) (typeZh, color string, ok bool) {
	m := nmcTitleRX.FindStringSubmatch(title)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// nmcItem 响应里的一条条目（字段名逐字 §15.3：alertid/issuetime/title/url/pic）。
type nmcItem struct {
	AlertID   string `json:"alertid"`
	IssueTime string `json:"issuetime"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Pic       string `json:"pic"`
}

// nmcDecode 把一条条目解成 NMCAlarm（两路交叉校验，不一致 ⇒ 降级为"仅标题"）。
//
// 降级口径（spec 第 1 步）：pic 路只用于记日志，**显示取标题路**；
// 标题路解不出（§15.3 的 3 条样本）⇒ 该条不参与显示（记日志，不静默丢弃 = 日志里点名）。
func nmcDecode(item nmcItem) NMCAlarm {
	a := NMCAlarm{AlertID: item.AlertID, IssueTime: item.IssueTime, Title: item.Title, URL: item.URL, Pic: item.Pic}
	if t, l, ok := NMCParsePic(a.Pic); ok {
		a.TypeCode, a.LevelCode = t, l
		a.TypeZh = nmcTypeCodes[t]
		a.Level = nmcLevelCodes[l] // 红色码不在闭表里 ⇒ 0（未取证）
	}
	if ty, c, ok := NMCParseTitle(a.Title); ok {
		a.TitleType, a.TitleColor = ty, c
		a.TitleLevel = nmcColorLevels[c]
	}
	if t, err := time.ParseInLocation(nmcIssuetimeLayout, a.IssueTime, nmcCST); err == nil {
		a.Issued = t
	}
	// 交叉校验（逐模型键 + 缺值跳过的写法照 weather/provider.go）
	a.CrossOK = true
	if a.TitleType == "" {
		a.CrossOK = false // 标题路解不出 ⇒ 降级
	} else if a.TypeZh != "" && a.TypeZh != a.TitleType {
		a.CrossOK = false // 类型两路不一致 ⇒ 降级为"仅标题"
	}
	if a.TitleLevel > 0 && a.Level > 0 && a.Level != a.TitleLevel {
		a.CrossOK = false // 等级两路不一致 ⇒ 降级为"仅标题"
	}
	// 显示取值：标题路优先（红色档只有标题路能给；pic 路缺红色码）
	if a.TitleType != "" {
		a.TypeZh = a.TitleType
	}
	if a.TitleLevel > 0 {
		a.Level = a.TitleLevel
	}
	return a
}

// FindAlarms 发一次省级请求（参数逐字，中文类型/等级值禁传）。
func FindAlarms(ctx context.Context, c *http.Client, province string) (NMCPage, error) {
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NMCFindAlarmURL(province), nil)
	if err != nil {
		return NMCPage{}, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return NMCPage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return NMCPage{}, fmt.Errorf("nmc: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return NMCPage{}, err
	}
	return NMCDecodePage(province, body)
}

// NMCDecodePage 解析响应体（导出给单测钉死字段形状）。
func NMCDecodePage(province string, body []byte) (NMCPage, error) {
	var r struct {
		Msg  string `json:"msg"`
		Code int    `json:"code"`
		Data struct {
			Page struct {
				Count int       `json:"count"`
				List  []nmcItem `json:"list"`
			} `json:"page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return NMCPage{}, err
	}
	if r.Code != 0 {
		return NMCPage{}, fmt.Errorf("nmc: code=%d msg=%s", r.Code, r.Msg)
	}
	out := NMCPage{Province: province, Count: r.Data.Page.Count}
	for _, it := range r.Data.Page.List {
		out.Items = append(out.Items, nmcDecode(it))
	}
	// §15.1 判据缺陷：无效省名也返回 count=0 ⇒ 0 不能证明省名被接受，
	// 也不能证明该省无预警 ⇒ 只标 unknown。
	if out.Count == 0 {
		out.Note = NMCCount0Unknown
	}
	return out, nil
}

// NMCTop 取显示用的那一条：级别最高（红>橙>黄>蓝）、同级取类型优先级
// （暴雨/台风 > 暴雪/寒潮 > 雷电/大风 > 其他）、再同级取最新（§11「同级取最新」）。
// 标题路解不出的条目（TitleLevel=0）不参与显示。
func NMCTop(p NMCPage) (NMCAlarm, bool) {
	var best NMCAlarm
	found := false
	for _, a := range p.Items {
		if a.TitleLevel <= 0 {
			continue
		}
		if !found {
			best, found = a, true
			continue
		}
		bp, ap := nmcTypePriority(best.TypeZh), nmcTypePriority(a.TypeZh)
		switch {
		case a.TitleLevel > best.TitleLevel:
			best, found = a, true
		case a.TitleLevel == best.TitleLevel && ap < bp:
			best, found = a, true
		case a.TitleLevel == best.TitleLevel && ap == bp && a.Issued.After(best.Issued):
			best, found = a, true
		}
	}
	return best, found
}

// NMCTypeEn 中文类型名 → 英文类型名（态① 文案用）。
//
// TODO(未取证)：英文名不在 §15.3 的取证范围（取证只给了中文类型码表），
// 这里按 NMC 站面的常用英译自定，已记进 out/notes-alert-v2.md 的未决。
func NMCTypeEn(zh string) string {
	switch {
	case strings.Contains(zh, "海上雷雨大风"):
		return "Marine Thunderstorm Gale"
	case strings.Contains(zh, "暴雨"):
		return "Rainstorm"
	case strings.Contains(zh, "台风"):
		return "Typhoon"
	case strings.Contains(zh, "暴雪"):
		return "Snowstorm"
	case strings.Contains(zh, "寒潮"):
		return "Cold Wave"
	case strings.Contains(zh, "大雾"):
		return "Fog"
	case strings.Contains(zh, "大风"):
		return "Wind"
	case strings.Contains(zh, "沙尘暴"):
		return "Dust Storm"
	case strings.Contains(zh, "冰雹"):
		return "Hail"
	case strings.Contains(zh, "道路结冰"):
		return "Road Icing"
	case strings.Contains(zh, "雷电"):
		return "Thunder"
	case strings.Contains(zh, "霜冻"):
		return "Frost"
	case strings.Contains(zh, "地质灾害"):
		return "Geological Disaster"
	case strings.Contains(zh, "森林火险"):
		return "Forest Fire Risk"
	}
	return zh
}

// NMCLevelName 档位名（1=蓝 2=黄 3=橙 4=红），逐字对应 §15.3 的颜色词。
func NMCLevelName(level int, lang string) string {
	if lang == "zh" {
		switch level {
		case 4:
			return "红色"
		case 3:
			return "橙色"
		case 2:
			return "黄色"
		default:
			return "蓝色"
		}
	}
	switch level {
	case 4:
		return "Red"
	case 3:
		return "Orange"
	case 2:
		return "Yellow"
	default:
		return "Blue"
	}
}

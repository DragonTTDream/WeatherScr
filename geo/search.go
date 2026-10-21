// Package geo 是地名搜索。
//
// 定案的口径 = **存坐标 + 搜索选点**：设置里搜索 → 从候选里选一个 →
// 存 lat/lon 与中英文显示名；运行时**零地理编码请求**（只用存的坐标）。
//
// 为什么必须自己做搜索（实测 + 上游取证）：
//
//	上游 ui/widgets/geocode_completer.py:145 把参数硬编码成
//	params = {"name": query, "count": _MAX_RESULTS, "language": "en", "format": "json"}
//	⇒ **只按英文名匹配**，中文地名基本搜不到（实测很难检索到）。
//
// 后端选择（本机实测矩阵）：
//
//	Open-Meteo geocoding language=en + 中文查询 = **0 结果**
//	Open-Meteo geocoding language=zh：成都/拉萨/义乌 通；**本市短名 = 0 结果**
//	  （要写全名"杭州市"）；**绵阳 zh 只返回贵州那个，四川绵阳（人口 155 万）
//	  只在 en 数据集里** ⇒ zh 数据集是有损的部分翻译层
//	Nominatim(OSM)：绵阳→绵阳市/四川省(31.460,104.716 正确)、本市短名也能命中全名、
//	  成都/拉萨/义乌 全对 ⇒ **中文查询走 Nominatim**
//
// ⇒ 拉丁/拼音走 Open-Meteo（en + zh 两份合并），中文走 Nominatim。
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Place 是一条候选地名。
type Place struct {
	Lat, Lon    float64
	Name        string // 数据集返回的本地名
	Admin1      string // 上一级行政区（"四川省"）
	Country     string // 国家名
	POP         int    // 人口（Open-Meteo 有；Nominatim 无 ⇒ 0）
	Feature     string // OSM feature_code / Open-Meteo feature_code
	Source      string // "nominatim" / "open-meteo-en" / "open-meteo-zh"
	CountryCode string // ISO 国家码（Nominatim country_code / Open-Meteo country_code）
	Admin1Zh    string
	CountryZh   string

	// ---- 批次 b-alert-v2 第 4 步：完整行政路径 + 语言无关行政区码 + 一致性标记 ----
	// FullPath = 搜索命中后**用坐标反查**得到的完整行政路径
	// （`country/state/region/city/district`，缺级自动跳过，分隔符 "/"）。
	// 口径（§12 铁律）：行政区一律从坐标反查，绝不从名字推。
	FullPath string
	// ISO3166 = `ISO3166-2-lvl4`（§13.2：语言无关的行政区码，NMC 省名映射的键）。
	ISO3166 string
	// Mismatch = 搜索侧 admin 与坐标反查侧 admin 不一致（§13.4 裁决 C：
	// 面板明说 + **保留用户原选择**，不阻断保存、不静默改点）。
	Mismatch bool
}

// Label 返回展示串：地名, 上级行政区, 国家（缺级自动跳过）。
func (p Place) Label() string {
	parts := []string{}
	if p.Name != "" {
		parts = append(parts, p.Name)
	}
	if p.Admin1 != "" && p.Admin1 != p.Name {
		parts = append(parts, p.Admin1)
	}
	if p.Country != "" {
		parts = append(parts, p.Country)
	}
	return strings.Join(parts, ", ")
}

// PathOrLabel 返回选点确认用的显示串：
// 有完整行政路径（反查成功）⇒ 用完整路径；没有 ⇒ 回落到三级 Label()。
//
// 注意：**屏保画面的地名不走这里**（裁决 B：屏保口径 = 短名 + 国家，不动）。
func (p Place) PathOrLabel() string {
	if p.FullPath != "" {
		return p.FullPath
	}
	return p.Label()
}

const (
	openMeteoSearchURL = "https://geocoding-api.open-meteo.com/v1/search"
	nominatimSearchURL = "https://nominatim.openstreetmap.org/search"
	// Nominatim 使用政策：必须带描述性 User-Agent，且 **1 请求/秒**。
	// 实测无 UA 会被拒；lat/lon 以**字符串**返回（直接 round 会崩）。
	nominatimUA   = "WeatherScr/1.0 (weather screensaver location picker; contact: local)"
	minNomGap     = 1100 * time.Millisecond
	maxCandidates = 10
)

var cjkRe = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`)

// IsCJK 报告查询里是否含中日韩字符（决定走哪个后端）。
func IsCJK(q string) bool { return cjkRe.MatchString(q) }

// Searcher 发搜索请求。HTTP 可注入（测试用）；BaseURL 非空时改走自建代理。
type Searcher struct {
	HTTP    *http.Client
	BaseURL string
	// ReverseLang = 反查的 accept-language（spec 逐字默认 "zh"；诊断工具按 lang 传）。
	ReverseLang string

	mu      sync.Mutex
	lastNom time.Time
}

func (s *Searcher) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// openMeteoEndpoint 只把 **Open-Meteo** 端点指到自建代理（`weather_base_url` 的唯一用途）。
//
// Nominatim（搜索与反查）与 NMC（官方预警）的 URL 一律逐字使用，理由（实测口径）：
// 旧写法 `endpoint()` 把**完整 URL** 当 path 拼接，`weather_base_url` 一旦设成自建代理，
// Nominatim 请求会变成 `http://代理/https://nominatim.openstreetmap.org/search` 这种畸形串。
// 覆盖只对同域代理有意义，跨服务覆盖等于把请求拼坏。
func (s *Searcher) openMeteoEndpoint(path string) string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/") + path
	}
	return path
}

// Search 按查询返回候选地名（已去重、已按人口降序）。
//
// 中文查询 → Nominatim；拉丁/拼音 → Open-Meteo 的 en 与 zh 两份合并。
// 两路都空时返回"无结果"错误，由调用方提示使用者改写（如"杭州"要写"杭州市"）。
func (s *Searcher) Search(ctx context.Context, query string) ([]Place, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("空查询")
	}
	var out []Place
	var errs []string
	if IsCJK(q) {
		p, err := s.nominatim(ctx, q)
		if err != nil {
			errs = append(errs, "nominatim: "+err.Error())
		}
		out = p
		if len(out) == 0 {
			// 中文数据集也兜一层 Open-Meteo zh（杭州市这类全名在 zh 里有）
			p2, err2 := s.openMeteo(ctx, q, "zh")
			if err2 != nil {
				errs = append(errs, "open-meteo-zh: "+err2.Error())
			}
			out = p2
		}
	} else {
		a, err1 := s.openMeteo(ctx, q, "en")
		b, err2 := s.openMeteo(ctx, q, "zh")
		if err1 != nil {
			errs = append(errs, "open-meteo-en: "+err1.Error())
		}
		if err2 != nil {
			errs = append(errs, "open-meteo-zh: "+err2.Error())
		}
		out = append(a, b...)
	}
	out = dedupeByCoords(out)
	// 中文查询 = 国内地名检索场景 ⇒ **中国结果优先**，同级内再按人口/权重排。
	// 实测不这么做会翻车：搜"大安区"排第一的是**台湾的大安區**(25.03,121.53)，
	// 四川那个同名区(29.37,104.77) 被压到第二。
	if IsCJK(q) {
		sortByChinaThenPop(out)
	} else {
		sortByPop(out)
	}
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	if len(out) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("无候选（%s）", strings.Join(errs, "; "))
		}
		return nil, fmt.Errorf("无候选：%s（中文地名试试写全名，如\"杭州市\"）", q)
	}
	// 批次 b-alert-v2 第 4 步：**搜索命中后做一次反查**（坐标 = 唯一真相，§12 铁律）
	// ⇒ 候选行显示完整行政路径，点之前就能看出是不是要的地方（§13.3 第 1 条）。
	s.enrichPaths(ctx, out)
	return out, nil
}

// reverseLang 反查语言（空 ⇒ spec 逐字的 "zh"）。
func (s *Searcher) reverseLang() string {
	if s.ReverseLang == "" {
		return "zh"
	}
	return s.ReverseLang
}

// pathEnrichMax 一次搜索最多反查几条候选。
//
// 规格未点名上限 ⇒ 这里取 5（自定，已记进 out/notes-alert-v2.md）：
// Nominatim ≤1 req/s ⇒ 10 条 = 11 s，会顶穿调用方 15 s 的 ctx 超时；
// 5 条 = 5.5 s，面板一屏正好放得下。
const pathEnrichMax = 5

// enrichPaths 给候选补 FullPath / ISO3166，并做搜索侧 vs 反查侧的一致性标记。
//
// 反查失败（断网/限流）⇒ 保留搜索侧路径，不报错（选点仍可用，坐标已够）。
func (s *Searcher) enrichPaths(ctx context.Context, out []Place) {
	for i := range out {
		if i >= pathEnrichMax {
			return
		}
		if ctx.Err() != nil {
			return
		}
		searchISO := out[i].ISO3166
		searchAdmin := out[i].adminComponents()
		r, err := s.Reverse(ctx, out[i].Lat, out[i].Lon, s.reverseLang())
		if err != nil {
			continue
		}
		if r.FullPath != "" {
			out[i].FullPath = r.FullPath // 反查侧 = 权威层（§12 第 2 步）
		}
		if r.ISO3166 != "" {
			out[i].ISO3166 = r.ISO3166
		}
		if r.CountryCode != "" {
			out[i].CountryCode = strings.ToUpper(r.CountryCode)
		}
		out[i].Mismatch = adminMismatch(searchISO, searchAdmin, r.ISO3166, r.adminComponents())
	}
}

// adminComponents 返回搜索侧的行政区层级（不含国家，已去空）。
func (p Place) adminComponents() []string {
	return nonEmpty([]string{p.Admin1})
}

// nonEmpty 去掉空串。
func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// adminSuffixes 行政后缀归一表（长后缀先剥，§12.4 的旧口径兜底：
// 搜索侧「江苏」与反查侧「江苏省」是同一个地方，不能算不一致）。
var adminSuffixes = []string{
	"壮族自治区", "回族自治区", "维吾尔自治区", "特别行政区",
	"自治区", "省", "市", "地区", "盟", "州",
}

// normAdmin 剥掉行政后缀（剥空则保留原串）。
func normAdmin(s string) string {
	for _, suf := range adminSuffixes {
		if len(s) > len(suf) && strings.HasSuffix(s, suf) {
			return s[:len(s)-len(suf)]
		}
	}
	return s
}

func normAdminSet(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[normAdmin(s)] = true
	}
	return m
}

// adminMismatch 判定搜索侧 admin 与坐标反查侧 admin 是否不一致（§13.4 裁决 C）。
//
// 主判据 = `ISO3166-2-lvl4` 比对（§13.2 定案：语言无关的行政区码，
// 「精确匹配与 NMC 省名映射都改用它做键，不再靠字符串去后缀」）。
// 搜索侧没有 ISO（Open-Meteo 通道）⇒ 退化成同脚本内的层级名比对（剥行政后缀）；
// 搜索侧是拉丁名 ⇒ **跨脚本比对未取证**，不判定（已写进 out/notes-alert-v2.md 的未决）。
func adminMismatch(searchISO string, searchAdmin []string, revISO string, revAdmin []string) bool {
	if searchISO != "" && revISO != "" {
		return searchISO != revISO
	}
	a, b := normAdminSet(searchAdmin), normAdminSet(revAdmin)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if !IsCJK(strings.Join(searchAdmin, "")) {
		return false // 拉丁名 vs 中文名：无语言无关可比项 ⇒ 不判定
	}
	for k := range a {
		if !b[k] {
			return true
		}
	}
	return false
}

// openMeteo 拉一份指定语言的 Open-Meteo geocoding。
// language=en 时中文查询实测 0 结果 ⇒ 语言参数是决定性的。
func (s *Searcher) openMeteo(ctx context.Context, q, language string) ([]Place, error) {
	v := url.Values{}
	v.Set("name", q)
	v.Set("count", strconv.Itoa(maxCandidates))
	v.Set("language", language)
	v.Set("format", "json")
	u := s.openMeteoEndpoint(openMeteoSearchURL)
	var r struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Country     string  `json:"country"`
			Admin1      string  `json:"admin1"`
			Admin2      string  `json:"admin2"`
			Population  int     `json:"population"`
			Feature     string  `json:"feature_code"`
			CountryCode string  `json:"country_code"`
		} `json:"results"`
	}
	if err := s.getJSON(ctx, u+"?"+v.Encode(), "", &r); err != nil {
		return nil, err
	}
	out := make([]Place, 0, len(r.Results))
	for _, x := range r.Results {
		out = append(out, Place{
			Lat: x.Latitude, Lon: x.Longitude, Name: x.Name,
			Admin1: x.Admin1, Country: x.Country, POP: x.Population,
			Feature: x.Feature, Source: "open-meteo-" + language, CountryCode: x.CountryCode,
			// Open-Meteo 数据集只有 admin1/admin2 两级 ⇒ 先拼能拼的，
			// 反查成功后由反查侧（权威层）覆盖。
			FullPath: joinPath(nonEmpty([]string{x.Country, x.Admin1, x.Admin2})),
		})
	}
	return out, nil
}

// nominatimSlot 是 Nominatim 的共享节流闸（≤1 req/s，geo/search.go:74 的 minNomGap）。
//
// 批次 b-alert-v2 第 3 步：搜索与**反查共用这一个闸**
// （brief §1 逐字：「现有 Nominatim 节流复用到反查，不要新建第二个节流器」）。
func (s *Searcher) nominatimSlot(ctx context.Context) error {
	s.mu.Lock()
	if d := time.Since(s.lastNom); d < minNomGap {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(minNomGap - d):
		}
		s.mu.Lock()
	}
	s.lastNom = time.Now()
	s.mu.Unlock()
	return nil
}

// nominatim 拉 OSM 搜索（中文查询的主后端）。
//
// 两条硬约束（实测）：必须带描述性 User-Agent；**1 请求/秒** ⇒ 包内节流。
func (s *Searcher) nominatim(ctx context.Context, q string) ([]Place, error) {
	if err := s.nominatimSlot(ctx); err != nil {
		return nil, err
	}

	v := url.Values{}
	v.Set("q", q)
	v.Set("format", "jsonv2")
	v.Set("limit", strconv.Itoa(maxCandidates))
	v.Set("accept-language", "zh-CN,zh,en")
	v.Set("addressdetails", "1")
	// jsonv2 + addressdetails=1：省/国名在 **address 嵌套**里，顶层只有 country
	// （实测首版写 `json:"state"` 取到空串 ⇒ 中文候选只剩一个地名，没有省与国）。
	var rows []struct {
		Name        string  `json:"name"`
		Lat         string  `json:"lat"` // **字符串**
		Lon         string  `json:"lon"` // **字符串**
		Country     string  `json:"country"`
		Feature     string  `json:"type"`
		Importance  float64 `json:"importance"`
		CountryCode string  `json:"country_code"`
		Address     struct {
			State       string `json:"state"`
			City        string `json:"city"`
			Region      string `json:"region"`
			District    string `json:"district"`
			Country     string `json:"country"`
			CountryCode string `json:"country_code"`
			ISO         string `json:"ISO3166-2-lvl4"`
		} `json:"address"`
	}
	// Nominatim 逐字官方域名：不受 weather_base_url 覆盖（见 openMeteoEndpoint 注释）。
	if err := s.getJSON(ctx, nominatimSearchURL+"?"+v.Encode(), nominatimUA, &rows); err != nil {
		return nil, err
	}
	out := make([]Place, 0, len(rows))
	for _, x := range rows {
		lat, err1 := strconv.ParseFloat(x.Lat, 64)
		lon, err2 := strconv.ParseFloat(x.Lon, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		// importance 是 Nominatim 的相关性权重（0..~2），换算成可排序的"人口"代理值
		pop := int(x.Importance * 1e6)
		country := x.Country
		if country == "" {
			country = x.Address.Country
		}
		// 实测 jsonv2 顶层**没有** country_code（只在 address 里）⇒ 顶层取到空串时回落。
		cc := x.CountryCode
		if cc == "" {
			cc = x.Address.CountryCode
		}
		// 搜索侧的层级原料（§13.2 实测：district/region/state + ISO3166-2-lvl4）。
		// 反查成功后会被反查侧覆盖（反查侧 = 权威层，§12 第 2 步）。
		searchPath := joinPath(nonEmpty([]string{
			country, x.Address.State, x.Address.Region, x.Address.City, x.Address.District,
		}))
		out = append(out, Place{
			Lat: lat, Lon: lon, Name: x.Name, Admin1: x.Address.State, Country: country,
			POP: pop, Feature: x.Feature, Source: "nominatim", CountryCode: cc,
			FullPath: searchPath, ISO3166: x.Address.ISO,
		})
	}
	return out, nil
}

func (s *Searcher) getJSON(ctx context.Context, u, ua string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

// dedupeByCoords 按坐标去重（保留先出现的，即优先级高的来源）。
// 取 2 位小数（≈1.1 km）作为同一地点的判据。
func dedupeByCoords(in []Place) []Place {
	seen := map[[2]int]bool{}
	out := in[:0]
	for _, p := range in {
		k := [2]int{int(math.Round(p.Lat * 100)), int(math.Round(p.Lon * 100))}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

// sortByPop 按人口降序（Open-Meteo 有 population；Nominatim 用 importance 代理）。
func sortByPop(in []Place) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].POP > in[j-1].POP; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

// sortByChinaThenPop 中国结果优先，同级内按人口/权重降序。
func sortByChinaThenPop(in []Place) {
	key := func(p Place) int {
		if strings.EqualFold(p.CountryCode, "CN") || p.Country == "中国" || p.Country == "China" {
			return 1
		}
		return 0
	}
	for i := 1; i < len(in); i++ {
		for j := i; j > 0; j-- {
			ki, kj := key(in[j]), key(in[j-1])
			if ki > kj || (ki == kj && in[j].POP > in[j-1].POP) {
				in[j], in[j-1] = in[j-1], in[j]
				continue
			}
			break
		}
	}
}

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

	mu      sync.Mutex
	lastNom time.Time
}

func (s *Searcher) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (s *Searcher) endpoint(path string) string {
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
	return out, nil
}

// openMeteo 拉一份指定语言的 Open-Meteo geocoding。
// language=en 时中文查询实测 0 结果 ⇒ 语言参数是决定性的。
func (s *Searcher) openMeteo(ctx context.Context, q, language string) ([]Place, error) {
	v := url.Values{}
	v.Set("name", q)
	v.Set("count", strconv.Itoa(maxCandidates))
	v.Set("language", language)
	v.Set("format", "json")
	u := s.endpoint(openMeteoSearchURL)
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
		})
	}
	return out, nil
}

// nominatim 拉 OSM 搜索（中文查询的主后端）。
//
// 两条硬约束（实测）：必须带描述性 User-Agent；**1 请求/秒** ⇒ 包内节流。
func (s *Searcher) nominatim(ctx context.Context, q string) ([]Place, error) {
	s.mu.Lock()
	if d := time.Since(s.lastNom); d < minNomGap {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(minNomGap - d):
		}
		s.mu.Lock()
	}
	s.lastNom = time.Now()
	s.mu.Unlock()

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
			State   string `json:"state"`
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"address"`
	}
	if err := s.getJSON(ctx, s.endpoint(nominatimSearchURL)+"?"+v.Encode(), nominatimUA, &rows); err != nil {
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
		out = append(out, Place{
			Lat: lat, Lon: lon, Name: x.Name, Admin1: x.Address.State, Country: country,
			POP: pop, Feature: x.Feature, Source: "nominatim", CountryCode: x.CountryCode,
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

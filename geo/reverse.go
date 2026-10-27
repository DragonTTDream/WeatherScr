package geo

// 坐标反查。
// 铁律：**坐标是唯一真相；名字只是检索入口；
// 行政区一律从坐标反查，绝不从名字推。**
// 请求逐字（第 3 步）：
//	GET https://nominatim.openstreetmap.org/reverse
//	 ?format=jsonv2&addressdetails=1&accept-language=zh
// 层级原料（实测逐字）：address 里的 `country / state / region / city / district`
// + `ISO3166-2-lvl4`（语言无关的行政区码，定案用它做键）。
// zoom 取值（实测，杭州 30.2495201,120.1583013）：
//	zoom=10 → city=留下街道（街道级混进路径，闸门期望的'国/省/市/区'形状被污染）
//	zoom=8 → district=西湖区, region=杭州市, state=浙江省, country=中国
//	 ⇒ 完整路径 = 中国/浙江省/杭州市/西湖区（逐字等于期望串）
//	zoom=14/18 → 多出 suburb / road 级 ⇒ 更脏
//	⇒ 本包钉 zoom=8。
// 限流合规（geo/search.go:73-74 的既有常量）：Nominatim ≤1 req/s + 必须带
// 描述性 User-Agent ⇒ **复用搜索侧同一个节流器与 UA**，
// 每地点一次、持久缓存、不进轮询。
// 持久缓存键 = `lat,lon` 量化到 **4 位小数**（逐字）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

const nominatimReverseURL = "https://nominatim.openstreetmap.org/reverse"

// reverseZoom 反查层级（实测见文件头注释）。
const reverseZoom = 8

// Reverse 是一次坐标反查的结果（层级原料 + 完整路径 + ISO3166-2 码）。
type Reverse struct {
	Country     string
	State       string // address.state；缺失时回落 address.province（实测 Tokyo 只有 province）
	Region      string
	City        string
	District    string
	ISO3166     string // address["ISO3166-2-lvl4"]，语言无关行政区码
	CountryCode string // address.country_code（小写两位）
	FullPath    string // 国/省/市/区/…（缺级自动跳过，分隔符 "/"）
	FromCache   bool
}

// AdminPath 返回完整行政路径（`country/state/region/city/district`，缺级自动跳过）。
// 顺序逐字按 第 3 步列出的顺序。
func (r Reverse) AdminPath() string {
	parts := make([]string, 0, 5)
	for _, s := range []string{r.Country, r.State, r.Region, r.City, r.District} {
		if s == "" {
			continue // 缺级自动跳过
		}
		parts = append(parts, s)
	}
	return joinPath(parts)
}

func joinPath(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "/"
		}
		out += p
	}
	return out
}

// adminComponents 返回反查侧的行政区层级（不含国家，已去空）。
func (r Reverse) adminComponents() []string {
	return nonEmpty([]string{r.State, r.Region, r.City, r.District})
}

// reverseCacheKey 持久缓存键 = `lat,lon` 4 位小数。
func reverseCacheKey(lat, lon float64) string {
	return fmt.Sprintf("%.4f,%.4f", lat, lon)
}

// reverseCache 是进程内 + 磁盘两层的反查缓存。
type reverseCache struct {
	mu     sync.Mutex
	path   string
	m      map[string]Reverse
	loaded bool
}

var geoCache = &reverseCache{}

// ReverseCachePath 返回反查缓存文件路径。
// 落点 = **既有 geo 缓存目录**：`os.UserConfigDir/WeatherScr`，
// 与 config.CachePath 同族（config 的缓存文件就在同一目录）。
// 注意：本包**不能 import config** —— config → ui → geo 会成 import 环，
// 故这里按同一族口径自行落点（安装目录不可写时的口径一致）。
func ReverseCachePath() string {
	if d, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(d, "WeatherScr")
		if err := os.MkdirAll(p, 0o755); err == nil {
			return filepath.Join(p, "geo-reverse-cache.json")
		}
	}
	return filepath.Join(os.TempDir(), "WeatherScr-geo-reverse-cache.json")
}

func (c *reverseCache) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.m = map[string]Reverse{}
	b, err := os.ReadFile(c.Path())
	if err != nil {
		return
	}
	var disk map[string]Reverse
	if json.Unmarshal(b, &disk) != nil {
		return
	}
	for k, v := range disk {
		v.FromCache = true
		c.m[k] = v
	}
}

func (c *reverseCache) Path() string {
	if c.path == "" {
		c.path = ReverseCachePath()
	}
	return c.path
}

func (c *reverseCache) get(k string) (Reverse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	r, ok := c.m[k]
	return r, ok
}

func (c *reverseCache) put(k string, r Reverse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.m[k] = r
	b, err := json.MarshalIndent(c.m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.Path(), b, 0o644)
}

// Reverse 按坐标反查行政区（命中缓存 ⇒ 零请求）。
// 节流：复用搜索侧的 `minNomGap` + `nominatimUA`。
// lang = accept-language。
func (s *Searcher) Reverse(ctx context.Context, lat, lon float64, lang string) (Reverse, error) {
	if lang == "" {
		lang = "zh"
	}
	key := reverseCacheKey(lat, lon)
	if r, ok := geoCache.get(key); ok {
		r.FromCache = true
		return r, nil
	}
	if err := s.nominatimSlot(ctx); err != nil {
		return Reverse{}, err
	}

	v := url.Values{}
	v.Set("format", "jsonv2")
	v.Set("lat", fmt.Sprintf("%.7f", lat))
	v.Set("lon", fmt.Sprintf("%.7f", lon))
	v.Set("zoom", fmt.Sprintf("%d", reverseZoom))
	v.Set("addressdetails", "1")
	v.Set("accept-language", lang)

	// jsonv2 + addressdetails=1：层级全在 **address 嵌套**里（实测顶层无 country/country_code，
	// 与搜索侧 geo/search.go 的同一坑）。ISO3166-2-lvl4 的键名大小写逐字照 OSM。
	var r struct {
		Address struct {
			Country     string `json:"country"`
			State       string `json:"state"`
			Province    string `json:"province"` // 实测 Tokyo 只给 province 不给 state
			Region      string `json:"region"`
			City        string `json:"city"`
			District    string `json:"district"`
			CountryCode string `json:"country_code"`
			ISO         string `json:"ISO3166-2-lvl4"`
		} `json:"address"`
	}
	// 反查逐字官方域名：不受 weather_base_url 覆盖（理由见 geo/search.go 的 openMeteoEndpoint 注释）。
	u := nominatimReverseURL + "?" + v.Encode()
	if err := s.getJSON(ctx, u, nominatimUA, &r); err != nil {
		return Reverse{}, err
	}
	out := Reverse{
		Country:     r.Address.Country,
		State:       r.Address.State,
		Region:      r.Address.Region,
		City:        r.Address.City,
		District:    r.Address.District,
		ISO3166:     r.Address.ISO,
		CountryCode: r.Address.CountryCode,
	}
	if out.State == "" {
		out.State = r.Address.Province
	}
	out.FullPath = out.AdminPath()
	geoCache.put(key, out)
	return out, nil
}

// ReverseCachedOnly 只读缓存（运行时口径：设置里选点时已反查过 ⇒ 命中缓存零请求）。
func ReverseCachedOnly(lat, lon float64) (Reverse, bool) {
	return geoCache.get(reverseCacheKey(lat, lon))
}

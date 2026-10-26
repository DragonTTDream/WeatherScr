package main

// 职责（卡9-app/scr）：取数与天气态：天气状态与缓存、官方信号拉取、月相、帧预算速度系数、地名显示
// 搬移来源：cmd/scr/main.go:1128-1188,1228-1267,1311-1434,1543-1569,1632-1650（逐字，含注释）
// 规格：plans/active.md:214 —— app/scr = 屏保生命周期；本卡零行为变化。
// refresher（旧 main.go:1478-1541）因闸门锚点留在 main.go，见该文件说明。

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/geo"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// weatherState 是帧环与后台刷新共享的天气状态。
type weatherState struct {
	mu    sync.Mutex
	s     *weather.Sample
	cls   effect.Class
	isDay bool
}

type snapshot struct {
	Sample *weather.Sample
	Class  effect.Class
	IsDay  bool
}

// moonPhaseOf 从 Sample 取今日月相，nil-safe（Sample 为 nil 时返回 nil ⇒
// 月亮层不画月亮，与新月一致，规格 2.3）。需求 5。
// lastMoonPhase 记住**上一次取到的有效月相**。
// 缺陷：取不到月相时返回 nil，而月亮层旧口径对 nil 画的是 p=0.5 = **满月**
// ⇒ 老机器上一次抓取打嗝（网络延迟/超时）就让残月当场变满月，抓到了又变回来
// ⇒ 读图发现残月和满月竟然在眼前切换了。
// （nil 的画法已改为"不画月亮"，这条沿用上一次的口径照旧保留：一天只走 0.034 的量，
// 没有理由在两次抓取之间从"有月"跳到"无月"。）
// 口径：月相一天只走约 1/29.5 ≈ 0.034，**没有理由在两次抓取之间跳变**
// ⇒ 取不到就沿用上一次的，绝不回落成满月。
var lastMoonPhase float64

func moonPhaseOf(s *weather.Sample) *float64 {
	if s != nil && s.MoonPhase != nil {
		lastMoonPhase = *s.MoonPhase
		v := lastMoonPhase
		return &v
	}
	if lastMoonPhase > 0 {
		v := lastMoonPhase
		return &v
	}
	return nil // 开机以来一次都没取到 ⇒ 交月亮层按满月画
}

func (st *weatherState) set(s *weather.Sample) {
	// 分档口径 = **三模型云量的中位数**（规格 1.6）：
	// <30 晴、30–70 多云见日、>70 阴；降水类（雨/雪/雷/雾/尘）仍由天气码决定。
	// 中位数取不到（三家都没给 / 当前小时不在 hourly 里）⇒ 退回既有口径
	// （current.cloud_cover + 天气码），不空不崩。
	cls, _ := weather.ClassFromCloud(s.WeatherCode, s.CloudMedian)
	if s.CloudMedian == nil {
		cls, _ = effect.ClassifyByCloudCover(s.WeatherCode, s.CloudCover)
	}
	// 天气类覆盖：实测云/雨/雾这些类时，低性能 x86 小主机当时的真实天气
	// 往往就是晴天（实测杭州"Mainly clear"），
	// 看不到云就无从判断。exe 旁放 scr.cls 写类名即强制该类。
	// 只影响动画类，不改温度/湿度/风力等真实读数与文字。
	if ov := classOverride(); ov != nil {
		cls = *ov
	}
	st.mu.Lock()
	st.s = s
	st.cls = cls
	st.isDay = s.IsDay == 1
	st.mu.Unlock()
}

func (st *weatherState) snapshot() snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	return snapshot{Sample: st.s, Class: st.cls, IsDay: st.isDay}
}

// lastGood 读上次成功的天气。无缓存返回 nil。
//
// **必须按位置键取，取不到再按 CachedAt 取最新的一条**
// "早上8点看到背景的天气从白天突然变成晚上，过了一会了又变回了白天"。
// 原实现 `for _, v := range cache { return &vv }` 取的是 **Go map 迭代的第一条**，
// 而 Go 的 map 顺序是随机的。低性能 x86 小主机缓存里存着 3 条记录：
//
//	"Hangzhou, China" → is_day=0（旧地名、旧时刻留下的）
//	"Hangzhou"     → is_day=1
//	"杭州市"       → is_day=1
//
// 每次 API 503 失败（低性能 x86 小主机日志里 503 频繁）就走这条回落路径，
// 于是有 1/3 概率捞到那条 is_day=0 的旧记录 ⇒ 整个场景翻成黑夜，
// 下次刷新成功又翻回来。日志逐字对应：每个 SCR_FETCH_FAIL 后必跟 isDay=false，
// 每个 SCR_FETCH_OK 后必跟 isDay=true。
func lastGood(key string) *weather.Sample {
	cache := weather.LoadCache(cachePath())
	if len(cache) == 0 {
		return nil
	}
	if v, ok := cache[key]; ok {
		vv := v
		return &vv
	}
	var best *weather.Sample
	for _, v := range cache {
		vv := v
		if best == nil || vv.CachedAt > best.CachedAt {
			b := vv
			best = &b
		}
	}
	return best
}

// ---- 官方信号层（批次 b-alert-v2 第 6/7 步）----
//
// 节奏：10 min 一次省级请求（spec/b-alert-v2.md 第 6 步逐字），与自算层的 1 min 刷新分开。
// 发请求的前置（spec 第 1 步）：**仅 country_code==CN 才发**，非 CN ⇒ 跳过。
// 省名来源（§12 铁律）：坐标反查得 `ISO3166-2-lvl4` ⇒ 闭表查官方全称，
// 绝不从地名推。运行时优先读反查缓存（设置里选点时已反查过 ⇒ 命中缓存零请求）。
type officialState struct {
	mu     sync.Mutex
	page   weather.NMCPage
	have   bool
	fetchd time.Time
}

var officialStateVar officialState

func setOfficial(p weather.NMCPage, at time.Time) {
	officialStateVar.mu.Lock()
	officialStateVar.page, officialStateVar.have, officialStateVar.fetchd = p, true, at
	officialStateVar.mu.Unlock()
}

// clearOfficial 取不到官方数据 ⇒ 清空，显示回退自算层（态②/③）。
func clearOfficial() {
	officialStateVar.mu.Lock()
	officialStateVar.page, officialStateVar.have = weather.NMCPage{}, false
	officialStateVar.mu.Unlock()
}

func getOfficial() (weather.NMCPage, bool) {
	officialStateVar.mu.Lock()
	defer officialStateVar.mu.Unlock()
	return officialStateVar.page, officialStateVar.have
}

// officialEvery 官方信号拉取节奏（spec/b-alert-v2.md 第 6 步逐字 = 10 min）。
const officialEvery = 10 * time.Minute

// nmcRequestAllowed 报告这个国家码是否允许发官方请求。
//
// spec/b-alert-v2.md 目标 1 逐字：**仅 country_code==CN 才发**，非中国区不发。
// 单测钉死在 cmd/scr/nmc_test.go（闸门第 8 步的 `NMC=skipped` 同源口径）。
func nmcRequestAllowed(countryCode string) bool {
	return strings.EqualFold(strings.TrimSpace(countryCode), "CN")
}

// officialRefresher 官方信号的独立后台循环。
//
// 批次 A1 S3（spec/a1-fetch-polite.md）：节奏 = officialEvery（常量逐字不动）
// **+ OfficialJitter(seed)**，0–30 s 的**确定性**抖动（同一 seed 可复现），
// 让官方拉取不与天气刷新钉在同一刻齐发。seed = 规范化地点名的 FNV-1a，
// 不读时钟、不含随机源 ⇒ 同机同点每次算出同一个抖动。
func officialRefresher(cfg config.Config) {
	var h uint64 = 14695981039346656037
	for _, b := range []byte(weather.NormalizeLocationKey(cfg.Location)) {
		h = (h ^ uint64(b)) * 1099511628211
	}
	seed := int64(h)
	for {
		refreshOfficial(cfg)
		time.Sleep(officialEvery + weather.OfficialJitter(seed))
	}
}

// refreshOfficial 跑一次官方拉取并打带外日志（spec 第 7 步逐字格式）。
//
// 带外日志逐字：`nmc: province=%s count=%d top=%s/%s`
// 且 **count=0 必须打 `NOTE=count0-unknown`**（§15.1：无效省名也返回 0，
// 不得当"该省无预警"上报）。
func refreshOfficial(cfg config.Config) {
	if cfg.LocationLat == 0 && cfg.LocationLon == 0 {
		log.Printf("nmc: skipped reason=no-coords")
		clearOfficial()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	se := &geo.Searcher{BaseURL: cfg.WeatherBaseURL}
	r, ok := geo.ReverseCachedOnly(cfg.LocationLat, cfg.LocationLon)
	if !ok {
		rr, err := se.Reverse(ctx, cfg.LocationLat, cfg.LocationLon, "zh")
		if err != nil {
			log.Printf("nmc: skipped reason=reverse-fail err=%v", err)
			clearOfficial()
			return
		}
		r = rr
	}
	if !nmcRequestAllowed(r.CountryCode) {
		// spec/b-alert-v2.md 目标 1：非中国区不发官方请求。
		log.Printf("nmc: skipped reason=not-cn country=%s", r.CountryCode)
		clearOfficial()
		return
	}
	prov, ok := geo.NMCProvince(r.ISO3166)
	if !ok {
		log.Printf("nmc: skipped reason=no-province-mapping iso=%s", r.ISO3166)
		clearOfficial()
		return
	}
	page, err := weather.FindAlarms(ctx, &http.Client{Timeout: 15 * time.Second}, prov)
	if err != nil {
		log.Printf("nmc: province=%s err=%v", prov, err)
		clearOfficial()
		return
	}
	setOfficial(page, time.Now())
	top, ok := weather.NMCTop(page)
	switch {
	case page.Count == 0:
		// §15.1 判据缺陷：count=0 不能证明省名被接受，也不能证明该省无预警。
		log.Printf("nmc: province=%s count=%d top=%s/%s NOTE=%s",
			prov, page.Count, "-", "-", weather.NMCCount0Unknown)
	case !ok:
		// 标题正则不匹配（§15.3 的 3 条样本）⇒ 降级，不静默丢弃：日志里点名条数。
		log.Printf("nmc: province=%s count=%d top=%s/%s NOTE=title-unparsed n=%d",
			prov, page.Count, "-", "-", len(page.Items))
	default:
		log.Printf("nmc: province=%s count=%d top=%s/%s", prov, page.Count, top.TypeZh, top.TitleColor)
		if !top.CrossOK {
			log.Printf("nmc: crosscheck province=%s alertid=%s pic=%s title=%q NOTE=cross-mismatch-degraded-title-only",
				prov, top.AlertID, top.Pic, top.Title)
		}
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func cachePath() string {
	return config.CachePath()
}

// quit 是唯一的退出出口：先还原任务栏再退出，
// 绝不把登录账户的任务栏留在隐藏态。
func quit(code int) {
	restoreTaskbar()
	os.Exit(code)
}

// speedFromBudget 由帧预算算位移速度系数（实测口径）：
// speedScale = 目标帧率 / 15 ⇒ 每帧位移恒定 = vx/15，与 15 fps 逐帧观感一致。
func speedFromBudget(budget time.Duration) float64 {
	if budget <= 0 {
		return 1
	}
	fps := float64(time.Second) / float64(budget)
	return fps / 15.0
}

// displayLocation 返回显示用的地名。
//
// 铁律：**display_language 默认 "en" = 当前样式，不改**；
// 设置里选了中文地名 **不等于** 显示语言变中文 —— 两者必须互相独立。
// ⇒ en 取存下的英文名 location_name_en，zh 取 location_name_zh，
//
//	对应语言没存过才回落到 location 原值（旧配置兼容）。
func displayLocation(cfg config.Config) string {
	if cfg.DisplayLanguage == "zh" {
		if cfg.LocationNameZh != "" {
			return cfg.LocationNameZh
		}
		return cfg.Location
	}
	if cfg.LocationNameEn != "" {
		return cfg.LocationNameEn
	}
	return cfg.Location
}

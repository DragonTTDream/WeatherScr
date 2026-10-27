package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/DragonTTDream/WeatherScr/weather"
)

// alertdiag = 取数自检（判据第 5 条）：
// 只打印请求参数与解析计数，**不渲染**。
// 打印的四项 = 第 4 步数据闸门的同一组数（cmd/scr/main.go refresher 里
// 那行 `alertdiag: HOURLY_SLOTS=… PROB_N=… MM_N=… MIN15_N=…` 逐字同格式）：
//
//	HOURLY_SLOTS = 响应 hourly.time 条数
//	PROB_N = 逐模型中位数后"有概率值"的格数
//	MM_N = 同口径的毫米格数
//	MIN15_N = 15 分钟层解析出的格数
//
// 默认坐标 = 取证坐标 30.2495,120.1583（的 P1–P4 探针坐标）。
func main() {
	lat := flag.Float64("lat", 30.2495, "纬度（取证坐标）")
	lon := flag.Float64("lon", 120.1583, "经度（取证坐标）")
	city := flag.String("city", "Hangzhou, China", "地名（只进 Sample.Location 与日志）")
	base := flag.String("base", "", "自建代理基址（空 = 直连 api.open-meteo.com）")
	flag.Parse()

	v := weather.RequestParams(*lat, *lon)
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + strings.Join(v[k], ",") + " ")
	}
	fmt.Printf("PARAMS: %s\n", strings.TrimSpace(b.String()))

	c := &weather.Client{
		HTTP:      &http.Client{Timeout: 25 * time.Second},
		BaseURL:   *base,
		HasCoords: true,
		Lat:       *lat,
		Lon:       *lon,
	}
	s, err := c.Fetch(*city)
	if err != nil {
		fmt.Printf("ALERTDIAG_FAIL=%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("HOURLY_SLOTS=%d PROB_N=%d MM_N=%d MIN15_N=%d\n",
		len(s.HourlyTime), s.ProbN, s.MMN, s.Min15N)
	// 时基口径与渲染端一致：local := now.UTC() + utc_offset_seconds
	local := time.Now().UTC().Add(time.Duration(s.UTCOffsetSeconds) * time.Second)
	if tier, mm, hrs := weather.DisasterTier(s, local); tier != weather.TierNone {
		fmt.Printf("DISASTER tier=%d mm=%.1f hours=%d\n", tier, mm, hrs)
	}
	// 带伞行（态③）判据的取证：2 h 视野内第一个达门槛的 15 分钟格。
	// 视野与门槛逐字取渲染端同一常量口径（weather/window.go: umbrellaViewHours=2、默认 0.05 mm/15min）。
	if at, prob, mmh, ok := weather.FirstWetCell(s, local, 2, 0.05); ok {
		fmt.Printf("UMBRELLA at=%s prob=%d mmph=%.2f\n", at.Format("15:04"), prob, mmh)
	} else {
		fmt.Printf("UMBRELLA none\n")
	}
}

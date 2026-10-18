// S2 实取验证：真打 Open-Meteo，证明参数集被服务器接受，
// 且产出与 golden fixture 同形状（10 字段）。
//
// 用法：wfetch [城市]   （默认 "Hangzhou, China"，与 golden 同城）
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/DragonTTDream/WeatherScr/weather"
)

func main() {
	city := "Hangzhou, China"
	if len(os.Args) > 1 {
		city = os.Args[1]
	}
	timeout := 30 * time.Second
	if v := os.Getenv("WF_TIMEOUT_S"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}

	c := &weather.Client{HTTP: &http.Client{Timeout: timeout}}

	t0 := time.Now()
	lat, lon, err := c.Geocode(city)
	if err != nil {
		fmt.Printf("GEOCODE_FAIL=%v 用时=%s\n", err, time.Since(t0).Round(time.Millisecond))
		os.Exit(1)
	}
	fmt.Printf("GEOCODED=%s -> %.4f,%.4f 用时=%s\n", city, lat, lon, time.Since(t0).Round(time.Millisecond))

	t1 := time.Now()
	s, err := c.Fetch(city)
	if err != nil {
		fmt.Printf("FETCH_FAIL=%v 用时=%s\n", err, time.Since(t1).Round(time.Millisecond))
		os.Exit(1)
	}
	fmt.Printf("FETCH_MS=%s\n", time.Since(t1).Round(time.Millisecond))

	cache := weather.Cache{city: *s}
	b, _ := json.MarshalIndent(cache, "", "    ")
	fmt.Printf("SAMPLE_JSON=\n%s\n", b)

	// 逐字段存在性（golden 的 10 字段口径）
	fmt.Printf("HAS_TEMPERATURE=%v\n", s.Temperature != nil)
	fmt.Printf("HAS_HUMIDITY=%v\n", s.Humidity != nil)
	fmt.Printf("HAS_PRECIP_PROB=%v\n", s.PrecipitationProbability != nil)
	fmt.Printf("HAS_WINDSPEED=%v\n", s.WindSpeed != nil)
	fmt.Printf("HAS_FORECAST=%v\n", s.Forecast != nil)
	fmt.Printf("CONDITION=%s\n", s.Condition)
	fmt.Printf("WEATHER_CODE=%d\n", s.WeatherCode)
	fmt.Printf("IS_DAY=%d\n", s.IsDay)
	fmt.Printf("WFETCH_DONE\n")
}

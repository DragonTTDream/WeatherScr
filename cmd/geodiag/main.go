package main

// geodiag = 地理选点的带外诊断（批次 b-alert-v2 第 8 步）。
//
// 用法：`geodiag <地名> <lang>`（lang = zh / en，只影响反查的 accept-language）
// 打印形状照 cmd/alertdiag/main.go（只打印、不渲染）：
//
//	FULLPATH=<国/省/市/区 完整行政路径，缺级自动跳过>
//	ISO=<ISO3166-2-lvl4>
//	LATLON=<lat,lon 4 位小数>
//	MISMATCH=<true/false>
//	NMC=<CN ⇒ province=… count=… top=…/…；非 CN ⇒ skipped>
//
// 口径（spec/alert-latency-and-logic.md §12 铁律）：坐标是唯一真相，
// 行政区一律从坐标反查 ⇒ FULLPATH/ISO 都来自**搜索命中坐标的反查结果**。
// 非 CN ⇒ **不发官方请求**（spec/b-alert-v2.md 目标 1）⇒ 打 `NMC=skipped`。
import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DragonTTDream/WeatherScr/geo"
	"github.com/DragonTTDream/WeatherScr/weather"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: geodiag <地名> <lang>")
		os.Exit(1)
	}
	q := os.Args[1]
	lang := "zh"
	if len(os.Args) > 2 {
		lang = os.Args[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	se := &geo.Searcher{ReverseLang: lang}
	places, err := se.Search(ctx, q)
	if err != nil {
		fmt.Printf("GEODIAG_FAIL=%v\n", err)
		os.Exit(1)
	}
	p := places[0]
	fmt.Printf("CANDIDATES=%d\n", len(places))
	fmt.Printf("FULLPATH=%s\n", p.PathOrLabel())
	fmt.Printf("ISO=%s\n", p.ISO3166)
	fmt.Printf("LATLON=%.4f,%.4f\n", p.Lat, p.Lon)
	fmt.Printf("MISMATCH=%v\n", p.Mismatch)

	if !strings.EqualFold(p.CountryCode, "CN") {
		fmt.Printf("NMC=skipped country=%s\n", p.CountryCode)
		return
	}
	prov, ok := geo.NMCProvince(p.ISO3166)
	if !ok {
		fmt.Printf("NMC=skipped reason=no-province-mapping iso=%s\n", p.ISO3166)
		return
	}
	page, err := weather.FindAlarms(ctx, &http.Client{Timeout: 25 * time.Second}, prov)
	if err != nil {
		fmt.Printf("NMC=province=%s err=%v\n", prov, err)
		os.Exit(1)
	}
	top, ok := weather.NMCTop(page)
	if !ok {
		fmt.Printf("NMC=province=%s count=%d top=%s/%s\n", prov, page.Count, "-", "-")
	} else {
		fmt.Printf("NMC=province=%s count=%d top=%s/%s\n", prov, page.Count, top.TypeZh, top.TitleColor)
	}
	if page.Note != "" {
		fmt.Printf("NMC_NOTE=%s\n", page.Note)
	}
}

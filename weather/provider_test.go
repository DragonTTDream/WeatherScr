package weather

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestGolden："天气 schema"判据 —— 用低性能 x86 小主机实况的 cache/weather.json 当 golden
// fixture，逐字段比对。fixture 原文来自低性能 x86 小主机上上游程序（SRPSS）自己写的
// 天气缓存 `<上游数据目录>\cache\weather.json`（取证）。
func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/weather.json")
	if err != nil {
		t.Fatalf("读 golden fixture 失败: %v", err)
	}

	// 1 顶层 key 集合
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("fixture 顶层解析失败: %v", err)
	}
	if len(top) != 1 {
		t.Fatalf("顶层条目数 = %d，期望 1", len(top))
	}
	if _, ok := top["Hangzhou, China"]; !ok {
		t.Fatalf("顶层 key 缺 \"Hangzhou, China\"，实得 %v", keysOf(top))
	}

	// 2 字段集合必须恰好是这 10 个，不得增删改名
	var obj map[string]any
	if err := json.Unmarshal(top["Hangzhou, China"], &obj); err != nil {
		t.Fatalf("条目解析失败: %v", err)
	}
	want := []string{
		"_cached_at", "condition", "forecast", "humidity", "is_day",
		"location", "precipitation_probability", "temperature", "weather_code", "windspeed",
	}
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("字段集合不匹配\n  实得 %v\n  期望 %v", got, want)
	}

	// 3 逐字段值比对
	var c Cache
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("按 Sample schema 解析失败: %v", err)
	}
	s, ok := c["Hangzhou, China"]
	if !ok {
		t.Fatalf("按 schema 解析后缺 \"Hangzhou, China\"")
	}
	fnum := func(name string, p *float64) float64 {
		if p == nil {
			t.Fatalf("%s 为 null，golden 里应为数值", name)
		}
		return *p
	}
	if v := fnum("temperature", s.Temperature); math.Abs(v-25.2) > 1e-9 {
		t.Errorf("temperature = %v，期望 25.2", v)
	}
	if s.Condition != "Clear sky" {
		t.Errorf("condition = %q，期望 \"Clear sky\"", s.Condition)
	}
	if s.Location != "Hangzhou, China" {
		t.Errorf("location = %q，期望 \"Hangzhou, China\"", s.Location)
	}
	if v := fnum("humidity", s.Humidity); math.Abs(v-66.0) > 1e-9 {
		t.Errorf("humidity = %v，期望 66.0", v)
	}
	if v := fnum("precipitation_probability", s.PrecipitationProbability); math.Abs(v-39.0) > 1e-9 {
		t.Errorf("precipitation_probability = %v，期望 39.0", v)
	}
	if v := fnum("windspeed", s.WindSpeed); math.Abs(v-4.0) > 1e-9 {
		t.Errorf("windspeed = %v，期望 4.0", v)
	}
	wantForecast := "Tomorrow: 20°-25°C/Light Drizzle"
	if s.Forecast == nil || *s.Forecast != wantForecast {
		t.Errorf("forecast = %v，期望 %q", s.Forecast, wantForecast)
	}
	if s.IsDay != 1 {
		t.Errorf("is_day = %d，期望 1", s.IsDay)
	}
	if s.WeatherCode != 0 {
		t.Errorf("weather_code = %d，期望 0", s.WeatherCode)
	}
	if s.CachedAt <= 0 {
		t.Errorf("_cached_at = %v，应为正数", s.CachedAt)
	}

	// 4 类型形态：上游把 humidity/precipitation_probability/windspeed 存成带小数点的浮点
	for _, k := range []string{"humidity", "precipitation_probability", "windspeed", "temperature"} {
		if _, isFloat := obj[k].(float64); !isFloat {
			t.Errorf("%s 在 fixture 里不是数值，实得 %T", k, obj[k])
		}
	}
	for _, k := range []string{"is_day", "weather_code"} {
		n, isNum := obj[k].(float64)
		if !isNum || n != math.Trunc(n) {
			t.Errorf("%s 在 fixture 里不是整数形态，实得 %v", k, obj[k])
		}
	}
}

// TestRequestShape：判据 —— 参数集合与逐键比对。
func TestRequestShape(t *testing.T) {
	v := RequestParams(29.34, 104.77)

	wantKeys := []string{
		"current", "daily", "forecast_days", "forecast_hours",
		"hourly", "latitude", "longitude", "models", "timezone",
	}
	got := make([]string, 0, len(v))
	for k := range v {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("参数键集合不匹配\n  实得 %v\n  期望 %v", got, wantKeys)
	}

	want := map[string]string{
		"latitude":  "29.34",
		"longitude": "104.77",
		// current 与上游逐字 + **cloud_cover**：本项目独有的一个字段。
		// 上游天气码只喂图标与文字、背景动画由设置里选场景；"背景随天气变"是本项目的产品需求，
		// 按逐小时云量分档 ⇒ 这条偏离是**已采纳的**。
		// + **precipitation_probability**（需求 1）：伞图标改取当前时刻；
		// 实测该字段被接受（需求 4） ⇒ 上游 open_meteo_provider.py:366-368
		// 那句"current 块里没有该字段"注释被实测推翻。
		"current": "temperature_2m,weather_code,wind_speed_10m,is_day,relative_humidity_2m,precipitation,rain,cloud_cover,precipitation_probability",
		// hourly + **precipitation**（mm）：预警改"未来 N 小时滚动窗口"要逐小时毫米数求和
		// （实测接受，units % 与 mm，需求 1）。
		// + **cloud_cover**（规格 1.6）：与 models= 配对，实测响应给出逐模型键
		// cloud_cover_gfs_seamless / cloud_cover_icon_seamless / cloud_cover_ecmwf_ifs025。
		"hourly": "precipitation_probability,precipitation,cloud_cover",
		// models（规格 1.6）：三家模型逐字，多模型云量的开关。
		"models": "gfs_seamless,icon_seamless,ecmwf_ifs025",
		// daily 与上游逐字 + **precipitation_probability_max / precipitation_sum / moon_phase**：
		// 降雨提醒的数据源+ 今日月相。
		// 上游无任何预警功能、daily 只有三键
		// （open_meteo_provider.py:332）⇒ 这些偏离与上面的 cloud_cover 同类，是**已采纳的**。
		"daily":          "temperature_2m_max,temperature_2m_min,weathercode,precipitation_probability_max,precipitation_sum,moon_phase",
		"forecast_days":  "6",
		"forecast_hours": "1",
		"timezone":       "auto",
	}
	for k, wv := range want {
		if got := v.Get(k); got != wv {
			t.Errorf("参数 %s = %q，期望 %q", k, got, wv)
		}
	}

	g := GeocodeParams("Hangzhou, China")
	gotG := make([]string, 0, len(g))
	for k := range g {
		gotG = append(gotG, k)
	}
	sort.Strings(gotG)
	wantG := []string{"count", "format", "language", "name"}
	if strings.Join(gotG, ",") != strings.Join(wantG, ",") {
		t.Fatalf("geocoding 参数键集合不匹配\n  实得 %v\n  期望 %v", gotG, wantG)
	}
	if g.Get("name") != "Hangzhou, China" || g.Get("count") != "1" ||
		g.Get("language") != "en" || g.Get("format") != "json" {
		t.Errorf("geocoding 参数值不匹配: %v", g)
	}
}

// TestConditionText：condition 文案必须逐字等于上游 WEATHER_CODES。
func TestConditionText(t *testing.T) {
	if len(Codes) != 28 {
		t.Fatalf("WEATHER_CODES 码数 = %d，期望 28", len(Codes))
	}
	pairs := map[int]string{
		0: "Clear sky", 1: "Mainly clear", 2: "Partly cloudy", 3: "Overcast",
		45: "Fog", 48: "Depositing rime fog",
		51: "Light drizzle", 53: "Moderate drizzle", 55: "Dense drizzle",
		56: "Light freezing drizzle", 57: "Dense freezing drizzle",
		61: "Slight rain", 63: "Moderate rain", 65: "Heavy rain",
		66: "Light freezing rain", 67: "Heavy freezing rain",
		71: "Slight snow", 73: "Moderate snow", 75: "Heavy snow", 77: "Snow grains",
		80: "Slight rain showers", 81: "Moderate rain showers", 82: "Violent rain showers",
		85: "Slight snow showers", 86: "Heavy snow showers",
		95: "Thunderstorm", 96: "Thunderstorm with slight hail", 99: "Thunderstorm with heavy hail",
	}
	for c, w := range pairs {
		if got := Condition(c); got != w {
			t.Errorf("Condition(%d) = %q，期望 %q", c, got, w)
		}
	}
	if got := Condition(12345); got != "Unknown" {
		t.Errorf("未知码应回 Unknown，实得 %q", got)
	}
}

// TestTomorrowText：明日行文案格式，含 golden 实况那一串。
func TestTomorrowText(t *testing.T) {
	mins := []float64{21, 20, 19}
	maxs := []float64{26, 25, 24}
	codes := []int{0, 51, 95}
	got := tomorrowText(mins, maxs, codes)
	if got == nil {
		t.Fatalf("tomorrowText 返回 nil")
	}
	want := "Tomorrow: 20°-25°C/Light Drizzle"
	if *got != want {
		t.Errorf("明日行 = %q，期望 %q", *got, want)
	}
	// 只有一天数据 → 无明日行（上游 available>1 才生成）
	if p := tomorrowText([]float64{21}, []float64{26}, []int{0}); p != nil {
		t.Errorf("单天数据应无明日行，实得 %q", *p)
	}
}

// TestTitleCase：对齐 Python str.title()。
func TestTitleCase(t *testing.T) {
	cases := map[string]string{
		"light drizzle":                 "Light Drizzle",
		"Light drizzle":                 "Light Drizzle",
		"thunderstorm with slight hail": "Thunderstorm With Slight Hail",
		"depositing rime fog":           "Depositing Rime Fog",
	}
	for in, want := range cases {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestNormalizeLocationKey：折叠空白 + casefold。
func TestNormalizeLocationKey(t *testing.T) {
	cases := map[string]string{
		"Hangzhou, China":       "hangzhou, china",
		"  Hangzhou,   China  ": "hangzhou, china",
		"HANGZHOU,CHINA":        "hangzhou,china",
		"Hangzhou\t\nChina":     "hangzhou china",
	}
	for in, want := range cases {
		if got := NormalizeLocationKey(in); got != want {
			t.Errorf("NormalizeLocationKey(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestCacheRoundTrip：落盘 → 读回，字段不丢不改名。
func TestCacheRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/weather.json")
	if err != nil {
		t.Fatalf("读 fixture 失败: %v", err)
	}
	c := Cache{}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	p := t.TempDir() + "/weather.json"
	if err := SaveCache(p, c); err != nil {
		t.Fatalf("写缓存失败: %v", err)
	}
	back := LoadCache(p)
	if len(back) != 1 {
		t.Fatalf("读回条目数 = %d，期望 1", len(back))
	}
	a := back["Hangzhou, China"]
	b := c["Hangzhou, China"]
	if a.Condition != b.Condition || a.WeatherCode != b.WeatherCode || a.IsDay != b.IsDay {
		t.Errorf("读回后字段漂移: %+v vs %+v", a, b)
	}
	fa, fb := a.Forecast, b.Forecast
	if (fa == nil) != (fb == nil) || (fa != nil && *fa != *fb) {
		t.Errorf("forecast 读回漂移: %v vs %v", fa, fb)
	}
}

// TestLoadCacheMissingIsNotFatal：文件缺失 → 空缓存，不报错（上游 last-good 语义）。
func TestLoadCacheMissingIsNotFatal(t *testing.T) {
	if c := LoadCache(t.TempDir() + "/nope.json"); len(c) != 0 {
		t.Errorf("缺失文件应得空缓存，实得 %d 条", len(c))
	}
	bad := t.TempDir() + "/bad.json"
	if err := os.WriteFile(bad, []byte("{ 这不是 json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := LoadCache(bad); len(c) != 0 {
		t.Errorf("损坏文件应得空缓存，实得 %d 条", len(c))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

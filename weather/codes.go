// Package weather 实现 Open-Meteo 客户端与上游逐字段对齐的数据 schema。
// 基准来源（逐字核对，2026-10-15）：上游 Basjohn/ShittyRandomPhotoScreenSaver
// main 分支 weather/open_meteo_provider.py。
package weather

// Codes 是上游 OpenMeteoProvider.WEATHER_CODES 的逐字复刻（28 码，WMO）。
// 顺序与文案必须与上游一致：condition 文案直接进 golden 对拍。
var Codes = map[int]string{
	0:  "Clear sky",
	1:  "Mainly clear",
	2:  "Partly cloudy",
	3:  "Overcast",
	45: "Fog",
	48: "Depositing rime fog",
	51: "Light drizzle",
	53: "Moderate drizzle",
	55: "Dense drizzle",
	56: "Light freezing drizzle",
	57: "Dense freezing drizzle",
	61: "Slight rain",
	63: "Moderate rain",
	65: "Heavy rain",
	66: "Light freezing rain",
	67: "Heavy freezing rain",
	71: "Slight snow",
	73: "Moderate snow",
	75: "Heavy snow",
	77: "Snow grains",
	80: "Slight rain showers",
	81: "Moderate rain showers",
	82: "Violent rain showers",
	85: "Slight snow showers",
	86: "Heavy snow showers",
	95: "Thunderstorm",
	96: "Thunderstorm with slight hail",
	99: "Thunderstorm with heavy hail",
}

// Condition 把 WMO 码翻成上游文案；未知码回 "Unknown"（上游 .get(code, "Unknown")）。
func Condition(code int) string {
	if s, ok := Codes[code]; ok {
		return s
	}
	return "Unknown"
}

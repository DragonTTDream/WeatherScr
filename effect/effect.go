// Package effect 把 WMO 码映射到背景特效类。
//
// 与规格的对齐说明（防止"7 类 vs 9 行"打架）：
//
//	定案是 **7 类**；上游实况表是 9 行。调和方式 = 7 个类，
//	其中 rain 与 snow 各带 light/heavy 两个强度变体（上游实况表里的 rain-light /
//	rain-heavy / snow-light / snow-heavy 就是变体，不是独立类。
//	类名对外用 "rain-light" 这种带强度的写法，与上游实况表逐行一致。
package effect

// 7 个类。
type Class string

const (
	Clear   Class = "clear"
	Cloudy  Class = "cloudy"
	Fog     Class = "fog"
	Rain    Class = "rain"
	Snow    Class = "snow"
	Thunder Class = "thunder"
	Dust    Class = "dust"
)

// 强度变体。
type Intensity string

const (
	Light Intensity = "light"
	Heavy Intensity = "heavy"
	None  Intensity = ""
)

// wmoTable：码 → 类 + 强度。基准 = 上游 WEATHER_CODES 的 28 码（0–99），
// 加扩展码段 101–110 给 dust。
//
// 诚实说明：Open-Meteo 核心码段是 0–99，**不含风沙码**。
// dust 只由扩展码 101–110 触发，核心码段下该类永不出现。
// 不得为了"看起来有 7 类"把它硬塞进 0–99 的某个码。
var wmoTable = map[int]struct {
	Class     Class
	Intensity Intensity
}{
	0:  {Clear, None},
	1:  {Clear, None},
	2:  {Cloudy, None},
	3:  {Cloudy, None},
	45: {Fog, None},
	48: {Fog, None},

	51: {Rain, Light},
	53: {Rain, Light},
	56: {Rain, Light},
	61: {Rain, Light},
	66: {Rain, Light},
	80: {Rain, Light},

	55: {Rain, Heavy},
	57: {Rain, Heavy},
	63: {Rain, Heavy},
	65: {Rain, Heavy},
	67: {Rain, Heavy},
	81: {Rain, Heavy},
	82: {Rain, Heavy},

	71: {Snow, Light},
	77: {Snow, Light},
	85: {Snow, Light},

	73: {Snow, Heavy},
	75: {Snow, Heavy},
	86: {Snow, Heavy},

	95: {Thunder, None},
	96: {Thunder, None},
	99: {Thunder, None},

	// 扩展码段（Open-Meteo 核心 0–99 不产出）
	101: {Dust, Light},
	102: {Dust, Light},
	103: {Dust, Light},
	104: {Dust, Light},
	105: {Dust, Heavy},
	106: {Dust, Heavy},
	107: {Dust, Heavy},
	108: {Dust, Heavy},
	109: {Dust, Heavy},
	110: {Dust, Heavy},
}

// Classify 返回码对应的类与强度。未知码 → clear（屏保不得因未知码空白或崩）。
func Classify(code int) (Class, Intensity) {
	if e, ok := wmoTable[code]; ok {
		return e.Class, e.Intensity
	}
	return Clear, None
}

// Variant 返回上游实况表里的行名（"rain-light" 这种），clear/cloudy/fog/thunder 无后缀。
func Variant(code int) string {
	c, i := Classify(code)
	if i == None {
		return string(c)
	}
	return string(c) + "-" + string(i)
}

// CoreCodes 返回核心码段（0–99，Open-Meteo 实际会产出的 28 码），升序。
func CoreCodes() []int {
	out := make([]int, 0, 28)
	for c := range wmoTable {
		if c <= 99 {
			out = append(out, c)
		}
	}
	sortInts(out)
	return out
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// ClassifyByCloudCover 按**逐小时云量**分档（需求）。
//
// 依据：上游 weather_code 只喂图标与文字（rendering/quick/widgets/weather.py:152-172
// _condition_icon_source，背景动画由设置里选场景；"背景随天气变"是本项目的产品需求。
// 实测落差：杭州 Open-Meteo weather_code=1（WMO"主要晴"）
// 而 hourly.cloud_cover=26%，国内天气应用同刻显示"多云" ⇒ 口径取按云量分档。
//
// 口径：
// - 降水/雾类（雨/雷/雪/雾）**仍由 WMO 码决定**——下没下雨、有没有雾不是云量能表达的；
// - 非降水区（码 0–3 那档，表里判 Clear 的）交给云量：
// <30% = Clear；≥30% = Cloudy（分档：<30 晴、30–60 少云、60–85 多云、>85 阴，
// 本项目只有 Clear/Cloudy 两档非降水类 ⇒ 30% 处切，少云起就给云粒子。
// - 云量缺失（API 未返回） ⇒ 退回 WMO 码，不猜。
func ClassifyByCloudCover(code int, cloudCover *int) (Class, Intensity) {
	c, i := Classify(code)
	if c != Clear {
		return c, i
	}
	if cloudCover == nil {
		return c, i
	}
	switch v := *cloudCover; {
	case v >= 30:
		return Cloudy, i
	default:
		return Clear, i
	}
}

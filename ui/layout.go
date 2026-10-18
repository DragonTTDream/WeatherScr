// Package ui 负责竖屏 1080×1920 的布局与绘制。
//
// 布局基准 = 上游 settings_v2.json 的 custom_layout 归一化 rect（实测取证）。
package ui

// Rect 是归一化矩形（0–1），与上游 custom_layout 同口径。
type Rect struct {
	X, Y, W, H float64
}

// 上游实况 rect。
var (
	ClockRect   = Rect{X: 0.013, Y: 0.244, W: 0.973, H: 0.210}
	WeatherRect = Rect{X: 0.027, Y: 0.531, W: 0.946, H: 0.291}
)

// Px 把归一化 rect 换算成像素矩形。
type PxRect struct{ X, Y, W, H int }

func (r Rect) Px(screenW, screenH int) PxRect {
	return PxRect{
		X: int(r.X * float64(screenW)),
		Y: int(r.Y * float64(screenH)),
		W: int(r.W * float64(screenW)),
		H: int(r.H * float64(screenH)),
	}
}

// 上游实况的时钟与天气呈现参数（实测自 settings_v2.json）。
const (
	ClockFontSize      = 144 // 声明值
	ClockFontSizeShown = 174 // 自定义缩放后的实际值
	ClockFormat24h     = true
	ClockShowSeconds   = true
	ClockShowDate      = true
	ClockShowWeekday   = true
	ClockShowTimezone  = false

	WeatherFontSize     = 40
	WeatherIconSize     = 192
	WeatherShowIcon     = false // 需求：撤 192px 状况图标（天气卡片布局失衡，只要求把文字描述改成图标）。实现与素材保留（weatherBlock 的 !WeatherShowIcon 分支 = 纯文字列），要加回来翻这一行即可。
	WeatherShowDetails  = true
	WeatherShowForecast = true
)

// 竖屏目标分辨率（低性能 x86 小主机实测 DISPLAY1 Bounds Width=1080 Height=1920）。
const (
	PortraitW = 1080
	PortraitH = 1920
)

// FontFamily 上游实况字体。gioui 走系统字体匹配，字体名仅作请求与记录用。
const FontFamily = "Inter"

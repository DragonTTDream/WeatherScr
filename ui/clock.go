package ui

import (
	"github.com/DragonTTDream/WeatherScr/ui/panel"
)

// （ui/panel 拆包）：时钟呈现参数的本体迁到 ui/panel/clock.go
// （Clock 结构 + TimeLine / DateLine 两行的逐字格式，含"日期行格式未取证"那条诚实说明）。
// 这里只留兼容转发：字段名、方法名与签名逐字不变 ⇒ 调用点
// （ui/render.go 的 Frame.Clock、cmd/scr/main.go、cmd/fxshot、cmd/scr/preview_windows.go
// 与 ui/*_test.go）一字不改。

// ClockSpec = panel.Clock 的兼容别名（字段名键序逐字不变，两行格式化方法随别名过来）。
type ClockSpec = panel.Clock

// DefaultClockSpec = 上游实况值。
// 上游实况的**常量**住在 ui/layout.go 的呈现参数块（ClockFormat24h / ClockShowSeconds /
// ClockShowDate / ClockShowWeekday / ClockShowTimezone， 不点名 layout.go）
// ⇒ 默认值构造留在 ui，panel 侧不二造数字（避免"双真相源"，见 render.go DefaultPanelStyle 的教训）。
func DefaultClockSpec() ClockSpec {
	return ClockSpec{
		Format24h:    ClockFormat24h,
		ShowSeconds:  ClockShowSeconds,
		ShowDate:     ClockShowDate,
		ShowWeekday:  ClockShowWeekday,
		ShowTimezone: ClockShowTimezone,
	}
}

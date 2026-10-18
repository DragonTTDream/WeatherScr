package ui

import (
	"fmt"
	"time"
)

// ClockSpec 是时钟呈现参数（值取自上游实况 settings_v2.json）。
type ClockSpec struct {
	Format24h    bool
	ShowSeconds  bool
	ShowDate     bool
	ShowWeekday  bool
	ShowTimezone bool
}

// DefaultClockSpec = 上游实况值。
func DefaultClockSpec() ClockSpec {
	return ClockSpec{
		Format24h:    ClockFormat24h,
		ShowSeconds:  ClockShowSeconds,
		ShowDate:     ClockShowDate,
		ShowWeekday:  ClockShowWeekday,
		ShowTimezone: ClockShowTimezone,
	}
}

// TimeLine 主时间行。24h + 秒 → "15:04:05"；无秒 → "15:04"。
func (s ClockSpec) TimeLine(t time.Time) string {
	if s.Format24h {
		if s.ShowSeconds {
			return t.Format("15:04:05")
		}
		return t.Format("15:04")
	}
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	if s.ShowSeconds {
		return fmt.Sprintf("%d:%02d:%02d", h, t.Minute(), t.Second())
	}
	return fmt.Sprintf("%d:%02d", h, t.Minute())
}

// DateLine 日期行。上游是英文界面（geocoding 请求 language=en 佐证），
// 故星期与月份用英文。
//
// 诚实说明：上游日期行的**逐字格式未取证**（只测到 show_date / show_day_of_week
// 两个开关为 true，没测到排版串。本实现取 "Wednesday, Oct 15 2026"，
// 属**待定点**，S3 截图对拍时与上游实况比对后定稿。
func (s ClockSpec) DateLine(t time.Time) string {
	var parts []string
	if s.ShowWeekday {
		parts = append(parts, t.Weekday().String())
	}
	if s.ShowDate {
		parts = append(parts, fmt.Sprintf("%s %d %d", t.Month().String(), t.Day(), t.Year()))
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}

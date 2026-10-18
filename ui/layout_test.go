package ui

import (
	"math"
	"testing"
	"time"
)

// TestLayout：判据 —— rect 与归一化值误差 ≤ 0.01。
func TestLayout(t *testing.T) {
	want := []struct {
		name       string
		got        Rect
		x, y, w, h float64
	}{
		{"clock", ClockRect, 0.013, 0.244, 0.973, 0.210},
		{"weather", WeatherRect, 0.027, 0.531, 0.946, 0.291},
	}
	const tol = 0.01
	for _, c := range want {
		for _, p := range []struct {
			f    string
			got  float64
			want float64
		}{
			{"x", c.got.X, c.x}, {"y", c.got.Y, c.y},
			{"w", c.got.W, c.w}, {"h", c.got.H, c.h},
		} {
			if math.Abs(p.got-p.want) > tol {
				t.Errorf("%s.%s = %v，期望 %v（容差 %v）", c.name, p.f, p.got, p.want, tol)
			}
		}
	}

	// rect 必须落在屏内
	for _, r := range []Rect{ClockRect, WeatherRect} {
		if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 {
			t.Errorf("rect 非法: %+v", r)
		}
		if r.X+r.W > 1.0+tol || r.Y+r.H > 1.0+tol {
			t.Errorf("rect 越出屏幕: %+v", r)
		}
	}
}

// TestLayoutPx：竖屏 1080×1920 下的像素矩形，与上游 rect 一致。
func TestLayoutPx(t *testing.T) {
	c := ClockRect.Px(PortraitW, PortraitH)
	want := PxRect{14, 468, 1050, 403}
	if c != want {
		t.Errorf("时钟像素矩形 = %+v，期望 %+v", c, want)
	}
	w := WeatherRect.Px(PortraitW, PortraitH)
	wantW := PxRect{29, 1019, 1021, 558}
	if w != wantW {
		t.Errorf("天气像素矩形 = %+v，期望 %+v", w, wantW)
	}
}

// TestClockFormat：24h + 秒的主时间行。
func TestClockFormat(t *testing.T) {
	// 固定时刻：2026-10-15 星期四 15:04:05 UTC
	fixed := time.Date(2026, 10, 15, 15, 4, 5, 0, time.UTC)
	s := DefaultClockSpec()
	if got := s.TimeLine(fixed); got != "15:04:05" {
		t.Errorf("TimeLine = %q，期望 \"15:04:05\"", got)
	}
	s2 := s
	s2.ShowSeconds = false
	if got := s2.TimeLine(fixed); got != "15:04" {
		t.Errorf("无秒时 TimeLine = %q，期望 \"15:04\"", got)
	}
	s3 := s
	s3.Format24h = false
	if got := s3.TimeLine(fixed); got != "3:04:05" {
		t.Errorf("12h 制 TimeLine = %q，期望 \"3:04:05\"", got)
	}
	midnight := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if got := s3.TimeLine(midnight); got != "12:00:00" {
		t.Errorf("12h 制零点 = %q，期望 \"12:00:00\"", got)
	}
}

// TestClockDateLine：日期行含星期（上游 show_day_of_week=true）。
func TestClockDateLine(t *testing.T) {
	fixed := time.Date(2026, 10, 15, 15, 4, 5, 0, time.UTC) // 星期四
	s := DefaultClockSpec()
	got := s.DateLine(fixed)
	if got == "" {
		t.Fatalf("日期行为空，上游 show_date/show_day_of_week 均为 true")
	}
	for _, want := range []string{"Thursday", "Oct", "15", "2026"} {
		if !contains(got, want) {
			t.Errorf("日期行 %q 缺 %q", got, want)
		}
	}
	// 关掉全部开关 → 空行
	s.ShowDate = false
	s.ShowWeekday = false
	if got := s.DateLine(fixed); got != "" {
		t.Errorf("全关时应为空行，实得 %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

//go:build linux

package ui

// 固定帧出图（逐像素对拍用）。
// 用法：WS_DUMP_DIR=/path go test -tags=novulkan -run TestDumpFramesForPixelDiff ./ui
// 目录未设置时跳过 ⇒ 常规 make gate 不受影响。
// 用途：改动前后各出一次同一组帧，逐像素比对，证明画面口径没动。
// 所有输入都是常量（时间、种子、步数、图层掩码），不含时间相关的量。
// 只走公开 API（Screen.Layout + Frame），因此改动前的代码树也能编过同一个文件。

import (
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

type dumpScenario struct {
	name   string
	class  effect.Class
	isDay  bool
	steps  int
	layers LayerMask
	full   bool // true = 详情三项齐全
	alert  bool
	yoff   int
}

func dumpScenarios() []dumpScenario {
	return []dumpScenario{
		{name: "rain_day_full", class: effect.Rain, isDay: true, steps: 40, layers: 0, full: true},
		{name: "cloudy_night_full", class: effect.Cloudy, isDay: false, steps: 40, layers: 0, full: true},
		{name: "clear_day_min", class: effect.Clear, isDay: true, steps: 20, layers: 0, full: false},
		{name: "thunder_night_alert", class: effect.Thunder, isDay: false, steps: 30, layers: 0, full: true, alert: true},
		{name: "text_only", class: effect.Fog, isDay: false, steps: 0, layers: LayerSky | LayerClock | LayerWeath, full: true},
		{name: "text_only_yoff", class: effect.Fog, isDay: false, steps: 0, layers: LayerSky | LayerClock | LayerWeath, full: true, yoff: -30},
	}
}

func dumpSample(full bool) *weather.Sample {
	tt := 22.4
	hu := 84.0
	pp := 12.0
	ws := 4.6
	cond := "Mainly clear"
	fc := "Tomorrow: 20C-25C/Rain"
	s := &weather.Sample{
		Condition:   cond,
		Temperature: &tt,
		WeatherCode: 1,
		IsDay:       1,
		Forecast:    &fc,
	}
	if full {
		s.Humidity = &hu
		s.PrecipitationProbability = &pp
		s.WindSpeed = &ws
	}
	return s
}

func TestDumpFramesForPixelDiff(t *testing.T) {
	dir := os.Getenv("WS_DUMP_DIR")
	if dir == "" {
		t.Skip("WS_DUMP_DIR 未设置，跳过固定帧出图")
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过出图: %v", err)
	}
	defer win.Release()

	now := time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC)
	s := New()

	for _, sc := range dumpScenarios() {
		field := NewField(sc.class, sc.isDay, 20261015)
		for i := 0; i < sc.steps; i++ {
			field.Step(66*time.Millisecond, PortraitW, PortraitH)
		}
		var alert *Alert
		if sc.alert {
			// 显隐由 State 决定（旧 Pct 触发线口径作废）⇒ 夹具给灾害态
			alert = &Alert{State: AlertDisaster, TierVal: 3, MM: 1.2, DisasterHours: 3, Pulse: 0.25, Lang: "zh"}
		}
		wp := WeatherRect.Px(PortraitW, PortraitH)
		wp.Y += sc.yoff
		var ops op.Ops
		for i := 0; i < 3; i++ {
			ops.Reset()
			gtx := layout.Context{
				Ops:         &ops,
				Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
				Now:         now,
			}
			s.Layout(gtx, Frame{
				Now:         now,
				Clock:       DefaultClockSpec(),
				Weather:     dumpSample(sc.full),
				Class:       sc.class,
				IsDay:       sc.isDay,
				Field:       field,
				Location:    "Hangzhou, China",
				Panel:       DefaultPanelStyle(),
				WeatherYOff: sc.yoff,
				Alert:       alert,
				MoonPhase:   nil,
				Layers:      sc.layers,
			})
			if err := win.Frame(&ops); err != nil {
				t.Fatalf("帧提交失败 %s: %v", sc.name, err)
			}
		}
		img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("Screenshot 失败 %s: %v", sc.name, err)
		}
		f, err := os.Create(dir + "/" + sc.name + ".png")
		if err != nil {
			t.Fatalf("建 PNG 失败: %v", err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("编码 PNG 失败: %v", err)
		}
		f.Close()
	}
}

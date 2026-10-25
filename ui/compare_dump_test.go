//go:build !vulkan

// P7 效果图出件（交付物 1/3 的原料）：满密度态（800 步）逐类固定帧出图。
// 复用 pixeldump_test.go 里现成的 dumpScenario / dumpSample / 出图链，
// 只换场景表与目录环境变量 ⇒ 零重复声明。
//
//	WS_COMPARE_DIR=/dir go test -tags=novulkan -count=1 -run TestCompareDumpP7 ./ui
//
// 目录未设 ⇒ 跳过，常规闸门不受影响。

package ui

import (
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

func p7Scenarios() []dumpScenario {
	return []dumpScenario{
		{name: "p7_overcast_800", class: effect.Overcast, isDay: false, steps: 800, layers: 0, full: true},
		{name: "p7_fog_800", class: effect.Fog, isDay: true, steps: 800, layers: 0, full: true},
		{name: "p7_rain_800", class: effect.Rain, isDay: true, steps: 800, layers: 0, full: true},
		{name: "p7_cloudy_800", class: effect.Cloudy, isDay: true, steps: 800, layers: 0, full: true},
		{name: "p7_snow_800", class: effect.Snow, isDay: true, steps: 800, layers: 0, full: true},
	}
}

func TestCompareDumpP7(t *testing.T) {
	dir := os.Getenv("WS_COMPARE_DIR")
	if dir == "" {
		t.Skip("WS_COMPARE_DIR 未设置，跳过 P7 出图")
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过出图: %v", err)
	}
	defer win.Release()

	now := time.Date(2026, 10, 23, 12, 34, 56, 0, time.UTC)
	s := New()

	for _, sc := range p7Scenarios() {
		field := NewField(sc.class, sc.isDay, 20261015)
		for i := 0; i < sc.steps; i++ {
			field.Step(66*time.Millisecond, PortraitW, PortraitH)
		}
		layers, dualIncl := field.DriftOverlapIncl()
		_, dualBandOnly := field.DriftOverlapBandOnly()
		t.Logf("%s 800 步：带-only=%.2f%% 含粒子=%.2f%% layers=%.2f", sc.name, dualBandOnly, dualIncl, layers)

		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
			Now:         now,
		}
		s.Layout(gtx, Frame{
			Now: now, Clock: DefaultClockSpec(), Weather: dumpSample(sc.full),
			Class: sc.class, IsDay: sc.isDay, Field: field,
			Location: "Hangzhou, China", Panel: DefaultPanelStyle(),
			Layers: sc.layers,
		})
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("帧提交失败 %s: %v", sc.name, err)
		}
		img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("Screenshot 失败 %s: %v", sc.name, err)
		}
		f, err := os.Create(dir + "/" + sc.name + ".png")
		if err != nil {
			t.Fatalf("建文件失败: %v", err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("编码失败: %v", err)
		}
		f.Close()
	}
}

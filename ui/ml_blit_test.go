package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

func blitSample() *weather.Sample {
	tt, hu, pp, ws := 18.0, 80.0, 10.0, 4.0
	fc := "Tomorrow: 18°-23°C/Clear sky"
	return &weather.Sample{
		Condition: "Clear sky", Temperature: &tt, WeatherCode: 0,
		IsDay: 0, Forecast: &fc, Location: "Hangzhou, China",
		Humidity: &hu, PrecipitationProbability: &pp, WindSpeed: &ws,
		TodayPrecipProbMax: &[]int{80}[0],
	}
}

// TestMoonBlitProbe 月亮独立层是否真的贴进帧（实测之后月亮由 moonBlit 绘制）。
func TestMoonBlitProbe(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skip("headless 不可用:", err)
	}
	s := New()
	var ops op.Ops
	th := time.Date(2026, 10, 17, 0, 30, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)), Now: th}
		s.Layout(gtx, Frame{
			Now: th, Clock: DefaultClockSpec(), Weather: blitSample(),
			Class: effect.Clear, IsDay: false, Location: "Hangzhou, China",
			Panel: DefaultPanelStyle(), MoonPhase: f64p(0.174),
			Layers: LayerSky | LayerClock,
		})
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("frame: %v", err)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	fw, fh := float64(PortraitW), float64(PortraitH)
	cx, cy := int(0.72*fw), int(0.095*fh)
	rc := int(moonRcFrac * fw)
	n, bright := 0, 0
	for y := cy - rc; y <= cy+rc; y++ {
		for x := cx - rc; x <= cx+rc; x++ {
			i := img.PixOffset(x, y)
			lum := (int(img.Pix[i]) + int(img.Pix[i+1]) + int(img.Pix[i+2])) / 3
			n++
			if lum > 150 {
				bright++
			}
		}
	}
	t.Logf("月核区 %d px，其中亮(>150) %d px", n, bright)
	if bright == 0 {
		t.Errorf("月亮没贴出来：月核区无亮像素")
	}
}

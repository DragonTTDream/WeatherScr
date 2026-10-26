package ui

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

func TestSkyBakeProbe(t *testing.T) {
	img := sky.RenderSkyCPU(540, 960, effect.Cloudy, true, nil, 0)
	if img == nil {
		t.Fatal("nil")
	}
	for _, p := range []struct{ x, y int }{{270, 30}, {270, 480}, {270, 930}, {15, 480}} {
		c := img.NRGBAAt(p.x, p.y)
		t.Logf("bake(%d,%d) = %d,%d,%d a=%d", p.x, p.y, c.R, c.G, c.B, c.A)
	}
	t.Logf("bounds=%v size=%v", img.Bounds(), img.Bounds().Size())
}

package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestBandCoverDisc：云带（"大云"）到底遮不遮得住日/月盘。
// 缺陷：部分大云没有遮盖月亮，反而被月亮遮盖。
func TestBandCoverDisc(t *testing.T) {
	f := NewField(effect.Cloudy, false, 20261015)
	f.adaptFps(15)
	var steps []int
	peak, peakAt := 0.0, 0
	bandSecs := 0
	for s := 1; s <= 900; s++ {
		f.Step(time.Second, PortraitW, PortraitH)
		f.adaptFps(15)
		b, c := f.CelestialCoverAll(PortraitW, PortraitH)
		if b > 0 {
			bandSecs++
		}
		if c > peak {
			peak, peakAt = c, s
		}
		if b > 0 && c >= 50 && len(steps) < 8 && s%37 == 0 {
			steps = append(steps, s)
		}
	}
	t.Logf("云带落在盘所在行的秒数=%d/900；含云带的盘面峰值覆盖=%.1f%% @ t=%ds；出图步数=%v", bandSecs, peak, peakAt, steps)
	// 云带 alpha 口径（drawCloudBands）
	for i := 0; i < cloudBandN; i++ {
		frac := float64(i) / float64(cloudBandN-1)
		a := (150.0 + 50.0*frac) / 255.0
		bp, _ := f.bandPlace(i, f.t, PortraitW, PortraitH)
		t.Logf("带%d: y=%.0f..%.0f px  alpha=%.3f  与月盘(117..247px)相交=%v",
			i, bp.y, bp.y+bp.dh, a, bp.y <= 247 && bp.y+bp.dh >= 117)
	}
	// 合成亮度算术：云体 216、月核 246、暗天空 90
	for _, a := range []float64{0.588, 0.700, 0.784, 0.860, 0.900, 0.950} {
		overMoon := 216*a + 246*(1-a)
		overSky := 216*a + 90*(1-a)
		t.Logf("alpha=%.3f ⇒ 云盖月面=%.0f  云盖暗天空=%.0f  差=%.0f 级", a, overMoon, overSky, overMoon-overSky)
	}
}

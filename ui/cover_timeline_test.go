package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestCoverTimeline：把"云压日/月盘"覆盖率随时间的变化打出来，
// 用来挑出**真正拍到事件的帧**再出图
// "截图恰好没有截图到大云经过太阳，就擅自下结论！而不是重新截图"。
func TestCoverTimeline(t *testing.T) {
	for _, isDay := range []bool{true, false} {
		f := NewField(effect.Cloudy, isDay, 20261015)
		tag := "白天(太阳)"
		if !isDay {
			tag = "夜间(月亮)"
		}
		var peakStep, peakCover = 0, 0.0
		var hi []int
		t.Logf("=== %s ===", tag)
		for s := 1; s <= 900; s++ {
			f.Step(time.Second, PortraitW, PortraitH)
			f.adaptFps(15)
			c, cov := f.CelestialCover(PortraitW, PortraitH)
			if cov > peakCover {
				peakCover, peakStep = cov, s
			}
			if cov >= 40 {
				hi = append(hi, s)
			}
			if s%60 == 0 {
				t.Logf("t=%3ds 云在盘上=%d 覆盖=%5.1f%%", s, c, cov)
			}
		}
		t.Logf("%s：峰值覆盖 %.1f%% @ t=%ds；覆盖≥40%% 的秒数 %d/900", tag, peakCover, peakStep, len(hi))
		if len(hi) > 0 {
			pick := []int{}
			for _, s := range hi {
				if len(pick) == 0 || s-pick[len(pick)-1] >= 60 {
					pick = append(pick, s)
				}
			}
			t.Logf("挑出的出图步数: %v", pick)
		}
	}
}

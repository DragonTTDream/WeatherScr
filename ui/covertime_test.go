package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestMoonCoveredOverTime 长时间模拟，统计"云盖住月亮"时长占比。
// 之前只拿单帧图下判断（云还没走到月亮那儿）。
//
// 盘半径口径 = **moonRcFrac = 0.060（月盘）**，不含光晕（光晕 0.192 会把矩形虚大、
// 占比虚高）；相交判据 = CelestialCover 的**真实相交**（24×24 网格采样盘面，
// 云矩形取绘制同口径 spriteScale），不是"中心落在矩形里"。
// 阈值 ≥25% 不变。
func TestMoonCoveredOverTime(t *testing.T) {
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		f := NewField(effect.Cloudy, false, seed)
		f.Step(time.Second, PortraitW, PortraitH)
		f.adaptFps(15) // 低性能 x86 小主机目标帧率
		act := f.ActiveCount()
		covered := 0
		frames := 0
		sumCover := 0.0
		maxCover := 0.0
		var firstCover time.Duration
		for sec := 1; sec <= 900; sec++ {
			f.Step(time.Second, PortraitW, PortraitH)
			f.adaptFps(15)
			frames++
			c, cov := f.CelestialCover(PortraitW, PortraitH)
			hit := c > 0 && cov > 0
			if hit {
				covered++
				sumCover += cov
				if cov > maxCover {
					maxCover = cov
				}
				if firstCover == 0 {
					firstCover = time.Duration(sec) * time.Second
				}
			}
		}
		meanCover := 0.0
		if covered > 0 {
			meanCover = sumCover / float64(covered)
		}
		t.Logf("seed=%d 激活=%d 总=%d：900 秒内云盖月亮 %d/%d 帧 = %.1f%%，首次覆盖在第 %v；盘面覆盖率 峰值=%.1f%% 覆盖帧均值=%.1f%%",
			seed, act, len(f.parts), covered, frames, 100*float64(covered)/float64(frames), firstCover, maxCover, meanCover)
		if float64(covered)/float64(frames) < 0.25 {
			t.Errorf("seed=%d 云盖月亮时长占比仅 %.1f%%（要求 ≥25%%）",
				seed, 100*float64(covered)/float64(frames))
		}
	}
}

package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestMoonCoveredOverTime 长时间模拟，统计"云盖住月亮"时长占比。
// 之前只拿单帧图下判断（云还没走到月亮那儿）。
func TestMoonCoveredOverTime(t *testing.T) {
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		f := NewField(effect.Cloudy, false, seed)
		f.Step(time.Second, PortraitW, PortraitH)
		f.adaptFps(15) // 低性能 x86 小主机目标帧率
		act := f.ActiveCount()
		// 月亮矩形（归一化）
		mR := moonRcFrac * (1.0 + moonHaloSpread)
		mcX, mcY := 0.72, 0.095
		padX := mR
		padY := mR * float64(PortraitW) / float64(PortraitH)
		covered := 0
		frames := 0
		var firstCover time.Duration
		for sec := 1; sec <= 900; sec++ {
			f.Step(time.Second, PortraitW, PortraitH)
			f.adaptFps(15)
			frames++
			hit := false
			for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
				p := &f.parts[i]
				if p.kind != pkCloud {
					continue
				}
				if p.x > mcX-padX && p.x < mcX+padX && p.y > mcY-padY && p.y < mcY+padY {
					hit = true
					break
				}
			}
			if hit {
				covered++
				if firstCover == 0 {
					firstCover = time.Duration(sec) * time.Second
				}
			}
		}
		t.Logf("seed=%d 激活=%d 总=%d：900 秒内云盖月亮 %d/%d 帧 = %.1f%%，首次覆盖在第 %v",
			seed, act, len(f.parts), covered, frames, 100*float64(covered)/float64(frames), firstCover)
		if float64(covered)/float64(frames) < 0.25 {
			t.Errorf("seed=%d 云盖月亮时长占比仅 %.1f%%（要求 ≥25%%）",
				seed, 100*float64(covered)/float64(frames))
		}
	}
}

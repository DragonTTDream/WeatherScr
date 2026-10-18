package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestHorizontalSpread：把"云横向分布是否均匀"随时间整条量出来。
// 缺陷：云的横向分布不均，某一小段时间屏幕中大部分是空白，或某一段云大量集中出现。
func TestHorizontalSpread(t *testing.T) {
	const cols = 12
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		f := NewField(effect.Cloudy, false, seed)
		empty, cluster, sumMean, worstGapSec := 0, 0, 0.0, 0
		var profAt []string
		for s := 1; s <= 900; s++ {
			f.Step(time.Second, PortraitW, PortraitH)
			f.adaptFps(15)
			p := f.CoverageProfile(PortraitW, PortraitH, cols)
			mean, mx, mn, zeros := 0.0, 0.0, 1.0, 0
			for _, v := range p {
				mean += v
				if v > mx {
					mx = v
				}
				if v < mn {
					mn = v
				}
				if v <= 0.001 {
					zeros++
				}
			}
			mean /= float64(cols)
			sumMean += mean
			if mean < 0.03 {
				empty++
			}
			if mx > 0.45 {
				cluster++
			}
			if zeros > worstGapSec {
				worstGapSec = zeros
			}
			if s%90 == 0 {
				line := ""
				for _, v := range p {
					line += sprintfPct(v)
				}
				profAt = append(profAt, line)
				t.Logf("seed=%d t=%3ds 平均=%.1f%% 最空列=%.1f%% 最满列=%.1f%% 全空列数=%d/12 剖面=%s",
					seed, s, mean*100, mn*100, mx*100, zeros, line)
			}
		}
		t.Logf("seed=%d：900 秒里 平均覆盖=%.1f%%；\"整屏基本空白\"(平均<3%%) %d 秒；\"某列云大量集中\"(单列>45%%) %d 秒；单秒最多全空列 %d/12",
			seed, sumMean/900*100, empty, cluster, worstGapSec)
	}
}

func sprintfPct(v float64) string {
	if v <= 0.001 {
		return "  · "
	}
	return " " + strconv.Itoa(int(v*100+0.5)) + " "
}

package ui

import (
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestBandEntryNoPop：云带必须"从屏幕外慢慢滑进来"，不许在屏幕边缘凭空出现。
// 需求：云不得突然刷新出来，要在屏幕外慢慢进场，不得在眼前突然出现。
//
// 可断言的形式：相邻两帧之间，一条带**在屏内的可见宽度**变化不得超过
// "这一帧它最多能移动的距离"。凭空出现 = 可见宽度从 0 跳到 dw/2，远超位移上限。
func TestBandEntryNoPop(t *testing.T) {
	const dt = time.Second
	maxShift := 0.06*float64(PortraitW)*0.725*dt.Seconds() + 1.0
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		f := NewField(effect.Cloudy, false, seed)
		prev := make([]float64, cloudBandN)
		init := false
		worst, worstAt, worstBand := 0.0, 0, -1
		for s := 1; s <= 900; s++ {
			f.Step(dt, PortraitW, PortraitH)
			for i := 0; i < cloudBandN; i++ {
				bp, ok := f.bandPlace(i, f.t, PortraitW, PortraitH)
				if !ok {
					continue
				}
				lo := math.Max(0, bp.x)
				hi := math.Min(float64(PortraitW), bp.x+bp.dw)
				vis := math.Max(0, hi-lo)
				if init {
					if d := math.Abs(vis - prev[i]); d > worst {
						worst, worstAt, worstBand = d, s, i
					}
				}
				prev[i] = vis
			}
			init = true
		}
		if worst > maxShift {
			t.Errorf("seed=%d：第 %d 条云带可见宽度在 t=%ds 处跳变 %.1f px，超过单帧位移上限 %.1f px ⇒ 云凭空出现",
				seed, worstBand, worstAt, worst, maxShift)
		} else {
			t.Logf("seed=%d：900 秒内最大单帧可见宽度变化 %.2f px ≤ 位移上限 %.2f px ⇒ 云带全程从屏外滑入，无凭空出现",
				seed, worst, maxShift)
		}
	}
}

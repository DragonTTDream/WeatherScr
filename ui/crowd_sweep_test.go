package ui

import (
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestCrowdSweep 多种子扫描：云之间的**最小间距**分布。
// 方法学纠正：fxshot 用固定种子 ⇒ 只验了**一个特定布局**就当结论，
// 低性能 x86 实机每次启动用 time.Now.UnixNano 做种子 ⇒ 布局随机。
// 低性能 x86 实机上看到的"顶部一堆云 / 画面被超大云占据"就是随机种子下的布局，
// 单样本测量对它没有发言权。
func TestCrowdSweep(t *testing.T) {
	const W, H = 1080, 1920
	type rec struct {
		seed                int64
		minD, topN, maxArea float64
	}
	var worst rec
	var sumMin, sumTop float64
	n := int64(40)
	for s := int64(1); s <= n; s++ {
		f := NewField(effect.Cloudy, true, s*97751)
		f.Step(time.Second, W, H)
		var ps []*particle
		for i := range f.parts {
			if f.parts[i].kind == pkCloud {
				ps = append(ps, &f.parts[i])
			}
		}
		minD := math.Inf(1)
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				dx := (ps[i].x - ps[j].x) * float64(W)
				dy := (ps[i].y - ps[j].y) * float64(H)
				// 中心距减去两半宽 = 边缘间隙
				g := math.Hypot(dx, dy) - cloudHalfPx(ps[i], W) - cloudHalfPx(ps[j], W)
				if g < minD {
					minD = g
				}
			}
		}
		topN := 0.0
		for _, p := range ps {
			if p.y*float64(H) < 300 {
				topN++
			}
		}
		r := rec{s, minD, topN, 0}
		if minD < worst.minD || worst.seed == 0 {
			worst = r
		}
		sumMin += minD
		sumTop += topN
	}
	t.Logf("种子数=%d 平均最小边缘间隙=%.1f px 最差=%.1f px(seed=%d) 平均顶部300px内云数=%.2f 最差=%.0f",
		n, sumMin/float64(n), worst.minD, worst.seed, sumTop/float64(n), worst.topN)
}

// cloudHalfPx 云的水平半宽（像素），口径同绘制式（spriteScale 原生 1:1）。
func cloudHalfPx(p *particle, w int) float64 {
	sz := spritesOnce().cloudSz[p.idx%len(spritesOnce().cloudSz)]
	return float64(sz.X) * spriteScale(p) / 2
}

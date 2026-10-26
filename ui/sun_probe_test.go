package ui

import (
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

// TestSunProfile 量烘焙太阳的径向亮度剖面（低性能 x86 小主机反馈"像个圆盘"）。
// 判据：真太阳是**核心亮 + 大范围柔和衰减**（无边界）；
// 圆盘是**内部平、外缘陡**（有边界）。
func TestSunProfile(t *testing.T) {
	img := sky.RenderSkyCPU(540, 960, effect.Clear, true, nil, 0)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	lum := func(x, y int) float64 {
		i := img.PixOffset(x, y)
		return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
	}
	// 用烘焙的**已知中心**，不要"找最亮点"：
	// 上一版探针找峰找到 (386,105)，而烘焙中心是 (0.72w,0.13h)=(389,125)
	// ⇒ 探针落在天空辉光带上，量出来的剖面是假的。
	cx, cy := int(0.72*float64(w)), int(0.13*float64(h))
	t.Logf("太阳中心 (%d,%d) 亮度=%.1f 基线(右移300px)=%.1f",
		cx, cy, lum(cx, cy), lum(mini(cx+300, w-1), cy))
	var prof []float64
	for rr := 0; rr <= 120; rr += 8 {
		x := cx + rr
		if x >= w {
			break
		}
		prof = append(prof, math.Round(lum(x, cy)))
	}
	t.Logf("径向亮度剖面(右向): %v", prof)
	// 圆盘判据：存在一段"内部近似平"后接"单步跌幅 > 25"
	plateau, cliff := 0, 0
	for i := 1; i < len(prof); i++ {
		d := prof[i] - prof[i-1]
		if d > -3 && d < 3 {
			plateau++
		} else {
			plateau = 0
		}
		if d < -25 {
			cliff++
		}
		if plateau >= 4 && cliff >= 1 {
			t.Errorf("圆盘特征：内部平台 %d 点后出现陡降 @i=%d (%.0f→%.0f)", plateau, i, prof[i-1], prof[i])
			break
		}
	}
}

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

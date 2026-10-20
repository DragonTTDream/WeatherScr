package ui

import (
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestCloudWidthMatchesBand 粒子云逐类屏宽表 + 云带同区间判据（只对压顶三档保留）。
//
// **口径变更（本次）**：上一批的"粒子云与云带同宽"口径（粒子云 sc 区间 [1.5,2.3]
// ⇒ 屏宽 768–1178 px，与云带逐档相同）已被云规则的**逐类屏宽表**取代：
//
//	晴 clear          300–600  px
//	多云见日 partly   300–700  px
//	多云 cloudy      500–900  px
//	阴 overcast      768–1178 px   ← 与云带同区间（云带 = 512×(1.5+0.8·frac)÷bandNetDiv = 768–1178 px）
//	雨/雷 rain/thunder 768–1178 px ← 与云带同区间（这两档没有粒子云，见下）
//
// 表落在 effects.go 的 cloudFormOf（sizeCloudByClass 按目标屏宽**反解** p.sc ⇒
// 逐朵**实画屏宽**逐字落在表内，尺寸抖动 scaleMul 被反解掉）。
//
// 三条断言（全部实测打印）：
//  1. 逐类**实画屏宽** ∈ 本类区间（逐类区间表，不放宽到全区间）
//  2. 云带 dw ∈ [768, 1178]（云带口径不动）
//  3. "粒子云名义屏宽与云带同区间"**只对 overcast 保留**；
//     clear/partly-cloudy/cloudy 是**故意**与云带不同区间（逐类表就是为此而设）
//
// 附带数量口径（不动）：Cloudy 3 / PartlyCloudy 2 / Overcast 5 / Clear 白天 4
// （Clear 的 4 是全场粒子数 = 1 朵云 + 3 个光晕，云按 idx%5==0 取）；
// Clear 夜间 110 是星不是云 ⇒ 云数应为 0。
func TestCloudWidthMatchesBand(t *testing.T) {
	const (
		wantSrcW = 512  // 云源图宽（px），换算的分母
		bandLo   = 768  // 云带最窄一档 512 × 1.5 / bandNetDiv
		bandHi   = 1178 // 云带最宽一档 512 × 2.3 / bandNetDiv
		tolDraw  = 1.0  // px
		tolNom   = 1.0  // px
		tolBand  = 1.0  // px
	)
	// 逐类实画屏宽区间 = cloudFormOf 的表（测试与实现共用同一张表 ⇒ 断言的是口径本身）
	drawWant := map[effect.Class][2]float64{
		effect.Clear:        {300, 600},
		effect.PartlyCloudy: {300, 700},
		effect.Cloudy:       {500, 900},
		effect.Overcast:     {768, 1178},
	}
	// "与云带同区间"只对压顶档保留
	sameAsBand := map[effect.Class]bool{
		effect.Clear:        false,
		effect.PartlyCloudy: false,
		effect.Cloudy:       false,
		effect.Overcast:     true,
	}
	counts := map[effect.Class]int{
		effect.Cloudy:       3,
		effect.PartlyCloudy: 2,
		effect.Overcast:     5,
		effect.Clear:        4,
	}
	cloudWant := map[effect.Class]int{
		effect.Cloudy:       3,
		effect.PartlyCloudy: 2,
		effect.Overcast:     5,
		effect.Clear:        1,
	}
	seeds := []int64{20261015, 20261016, 20261017, 23, 11}
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast, effect.Clear} {
		want := drawWant[cls]
		for _, seed := range seeds {
			f := NewField(cls, true, seed)
			f.Step(time.Second, PortraitW, PortraitH)
			if got := f.TotalCount(); got != counts[cls] {
				t.Errorf("%s seed=%d: 全场粒子数 = %d，应为 %d", cls, seed, got, counts[cls])
			}
			clouds := 0
			drawMin, drawMax := math.Inf(1), math.Inf(-1)
			nomMin, nomMax := math.Inf(1), math.Inf(-1)
			for i := range f.parts {
				p := &f.parts[i]
				if p.kind != pkCloud {
					continue
				}
				clouds++
				sz, _, ok := f.pick(p)
				if !ok {
					t.Fatalf("%s seed=%d i=%d 取不到贴图", cls, seed, i)
				}
				if sz.X != wantSrcW {
					t.Fatalf("%s seed=%d: 云源图宽 = %d，应为 %d（换算口径失效）", cls, seed, sz.X, wantSrcW)
				}
				draw := float64(sz.X) * spriteScale(p)
				nom := float64(sz.X) * p.sc / bandNetDiv
				if draw < want[0]-tolDraw || draw > want[1]+tolDraw {
					t.Errorf("%s seed=%d i=%d: 实画屏宽 %.1f px 越出本类区间 [%.0f, %.0f]（sc=%.3f scaleMul=%.3f）",
						cls, seed, i, draw, want[0], want[1], p.sc, p.scaleMul)
				}
				if nom < nomMin {
					nomMin = nom
				}
				if nom > nomMax {
					nomMax = nom
				}
				if draw < drawMin {
					drawMin = draw
				}
				if draw > drawMax {
					drawMax = draw
				}
			}
			if want := cloudWant[cls]; clouds != want {
				t.Errorf("%s seed=%d: 云数 = %d，应为 %d", cls, seed, clouds, want)
			}
			// 云带 dw 区间（云带口径不动：768–1178 px）
			bandMin, bandMax := math.Inf(1), math.Inf(-1)
			for i := 0; i < f.bandCount(); i++ {
				bp, ok := f.bandPlace(i, f.t, PortraitW, PortraitH)
				if !ok {
					continue
				}
				if bp.dw < bandMin {
					bandMin = bp.dw
				}
				if bp.dw > bandMax {
					bandMax = bp.dw
				}
			}
			if bandMin == math.Inf(1) {
				if cls == effect.Clear {
					bandMin, bandMax = bandLo, bandHi // Clear 不画云带 ⇒ 只比常量端点
				} else {
					t.Fatalf("%s seed=%d: 取不到云带几何，测试无意义", cls, seed)
				}
			}
			if bandMin < bandLo-tolBand || bandMax > bandHi+tolBand {
				t.Errorf("%s seed=%d: 云带 dw 区间 [%.1f, %.1f] 越出 [%d, %d]", cls, seed, bandMin, bandMax, bandLo, bandHi)
			}
			if sameAsBand[cls] {
				// 同区间的比较量 = **实画屏宽**（Draw 真正画出来的宽度）。
				// 旧口径比的是"名义屏宽"（不含 scaleMul 抖动）：那轮 sc 是旋钮、
				// 抖动把区间撑宽，所以只能比名义值。现口径 sc 由目标屏宽**反解**，
				// 抖动被反解掉 ⇒ 实画屏宽就是口径本身，比它才是比屏幕上画出来的东西。
				if drawMin < bandMin-tolNom || drawMax > bandMax+tolNom {
					t.Errorf("%s seed=%d: 粒子云实画屏宽 [%.1f, %.1f] 与云带 [%.1f, %.1f] 不同区间（本档要求同区间）",
						cls, seed, drawMin, drawMax, bandMin, bandMax)
				}
			}
			t.Logf("%s seed=%d 云=%d 朵 实画屏宽 [%.0f, %.0f] px（本类区间 [%.0f, %.0f]）名义屏宽 [%.0f, %.0f] px | 云带 dw [%.0f, %.0f] px | 与云带同区间要求=%v",
				cls, seed, clouds, drawMin, drawMax, want[0], want[1], nomMin, nomMax, bandMin, bandMax, sameAsBand[cls])
		}
	}
	// 雨/雷档：粒子云数 = 0（counts 全给雨丝），逐类表的 768–1178 由云带承担
	for _, cls := range []effect.Class{effect.Rain, effect.Thunder} {
		f := NewField(cls, true, 20261015)
		f.Step(time.Second, PortraitW, PortraitH)
		n := 0
		for i := range f.parts {
			if f.parts[i].kind == pkCloud {
				n++
			}
		}
		bandMin, bandMax := math.Inf(1), math.Inf(-1)
		for i := 0; i < f.bandCount(); i++ {
			bp, ok := f.bandPlace(i, f.t, PortraitW, PortraitH)
			if !ok {
				continue
			}
			if bp.dw < bandMin {
				bandMin = bp.dw
			}
			if bp.dw > bandMax {
				bandMax = bp.dw
			}
		}
		if bandMin < bandLo-tolBand || bandMax > bandHi+tolBand {
			t.Errorf("%s: 云带 dw 区间 [%.1f, %.1f] 越出 [%d, %d]", cls, bandMin, bandMax, bandLo, bandHi)
		}
		t.Logf("%s seed=20261015 粒子云=%d 朵（counts 全给雨丝）⇒ 逐类表 768–1178 px 由云带承担：云带 dw [%.0f, %.0f] px",
			cls, n, bandMin, bandMax)
	}
	// Clear 夜间：110 是星，云数必须为 0
	f := NewField(effect.Clear, false, 20261015)
	if got := f.TotalCount(); got != 110 {
		t.Errorf("Clear 夜间粒子数 = %d，应为 110（星，不动）", got)
	}
	for i := range f.parts {
		if f.parts[i].kind == pkCloud {
			t.Errorf("Clear 夜间出现云粒子 i=%d", i)
		}
	}
}

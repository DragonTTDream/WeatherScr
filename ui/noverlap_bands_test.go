package ui

// P6 云带不重叠（spec/p6-noverlap.md 判据第 1 条）的钉死单测。
//
// 口径（全部走 bandPlace 真落位，不另抄公式；与绘制共用同一套几何）：
//   - w=1080, h=1920；逐类 Overcast/Cloudy/Rain/Thunder/Fog；
//   - t ∈ {400, 800} 固定步（fixedStep 口径，与 ui/fixedstep_test.go 同一模板）；
//   - 每条带取 ±span 镜像三次参与配对（周期折回，配对取跨周期最坏值）；
//   - 断言：任取两带中心距离 ≥ (dw_i + dw_j)/2 + 20 px ⇒ 带-only 重叠 = 0%；
//   - 断言：BandWidthsPx 逐条 ≤1000、均值 ≤1000（P6 设计第 2 条等宽 + 硬上限）；
//   - 断言：bandCount() 逐类 = 3/3/3/3/2（P6 设计第 1 条）；
//   - 断言：覆盖率 N·T/span ≥ 0.90（P6 设计第 5 条，用现成 AvgLayersOf 口径；
//     N=3 档 = 3000/3080 = 97.4%；Fog 是保持档（N=2），覆盖率线按 N=3 档口径逐字只约束
//     Overcast/Cloudy/Rain/Thunder，Fog 只钉带数与带宽）。
//   - 断言：DriftOverlapBandOnly().dualPct ≤ 5%（P6 设计第 6 条带-only 判据线）。

import (
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// bandCenterWrapped 第 i 条带在 t 时刻的中心 x，并给出 ±span 镜像的三次取值
// （周期口径下"任取两带"必须对折回后的所有代表取值成立，不是只对原始 x 成立）。
func bandCenterWrapped(t0 float64, f *Field, i, w, h int, span float64) [3]float64 {
	bp, ok := f.bandPlace(i, t0, w, h)
	if !ok {
		return [3]float64{math.NaN(), math.NaN(), math.NaN()}
	}
	c := bp.x + bp.dw/2
	return [3]float64{c - span, c, c + span}
}

func TestNoverlapBandsGeometry(t *testing.T) {
	const w, h = PortraitW, PortraitH // 1080×1920
	cases := []struct {
		cls  effect.Class
		want int
	}{
		{effect.Overcast, 3},
		{effect.Cloudy, 3},
		{effect.Rain, 3},
		{effect.Thunder, 3},
		{effect.Fog, 2},
	}
	for _, c := range cases {
		f := NewField(c.cls, true, budgetSeed)
		if n := f.bandCount(); n != c.want {
			t.Errorf("%s bandCount() = %d，期望 %d（P6 设计第 1 条：3/3/3/3/2）", c.cls, n, c.want)
		}
		for _, steps := range []int{400, 800} {
			g := NewField(c.cls, true, budgetSeed)
			for k := 0; k < steps; k++ {
				g.Step(fixedStep, w, h)
			}
			n := g.bandCount()
			span := g.bandSpan(w)
			type bi struct {
				center [3]float64
				dw     float64
			}
			bs := make([]bi, 0, n)
			for i := 0; i < n; i++ {
				bp, ok := g.bandPlace(i, g.t, w, h)
				if !ok {
					t.Fatalf("%s t=%d步：带 %d 取不到落位 ⇒ 测试无意义", c.cls, steps, i)
				}
				bs = append(bs, bi{center: bandCenterWrapped(g.t, g, i, w, h, span), dw: bp.dw})
			}
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					// 跨周期最坏值 = 三次镜像配对里的最小中心距离
					minD := math.Inf(1)
					for _, ci := range bs[i].center {
						for _, cj := range bs[j].center {
							if d := math.Abs(ci - cj); d < minD {
								minD = d
							}
						}
					}
					limit := (bs[i].dw+bs[j].dw)/2 + 20
					if minD < limit {
						t.Errorf("%s t=%d步：带%d↔带%d 中心距离 %.2f px < (dw_i+dw_j)/2 + 20 = %.2f px ⇒ 带重叠（P6 判据：带-only 重叠 0%%）",
							c.cls, steps, i, j, minD, limit)
					}
				}
			}
			// 带-only 真落位双层占比 ≤5%（判据线本身）
			layers, dual := g.DriftOverlapBandOnly()
			t.Logf("%s t=%d步：带-only layers=%.4f dual=%.2f%% span=%.1f 中心间距=%.1f px",
				c.cls, steps, layers, dual, span, span/float64(n))
			if dual > 5 {
				t.Errorf("%s t=%d步：DriftOverlapBandOnly dual = %.2f%% > 5%%（P6 判据线）", c.cls, steps, dual)
			}
		}
		// 等宽：BandWidthsPx 逐条 ≤1000、均值 ≤1000（t = 场当前时刻；dw 与 t 无关）
		widths := f.BandWidthsPx(w, h)
		if len(widths) != c.want {
			t.Fatalf("%s BandWidthsPx 条数 = %d，期望 %d", c.cls, len(widths), c.want)
		}
		sum := 0.0
		for i, dw := range widths {
			sum += dw
			if dw > 1000+1e-9 {
				t.Errorf("%s 带%d 宽 %.2f px > 1000（P6 设计第 2 条：T = round(w×1000/1080) 且 ≤0.95w）", c.cls, i, dw)
			}
		}
		if mean := sum / float64(len(widths)); mean > 1000+1e-9 {
			t.Errorf("%s 带宽均值 %.2f px > 1000", c.cls, mean)
		}
		// 覆盖率线（P6 设计第 5 条，AvgLayersOf 现成口径）：N=3 档 N·T/span ≥ 0.90
		span := SpanOf(widths, w)
		if got := AvgLayersOf(widths, span); got < 0.90 && n3Class(c.cls) {
			t.Errorf("%s 带-only 覆盖率 layers=%.4f < 0.90（P6 设计第 5 条：3×1000/3080 = 97.4%%）", c.cls, got)
		} else {
			t.Logf("%s 带-only 覆盖率 layers=%.4f span=%.1f", c.cls, AvgLayersOf(widths, span), span)
		}
	}
}

func n3Class(c effect.Class) bool {
	switch c {
	case effect.Overcast, effect.Cloudy, effect.Rain, effect.Thunder:
		return true
	}
	return false
}

// TestNoverlapBandsFixedStepsDrift 钉"漂移全周期"判据的抽样面：
// 400/800 固定步两点已钉在上面的几何测试；这里再加一条"带-only 双层占比在
// 全程 0–800 步每一拍都 ≤5%"的扫描（每 25 步取一拍，覆盖多个 span 圈）。
func TestNoverlapBandsDriftSweep(t *testing.T) {
	const w, h = PortraitW, PortraitH
	for _, cls := range []effect.Class{effect.Overcast, effect.Cloudy, effect.Rain, effect.Thunder, effect.Fog} {
		f := NewField(cls, true, budgetSeed)
		worst := 0.0
		for k := 1; k <= 800; k++ {
			f.Step(fixedStep, w, h)
			if k%25 != 0 {
				continue
			}
			_, dual := f.DriftOverlapBandOnly()
			if dual > worst {
				worst = dual
			}
		}
		t.Logf("%s：0–800 步逐 25 步抽样 带-only dual 最大 %.2f%%", cls, worst)
		if worst > 5 {
			t.Errorf("%s：漂移全程带-only dual 最大 %.2f%% > 5%%（P6 判据：漂移全周期不重叠）", cls, worst)
		}
	}
	// 速度口径回归（P6"不做什么"：0.03·w·t·0.725 不动）：带每秒位移 = 0.03·w·0.725
	f := NewField(effect.Cloudy, true, 20261015)
	span := f.bandSpan(PortraitW)
	want := 0.03 * float64(PortraitW) * 0.725
	a, _ := f.bandPlace(0, 0.0, PortraitW, PortraitH)
	b, _ := f.bandPlace(0, 1.0, PortraitW, PortraitH)
	d := b.x - a.x
	if d < 0 {
		d += span
	}
	if math.Abs(d-want) > 1e-6 {
		t.Errorf("带每秒位移 %.4f px ≠ 0.03·w·0.725 = %.4f px ⇒ 速度口径被改", d, want)
	}
}

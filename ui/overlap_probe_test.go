package ui

import (
	"math"
	"testing"

	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// P1 探针自检（spec/p1-probe.md 判据 3/4/5）。
//
// 口径全部来自 ui/overlap_probe.go，且与绘制共用同一套几何
// （bandPlace / spriteScale / CoverageProfile），测的就是屏幕上真画出来的东西。
// 固定种子 = 与 cmd/overlapprobe 同一个（表格要能对外复现）。

const (
	probeTestSeed int64 = 20261023
	probeTestCols       = 24
)

// probeTestRow 建探针场并出一行（w/h = 竖屏目标口径）。
func probeTestRow(t *testing.T, cls effect.Class, isDay bool) OverlapRow {
	t.Helper()
	f := NewFieldProbeCounted(cls, isDay, probeTestSeed)
	return f.OverlapRow(PortraitW, PortraitH, probeTestCols)
}

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

// 判据 3：复现主对话的算式（基线见 spec/p1-brief.md 的"现场基线"）。
//
// 算式（主对话口径，基线源宽 512）：
//
//	带宽  dw_i = 512 × (1.5 + 0.8·frac) ÷ bandNetDiv(1.00)，frac = i/(n−1)
//	周期  span = w + 2·平均dw（= bandSpan，ui/effects.go:2019），w = 1080
//	层数  avgLayers = Σ dw ÷ span
//	双层  dualPct = 等相位布局（中心 = span·i/n）下被 ≥2 层覆盖的长度占比
//
// 基线：n=4 ⇒ 1.29 层 / 29%；n=6 ⇒ 1.93 层 / 93%；n=2 ⇒ 0.64 层 / 0%。
func TestProbeBandFormulaMatchesBrief(t *testing.T) {
	cases := []struct {
		n                int
		wantLayers, dual float64
		dualTol          float64
	}{
		{4, 1.29, 29, 2},
		{6, 1.93, 93, 5},
		{2, 0.64, 0, 1},
	}
	for _, c := range cases {
		ws := BandWidthsNominal(512, c.n)
		span := SpanOf(ws, PortraitW)
		gotL := AvgLayersOf(ws, span)
		gotD := DualPctOf(ws, span)
		if !near(gotL, c.wantLayers, 0.02) {
			t.Errorf("n=%d avgLayers=%.4f 期望 %.2f（±0.02）", c.n, gotL, c.wantLayers)
		}
		if !near(gotD, c.dual, c.dualTol) {
			t.Errorf("n=%d dualPct=%.2f 期望 %.0f（±%.0f）", c.n, gotD, c.dual, c.dualTol)
		}
	}
	// 同一套算式打在**真实云带几何**上必须与名义 512 口径对得上
	// （证明带宽公式与 bandPlace 的缩放式没有漂移）。
	real := []struct {
		cls effect.Class
		n   int
	}{
		{effect.Cloudy, 3},   // P6 设计第 1 条：N = 3
		{effect.Rain, 3},     // P6 设计第 1 条：N = 3
		{effect.Overcast, 3}, // P6 设计第 1 条：4 → 3
		{effect.PartlyCloudy, 2},
	}
	for _, c := range real {
		f := NewFieldProbeCounted(c.cls, true, probeTestSeed)
		ws := f.BandWidthsPx(PortraitW, PortraitH)
		if len(ws) != c.n {
			t.Fatalf("%s 云带条数=%d 期望 %d", c.cls, len(ws), c.n)
		}
		nom := BandWidthsNominal(512, c.n)
		gotL := AvgLayersOf(ws, f.bandSpan(PortraitW))
		wantL := AvgLayersOf(nom, SpanOf(nom, PortraitW))
		if math.Abs(gotL-wantL)/wantL > 0.02 {
			t.Errorf("%s 真实带几何 avgLayers=%.4f 与名义口径 %.4f 偏差 >2%%", c.cls, gotL, wantL)
		}
	}
}

// 判据 4 之一：Clear 白天（无云带、1 朵薄云 + 3 个光斑；云影层对晴天也投 4 条带影）。
func TestProbeRowClearDay(t *testing.T) {
	r := probeTestRow(t, effect.Clear, true)
	if r.Total != 2 {
		t.Errorf("total=%d 期望 2（B3/R4 counts 口径，Clear 白天 = 1 云 + 1 光晕；原钉 4）", r.Total)
	}
	if !near(r.AvgLayers, 0.11, 1) {
		t.Errorf("avgLayers=%.2f 期望 0.11（整数容差 ±1）", r.AvgLayers)
	}
	// P3 判据 C（空影守卫）：Clear 白天本体没有一条云带 ⇒ 4 次带影不再投。
	// 原断言 9（= 1 云影 + 4 带影 + 4 粒子本体）⇒ 5（= 1 云影 + 4 粒子本体）；
	// B3/R4 云档封顶 2 朵 ⇒ 再降到 3（= 1 云影 + 1 云本体 + 1 光晕），实跑见 out/notes-b3-kinetics.md。
	if !near(float64(r.DrawsPerFrame), 3, 1) {
		t.Errorf("drawsPerFrame=%d 期望 3（±1）= 1 云影 + 1 云本体 + 1 光晕（B3/R4 封顶 2 朵）", r.DrawsPerFrame)
	}
}

// 判据 4 之二：Cloudy（4 条云带 + 3 朵粒子云，白天 ⇒ 影层 7 次）。
func TestProbeRowCloudy(t *testing.T) {
	r := probeTestRow(t, effect.Cloudy, true)
	if !near(r.AvgLayers, 1.91, 1) {
		t.Errorf("avgLayers=%.2f 期望 1.91（±1）", r.AvgLayers)
	}
	if !near(float64(r.DrawsPerFrame), 10, 1) {
		t.Errorf("drawsPerFrame=%d 期望 10（±1）= 3 带本体 + 3 带影 + 2 云影 + 2 云本体（B3/R4 云档 3→2 朵）", r.DrawsPerFrame)
	}
	// P3 判据 2 把 dualPct 口径换成**真实落位实测**；P6 设计第 6 条再拆两口径：
	// dualPct = 含粒子（DriftOverlapIncl，同带粒子对），DualBandOnly = 带-only（设计上 0）。
	// 旧钉值（实测 53.67 / 等相位 81.61，P3 几何：4 带 768–1178 px）按 P6 等宽几何重测：
	// P7（用户判据"最多的时候也只能首尾相连，不能重叠"）：云宽表封顶到 3 朵 ×340 px ⇒
	// Σ1020 px ≤ 1080 px ⇒ 含粒子占比也归 0（P6b 期的 22.53% 是 3×900 px 的算术必然）。
	if !near(r.DualPct, 0.00, 2) {
		t.Errorf("dualPct 含粒子=%.2f 期望 0.00（±2，P7 云宽封顶后落位，DriftOverlapIncl）", r.DualPct)
	}
	if r.DualPct > 10 {
		t.Errorf("dualPct 含粒子=%.2f 越过判据线 10%%", r.DualPct)
	}
	if r.DualBandOnly > 5 {
		t.Errorf("dualBandOnly=%.2f 越过 P6 判据线 5%%（设计上应恒 0）", r.DualBandOnly)
	}
	if !near(r.DualPctNominal, 33.75, 2) {
		t.Errorf("dualPct 等相位=%.2f 期望 33.75（±2，B3/R4 云档 480–529 px 重测；P7 期钉 38.36）", r.DualPctNominal)
	}
}

// 判据 4 之三：Overcast（6 条云带 + 5 朵粒子云；夜间档 ⇒ 无影层）。
func TestProbeRowOvercast(t *testing.T) {
	r := probeTestRow(t, effect.Overcast, false)
	// P3 判据 A/B 更新常量（带 6→4、粒子云 5→2）→ P6 再更新（带 4→3、等宽 T=1000 px）：
	// bandCount 4 → 3；avgLayers 1.89 → 1.56（= 3×1000/3080 + 2 朵粒子云 1955/3080）；
	// draws 6 → 5（= 3 带本体 + 2 云本体，夜间无影层）；
	// dualPct 含粒子钉 0.00（±2，Overcast 2 朵粒子云 spreadAir 后同带对不重叠）
	// 且 ≤10（P6 判据线）；带-only 钉 ≤5（设计恒 0）；等相位口径重测钉 56.45（±2）。
	if r.BandCount != 3 {
		t.Errorf("bandCount=%d 期望 3（P6 设计第 1 条：Overcast 带 4 → 3）", r.BandCount)
	}
	if !near(r.AvgLayers, 1.56, 1) {
		t.Errorf("avgLayers=%.2f 期望 1.56（±1，P6 后 3 带 + 2 云）", r.AvgLayers)
	}
	if !near(float64(r.DrawsPerFrame), 5, 1) {
		t.Errorf("drawsPerFrame=%d 期望 5（±1）= 3 带本体 + 2 云本体（夜间无影层）", r.DrawsPerFrame)
	}
	if !near(r.DualPct, 0.0, 2) {
		t.Errorf("dualPct 含粒子=%.2f 期望 0.00（±2，P6 几何重测，DriftOverlapIncl）", r.DualPct)
	}
	if r.DualPct > 10 {
		t.Errorf("dualPct 含粒子=%.2f 越过 P6 判据线 10%%", r.DualPct)
	}
	if r.DualBandOnly > 5 {
		t.Errorf("dualBandOnly=%.2f 越过 P6 判据线 5%%（设计上应恒 0）", r.DualBandOnly)
	}
	if !near(r.DualPctNominal, 33.75, 2) {
		t.Errorf("dualPct 等相位=%.2f 期望 33.75（±2，P7 云宽封顶后重测，P1 基线口径）", r.DualPctNominal)
	}
}

// 判据 4 之四：Fog（4 带 + 20 雾团 + 1 次 drawHaze 梯度）。
//
// 基线对照：主对话的"≈33 层当量"口径 = Σ雾团屏宽 ÷ **屏宽**（不含 span 的
// 2·平均带宽项）。本行的 avgLayers 用规格指定的 span 口径（= bandSpan），
// 两口径的差在 out/notes-p1-probe.md 逐式解释。
func TestProbeRowFog(t *testing.T) {
	r := probeTestRow(t, effect.Fog, true)
	// P3 判据 A/B 更新常量（Fog：带 4→2、20 粒子 → 2 粒子，其中 1 团雾幕 + 1 朵粒子云）：
	// 原断言 → 新断言：avgLayers 12.54 → 1.29；draws 29 → 8（= 2 带本体 + 2 带影 + 1 雾团 + 1 云本体 + 1 云影 + 1 drawHaze）；
	// 屏宽当量 33（20 团）→ 1.5（1 团）：口径不变（Σ雾宽 ÷ 屏宽），只随团数换常量，
	// 15% 偏差带保留 ⇒ 钉成 near(1.5, 0.45)（= 33→1.5 的同一相对带宽 0.15 换算到新值）。
	// 判据线一并钉：avgLayers ≤2.00、实测 dualPct ≤60%%。
	if !near(r.AvgLayers, 1.29, 1) {
		t.Errorf("avgLayers=%.2f 期望 1.29（±1，P3 后 2 带 + 1 团雾 + 1 朵云）", r.AvgLayers)
	}
	if !near(float64(r.DrawsPerFrame), 8, 1) {
		t.Errorf("drawsPerFrame=%d 期望 8（±1）= 2 带本体 + 2 带影 + 1 雾团 + 1 云本体 + 1 云影 + 1 drawHaze", r.DrawsPerFrame)
	}
	// P7：Fog 档雾团封顶 540 px（+云 500 px）⇒ 屏宽当量 = (540+500)/1080 × 净系数 = 0.87
	// （封顶前 1.5 = 884 px 的雾团 + 1063 px 的云，两者相加 1947 px > 1080 px ⇒ 必叠）
	if !near(r.FogEQ, 0.87, 0.2) {
		t.Errorf("雾的屏宽当量=%.2f 期望 0.87（±0.2，P7 雾团封顶 540 px，DriftLayers 口径）", r.FogEQ)
	}
	if r.AvgLayers > 2.0 {
		t.Errorf("avgLayers=%.2f 越过 P3 判据线 2.00", r.AvgLayers)
	}
	if r.DualPct > 60 {
		t.Errorf("dualPct 实测=%.2f 越过 P3 判据线 60%%", r.DualPct)
	}
}

// 判据 5：计数器开与关，Field.Draw 产生的**图元序列一致**
// （gioui v0.10.3 的 op.Ops 不导出长度，取序列化缓冲 data 与 refs 的长度对比）。
func TestProbeCounterDoesNotChangeOps(t *testing.T) {
	cases := []struct {
		cls   effect.Class
		isDay bool
	}{
		{effect.Clear, true},
		{effect.Clear, false},
		{effect.Cloudy, true},
		{effect.Overcast, false},
		{effect.Fog, true},
		{effect.Rain, true},
		{effect.Thunder, true},
		{effect.Snow, true},
		{effect.Dust, true},
	}
	for _, c := range cases {
		on := NewFieldProbeCounted(c.cls, c.isDay, probeTestSeed)
		off := NewField(c.cls, c.isDay, probeTestSeed) // 生产路径：probeCount 恒 false
		var oOn, oOff op.Ops
		on.Draw(probeGTX(&oOn, PortraitW, PortraitH), PortraitW, PortraitH)
		off.Draw(probeGTX(&oOff, PortraitW, PortraitH), PortraitW, PortraitH)
		dOn, rOn := OpsLen(&oOn)
		dOff, rOff := OpsLen(&oOff)
		if dOn < 0 || dOff < 0 {
			t.Fatalf("%s/isDay=%v OpsLen 没取到内部切片（探针失效）", c.cls, c.isDay)
		}
		if dOn != dOff || rOn != rOff {
			t.Errorf("%s/isDay=%v 图元序列不一致：开计数 data=%d refs=%d，关计数 data=%d refs=%d",
				c.cls, c.isDay, dOn, rOn, dOff, rOff)
		}
		if off.Draws() != 0 {
			t.Errorf("%s/isDay=%v 生产路径（NewField）计数器不为 0：%d", c.cls, c.isDay, off.Draws())
		}
		if on.Draws() <= 0 {
			t.Errorf("%s/isDay=%v 探针路径计数为 0（计数点没挂上）", c.cls, c.isDay)
		}
	}
}

// 判据 5 的补充：同一场开/关计数器（SetProbeCounting）连画两帧，
// 图元长度逐帧相等 ⇒ 计数器不参与任何绘制参数。
func TestProbeToggleKeepsOpsStable(t *testing.T) {
	f := NewFieldProbeCounted(effect.Cloudy, true, probeTestSeed)
	var lens [][2]int
	for _, on := range []bool{true, false, true, false} {
		f.SetProbeCounting(on)
		var o op.Ops
		f.Draw(probeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
		d, r := OpsLen(&o)
		lens = append(lens, [2]int{d, r})
	}
	for i := 1; i < len(lens); i++ {
		if lens[i] != lens[0] {
			t.Fatalf("第 %d 帧图元长度与第 0 帧不一致：%v vs %v", i, lens[i], lens[0])
		}
	}
	f.SetProbeCounting(true)
	f.ResetDraws()
	f.Draw(probeGTX(new(op.Ops), PortraitW, PortraitH), PortraitW, PortraitH)
	if f.Draws() != 10 {
		t.Errorf("Cloudy 白天 draws=%d 期望 10（B3/R4 云档 3→2：3 带本体 + 3 带影 + 2 云影 + 2 云本体）", f.Draws())
	}
	f.SetProbeCounting(false)
	f.ResetDraws()
	f.Draw(probeGTX(new(op.Ops), PortraitW, PortraitH), PortraitW, PortraitH)
	if f.Draws() != 0 {
		t.Errorf("关计数器后 draws=%d 期望 0", f.Draws())
	}
}

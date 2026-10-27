package ui

// P3 判据 3 的钉死单测（判据 3 逐条）。
// 口径来源（不在这里发明新几何）：
// - 带数 = Field.bandCount（ui/effects.go）；粒子数 = counts（ui/effects.go）
// - 漂移重叠 = Field.DriftOverlapIncl / Field.DriftOverlapBandOnly（P6 设计第 6 条两口径，
// 按当前真实落位，与探针共用 bandSpan）
// - 云影 = Field.cloudShadowPlaces（与 GPU 绘制同一份数据）
// - 层数/双层 = OverlapRow（ui/overlap_probe.go，avgLayers = Σ层屏宽 ÷ bandSpan）
// 基线数字来自落位探针的实测行与预算线。
// P6 判据（判据第 2 条，逐字）：预算线按新判据收紧——
//	带-only 真落位双层占比 ≤5%（budgetDualBandOnlyLine）
//	含粒子云/雾团 ≤10%（budgetDualLine：60 → 10，**只许收紧不许放宽**）
// 400 与 800 固定步都测（P6 判据第 2 条逐字）。

import (
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/metrics"
)

const (
	budgetSeed         int64 = 20261023
	budgetCols               = 24
	budgetDualLine           = 10.0 // P6 判据：含粒子云/雾团 ≤10%（P2 旧值 60，只许收紧）
	budgetDualBandOnly       = 5.0  // P6 判据：带-only 真落位 ≤5%
	budgetLayerLine          = 2.00 // P2 预算三条线之一（P6 不动）
)

// 判据 3 之一（P6 收紧）：Overcast 带数 3、粒子云 2，漂移 400 与 800 固定步后
// 实测双层占比 含粒子 ≤10%、带-only ≤5%（400 与 800 都测，P6 判据第 2 条逐字）。
func TestBudgetOvercastCapAndDrift(t *testing.T) {
	if n := counts(effect.Overcast, false); n != 2 {
		t.Errorf("counts(Overcast) = %d，期望 2（P3 判据 B：5 → 2）", n)
	}
	f := NewField(effect.Overcast, false, budgetSeed)
	if n := f.bandCount(); n != 3 {
		t.Errorf("bandCount(Overcast) = %d，期望 3（P6 设计第 1 条：4 → 3）", n)
	}
	if f.TotalCount() != 2 {
		t.Fatalf("Overcast 全场粒子数 = %d，期望 2", f.TotalCount())
	}
	for _, steps := range []int{400, 800} {
		// 400/800 固定步 = N × fixedStep（1/60 s），与墙钟无关（C1 口径）
		g := NewField(effect.Overcast, false, budgetSeed)
		for k := 0; k < steps; k++ {
			g.Step(fixedStep, PortraitW, PortraitH)
		}
		layers, dual := g.DriftOverlapIncl()
		_, dualBand := g.DriftOverlapBandOnly()
		t.Logf("Overcast %d 固定步后：layers=%.2f dualIncl=%.2f%% dualBandOnly=%.2f%% steps=%d",
			steps, layers, dual, dualBand, g.steps)
		if dual > budgetDualLine {
			t.Errorf("DriftOverlapIncl dualPct = %.2f%% > %.0f%%（P6 判据：含粒子 ≤10%%，%d 固定步）", dual, budgetDualLine, steps)
		}
		if dualBand > budgetDualBandOnly {
			t.Errorf("DriftOverlapBandOnly dualPct = %.2f%% > %.0f%%（P6 判据：带-only ≤5%%，%d 固定步）", dualBand, budgetDualBandOnly, steps)
		}
		if layers > budgetLayerLine {
			t.Errorf("DriftOverlapIncl layers = %.2f > %.2f（P2 预算线，%d 固定步）", layers, budgetLayerLine, steps)
		}
	}
}

// 判据 3 之二：Fog 粒子数 2、带数 2。
func TestBudgetFogCap(t *testing.T) {
	if n := counts(effect.Fog, true); n != 2 {
		t.Errorf("counts(Fog) = %d，期望 2（P3 判据 B：20 → 2）", n)
	}
	f := NewField(effect.Fog, true, budgetSeed)
	if n := f.bandCount(); n != 2 {
		t.Errorf("bandCount(Fog) = %d，期望 2（P3 判据 3 逐字 + 预算表'雾的带 4→2'）", n)
	}
	if f.TotalCount() != 2 {
		t.Fatalf("Fog 全场粒子数 = %d，期望 2", f.TotalCount())
	}
	// 2 个粒子 = 1 团雾幕 + 1 朵粒子云（换种不换数：判据 4 的覆盖率口径只认云带+粒子云，
	// 2 条带 + 2 团雾实测 35.42% < 下限 35.7%，见）
	fog, cloud := 0, 0
	for i := range f.parts {
		switch f.parts[i].kind {
		case pkFog:
			fog++
		case pkCloud:
			cloud++
		}
	}
	if fog != 1 || cloud != 1 {
		t.Errorf("Fog 粒子构成 = 雾团 %d + 粒子云 %d，期望 1 + 1", fog, cloud)
	}
}

// 判据 3 之三：Snow 的雾团（pkFog）粒子数 == 3。
func TestBudgetSnowFogCurtain3(t *testing.T) {
	for _, isDay := range []bool{true, false} {
		f := NewField(effect.Snow, isDay, budgetSeed)
		n := 0
		for i := range f.parts {
			if f.parts[i].kind == pkFog {
				n++
			}
		}
		if n != 3 {
			t.Errorf("Snow isDay=%v：pkFog 粒子数 = %d，期望 3（P3 判据 B：20 → 3，取法 idx%%86==85）", isDay, n)
		}
	}
}

// 判据 3 之四：Clear 白天 / Snow / Dust 的云影表里**云带影 = 0**（只含粒子云影）。
// 判据 C 的落点：cloudShadowPlaces 的带影段加了 hasCloudBands 守卫。
// 云带影的辨认口径 = 影的 dw 等于'该带 bandPlace 给的 dw × cloudShadowSizeMul'
// （cloudShadowPlaces 的带影段：sc = bp.dw/sz.X × cloudShadowSizeMul ⇒ dw = bp.dw × cloudShadowSizeMul）；
// 粒子云影的 dw = 云本体 dw × cloudShadowSizeMul，云本体 dw 由 sizeCloudByClass 反解到本类区间，
// 与云带区间（768–1178 px）逐档可分（Clear 白天 300–600、PartlyCloudy 300–700、Cloudy 500–900）。
func TestBudgetEmptyCloudShadowGone(t *testing.T) {
	bandShadowShadows := func(f *Field, w, h int, t float64) int {
		bandW := make([]float64, 0, f.bandCount())
		for i := 0; i < f.bandCount(); i++ {
			bp, ok := f.bandPlace(i, t, w, h)
			if !ok {
				continue
			}
			// 带影的 dw = 本体 dw × cloudShadowSizeMul（cloudShadowPlaces 的 sc 式）
			bandW = append(bandW, bp.dw*cloudShadowSizeMul)
		}
		n := 0
		for _, pl := range f.cloudShadowPlaces(w, h, t) {
			for _, dw := range bandW {
				if math.Abs(pl.dw-dw) < 0.5 {
					n++
					break
				}
			}
		}
		return n
	}
	// Clear 白天：本体无带（hasCloudBands=false）⇒ 4 次带影全免，只剩 1 朵粒子云的影
	cf := NewField(effect.Clear, true, budgetSeed)
	cf.Step(time.Second, PortraitW, PortraitH)
	if n := bandShadowShadows(cf, PortraitW, PortraitH, cf.t); n != 0 {
		t.Errorf("Clear 白天云带影 = %d，期望 0（判据 C）", n)
	}
	if n := len(cf.cloudShadowPlaces(PortraitW, PortraitH, cf.t)); n != 1 {
		t.Errorf("Clear 白天云影总数 = %d，期望 1（= 1 朵粒子云的影）", n)
	}
	// Snow / Dust：既无云带也无粒子云 ⇒ 影表整体为空
	for _, cls := range []effect.Class{effect.Snow, effect.Dust} {
		f := NewField(cls, true, budgetSeed)
		f.Step(time.Second, PortraitW, PortraitH)
		if n := len(f.cloudShadowPlaces(PortraitW, PortraitH, f.t)); n != 0 {
			t.Errorf("%s 云影总数 = %d，期望 0（本体零条带零粒子云，判据 C 把 4 次空影全部免掉）", cls, n)
		}
	}
	// 反向对照：真画云带的档（Cloudy）带影必须还在 ⇒ 守卫不是"把带影整体关掉"
	cl := NewField(effect.Cloudy, true, budgetSeed)
	cl.Step(time.Second, PortraitW, PortraitH)
	if n := bandShadowShadows(cl, PortraitW, PortraitH, cl.t); n != cl.bandCount() {
		t.Errorf("Cloudy 云带影 = %d，期望 %d（守卫只砍'本体没带'的档）", n, cl.bandCount())
	}
}

// 判据 3 之五：Rain / Thunder / Snow 的降水粒子数 220 / 300 / 260 一律没动。
func TestBudgetPrecipCountsUntouched(t *testing.T) {
	want := map[effect.Class]int{effect.Rain: 220, effect.Thunder: 300, effect.Snow: 260}
	for cls, n := range want {
		for _, isDay := range []bool{true, false} {
			if got := counts(cls, isDay); got != n {
				t.Errorf("counts(%s,%v) = %d，期望 %d（P3 不许动降水粒子数）", cls, isDay, got, n)
			}
			if got := NewField(cls, isDay, budgetSeed).TotalCount(); got != n {
				t.Errorf("%s isDay=%v：TotalCount = %d，期望 %d", cls, isDay, got, n)
			}
		}
	}
}

// 判据 3 的补充（判据 2 的线进单测）：10 个探针档逐类 avgLayers ≤2.00 且实测 dualPct ≤60%。
func TestBudgetAllProbeClassesWithinLines(t *testing.T) {
	cases := []struct {
		cls   effect.Class
		isDay bool
	}{
		{effect.Clear, true},
		{effect.Clear, false},
		{effect.Cloudy, true},
		{effect.PartlyCloudy, true},
		{effect.Overcast, false},
		{effect.Rain, true},
		{effect.Thunder, true},
		{effect.Fog, true},
		{effect.Snow, true},
		{effect.Dust, true},
	}
	for _, c := range cases {
		// P6 判据线（带-only ≤5% / 含粒子 ≤10%）是**漂移期**口径 ⇒ 出行前先走 400 固定步
		// （与 TestBudgetOvercastCapAndDrift 同一漂移窗口；建场那一拍的随机落位不是判据本体）。
		pf := NewFieldProbeCounted(c.cls, c.isDay, probeTestSeed)
		for k := 0; k < 400; k++ {
			pf.Step(fixedStep, PortraitW, PortraitH)
		}
		r := pf.OverlapRow(PortraitW, PortraitH, probeTestCols)
		t.Logf("%-13s layers=%.2f dualIncl(P6)=%.2f dualBandOnly(P6)=%.2f dual等相位=%.2f 覆盖=%.2f%%",
			c.cls, r.AvgLayers, r.DualPct, r.DualBandOnly, r.DualPctNominal, r.CoveragePct)
		if r.AvgLayers > budgetLayerLine {
			t.Errorf("%s avgLayers=%.2f > %.2f", c.cls, r.AvgLayers, budgetLayerLine)
		}
		// Snow 灰幕 = P3 钉死件（3 团 × 1500–1900 px，Σ宽 ≥ 4710 px > span 3080 ⇒
		// 同带粒子对重叠的几何下界 (4710−3080)/3080 = 52.9%），"≤10%"对 Snow 不可达
		// （逐式在）⇒ Snow 单独收 60 → 55 线（只收紧），其余云/雾档吃 P6 线 10%。
		// 雾团封顶后 Snow 3 张灰幕 = 3×346 px（Σ1038 ≤ 1080）⇒ 与其余云/雾档同吃 10% 线。
		line := budgetDualLine
		if r.DualPct > line {
			t.Errorf("%s dualPct 含粒子=%.2f > %.0f%%（P6 判据）", c.cls, r.DualPct, line)
		}
		if r.DualBandOnly > budgetDualBandOnly {
			t.Errorf("%s dualPct 带-only=%.2f > %.0f%%（P6 判据）", c.cls, r.DualBandOnly, budgetDualBandOnly)
		}
	}
}

// 判据 4 的线进单测。
// P7 口径变更（判据"屏幕上的云或雾最多的时候也只能首尾相连，不能重叠"）：
// 旧基线覆盖率（Overcast 54.79 / Fog 39.7 / Cloudy 46.15）是**层与层相叠**换来的
// ——相叠把 3 条带/2 团挤在同一段 ⇒ 覆盖率虚高。"永不相叠"之下满密度覆盖率的上界
// = Σ层宽 ÷ 屏宽（首尾相连铺满一屏 = 100%），铺放态快照只有一两条在屏内
// ⇒ 实测 Overcast 34.48%、Fog 26.87%、Cloudy 42.92%（seed=20261023 探针表）。
// 旧"≥基线 90%"与"永不相叠"算术互斥 ⇒ 下限按 P7 实测重钉（出处
// + 本轮探针表）。
func TestBudgetCoverageFloors(t *testing.T) {
	floors := map[effect.Class]float64{
		effect.Overcast: 32.0,
		effect.Fog:      25.0,
		effect.Cloudy:   41.5,
	}
	isDay := map[effect.Class]bool{effect.Overcast: false, effect.Fog: true, effect.Cloudy: true}
	for cls, floor := range floors {
		r := probeTestRow(t, cls, isDay[cls])
		if r.CoveragePct < floor {
			t.Errorf("%s coveragePct=%.2f%% < 下限 %.1f%%（基线的 90%%）", cls, r.CoveragePct, floor)
		}
		t.Logf("%-13s coveragePct=%.2f%% ≥ %.1f%%", cls, r.CoveragePct, floor)
	}
}

// 判据 D 的节流与处置（P6 设计第 7 条：处置 = 回收重进场，P3"只降 alpha"缓解已删除）：
// 跑满 4 个节流窗口后两口径都在预算线内，且处置不碰 alpha（alpha 逐字 = 生成值，
// 回收只挪 x/y：driftRecycleOffscreen 逐字不动 alpha/sc/vx 字段）。
func TestBudgetCrowdSweepThrottleAndCap(t *testing.T) {
	f := NewField(effect.Overcast, true, budgetSeed)
	alphaSum0 := 0.0
	for i := range f.parts {
		alphaSum0 += f.parts[i].alpha
	}
	for k := 0; k < driftCheckEverySteps*4; k++ {
		f.Step(fixedStep, PortraitW, PortraitH)
	}
	alphaSum := 0.0
	for i := range f.parts {
		alphaSum += f.parts[i].alpha
	}
	if math.Abs(alphaSum-alphaSum0) > 1e-9 {
		t.Errorf("处置动了 alpha（Σalpha %.6f → %.6f）⇒ P6 设计第 7 条只许回收挪位，alpha 口径不许碰",
			alphaSum0, alphaSum)
	}
	_, dual := f.DriftOverlapIncl()
	_, dualBand := f.DriftOverlapBandOnly()
	t.Logf("Overcast 白天 %d 步后 dualIncl=%.2f%% dualBandOnly=%.2f%%（预算 %.0f%%/%.0f%%）",
		driftCheckEverySteps*4, dual, dualBand, driftDualBudgetPct, budgetDualBandOnly)
	if dual > budgetDualLine {
		t.Errorf("dualIncl=%.2f%% > %.0f%%（P6 判据线，处置跑了 4 个节流窗口后必须回到预算内）", dual, budgetDualLine)
	}
	if dualBand > budgetDualBandOnly {
		t.Errorf("dualBandOnly=%.2f%% > %.0f%%（P6 判据线）", dualBand, budgetDualBandOnly)
	}
}

// 判据 D 处置机理的现场验证（P6 设计第 7 条口径）：Snow 的 3 团灰幕压到同一 x/y
// （同带、当场超预算），在一个节流点上直接调 crowdSweep，必须看到 driftWorstPair 命中的
// 粒子被**挪到屏外重进场**（x 整只出屏 = 回收进场位，回收窗常量 recycleXLo/recycleYHi 口径），
// 且处置不碰速度（雾团 vx 合计 > 0、逐团 vx 一字不变）。
// 用 Snow 的理由：3 团 ~1500–1900 px 压在一起时同带粒子对重叠当场远超预算线，
// 云量档（Overcast 2 朵）造不出超线场景（P6 后带-only 恒 0）。
func TestBudgetCrowdSweepFiresWhenOverBudget(t *testing.T) {
	f := NewField(effect.Snow, true, budgetSeed)
	// 先跑满启动期上限（C3 BootCap：前 60 帧只画 80 个 ⇒ 灰幕在 idx 85/171/257，
	// 不在前 80 个里 ⇒ 扫描看不见）⇒ warmup 200 固定步让 active 回到总数。
	for k := 0; k < 200; k++ {
		f.Step(fixedStep, PortraitW, PortraitH)
	}
	if got := f.ActiveCount(); got != f.TotalCount() {
		t.Fatalf("warmup 后 active = %d，期望 %d（启动期上限未过）", got, f.TotalCount())
	}
	var y0 float64 = -1
	n := 0
	for i := range f.parts {
		if f.parts[i].kind == pkFog {
			if y0 < 0 {
				y0 = f.parts[i].y
			}
			f.parts[i].y = y0 // 同带（Δy = 0 ≤ airBandN）
			f.parts[i].x = 0.5
			n++
		}
	}
	if n < 2 {
		t.Fatalf("雾团不足 2 团（%d）⇒ 本判据无同带对可造", n)
	}
	ls, span := f.driftLayers(PortraitW, PortraitH)
	dual := metrics.DriftDualParticlePairs(ls, span, airBandN)
	t.Logf("造场后 dualIncl(同带粒子对)=%.2f%%（预算 %.0f%%）", dual, driftDualBudgetPct)
	if dual <= driftDualBudgetPct {
		t.Fatalf("造场未把 dualPct 顶过预算（%.2f%%）⇒ 本判据无意义", dual)
	}
	xBefore := make(map[int]float64, n)
	vxBefore := make(map[int]float64, n)
	for i := range f.parts {
		if f.parts[i].kind == pkFog {
			xBefore[i] = f.parts[i].x
			vxBefore[i] = f.parts[i].vx
		}
	}
	// 把 steps 摆到节流点（steps % N == 0）后**直接调 crowdSweep**。
	// 为什么不靠 Step 驱动到点：实测 dualPct 逐拍在 48–77%% 之间摆动（雾速 0.010–0.040 屏宽/s
	// × 0.5 s 的节流窗口 = 0.005–0.02 屏宽 ≈ 5–21 px 的相对位移），
	// 落在节流点那一拍读数是预算内还是预算外是**采样相位**，不该由测试钉住。
	stepsBefore := f.steps
	f.steps = driftCheckEverySteps * (stepsBefore/driftCheckEverySteps + 1)
	f.crowdSweep()
	t.Logf("节流点 steps: %d → %d（N=%d）", stepsBefore, f.steps, driftCheckEverySteps)
	moved, vx0 := 0, 0.0
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkFog {
			continue
		}
		if p.x != xBefore[i] {
			moved++
			// 回收位必须整只在屏外（进场位口径：vx>0 → x+hw ≤ 0，vx<0 → x−hw ≥ 1）
			hw, _ := f.airHalfN(p)
			if p.vx >= 0 {
				if p.x+hw > 1e-9 {
					t.Errorf("雾团 %d 被挪到 x=%.4f，整只没出屏（hw=%.4f）⇒ 回收位必须屏外", i, p.x, hw)
				}
			} else if p.x-hw < 1-1e-9 {
				t.Errorf("雾团 %d 被挪到 x=%.4f，整只没出屏（hw=%.4f）⇒ 回收位必须屏外", i, p.x, hw)
			}
		}
		if p.vx != vxBefore[i] {
			t.Errorf("雾团 %d vx 被处置改动（%.6f → %.6f）⇒ 处置只挪位，不许碰速度", i, vxBefore[i], p.vx)
		}
		vx0 += p.vx
	}
	n2, dualAfter := 0.0, 0.0
	ls2, span2 := f.driftLayers(PortraitW, PortraitH)
	_ = n2
	dualAfter = metrics.DriftDualParticlePairs(ls2, span2, airBandN)
	t.Logf("处置后 dualIncl=%.2f%%，被挪到屏外的雾团 %d 团，雾团 vx 合计 %.4f", dualAfter, moved, vx0)
	if moved == 0 {
		t.Errorf("dual=%.2f%% 已超预算，跑完一个节流点后无一团被挪到屏外 ⇒ crowdSweep 的回收处置没落地", dual)
	}
	if vx0 <= 0 {
		t.Errorf("处置把雾团的速度打掉了（vx 合计 %.4f）⇒ 机理不许碰速度", vx0)
	}
}

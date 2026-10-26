package ui

// P1 探针（云/雾重叠与每帧绘制次数）——**只测量，不改行为**。
//
// 口径来源（逐条对得上现场代码，不在这里发明新几何）：
//   - 云带屏宽：ui/effects.go bandPlace（P6 等宽口径 sc = T/sz.X，T = bandWidthPx(w)
//     = round(w×1000/1080) 且 T ≤ 0.95w ⇒ dw = T 逐带相同）
//   - 横向周期：ui/effects.go bandSpan（P6 常量式 span = w + 2·T）
//   - 粒子云屏宽：ui/effects.go:3727 cloudDrawWidthPx（源宽 × spriteScale）
//   - 雾团屏宽：fogSz × spriteScale（spriteScale 的 pkFog 分支，ui/effects.go:3808-3812）
//   - 覆盖率：ui/effects.go:4144 CoverageProfile（逐列 40 行采样，不重新发明）
//
// 绘制计数：只读 Field.probeCount（探针构造路径 NewFieldProbeCounted 才置位），
// 普通 NewField / NewFieldEntering 路径该字段恒为 false ⇒ 分支恒假，零行为变化。

import (
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/metrics"
)

// ---- 探针构造口 ----

// NewFieldProbeCounted 建一个**开了绘制计数器**的场（P1 探针专用）。
// 与 NewField 的唯一区别：probeCount=true（只影响自增，不影响任何绘制参数）。
func NewFieldProbeCounted(cls effect.Class, isDay bool, seed int64) *Field {
	f := newField(cls, isDay, seed, false)
	f.probeCount = true
	return f
}

// SetProbeCounting 开/关绘制计数器（判据 5 用它做同一场开/关对比）。
func (f *Field) SetProbeCounting(on bool) { f.probeCount = on }

// Draws 累计的"贴图提交"次数（精灵 Add + paint.PaintOp 的组合，1 次/组合）。
func (f *Field) Draws() int { return f.draws }

// ResetDraws 清零计数。
func (f *Field) ResetDraws() { f.draws = 0 }

// ---- P5b 测量件（spec/p5b-dbg.md 第 1 条）--------------------------------
//
// 复用 P1 已有的 probeCount/draws 通路，**不新建第二条计数通路**。
// 生产构造路径（NewField / NewFieldEntering）是否置位 probeCount，完全由
// 环境变量 WS_DBG_FRAMES 决定（接线点：ui/effects.go 的 newField）：
// 未设 ⇒ probeCount 恒 false ⇒ 绘制路径里 `if f.probeCount { f.draws++ }`
// 分支恒假 ⇒ 零计数开销、零额外日志（P5b 判据 4"默认行为一字不变"）。

// WSDbgFrames 环境变量 WS_DBG_FRAMES=1 ⇒ 开启全帧测量（P5b 测量件的唯一开关）。
// R5 迁移：算式本体逐字迁出到 ui/metrics/frames.go（原定义 = ui/overlap_probe.go:57-60）。
// 这里留一个**兼容出口**：cmd/scr/main.go:695 的调用点（ui.WSDbgFrames()）一字不改。
func WSDbgFrames() bool { return metrics.WSDbgFrames() }

// DrawCount 本帧累计的"贴图提交"次数（与 Draws 同一个计数器、同一批计数点）。
// 未开 WS_DBG_FRAMES 时 probeCount 恒 false ⇒ 恒返回 0。
func (f *Field) DrawCount() int { return f.draws }

// ResetDrawCount 清零计数（主循环每帧打印后调用 ⇒ draws 表示"本帧"绘制次数）。
func (f *Field) ResetDrawCount() { f.draws = 0 }

// BandCountForProbe 云带条数（bandCount 口径，ui/effects.go:1998）的导出口，
// 给探针命令与外部自检用（只读，不改 bandCount 本身）。
func (f *Field) BandCountForProbe() int { return f.bandCount() }

// ---- 层宽（屏上 px） ----

// BandWidthsPx 本场云带的屏宽序列（px）。
// 逐条走 bandPlace（与 GPU 绘制同一套几何，探针不另抄一份公式）。
func (f *Field) BandWidthsPx(w, h int) []float64 {
	if !f.hasCloudBands() {
		return nil
	}
	n := f.bandCount()
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		bp, ok := f.bandPlace(i, f.t, w, h)
		if !ok {
			continue
		}
		out = append(out, bp.dw)
	}
	return out
}

// AirWidthsPx 空中层（粒子云 + 雾团）的屏宽序列（px）。
// 取尺寸方式与 CoverageProfile 一致（贴图尺寸 × spriteScale）。
func (f *Field) AirWidthsPx() []float64 {
	sp := f.sp
	out := make([]float64, 0, 32)
	for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
		p := &f.parts[i]
		switch p.kind {
		case pkCloud:
			out = append(out, cloudDrawWidthPx(p))
		case pkFog:
			sz := metrics.SpriteSizeOr(sp.fogSz, p.spr)
			if sz.X <= 0 {
				continue
			}
			out = append(out, float64(sz.X)*spriteScale(p))
		}
	}
	return out
}

// ---- 纯算式（判据 3 复现主对话基线用，全部不读 Field） ----

// ---- 行汇总 ----

// OverlapRow 一行的探针输出（TSV 的 7 列 + 类标识 + 算式中间量）。
type OverlapRow struct {
	Class          effect.Class
	IsDay          bool
	Total          int     // counts 口径的粒子总数 = len(parts)
	BandCount      int     // bandCount 口径的云带条数
	AvgLayers      float64 // Σ层屏宽 ÷ span（span = bandSpan 口径）
	DualPct        float64 // 含粒子云/雾团的真实落位双层占比% = Field.DriftOverlapIncl（P6 设计第 6 条两口径之一）
	DualBandOnly   float64 // 带-only 真实落位双层占比% = Field.DriftOverlapBandOnly（P6 设计第 6 条：只算云带彼此，判据 ≤5%）
	DualPctNominal float64 // 同一算式的**等相位假设值**（P1 基线口径，粒子层按 span·i/n 摆放，只作对照）
	DrawsPerFrame  int     // Field.Draw 一帧的贴图提交次数（计数器口径）
	CoveragePct    float64 // CoverageProfile 逐列覆盖率的均值（%）

	Widths []float64 // 参与算式的层屏宽（带 + 粒子云 + 雾团）
	Span   float64   // 横向周期（bandSpan 口径）
	FogEQ  float64   // 主对话 Fog 基线口径：Σ雾团屏宽 ÷ 屏宽
}

// OverlapRow 汇总本场一行探针数（w/h = 目标屏；cols = CoverageProfile 列数）。
// drawsPerFrame 只有由 NewFieldProbeCounted 建出的场才有数（未计数时为 0）。
func (f *Field) OverlapRow(w, h, cols int) OverlapRow {
	widths := append(f.BandWidthsPx(w, h), f.AirWidthsPx()...)
	span := f.bandSpan(w)
	draws := 0
	if f.probeCount {
		var o op.Ops
		f.Draw(metrics.ProbeGTX(&o, w, h), w, h)
		draws = f.draws
		f.ResetDraws()
	}
	// P3 判据 2 的口径升级：dualPct 取**真实落位**（DriftOverlap），等相位假设值一并输出。
	// 理由（out/notes-p2-budget.md 风险第 2 条）：粒子云/雾团是 spawn+spreadAir 随机落位，
	// 等相位值不是屏幕上真存在的东西 ⇒ 判据线要能量在实测值上。
	// P6 设计第 6 条：两口径分开出数 —— 带-only（DriftOverlapBandOnly）与含粒子（DriftOverlapIncl）。
	driftLayersAll, driftDual := f.DriftOverlapIncl()
	_, bandDual := f.DriftOverlapBandOnly()
	_ = driftLayersAll
	nominal := metrics.DualPctOf(widths, span)
	prof := f.CoverageProfile(w, h, cols)
	cover := metrics.MeanCoveragePct(prof)
	return OverlapRow{
		Class:          f.cls,
		IsDay:          f.isDay,
		Total:          f.TotalCount(),
		BandCount:      f.bandCount(),
		AvgLayers:      metrics.AvgLayersOf(widths, span),
		DualPct:        driftDual,
		DualBandOnly:   bandDual,
		DualPctNominal: nominal,
		DrawsPerFrame:  draws,
		CoveragePct:    cover,
		Widths:         widths,
		Span:           span,
		FogEQ:          f.FogLayersScreenWidth(w),
	}
}

// FogLayersScreenWidth 主对话 Fog 基线口径（Σ雾团屏宽 ÷ 屏宽）。
// notes 用它解释 span 口径（bandSpan）与"≈33 层当量"口径的差。
func (f *Field) FogLayersScreenWidth(w int) float64 {
	if w <= 0 {
		return 0
	}
	sum := 0.0
	for _, dw := range f.AirWidthsPx() {
		sum += dw
	}
	return sum / float64(w)
}

// ---- 图元序列长度（判据 5） ----

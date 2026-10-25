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
	"image"
	"math"
	"os"
	"reflect"
	"sort"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
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
// 每次调用现读环境：建场是低频事件 ⇒ 开销可忽略，且测试里 setenv/unsetenv
// 立刻生效，没有缓存陈旧窗口。
func WSDbgFrames() bool { return os.Getenv("WS_DBG_FRAMES") == "1" }

// DrawCount 本帧累计的"贴图提交"次数（与 Draws 同一个计数器、同一批计数点）。
// 未开 WS_DBG_FRAMES 时 probeCount 恒 false ⇒ 恒返回 0。
func (f *Field) DrawCount() int { return f.draws }

// ResetDrawCount 清零计数（主循环每帧打印后调用 ⇒ draws 表示"本帧"绘制次数）。
func (f *Field) ResetDrawCount() { f.draws = 0 }

// BandCountForProbe 云带条数（bandCount 口径，ui/effects.go:1998）的导出口，
// 给探针命令与外部自检用（只读，不改 bandCount 本身）。
func (f *Field) BandCountForProbe() int { return f.bandCount() }

// probeGTX 探针用的空 layout.Context（只给 Ops，离屏计数不真渲染）。
func probeGTX(o *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         o,
		Constraints: layout.Exact(image.Pt(w, h)),
		Now:         time.Now(),
	}
}

// ---- 层宽（屏上 px） ----

// spriteSizeOr 下标取贴图尺寸，越界时取首张（与绘制路径的兜底一致）。
func spriteSizeOr(sizes []image.Point, spr int) image.Point {
	if len(sizes) == 0 {
		return image.Point{}
	}
	if spr >= 0 && spr < len(sizes) {
		return sizes[spr]
	}
	return sizes[0]
}

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
			sz := spriteSizeOr(sp.fogSz, p.spr)
			if sz.X <= 0 {
				continue
			}
			out = append(out, float64(sz.X)*spriteScale(p))
		}
	}
	return out
}

// ---- 纯算式（判据 3 复现主对话基线用，全部不读 Field） ----

// BandWidthsNominal 主对话基线口径的名义云带宽（px）：
// dw = srcW × (1.5 + 0.8·frac) ÷ bandNetDiv，frac = i/(n−1)（n=1 时 frac=0）。
//
// P6 起真实带几何改等宽（dw = T，逐带相同），本名义式只作 P1 基线算术保留
// （TestProbeBandFormulaMatchesBrief 用它对照"逐带 768–1178 的平均"与"等宽 T"
// 的 avgLayers 相对偏差 ≤2%——两者求和后同为 ~3T 量级，偏差来自精灵长宽比）。
func BandWidthsNominal(srcW float64, n int) []float64 {
	if n <= 0 {
		return nil
	}
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		frac := 0.0
		if n > 1 {
			frac = float64(i) / float64(n-1)
		}
		out = append(out, srcW*(1.5+0.8*frac)/bandNetDiv)
	}
	return out
}

// SpanOf 横向周期（px），与 bandSpan 同式（P6 常量式：span = w + 2·T，T 由 widths 均值给出）。
func SpanOf(widths []float64, w int) float64 {
	if len(widths) == 0 {
		return float64(w)
	}
	sum := 0.0
	for _, dw := range widths {
		sum += dw
	}
	return float64(w) + 2*(sum/float64(len(widths)))
}

// AvgLayersOf 平均覆盖层数 = Σ屏宽 ÷ span。
func AvgLayersOf(widths []float64, span float64) float64 {
	if span <= 0 {
		return 0
	}
	sum := 0.0
	for _, dw := range widths {
		sum += dw
	}
	return sum / span
}

// layerSpanInterval 一层在一个周期内的覆盖区间（起点可绕回周期末）。
type layerSpanInterval struct {
	start, end float64 // end > span 表示绕回
	full       bool    // dw ≥ span ⇒ 铺满整周期
}

func spanIntervals(widths []float64, span float64) []layerSpanInterval {
	n := len(widths)
	out := make([]layerSpanInterval, 0, n)
	for i, dw := range widths {
		if dw <= 0 {
			continue
		}
		if dw >= span {
			out = append(out, layerSpanInterval{full: true})
			continue
		}
		start := math.Mod(float64(i)*span/float64(n)-dw/2, span)
		if start < 0 {
			start += span
		}
		out = append(out, layerSpanInterval{start: start, end: start + dw})
	}
	return out
}

// covers 区间是否覆盖周期内的点 x（span = 本次扫描的周期，逐次传入，无全局态）。
func (iv layerSpanInterval) covers(x, span float64) bool {
	if iv.full {
		return true
	}
	if iv.end <= span {
		return x >= iv.start && x <= iv.end
	}
	return x >= iv.start || x <= iv.end-span
}

// DualPctOf 一个 span 周期内被 **≥2 层**覆盖的长度占比（%）。
//
// 布局按规格指定：**等相位** ⇒ 第 i 层中心 = span·i/n（bandPlace 的相位等分口径，
// ui/effects.go:2089 的 span·i/n），中心间距 = span/n；
// 区间按 span 折回周期内做重数统计（并集 + 重数 ≥2 的段长求和）。
func DualPctOf(widths []float64, span float64) float64 {
	n := len(widths)
	if n < 2 || span <= 0 {
		return 0
	}
	ivs := spanIntervals(widths, span)
	pts := []float64{0, span}
	for _, iv := range ivs {
		if iv.full {
			continue
		}
		pts = append(pts, iv.start)
		if iv.end <= span {
			pts = append(pts, iv.end)
		} else {
			pts = append(pts, span, 0, iv.end-span)
		}
	}
	sort.Float64s(pts)
	uniq := pts[:1]
	for _, p := range pts[1:] {
		if p > uniq[len(uniq)-1]+1e-9 {
			uniq = append(uniq, p)
		}
	}
	if uniq[0] > 0 {
		uniq = append([]float64{0}, uniq...)
	}
	if uniq[len(uniq)-1] < span {
		uniq = append(uniq, span)
	}
	dual := 0.0
	for k := 0; k+1 < len(uniq); k++ {
		lo, hi := uniq[k], uniq[k+1]
		if hi <= lo {
			continue
		}
		mid := (lo + hi) / 2
		cnt := 0
		for _, iv := range ivs {
			if iv.covers(mid, span) {
				cnt++
			}
		}
		if cnt >= 2 {
			dual += hi - lo
		}
	}
	pct := 100 * dual / span
	if pct > 100 {
		pct = 100
	}
	return pct
}

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
		f.Draw(probeGTX(&o, w, h), w, h)
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
	nominal := DualPctOf(widths, span)
	prof := f.CoverageProfile(w, h, cols)
	cover := 0.0
	if len(prof) > 0 {
		sum := 0.0
		for _, v := range prof {
			sum += v
		}
		cover = 100 * sum / float64(len(prof))
	}
	return OverlapRow{
		Class:          f.cls,
		IsDay:          f.isDay,
		Total:          f.TotalCount(),
		BandCount:      f.bandCount(),
		AvgLayers:      AvgLayersOf(widths, span),
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

// OpsLen 返回 gtx.Ops 的序列化缓冲长度与引用数。
// gioui v0.10.3 的 op.Ops 不导出长度，这里通过导出字段 Ops.Internal
// 反射读内部 data / refs 切片的长度（只读，不改 op.Ops 内容）。
func OpsLen(o *op.Ops) (data, refs int) {
	if o == nil {
		return -1, -1
	}
	v := reflect.ValueOf(&o.Internal).Elem()
	data, refs = -1, -1
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		switch f.Name {
		case "data":
			data = v.Field(i).Len()
		case "refs":
			refs = v.Field(i).Len()
		}
	}
	return data, refs
}

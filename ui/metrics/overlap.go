package metrics

// 迁移（逐字搬移，ui/metrics 卡）——来源与处置：
//	SpriteSizeOr ← ui/overlap_probe.go:84-93（spriteSizeOr）
//	BandWidthsNominal ← ui/overlap_probe.go:136-155（BandWidthsNominal；分母 bandNetDiv 由调用方注入 netDiv）
//	SpanOf ← ui/overlap_probe.go:157-167
//	AvgLayersOf ← ui/overlap_probe.go:169-179
//	layerSpanInterval ← ui/overlap_probe.go:181-185
//	spanIntervals ← ui/overlap_probe.go:187-205
//	covers ← ui/overlap_probe.go:207-216
//	DualPctOf ← ui/overlap_probe.go:218-276
//	MeanCoveragePct ← ui/overlap_probe.go:319-326（OverlapRow 里的覆盖率均值**行内式**，公式逐字提取）
// 算式、常量、分支一字未改。唯一的签名变化 = BandWidthsNominal 多收一个 netDiv
// （ui 侧私有常量 bandNetDiv=1.00 不在本包复制，由调用点注入；注释里的 bandNetDiv 字样按原文保留）。

import (
	"image"
	"math"
	"sort"
)

// SpriteSizeOr 下标取贴图尺寸，越界时取首张（与绘制路径的兜底一致）。
func SpriteSizeOr(sizes []image.Point, spr int) image.Point {
	if len(sizes) == 0 {
		return image.Point{}
	}
	if spr >= 0 && spr < len(sizes) {
		return sizes[spr]
	}
	return sizes[0]
}

// BandWidthsNominal 主对话基线口径的名义云带宽（px）：
// dw = srcW × (1.5 + 0.8·frac) ÷ bandNetDiv，frac = i/(n−1)（n=1 时 frac=0）。
// P6 起真实带几何改等宽（dw = T，逐带相同），本名义式只作 P1 基线算术保留
// （TestProbeBandFormulaMatchesBrief 用它对照"逐带 768–1178 的平均"与"等宽 T"
// 的 avgLayers 相对偏差 ≤2%——两者求和后同为 ~3T 量级，偏差来自精灵长宽比）。
func BandWidthsNominal(srcW float64, n int, netDiv float64) []float64 {
	if n <= 0 {
		return nil
	}
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		frac := 0.0
		if n > 1 {
			frac = float64(i) / float64(n-1)
		}
		out = append(out, srcW*(1.5+0.8*frac)/netDiv)
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

// MeanCoveragePct 覆盖率剖面（CoverageProfile 的逐列占比 0..1）的均值，出数口径 %。
// 迁移：这段原本是 ui/overlap_probe.go 的 OverlapRow 里的**行内式**（319-326 行），
// 逐字提出来放这里，公式与分支一字未改；OverlapRow 改为调用本函数。
func MeanCoveragePct(prof []float64) float64 {
	cover := 0.0
	if len(prof) > 0 {
		sum := 0.0
		for _, v := range prof {
			sum += v
		}
		cover = 100 * sum / float64(len(prof))
	}
	return cover
}

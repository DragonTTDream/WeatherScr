package metrics

// 迁移（逐字搬移，ui/metrics 卡）——来源与处置：
//	Layer ← ui/effects.go:4718-4725（type driftLayer；导出名 Layer，ui 侧留类型别名 driftLayer）
//	WrapPeriod ← ui/effects.go:4727-4737（wrapPeriod）
//	DriftLayersStats ← ui/effects.go:4790-4854（driftLayersStats）
//	DriftDualParticlePairs← ui/effects.go:4856-4948（driftDualParticlePairs；"同道"带宽 airBandN 由调用方注入 bandN）
//	DriftArcOverlap ← ui/effects.go:4982-5000（driftArcOverlap）
// 字段可见性：type 搬到导出包后字段必须导出，字段名 start/width/pi/cy → Start/Width/Pi/Cy
// （只改名，字段类型/顺序/语义/默认值一字未改）。
// 算式、常量、分支一字未改。唯一的签名变化 = DriftDualParticlePairs 多收一个 bandN
// （ui 侧私有常量 airBandN=0.12 不在本包复制，由调用点注入；注释里的 airBandN 字样按原文保留）。

import (
	"math"
	"sort"
)

// Layer 参与漂移重叠扫描的一层 = bandSpan 周期内的横轴区间 [Start, Start+Width)（px）。
// Pi = 粒子在 parts 里的下标；云带记 -1（云带落位由 bandPlace 的等相位恒等式导出、不可挪）。
type Layer struct {
	Start float64
	Width float64
	Pi    int
	Cy    float64 // 中心 y（归一化屏高）：与 coupleClouds 的"同道"判据共用 airBandN 口径
}

// WrapPeriod 把任意横坐标折进 [0, span)。
func WrapPeriod(v, span float64) float64 {
	if span <= 0 {
		return 0
	}
	m := math.Mod(v, span)
	if m < 0 {
		m += span
	}
	return m
}

// DriftLayersStats 由区间序列导出 (Σ宽 ÷ span, 一个周期内被 ≥2 层覆盖的长度占比%)。
// 与探针的 DualPctOf（ui/overlap_probe.go）同一口径，唯一区别 = 区间起点来自真实落位：
// 把每层折进 [0, span)（宽 ≥ span 的层记 full，铺满整周期），
// 绕回段拆成 [Start, span) 与 [0, end−span)，事件按 x 排序后扫重数，
// 段中重数 ≥2 的段长累加（三层段只计一次 = 占比口径，与 DualPctOf 逐字一致）。
func DriftLayersStats(ls []Layer, span float64) (float64, float64) {
	if span <= 0 {
		return 0, 0
	}
	type ev struct {
		x float64
		d int
	}
	evs := make([]ev, 0, 2*len(ls)+2)
	full, sum := 0, 0.0
	dual := 0.0
	for _, l := range ls {
		if l.Width <= 0 {
			continue
		}
		sum += l.Width
		if l.Width >= span {
			full++
			continue
		}
		s := WrapPeriod(l.Start, span)
		e := s + l.Width
		if e <= span {
			evs = append(evs, ev{s, 1}, ev{e, -1})
		} else {
			evs = append(evs, ev{s, 1}, ev{span, -1}, ev{0, 1}, ev{e - span, -1})
		}
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].x < evs[j].x })
	cnt := full
	k := 0
	for k < len(evs) && evs[k].x <= 0 {
		cnt += evs[k].d
		k++
	}
	prev := 0.0
	for k < len(evs) {
		x := evs[k].x
		if x > span {
			x = span
		}
		if x > prev && cnt >= 2 {
			dual += x - prev
		}
		for k < len(evs) && evs[k].x <= x+1e-9 {
			cnt += evs[k].d
			k++
		}
		prev = x
	}
	if prev < span && cnt >= 2 {
		dual += span - prev
	}
	pct := 100 * dual / span
	if pct > 100 {
		pct = 100
	}
	return sum / span, pct
}

// DriftDualParticlePairs P6 设计第 6 条的"含粒子"双层算式（dualIncl）：
// **同带**粒子云/雾团对（Pi ≥ 0，|Δcy| ≤ airBandN，与 driftWorstPair 的"同道"同一判据）
// 在一个 span 周期内的重叠并集长度占比%。
// 口径说明（数值推导，不是调参；同一口径写进）：
// P6 设计下带 = 97.4% 覆盖的单层"幕"（带间间隙 span/N − T = 26.7 px）。若把带∩云
// 计入"双层占比"，等于把"粒子云压在云带上"这一**视觉设计本身**记成拥挤：
// Σ粒子宽 ≥ 2×768 = 1536 px ⇒ dual 下界 (3000+1536−3080)/3080 = 48.6%（Overcast），
// "含粒子云/雾团 ≤10%"在全层口径下几何不可能 ⇒ 判据线只在"粒子对"口径（=
// driftWorstPair 处置用的同一口径）下自洽。layers（Σ宽÷span）口径不变，仍含全部层。
func DriftDualParticlePairs(ls []Layer, span, bandN float64) float64 {
	if span <= 0 {
		return 0
	}
	type iv struct{ lo, hi float64 }
	var ivs []iv
	add := func(lo, hi float64) {
		if hi <= lo {
			return
		}
		if hi-lo >= span {
			ivs = append(ivs, iv{0, span})
			return
		}
		m0, m1 := WrapPeriod(lo, span), WrapPeriod(hi, span)
		if m1 > m0 {
			ivs = append(ivs, iv{m0, m1})
		} else {
			ivs = append(ivs, iv{m0, span}, iv{0, m1})
		}
	}
	for i := 0; i < len(ls); i++ {
		if ls[i].Pi < 0 {
			continue
		}
		for j := i + 1; j < len(ls); j++ {
			if ls[j].Pi < 0 {
				continue
			}
			if math.Abs(ls[i].Cy-ls[j].Cy) > bandN {
				continue
			}
			a, b := ls[i], ls[j]
			if a.Width >= span && b.Width >= span {
				ivs = append(ivs, iv{0, span})
				continue
			}
			if a.Width >= span {
				add(b.Start, b.Start+b.Width)
				continue
			}
			if b.Width >= span {
				add(a.Start, a.Start+a.Width)
				continue
			}
			for _, s := range []float64{0, -span, span} {
				lo := math.Max(a.Start, b.Start+s)
				hi := math.Min(a.Start+a.Width, b.Start+s+b.Width)
				if hi > lo {
					add(lo, hi)
				}
			}
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].lo < ivs[j].lo })
	total, have := 0.0, false
	curLo, curHi := 0.0, 0.0
	for _, v := range ivs {
		if v.hi <= v.lo {
			continue
		}
		if !have {
			curLo, curHi, have = v.lo, v.hi, true
			continue
		}
		if v.lo <= curHi {
			if v.hi > curHi {
				curHi = v.hi
			}
			continue
		}
		total += curHi - curLo
		curLo, curHi = v.lo, v.hi
	}
	if have {
		total += curHi - curLo
	}
	pct := 100 * total / span
	if pct > 100 {
		pct = 100
	}
	return pct
}

// DriftArcOverlap 两段在一个周期内的最大重叠长度（px）：把 b 允许平移整周期取最坏值。
func DriftArcOverlap(a, b Layer, span float64) float64 {
	if a.Width >= span || b.Width >= span {
		lo := math.Min(a.Width, b.Width)
		if lo > span {
			lo = span
		}
		return lo
	}
	best := 0.0
	for _, s := range []float64{b.Start, b.Start - span, b.Start + span} {
		lo := math.Max(a.Start, s)
		hi := math.Min(a.Start+a.Width, s+b.Width)
		if hi > lo && hi-lo > best {
			best = hi - lo
		}
	}
	return best
}

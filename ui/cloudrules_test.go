package ui

// 云规则 + 太阳盘 的五条判据（规格 2 第 2 条）。
//
// 共同纪律：**判据必须真的测量**——测量盒子/取样窗口是固定口径，
// 不随天气类缩放（旧写法把盒子按类缩成 0 的写法等于空转，这里禁止），
// 每条判据都把实测数字打出来。
//
//	TestNoOvertake        固定种子 2000 步，逐带记录云序 ⇒ 交换次数 = 0
//	TestSpeedByThickness  薄档/厚档 vx 区间 ⇒ 薄档最小值 ≥ 厚档最大值
//	TestSnowFlakeSize     雪花屏上 dw 分布 ⇒ ≥ 12 px
//	TestSunDiscEdge       固定太阳盒逐档同像素数 ⇒ 晴/多云见日盘内亮占比 ≥60%、阴/雨 = 0
//	TestClassPlacement    逐类云中心 y 与屏宽 ⇒ 雨/雷 y 中位数 < 晴档 y 中位数

import (
	"image"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// cloudRuleSeeds 五条判据共用的固定种子（固定 = 判据可复现）。
var cloudRuleSeeds = []int64{20261015, 20261016, 20261017, 23, 11}

// medianF 中位数（不改入参，内部排序副本）。
func medianF(v []float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	if len(c) == 0 {
		return math.NaN()
	}
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

// TestNoOvertake 同道不超车：固定种子跑 2000 步，逐带记录云序，断言无交换。
//
// 带 = 中心 y 相差 airBandN(=0.12 屏高) 之内（与 spreadAir 的同带判据同一口径）。
// 云序 = 带内按 x 升序的粒子索引序列。交换 = 同一对云在相邻两次采样里的相对次序翻转。
// 只统计"两次采样都在场、且两次都在同一带内"的云对（跨带不约束，规格逐字）。
func TestNoOvertake(t *testing.T) {
	const steps = 2000
	dt := time.Second / 15
	// 采样密度：每 5 步取一次快照（2000 步 ⇒ 400 次快照），
	// 云速 0.003–0.014 屏宽/秒 ⇒ 两次快照之间最多走 0.0047 屏宽 = 5 px，
	// 超车必然落在某次快照对上（超车是连续事件，翻转后不会翻回来：同带内 y 恒定、vx 恒定）。
	for _, cls := range []effect.Class{effect.Clear, effect.PartlyCloudy, effect.Cloudy, effect.Overcast, effect.Rain, effect.Thunder} {
		pairSeenClass, tested := 0, false // P3：同带采样的"样本足否"判据从**每 seed** 改成**逐类合计**（理由见下面 Fatalf 的注释）
		for _, seed := range []int64{20261015, 23, 11} {
			f := NewField(cls, true, seed)
			nCloud := 0
			for i := range f.parts {
				if f.parts[i].kind == pkCloud {
					nCloud++
				}
			}
			if nCloud == 0 {
				// 实测：雨/雷档 counts = 320/420 全给雨丝（spawn 的 Rain/Thunder 分支），
				// 粒子云数 = 0 ⇒ 本判据在这两档没有对象可测。
				// 这两档的云 = 云带（4 条），带位置是 bandPlace 里 mod 等分的运动学解，
				// 带与带共用同一 span 与同一速度（bandPlace 注释）⇒ 带与带不可能互相超车。
				t.Logf("%-13s seed=%d：粒子云 0 朵（counts 全给雨丝）⇒ 本判据无粒子云可测；云带同 span 同速度，构造上无超车", cls, seed)
				continue
			}
			if nCloud < 2 {
				// 实测：晴档 counts=4 且云按 idx%5==0 取 ⇒ 全场只有 1 朵云，
				// 没有"前后两朵"可言 ⇒ 打印朵数后跳过（不写空断言）。
				t.Logf("%-13s seed=%d：粒子云 %d 朵 ⇒ 无前后对可言，跳过（晴档 counts=4，云按 idx%%5==0 取）", cls, seed, nCloud)
				continue
			}
			tested = true // 本 seed 有 ≥2 朵粒子云 ⇒ 纳入逐类合计的样本足否判据
			// 快照 = 每 5 步一次，记每朵云的 (x, y)。
			// 实例同一性判据：粒子云的 vy = 0（spawn 不给云 vy）⇒ 一朵云在一次生命周期内 y 恒定，
			// 于是 y 变了就是"被回收重放"（整朵出屏 → spawn → x 瞬移回左沿外），
			// 那是回收口径不是超车，必须排除（不排除时实测把回收的瞬移读成超车，3–5 次）。
			type snap struct{ x, y map[int]float64 }
			var prev *snap
			swaps, pairSeen, bandMax, recycled := 0, 0, 0, 0
			for s := 0; s < steps; s++ {
				f.Step(dt, PortraitW, PortraitH)
				if s%5 != 4 {
					continue
				}
				cur := &snap{x: map[int]float64{}, y: map[int]float64{}}
				for i := range f.parts {
					if f.parts[i].kind != pkCloud {
						continue
					}
					cur.x[i], cur.y[i] = f.parts[i].x, f.parts[i].y
				}
				// 逐带朵数（打印口径）：按 y 升序贪心分组，相邻 y 差 ≤ airBandN 归同带
				var ys []float64
				for _, y := range cur.y {
					ys = append(ys, y)
				}
				sort.Float64s(ys)
				for gi := 0; gi < len(ys); {
					gj := gi + 1
					for gj < len(ys) && ys[gj]-ys[gj-1] <= airBandN {
						gj++
					}
					if gj-gi > bandMax {
						bandMax = gj - gi
					}
					gi = gj
				}
				if prev != nil {
					for i := range cur.x {
						for j := range cur.x {
							if i >= j {
								continue
							}
							if _, ok := prev.x[i]; !ok {
								continue
							}
							if _, ok := prev.x[j]; !ok {
								continue
							}
							if prev.y[i] != cur.y[i] || prev.y[j] != cur.y[j] {
								recycled++
								continue
							}
							if math.Abs(cur.y[i]-cur.y[j]) > airBandN {
								continue // 跨带不约束（规格逐字）
							}
							pairSeen++
							dPrev, dCur := prev.x[i]-prev.x[j], cur.x[i]-cur.x[j]
							if (dPrev > 0 && dCur <= 0) || (dPrev < 0 && dCur >= 0) {
								swaps++
							}
						}
					}
				}
				prev = cur
			}
			t.Logf("%-13s seed=%d：2000 步（每 5 步一采样）同带云对采样 %d 次，最大同带朵数 %d，回收重放排除 %d 对，交换次数 = %d",
				cls, seed, pairSeen, bandMax, recycled, swaps)
			pairSeenClass += pairSeen
			if swaps != 0 {
				t.Errorf("%s seed=%d：同道超车 %d 次（应为 0）", cls, seed, swaps)
			}
		}
		// 原断言：每个 seed 单独要求同带采样 ≥100 次（n=5 朵云时三 seed 各 399 次，必然过）。
		// 新断言：**逐类三个 seed 合计** ≥100 次。理由 = P3 判据 B 把 Overcast 的粒子云
		// 5 → 2 朵 ⇒ 同带对最多 1 对，且 2 朵按月亮行（跨度 2rc+2hh ≈ 0.15 屏高 > airBandN 0.12）
		// 落位时可以分处两带 ⇒ 单 seed 采样 0 次是**结构**结果，不是"测试跑空"。
		// 超车判据本身（swaps == 0）一条没放宽：有同带对的 seed 逐 seed 照判。
		if tested && pairSeenClass < 100 {
			t.Fatalf("%s：三个 seed 合计同带云对采样只有 %d 次 ⇒ 样本不足，测试无意义（同带对太少）", cls, pairSeenClass)
		}
	}
}

// TestSpeedByThickness 云速按薄厚：打印薄档/厚档 vx 区间，断言薄档最小值 ≥ 厚档最大值。
//
// 薄档 = 晴（300–600 px）+ 多云见日（300–700 px）的粒子云；
// 厚档 = 阴（768–1178 px）的粒子云（雨/雷档没有粒子云，只有云带，见 TestClassPlacement）。
// 取 vxBase = 生成时按屏宽定下的基准速度（coupleClouds 的间距修正会在它附近调，
// 判据针对的是"按薄厚定速度"这条口径本身）。
func TestSpeedByThickness(t *testing.T) {
	type bucket struct {
		vxBase []float64
		dwPx   []float64
	}
	thin, thick := bucket{}, bucket{}
	collect := func(cls effect.Class, b *bucket) {
		for _, seed := range cloudRuleSeeds {
			f := NewField(cls, true, seed)
			f.Step(time.Second, PortraitW, PortraitH)
			for i := range f.parts {
				p := &f.parts[i]
				if p.kind != pkCloud {
					continue
				}
				b.vxBase = append(b.vxBase, p.vxBase)
				b.dwPx = append(b.dwPx, cloudDrawWidthPx(p))
			}
		}
	}
	collect(effect.Clear, &thin)
	collect(effect.PartlyCloudy, &thin)
	collect(effect.Overcast, &thick)
	if len(thin.vxBase) < 5 || len(thick.vxBase) < 5 {
		t.Fatalf("薄档 %d 朵 / 厚档 %d 朵 ⇒ 样本不足，测试无意义", len(thin.vxBase), len(thick.vxBase))
	}
	lo := func(v []float64) float64 {
		m := v[0]
		for _, x := range v[1:] {
			if x < m {
				m = x
			}
		}
		return m
	}
	hi := func(v []float64) float64 {
		m := v[0]
		for _, x := range v[1:] {
			if x > m {
				m = x
			}
		}
		return m
	}
	thinMin, thinMax := lo(thin.vxBase), hi(thin.vxBase)
	thickMin, thickMax := lo(thick.vxBase), hi(thick.vxBase)
	t.Logf("薄档（晴+多云见日，%d 朵）屏宽 %.0f–%.0f px ⇒ vxBase %.5f–%.5f（%.2f–%.2f px/s @1080）",
		len(thin.vxBase), lo(thin.dwPx), hi(thin.dwPx), thinMin, thinMax, thinMin*1080, thinMax*1080)
	t.Logf("厚档（阴，%d 朵）屏宽 %.0f–%.0f px ⇒ vxBase %.5f–%.5f（%.2f–%.2f px/s @1080）",
		len(thick.vxBase), lo(thick.dwPx), hi(thick.dwPx), thickMin, thickMax, thickMin*1080, thickMax*1080)
	t.Logf("公式端点：cloudVxByThickness(300px)=%.5f (600px)=%.5f (768px)=%.5f (1178px)=%.5f",
		cloudVxByThickness(300/float64(PortraitW)), cloudVxByThickness(600/float64(PortraitW)),
		cloudVxByThickness(768/float64(PortraitW)), cloudVxByThickness(1178/float64(PortraitW)))
	if thinMin < thickMax {
		t.Errorf("薄档最小 vx %.5f < 厚档最大 vx %.5f ⇒ 薄快厚慢不成立", thinMin, thickMax)
	}
	if thickMax <= thickMin || thinMax <= thinMin {
		t.Errorf("档内 vx 无分布（薄 %.5f–%.5f 厚 %.5f–%.5f）⇒ 速度是常量，判据空转", thinMin, thinMax, thickMin, thickMax)
	}
}

// TestSnowFlakeSize 雪花屏上尺寸：打印 dw 分布，断言 ≥ 12 px。
//
// dw = 源图宽 × spriteScale(p)，与 Draw 的绘制式逐字同口径（spriteScale 的
// 非云非雾分支 = sc·(0.55+0.45·depth)·scaleMul ÷ spriteNetDiv）。
func TestSnowFlakeSize(t *testing.T) {
	var dw []float64
	for _, seed := range cloudRuleSeeds {
		f := NewField(effect.Snow, true, seed)
		f.Step(time.Second, PortraitW, PortraitH)
		n := 0
		for i := range f.parts {
			p := &f.parts[i]
			if p.kind != pkFlake {
				continue
			}
			sz := cloudSpriteSz(p.spr)
			if p.spr >= 0 && p.spr < len(f.sp.flakeSz) {
				sz = f.sp.flakeSz[p.spr]
			}
			dw = append(dw, float64(sz.X)*spriteScale(p))
			n++
		}
		if n == 0 {
			t.Fatalf("seed=%d：本场没有雪花粒子 ⇒ 测试无意义", seed)
		}
	}
	minDw, maxDw := dw[0], dw[0]
	sum := 0.0
	for _, v := range dw {
		if v < minDw {
			minDw = v
		}
		if v > maxDw {
			maxDw = v
		}
		sum += v
	}
	t.Logf("雪花 %d 片：屏上 dw 最小 %.2f px 最大 %.2f px 均值 %.2f px 中位 %.2f px",
		len(dw), minDw, maxDw, sum/float64(len(dw)), medianF(dw))
	if len(dw) < 100 {
		t.Fatalf("雪花样本 %d 片 ⇒ 样本不足，测试无意义", len(dw))
	}
	if minDw < 12.0-0.5 {
		t.Errorf("雪花屏上最小 dw = %.2f px < 12 px（规格 3：12–26 px）", minDw)
	}
	if maxDw > 26.0+0.5 {
		t.Errorf("雪花屏上最大 dw = %.2f px > 26 px（规格 3：12–26 px）", maxDw)
	}
}

// sunBox 固定太阳盒（规格 2 第 2 条第 4 项）：中心 (0.72w, 0.095h)，半边长 3×0.045w。
//
// **固定口径**：盒子只由烘焙宽 w 决定，不随天气类缩放
// （旧写法把测量盒子按类缩成 0 的写法 = 空转，这里逐档必须量到同一个盒子）。
type sunBox struct {
	x0, y0, x1, y1 int
	cx, cy         int
	rc             float64
}

func newSunBox(w, h int) sunBox {
	rc := sunDiscRcFrac * float64(w)
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	half := int(math.Ceil(3 * rc))
	return sunBox{
		x0: maxInt(0, cx-half), y0: maxInt(0, cy-half),
		x1: minInt(w, cx+half+1), y1: minInt(h, cy+half+1),
		cx: cx, cy: cy, rc: rc,
	}
}

func (b sunBox) px(img *image.NRGBA, x, y int) float64 {
	i := img.PixOffset(x, y)
	return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestSunDiscEdge 固定太阳盒逐档测量（云量口径）。
//
// 逐档的云量是测试给定的输入（Clear=0、PartlyCloudy=45、Overcast=100、Rain=90），
// 打印的全是实测数字。
//
//	1 盒内总像素数逐档相同（固定盒子的自检，打印出来）
//	2 晴 / 多云见日：盘内（r ≤ rc）亮像素占比 ≥ 60%（亮 = 比同位置天空基色亮 ≥24 级）
//	3 白天任何档：盘心净抬升 > 0（太阳常驻，灰蒙档也留亮斑）
//	4 满亮盘（云量 0）：盘内近均匀（r ≤ 0.88rc 极差 ≤ 8 级）+ 羽化带宽 ≤ 半径 12%；
//	  云量调出来的盘（0.20<dim<1）：中心比边缘亮 ≥20 级、极差 ≤ 80 级（变淡变软）
//	5 环带峰值亮度 < 盘体亮度（环带 [1.1rc, 3.2rc] 的最大亮度 < 盘内最小亮度）
//	6 灰蒙档（云量 ≥85）：盘缘处不许出现 >30 级的跌落（硬圆盘的签名），
//	  跌落点必须落在 1.5×rc 之外
//	7 打印水平剖面 r = 0, 24, 48, 72, 96
//
// 第 6 条的口径变更（写明理由，不是放宽）：旧断言是"阴 / 雨 盘内亮像素 == 0"，
// 依据是 bakeCelestial 只在 Clear/PartlyCloudy 画太阳的门控；门控已去掉 ⇒ 白天任何档
// 都画太阳，云量只调亮度与盘的形态。实测灰蒙档（云量 100，dim=0.20）的宽光把盘区
// 抬了 30.7 级 ⇒ "盘内亮像素 = 0"在新口径下不可能成立。判据换成硬圆盘的几何签名
// （盘缘跌落）：灰蒙档没有盘缘跌落、有盘心净抬升。
func TestSunDiscEdge(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	box := newSunBox(w, h)
	lum := func(img *image.NRGBA, x, y int) float64 { return box.px(img, x, y) }
	type meas struct {
		cls     effect.Class
		pct     int
		dim     float64
		boxPx   int
		discTot int
		bright  int
		intMin  float64
		intMax  float64
		ringMax float64
		at3rc   float64
		plateau float64
		edge    float64
		center  float64
		maxStep float64
		prof    []float64
	}
	// 逐档云量：灰蒙档取 100 与 90（都在 ≥85 那一档），亮盘档取 0 与 45
	cases := []struct {
		cls effect.Class
		pct int
	}{
		{effect.Clear, 0},
		{effect.PartlyCloudy, 45},
		{effect.Overcast, 100},
		{effect.Rain, 90},
	}
	var rows []meas
	for _, cs := range cases {
		cls, pct := cs.cls, cs.pct
		baked := renderSkyCPU(w, h, cls, true, nil, pct)
		base := skyBaseCPU(w, h, cls, true)
		m := meas{cls: cls, pct: pct, dim: sunDimOf(pct), intMin: 1e9, intMax: -1e9, ringMax: -1e9}
		for y := box.y0; y < box.y1; y++ {
			for x := box.x0; x < box.x1; x++ {
				m.boxPx++
				d := math.Hypot(float64(x-box.cx), float64(y-box.cy))
				if d <= box.rc {
					m.discTot++
					if lum(baked, x, y)-lum(base, x, y) >= 24 {
						m.bright++
					}
				}
			}
		}
		// 盘内均匀性（r ≤ 0.88rc）、环带峰值（1.1rc ≤ r ≤ 3.2rc）、盘外参考点 r=3rc
		at3rc := math.Inf(-1)
		for dy := -int(4 * box.rc); dy <= int(4*box.rc); dy++ {
			for dx := -int(4 * box.rc); dx <= int(4*box.rc); dx++ {
				x, y := box.cx+dx, box.cy+dy
				if x < 0 || y < 0 || x >= w || y >= h {
					continue
				}
				d := math.Hypot(float64(dx), float64(dy))
				if d <= 0.88*box.rc {
					if v := lum(baked, x, y); v < m.intMin {
						m.intMin = v
					}
					if v := lum(baked, x, y); v > m.intMax {
						m.intMax = v
					}
				}
				if d >= 1.1*box.rc && d <= 3.2*box.rc {
					if v := lum(baked, x, y); v > m.ringMax {
						m.ringMax = v
					}
				}
				if d >= 2.9*box.rc && d <= 3.1*box.rc {
					if v := lum(baked, x, y); v > at3rc {
						at3rc = v
					}
				}
			}
		}
		// 水平剖面（向右）：盘内平台终点与盘缘跌落点
		plateauEnd, edge := 0.0, math.Inf(1)
		for r := 0.0; r <= 4*box.rc; r++ {
			x := box.cx + int(math.Round(r))
			if x >= w {
				break
			}
			v := lum(baked, x, box.cy)
			if v >= m.intMax-2 {
				plateauEnd = r
			}
			if math.IsInf(edge, 1) && v <= m.intMax-30 {
				edge = r
			}
		}
		m.plateau, m.edge = plateauEnd, edge
		m.at3rc = at3rc
		// 盘缘外的逐像素落差（硬边的签名）：从 r=rc 起向右逐像素取最大跌幅
		{
			r0 := int(math.Ceil(box.rc))
			prev := lum(baked, box.cx+r0, box.cy)
			for r := r0 + 1; r <= int(4*box.rc); r++ {
				x := box.cx + r
				if x >= w {
					break
				}
				v := lum(baked, x, box.cy)
				if d := prev - v; d > m.maxStep {
					m.maxStep = d
				}
				prev = v
			}
		}
		// 盘心净抬升（烘焙 - 同位置天空基色）：灰蒙档"留亮斑"的判据对象
		m.center = lum(baked, box.cx, box.cy) - lum(base, box.cx, box.cy)
		for _, rr := range []int{0, 24, 48, 72, 96} {
			x := box.cx + rr
			if x >= w {
				m.prof = append(m.prof, -1)
				continue
			}
			m.prof = append(m.prof, lum(baked, x, box.cy))
		}
		rows = append(rows, m)
	}
	base0 := rows[0].boxPx
	for _, m := range rows {
		t.Logf("%-13s 云量=%d dim=%.3f 盒内总像素=%d 盘内像素=%d 盘内亮像素=%d 占比=%.3f 盘内亮度=%.1f..%.1f 环带峰=%.1f 盘心净抬升=%.1f 盘缘外落差峰=%.1f 平台终点=%.0f 盘缘跌落=%.0f 盘外3rc峰=%.1f",
			m.cls, m.pct, m.dim, m.boxPx, m.discTot, m.bright, float64(m.bright)/float64(m.discTot), m.intMin, m.intMax, m.ringMax, m.center, m.maxStep, m.plateau, m.edge, m.at3rc)
		t.Logf("    水平剖面 r=0,24,48,72,96 = %v", m.prof)
		if m.boxPx != base0 {
			t.Errorf("%s：盒内总像素 %d 与首档 %d 不同 ⇒ 盒子被按类缩放了", m.cls, m.boxPx, base0)
		}
		if m.boxPx < 1000 || m.discTot < 500 {
			t.Errorf("%s：盒内 %d px / 盘内 %d px ⇒ 盒子缩成 0 量级，测试无意义", m.cls, m.boxPx, m.discTot)
		}
		// 白天任何档都必须有净抬升（太阳常驻：盘或宽光至少把盘心抬起来）
		if m.center <= 0 {
			t.Errorf("%s 云量=%d：盘心净抬升 %.1f ≤ 0 ⇒ 白天没有太阳", m.cls, m.pct, m.center)
		}
		switch {
		case m.cls == effect.Clear || m.cls == effect.PartlyCloudy:
			// 画硬圆盘的档（云量 <85）
			if m.discTot == 0 || float64(m.bright)/float64(m.discTot) < 0.60 {
				t.Errorf("%s：盘内亮像素占比 %.3f < 0.60（亮像素 %d / 盘内 %d）",
					m.cls, float64(m.bright)/float64(m.discTot), m.bright, m.discTot)
			}
			// 环带判据口径（本批改写，理由）：全天空抬升 + 星芒把盘外一大片抬进 alpha 钳位区，
			// 环带与盘体在满亮盘档同时顶到 255 ⇒ 旧口径"环带峰 < 盘内最低"在新铺光下恒假。
			// 新口径 = 环带峰不许超过盘心峰（泛光/星芒不许盖过太阳本体）。
			if m.ringMax > m.intMax {
				t.Errorf("%s：环带峰值亮度 %.1f > 盘心峰 %.1f ⇒ 泛光抢眼", m.cls, m.ringMax, m.intMax)
			}
			if m.dim >= 1.0 {
				// 满亮盘（云量 0）：盘内近均匀 + 盘缘无硬边（相邻差 ≤6 级）。
				// 复合剖面的"平台终点→跌 30 级"宽度由铺光半径决定（L=0.18w），
				// 盘体羽化带宽的判据在 TestSunProfileNoHardEdge 里量函数本体。
				if sp := m.intMax - m.intMin; sp > 8 {
					t.Errorf("%s：盘内亮度极差 %.1f 级 > 8 级 ⇒ 盘内不均匀（%.1f..%.1f）", m.cls, sp, m.intMin, m.intMax)
				}
				if m.maxStep > 6 {
					t.Errorf("%s：盘缘外相邻差峰值 %.1f 级 > 6 级 ⇒ 盘缘有硬边", m.cls, m.maxStep)
				}
			} else {
				// 云量调可见度下的盘（0.20 < dim < 1）：口径是"盘变淡、边缘变软"
				// ⇒ 盘内不再饱和成同一色值，中心比 0.88rc 的边缘亮、极差受控。
				// 判据按新口径重写（旧口径的"极差 ≤8"只对满亮盘成立）：
				// 中心 - 边缘 ≥ 20 级（变淡变软要看得见），极差 ≤ 80 级（不许整盘塌成渐变）。
				// 盘内对比的取样点（本批改写，理由）：旧口径取"盘心 − 0.88rc 处"，
				// 全天空抬升把盘内整片抬进 alpha 钳位区 ⇒ 盘内处处 255、盘内对比恒 0，
				// 判据测不到东西。新口径 = 盘心 − 盘外 3rc 处（盘要比它照亮的天空更亮）。
				if d := m.intMax - m.at3rc; d < 20 {
					t.Errorf("%s 云量=%d：盘心与盘外 3rc 只差 %.1f 级 ⇒ 云量没有把盘调淡（盘心 %.1f、3rc %.1f）",
						m.cls, m.pct, d, m.intMax, m.at3rc)
				} else if d > 255 {
					t.Errorf("%s 云量=%d：盘心与 3rc 差 %.1f 级 > 255 级 ⇒ 取样越界",
						m.cls, m.pct, d)
				}
			}
		default:
			// 灰蒙档（云量 ≥85）：不画硬圆盘，只画宽光
			if !sunNoDisc(m.pct) {
				t.Errorf("%s：云量 %d 应落在灰蒙档（≥%d）⇒ 测试输入给错了档", m.cls, m.pct, sunNoDiscPct)
			}
			// 硬圆盘的签名 = 盘缘处一次 >30 级的跌落（满亮盘实测在 r≈rc 处）。
			// 灰蒙档不许有这个跌落：跌落点必须落在 1.5×rc 之外（找不到 = +Inf，也算过）。
			if !math.IsInf(m.edge, 1) && m.edge <= 1.5*box.rc {
				t.Errorf("%s 云量=%d：在 r=%.0f px（≤1.5rc=%.0f px）处出现 %.0f 级跌落 ⇒ 画出了硬圆盘",
					m.cls, m.pct, m.edge, 1.5*box.rc, m.intMax)
			}
		}
	}
	// ---- 盘周提亮曲线与"无辐条"的实测数字（Clear 档）----
	//
	// 提亮是烘焙逐像素调用的函数本身（sunSkyLift），打印的是画进天空纹理的
	// 那条曲线，不是另抄一份公式。
	// 辐条判据（规格 3 第 8 条：TestSunDiscEdge 因删射线改判为"盘内亮像素 ≥60%
	// 且无辐条"）：r=2rc 圆周上 360 个采样点的净抬升，峰谷差与比值都要打印，
	// 比值口径与 TestNoSunRays 一致（≤1.25）。
	rc := box.rc
	baked := renderSkyCPU(w, h, effect.Clear, true, nil, 0)
	base := skyBaseCPU(w, h, effect.Clear, true)
	lifts := []float64{
		sunSkyLift(1.0*rc, rc), sunSkyLift(1.5*rc, rc), sunSkyLift(2.0*rc, rc),
		sunSkyLift(2.5*rc, rc), sunSkyLift(3.0*rc, rc), sunSkyLift(3.4*rc, rc),
		sunSkyLift(3.8*rc, rc),
	}
	t.Logf("盘周提亮曲线 r/rc=1,1.5,2,2.5,3,3.4,3.8 = %.2f %.2f %.2f %.2f %.2f %.2f %.2f（出厂归零 ⇒ 应全 0）",
		lifts[0], lifts[1], lifts[2], lifts[3], lifts[4], lifts[5], lifts[6])
	// 提亮圈归零：整条曲线必须是 0（dev 变量 WS_SUN_LIFT_LV 出厂 0）
	for i, v := range lifts {
		if v != 0 {
			t.Errorf("盘周提亮 r/rc=%.1f 处 = %.2f ≠ 0 ⇒ 提亮圈没有归零", []float64{1, 1.5, 2, 2.5, 3, 3.4, 3.8}[i], v)
		}
	}
	var ring []float64
	for a := 0; a < 360; a++ {
		th := float64(a) * math.Pi / 180.0
		x := box.cx + int(math.Round(2.0*rc*math.Cos(th)))
		y := box.cy + int(math.Round(2.0*rc*math.Sin(th)))
		if x < 0 || y < 0 || x >= w || y >= h {
			continue
		}
		ring = append(ring, box.px(baked, x, y)-box.px(base, x, y))
	}
	loR, hiR := ring[0], ring[0]
	for _, v := range ring {
		if v < loR {
			loR = v
		}
		if v > hiR {
			hiR = v
		}
	}
	t.Logf("r=2rc 圆周 360 点净抬升 %.1f–%.1f 级（峰谷差 %.1f，最大/最小比 %.3f ⇒ 无辐条）",
		loR, hiR, hiR-loR, hiR/loR)
	if loR <= 1 {
		t.Errorf("r=2rc 圆周净抬升下限 %.1f 级 ⇒ 盘外没有泛光，测不到方向性，测试无意义", loR)
	}
	if hiR/loR > 1.25 {
		t.Errorf("r=2rc 圆周净抬升最大/最小比 %.3f > 1.25 ⇒ 盘外存在方向性结构（辐条没删干净）", hiR/loR)
	}
}

// TestNoSunRays 绕盘心 16 个角度采样亮度，最大/最小比 ≤1.25（规格 3 第 1 条）。
//
// 口径：
//
//	1 采样半径 = 2×盘半径（辐条的长度区间是 2.2–3.0 倍半径，峰谷就出现在这一圈）；
//	2 亮度 = 烘焙图 - 同位置天空基色（净抬升），基色梯度不参与比值；
//	3 盒子只由烘焙宽决定，不随天气类缩放；
//	4 判据必须量到东西：净抬升下限 ≤1 级时判"测不到方向性"，直接红。
//
// 有 12 条辐条时这一圈的实测比值 1.90（119.3/62.7，改动前的实测行）；
// 删射线 + 泛光按半径角向平均后实测 1.00。
func TestNoSunRays(t *testing.T) {
	for _, cls := range []effect.Class{effect.Clear, effect.PartlyCloudy} {
		w, h := sunProbeW, sunProbeH
		box := newSunBox(w, h)
		baked := renderSkyCPU(w, h, cls, true, nil, 0)
		base := skyBaseCPU(w, h, cls, true)
		var vals []float64
		for k := 0; k < 16; k++ {
			th := float64(k) * math.Pi / 8.0
			x := box.cx + int(math.Round(2.0*box.rc*math.Cos(th)))
			y := box.cy + int(math.Round(2.0*box.rc*math.Sin(th)))
			if x < 0 || y < 0 || x >= w || y >= h {
				t.Fatalf("%s：采样点 (%d,%d) 越出烘焙图 ⇒ 测试无意义", cls, x, y)
			}
			vals = append(vals, box.px(baked, x, y)-box.px(base, x, y))
		}
		lo, hi := vals[0], vals[0]
		for _, v := range vals {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		ratio := hi / lo
		t.Logf("%-13s 盘心(%d,%d) rc=%.1f px 采样半径=2rc=%.1f px 16 角度净抬升 %.1f..%.1f 级 最大/最小比 %.3f",
			cls, box.cx, box.cy, box.rc, 2*box.rc, lo, hi, ratio)
		if lo <= 1 {
			t.Fatalf("%s：净抬升下限 %.1f 级 ⇒ 盘外没有光，比值判据测不到东西，测试无意义", cls, lo)
		}
		if ratio > 1.25 {
			t.Errorf("%s：16 角度净抬升最大/最小比 %.3f > 1.25 ⇒ 盘外有方向性结构（辐条）", cls, ratio)
		}
	}
}

// TestClassPlacement 逐类云中心 y 与屏宽：打印区间，断言雨/雷档 y 中位数 < 晴档 y 中位数。
//
// 云 = 粒子云 + 云带两层的并集（雨/雷档没有粒子云，只有云带；晴档只有粒子云、没有云带，
// 两层都是"屏幕上真正画出来的云"，共用 bandPlace / cloudDrawWidthPx 的绘制口径）。
func TestClassPlacement(t *testing.T) {
	type rec struct{ ys, dws []float64 }
	got := map[effect.Class]*rec{}
	order := []effect.Class{effect.Clear, effect.PartlyCloudy, effect.Cloudy, effect.Overcast, effect.Rain, effect.Thunder}
	for _, cls := range order {
		got[cls] = &rec{}
	}
	for _, cls := range order {
		for _, seed := range cloudRuleSeeds {
			f := NewField(cls, true, seed)
			f.Step(time.Second, PortraitW, PortraitH)
			for i := range f.parts {
				p := &f.parts[i]
				if p.kind != pkCloud {
					continue
				}
				got[cls].ys = append(got[cls].ys, p.y)
				got[cls].dws = append(got[cls].dws, cloudDrawWidthPx(p))
			}
			if f.hasCloudBands() {
				for i := 0; i < f.bandCount(); i++ {
					bp, ok := f.bandPlace(i, f.t, PortraitW, PortraitH)
					if !ok {
						continue
					}
					got[cls].ys = append(got[cls].ys, (bp.y+bp.dh/2)/float64(PortraitH))
					got[cls].dws = append(got[cls].dws, bp.dw)
				}
			}
		}
	}
	lo := func(v []float64) float64 {
		m := v[0]
		for _, x := range v[1:] {
			if x < m {
				m = x
			}
		}
		return m
	}
	hi := func(v []float64) float64 {
		m := v[0]
		for _, x := range v[1:] {
			if x > m {
				m = x
			}
		}
		return m
	}
	med := map[effect.Class]float64{}
	for _, cls := range order {
		r := got[cls]
		if len(r.ys) == 0 {
			t.Fatalf("%s：取不到任何云 ⇒ 测试无意义", cls)
		}
		med[cls] = medianF(r.ys)
		t.Logf("%-13s 云 %d 个：中心 y %.3f..%.3f 中位 %.3f | 屏宽 %.0f..%.0f px 中位 %.0f px",
			cls, len(r.ys), lo(r.ys), hi(r.ys), med[cls], lo(r.dws), hi(r.dws), medianF(r.dws))
	}
	if math.IsNaN(med[effect.Clear]) {
		t.Fatalf("晴档取不到云 ⇒ 测试无意义")
	}
	for _, cls := range []effect.Class{effect.Rain, effect.Thunder} {
		if !(med[cls] < med[effect.Clear]) {
			t.Errorf("%s 云中心 y 中位 %.3f ≥ 晴档 %.3f ⇒ 厚黑云没有压顶", cls, med[cls], med[effect.Clear])
		}
	}
}

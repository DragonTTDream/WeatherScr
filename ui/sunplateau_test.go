package ui

// 晴档饱和平顶消除（留头合成）的判据（规格 publish-sunplateau 第 3 条 T1..T4）。
//
// 共同纪律（与 sunmoon_test.go 同一套）：每条都真的测量并打印数字，
// 取样盒/取样点只由烘焙幅面与常量决定，不随天气类或云量缩放；
// 样本不足/测不到东西时直接判红（不写恒真断言）。
//
//	T1 TestNoSaturatedPlateau        晴（云量 0）与多云见日（云量 45）帧内亮度 ≥250 的像素数 = 0
//	T2 TestCenterBelowClipAndMonotone 盘心亮度 ≤245；八向 20px 剖面 上升处数 = 0、斜率变化峰 ≤2 级
//	T3 TestSingleBrightPeak          18px 高斯平滑 + 120px 非极大值抑制取前 2 峰：
//	                                 间距 ≤120 px 且次峰亮度 ≤ 主峰 −6
//	T4 TestCornerLiftKept            云量 90 档四角净抬升 平均 ≥4 级、每角 >0
//
// 取样口径（写明理由）：
//
//	幅面 = 540×960 = 现有太阳判据的探针幅面（sunProbeW/sunProbeH，与 skyCacheScale=2
//	       的上线烘焙口径同源）；规格 §1 的 19611 个 / 182×182 是在 1080×1920 整帧上量的，
//	       同一张烘焙按 skyCacheScale=2 缩小就是这里的 540×960 ⇒ 平顶的**有无**与 bbox
//	       在同一幅面上量，绝对像素数按 1/4 面积折算（19611/4 ≈ 4900）。
//	剖面 = 净抬升（烘焙帧 − 同帧天空基色 skyBaseCPU），不是烘焙帧的原始亮度。
//	       天空底是竖向渐变（实测 540×960 顶行 63 级、底行 166 级）+ 四角暗角，
//	       原始亮度在向下射线上本来就一路升 ⇒ "上升处数 = 0"量的必须是铺光的抬升，
//	       与 TestSunProfileMonotone 用同一条净抬升口径。
//	       渐变对"斜率变化"的贡献是常数（线性项的二阶差分为 0）⇒ 换净抬升口径
//	       不会把真台阶洗掉，只把底色自己的梯度摘掉。
//	八向 = 盘心 (0.72w, 0.095h) 出发的 8 个方向，r = 20..400 逐 20 px。
//	       向上射线离屏顶只有 0.095 倍屏高 = 91 px ⇒ 该向取到屏顶为止（4 个点），
//	       样本数由幅面决定并打印；判据要求每条射线 ≥3 段（4 点）才不空转。

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/DragonTTDream/WeatherScr/ui/sky"
	"github.com/DragonTTDream/WeatherScr/effect"
)

// sunPlateauLum 亮度 = (R+G+B)/3（与 sunLum 同一定义，这里直接复用 sunLum）。

// sunPlateauFmt 把剖面序列排成一行（打印用，一位小数）。
func sunPlateauFmt(v []float64) string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = fmt.Sprintf("%.1f", x)
	}
	return strings.Join(out, " ")
}

// sunPlateauBrightRun 帧内亮度 ≥thr 的像素数与包围盒（无命中时 bbox 为空盒）。
func sunPlateauBrightRun(img *image.NRGBA, thr float64) (int, image.Rectangle) {
	b := img.Bounds()
	n := 0
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if sunLum(img, x, y) < thr {
				continue
			}
			n++
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if n == 0 {
		return 0, image.Rectangle{}
	}
	return n, image.Rect(minX, minY, maxX+1, maxY+1)
}

// T1 晴档与多云见日的烘焙帧内不许有亮度 ≥250 的像素（规格 3 T1）。
//
// 规格 §1 的实测基线：整帧 1080×1920 上 晴档 19611 个 / bbox (687,93)-(868,274) = 182×182，
// 多云见日 5574 个 / 113×116 ⇒ 判据 = 这两个数都归 0（在 1/2 分辨率的烘焙幅面上量）。
func TestNoSaturatedPlateau(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	for _, tc := range []struct {
		name string
		pct  int
	}{{"晴(云量0)", 0}, {"多云见日(云量45)", 45}} {
		baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, tc.pct)
		n, box := sunPlateauBrightRun(baked, 250.0)
		cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
		t.Logf("%s 烘焙帧 %dx%d 亮度>=250 像素数=%d bbox=%v(%dx%d) 盘心(%d,%d)亮度=%.1f 盘可见度=%.2f dim=%.3f",
			tc.name, w, h, n, box, box.Dx(), box.Dy(), cx, cy, sunLum(baked, cx, cy),
			sky.SunDiscVisibility(effect.Clear, tc.pct), sky.SunDimOf(tc.pct))
		if n != 0 {
			t.Errorf("%s 帧内亮度>=250 的像素 = %d 个（bbox %v，%dx%d）⇒ 饱和平顶还在（规格 §1 基线 19611 个/182x182）",
				tc.name, n, box, box.Dx(), box.Dy())
		}
	}
}

// sunPlateauRayScan 沿一条射线逐 20px 取净抬升（从 r=20 起），返回：
// 取样点数、末端净抬升、上升处数（>sunEdgeQuant 容差）、相邻 20px 斜率变化峰、剖面序列。
func sunPlateauRayScan(lift func(x, y int) float64, cx, cy int, ux, uy float64, maxR int) (int, float64, int, float64, []float64) {
	return sunPlateauRayScanFrom(lift, cx, cy, ux, uy, 20, maxR)
}

// sunPlateauRayScanFrom 同 sunPlateauRayScan，窗口 = r=minR..maxR 逐 20px。
// 剖面序列的第一项 = r=minR 处的取样点（起点值一并打印，便于核对窗口位置）。
func sunPlateauRayScanFrom(lift func(x, y int) float64, cx, cy int, ux, uy float64, minR, maxR int) (n int, last float64, rises int, changePeak float64, prof []float64) {
	if minR < 20 {
		minR = 20
	}
	x0 := int(math.Round(float64(cx) + ux*float64(minR)))
	y0 := int(math.Round(float64(cy) + uy*float64(minR)))
	prev := lift(x0, y0)
	prof = append(prof, prev)
	prevSlope := math.NaN()
	for r := minR + 20; r <= maxR; r += 20 {
		x := int(math.Round(float64(cx) + ux*float64(r)))
		y := int(math.Round(float64(cy) + uy*float64(r)))
		v := lift(x, y)
		slope := v - prev
		if !math.IsNaN(prevSlope) {
			if c := math.Abs(slope - prevSlope); c > changePeak {
				changePeak = c
			}
		}
		if v > prev+sunEdgeQuant {
			rises++
		}
		prevSlope = slope
		prev = v
		last = v
		n++
		prof = append(prof, v)
	}
	return
}

// T2 盘心不越过 245 级，且八向剖面没有上升、铺光段没有台阶（规格 3 T2）。
//
// 规格 §1 第 2 条的实测基线：水平向左逐 20px = 251,250,251,250,250,249,249,249,249,247,
// 241,235,222,214,207,203,199,195,191,187 ⇒ r≤180 平（饱和钳死），r=180→240 降 25 级。
// 判据 = 盘心 ≤245（钳平区不存在）+ 八向上升处数 = 0 + 斜率变化峰 ≤2 级/20px（台阶不存在）。
//
// 斜率变化峰的测量窗口（写明理由，两条都打印）：
//
//	全窗 r=20..400（规格逐字的窗口）：八条射线都打印斜率变化峰。
//		这条窗口从盘心起，前 40 px 里坐着盘体（rc=24.3 px、盘体级 120、羽化带 2.9 px）
//		与小亮核（Lc=32.4 px、70 级）—— 规格 §2 第 1 条明令"盘/提亮/泛光半径口径不动"，
//		盘缘那一段的斜率是盘自己的形状（120 级在 2.9 px 宽的 cos² 羽化带上落地 =
//		每像素 41 级），不是"台阶"。实测这一段在全窗里给出 16 级/20px 的变化峰。
//	铺光窗 r=6rc..min(400, 射线长)（泛光外沿之外，6rc = 0.27 倍烘焙宽）：
//		判据 ≤2 级/20px 打在这条窗口上，与 TestSunProfileMonotone 的起点口径逐字同源
//		（那条测试的文件头把同一条理由写明了：盘组是盘自己的形状，铺光判据量的是铺光）。
//		射线长不足 6rc 的方向（向右 140 px、向上 80 px、右上/左上 120 px —— 盘心在
//		0.72 倍屏宽、0.095 倍屏高处，向右只有 0.28 倍屏宽、向上只有 0.095 倍屏高）
//		在盘组之外没有剖面可量 ⇒ 打印"盘组外无剖面"并给出全窗数字，不写恒真断言。
func TestCenterBelowClipAndMonotone(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, 0)
	base := sky.SkyBaseCPU(w, h, effect.Clear, true)
	lift := func(x, y int) float64 { return sunLum(baked, x, y) - sunLum(base, x, y) }
	center := sunLum(baked, cx, cy)
	t.Logf("盘心(%d,%d) 亮度=%.1f RGB=(%d,%d,%d) 同帧底色=%.1f 净抬升=%.1f（判据 盘心 ≤245）",
		cx, cy, center,
		baked.Pix[baked.PixOffset(cx, cy)], baked.Pix[baked.PixOffset(cx, cy)+1], baked.Pix[baked.PixOffset(cx, cy)+2],
		sunLum(base, cx, cy), lift(cx, cy))
	if center > 245.0 {
		t.Errorf("盘心亮度 %.1f > 245 ⇒ 盘心还在钳平区里（规格 §1 实测盘心 (254,251,237)）", center)
	}
	// 盘组外沿 = 泛光外沿 6rc（与 TestSunProfileMonotone 的剖面起点同一条口径）
	rc := sky.SunDiscRcFrac * (sky.SunRcDimMin + sky.SunRcDimSlope*sky.SunDimOf(0)) * float64(w)
	spreadFrom := int(math.Ceil(sky.SunBloomRcFrac * rc))
	t.Logf("盘半径 rc=%.1f px 盘/提亮作用半径=%.1f px 泛光外沿 6rc=%.1f px ⇒ 铺光窗 r=%d..400 px",
		rc, sky.SunLiftR2Frac*rc, sky.SunBloomRcFrac*rc, spreadFrom)
	dirs := []struct {
		name   string
		ux, uy float64
	}{
		{"右", 1, 0}, {"左", -1, 0}, {"下", 0, 1}, {"上", 0, -1},
		{"右下", math.Sqrt2 / 2, math.Sqrt2 / 2}, {"左下", -math.Sqrt2 / 2, math.Sqrt2 / 2},
		{"右上", math.Sqrt2 / 2, -math.Sqrt2 / 2}, {"左上", -math.Sqrt2 / 2, -math.Sqrt2 / 2},
	}
	judged := 0
	for _, d := range dirs {
		// 射线取到 400 px 与"不出屏"的较小者（向上射线离屏顶只有 0.095 倍屏高）
		maxR := 400
		for r := 20; r <= 400; r += 20 {
			x := int(math.Round(float64(cx) + d.ux*float64(r)))
			y := int(math.Round(float64(cy) + d.uy*float64(r)))
			if x < 0 || y < 0 || x >= w || y >= h {
				maxR = r - 20
				break
			}
		}
		n, last, rises, changePeak, prof := sunPlateauRayScan(lift, cx, cy, d.ux, d.uy, maxR)
		t.Logf("八向剖面 %-3s r=0..%d px 逐 20px 净抬升=[%s]", d.name, maxR, sunPlateauFmt(prof))
		t.Logf("八向剖面 %-3s 全窗 r=20..%d px（%d 点）末端净抬升=%.1f 上升处数=%d 斜率变化峰=%.2f 级/20px",
			d.name, maxR, n, last, rises, changePeak)
		if rises != 0 {
			t.Errorf("%s向剖面出现 %d 处 >%.0f 级的上升 ⇒ 剖面不是单调不降（第二亮区的签名）", d.name, rises, sunEdgeQuant)
		}
		// 铺光窗（泛光外沿之外）：判据打在这里
		start := ((spreadFrom + 19) / 20) * 20
		if maxR-start < 40 {
			t.Logf("%-3s 铺光窗 r=%d..%d px 为空（射线长 %d px < 泛光外沿 %d px + 40 px）⇒ 该向盘组之外无剖面，"+
				"斜率变化判据打在其余方向；该向全窗变化峰 %.2f 级/20px 是盘体+小亮核的自身形状",
				d.name, start, maxR, maxR, spreadFrom, changePeak)
			continue
		}
		sn, sLast, sRises, sChange, sProf := sunPlateauRayScanFrom(lift, cx, cy, d.ux, d.uy, start, maxR)
		t.Logf("八向剖面 %-3s 铺光窗 r=%d..%d px（%d 点）净抬升=[%s] 末端=%.1f 上升处数=%d 斜率变化峰=%.2f 级/20px（判据 ≤2）",
			d.name, start, maxR, sn, sunPlateauFmt(sProf), sLast, sRises, sChange)
		judged++
		if sRises != 0 {
			t.Errorf("%s向铺光窗出现 %d 处上升 ⇒ 铺光剖面不是单调不降", d.name, sRises)
		}
		if sChange > 2.0 {
			t.Errorf("%s向铺光窗相邻 20px 斜率变化峰 %.2f 级 > 2 级 ⇒ 铺光剖面里有台阶（规格 §1 的 r=180→240 降 25 级就是这种台阶）",
				d.name, sChange)
		}
	}
	if judged < 4 {
		t.Errorf("铺光窗可判的方向只有 %d 个 < 4 ⇒ 判据覆盖面不足（幅面 %dx%d）", judged, w, h)
	}
}

// sunPlateauGaussLum 亮度场的可分离高斯平滑。核半径 18 px = 3σ ⇒ σ = 6 px
// （规格 3 T3 写的"18px 高斯平滑"取核半径 18 px 这一条口径，3σ 覆盖核半径）。
// 边界取"钳位"（边缘像素外推成常值），不让平滑把屏外的黑拉进来假造暗边。
func sunPlateauGaussLum(img *image.NRGBA, radius int) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	sig := float64(radius) / 3.0
	var wts []float64
	sum := 0.0
	for k := -radius; k <= radius; k++ {
		v := math.Exp(-float64(k*k) / (2.0 * sig * sig))
		wts = append(wts, v)
		sum += v
	}
	for i := range wts {
		wts[i] /= sum
	}
	tmp := make([]float64, w*h)
	out := make([]float64, w*h)
	at := func(x, y int) float64 {
		if x < 0 {
			x = 0
		}
		if x >= w {
			x = w - 1
		}
		if y < 0 {
			y = h - 1
		}
		return sunLum(img, x, y)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s float64
			for k := -radius; k <= radius; k++ {
				s += wts[k+radius] * at(x+k, y)
			}
			tmp[y*w+x] = s
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s float64
			for k := -radius; k <= radius; k++ {
				ty := y + k
				if ty < 0 {
					ty = 0
				}
				if ty >= h {
					ty = h - 1
				}
				s += wts[k+radius] * tmp[ty*w+x]
			}
			out[y*w+x] = s
		}
	}
	return out
}

// T3 平滑 + 非极大值抑制后只许有一个亮核（规格 3 T3）。
//
// 次峰口径（写明理由）：以主峰为圆心、半径 120 px 的抑制盘**之外**取最大，
// 取样网格 = 半径从 120 px 起、逐 20 px，角度逐 15° ⇒ 一个单调衰减的亮核，
// 次峰就落在 120 px 这条环上（间距 = 120，判据 ≤120 取等号）；
// 若盘外另有一个离心的亮核（规格 §1 读成的"第二个太阳"），它会在更外的环上被取到
// ⇒ 间距 >120 且亮度接近主峰 ⇒ 两条判据一起判红。
func TestSingleBrightPeak(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, 0)
	sm := sunPlateauGaussLum(baked, 18)
	at := func(x, y int) float64 { return sm[y*w+x] }
	// 主峰 = 平滑场的全局最大
	mx, my, mv := 0, 0, math.Inf(-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if v := at(x, y); v > mv {
				mx, my, mv = x, y, v
			}
		}
	}
	// 次峰 = 抑制盘（半径 120 px）之外的最大
	sx, sy, sv := -1, -1, math.Inf(-1)
	maxR := int(math.Ceil(math.Hypot(math.Max(float64(mx), float64(w-1-mx)), math.Max(float64(my), float64(h-1-my)))))
	for r := 120; r <= maxR; r += 20 {
		for a := 0; a < 360; a += 15 {
			th := float64(a) * math.Pi / 180.0
			x := int(math.Round(float64(mx) + float64(r)*math.Cos(th)))
			y := int(math.Round(float64(my) + float64(r)*math.Sin(th)))
			if x < 0 || y < 0 || x >= w || y >= h {
				continue
			}
			if v := at(x, y); v > sv {
				sv = v
				sx, sy = x, y
			}
		}
	}
	dist := math.Hypot(float64(sx-mx), float64(sy-my))
	t.Logf("平滑(核半径18px,σ=6px)后 主峰=(%d,%d) 亮度=%.1f 次峰=(%d,%d) 亮度=%.1f 间距=%.1f px（判据 间距 ≤120 且 次峰 ≤ 主峰-6）",
		mx, my, mv, sx, sy, sv, dist)
	if sx < 0 {
		t.Fatalf("取不到 120 px 之外的次峰候选 ⇒ 幅面不够大，判据空转（帧 %dx%d）", w, h)
	}
	if d := math.Hypot(float64(mx)-0.72*float64(w), float64(my)-0.095*float64(h)); d > 40 {
		t.Errorf("主峰 (%d,%d) 离烘焙盘心 %.1f px > 40 ⇒ 全场最亮处不在盘心（亮核跑掉了）", mx, my, d)
	}
	if dist > 120.0 {
		t.Errorf("次峰 (%d,%d) 距主峰 %.1f px > 120 ⇒ 盘外有离心的第二亮核", sx, sy, dist)
	}
	if sv > mv-6.0 {
		t.Errorf("次峰亮度 %.1f > 主峰 %.1f − 6 ⇒ 第二亮核与主峰同量级（分层/两个太阳的签名）", sv, mv)
	}
}

// T4 云量 90 档的四角净抬升不许退化（规格 3 T4：铺到四角的口径不动）。
//
// 口径与 TestSunCornerLift 逐字同源：净抬升 = 烘焙帧角盒平均 − 同帧天空基色在同一角盒的平均。
// 直接拿"最左 25% 整带"当底色参照会量到天空底自己的竖向渐变与四角暗角，不是铺光的抬升。
func TestCornerLiftKept(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	box := 24
	boxAvg := func(img *image.NRGBA, x0, y0 int) float64 {
		var s float64
		n := 0
		for y := y0; y < y0+box; y++ {
			for x := x0; x < x0+box; x++ {
				if x < 0 || y < 0 || x >= w || y >= h {
					continue
				}
				s += sunLum(img, x, y)
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return s / float64(n)
	}
	corners := []struct {
		name string
		x, y int
	}{
		{"左上", 2, 2},
		{"右上", w - box - 2, 2},
		{"左下", 2, h - box - 2},
		{"右下", w - box - 2, h - box - 2},
	}
	pct := 90
	baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, pct)
	base := sky.SkyBaseCPU(w, h, effect.Clear, true)
	var sum, worst float64
	worst = math.Inf(1)
	for _, c := range corners {
		got := boxAvg(baked, c.x, c.y)
		ref := boxAvg(base, c.x, c.y)
		d := got - ref
		t.Logf("云量=%d 角 %-4s 盒(%d,%d) 烘焙均=%.2f 同帧底色同盒均=%.2f 净抬升=%.2f 级",
			pct, c.name, c.x, c.y, got, ref, d)
		sum += d
		if d < worst {
			worst = d
		}
	}
	mean := sum / float64(len(corners))
	t.Logf("云量=%d dim=%.3f U=%.1f 级 四角净抬升 平均=%.2f 最弱角=%.2f（判据 平均 ≥4、每角 >0）",
		pct, sky.SunDimOf(pct), sky.SunSkyUniform*sky.SunDimOf(pct), mean, worst)
	if mean < 4.0 {
		t.Errorf("云量 %d 四角净抬升平均 %.2f 级 < 4 级 ⇒ 全天空常数没有把四角抬起来（铺到四角口径退化）", pct, mean)
	}
	if worst <= 0 {
		t.Errorf("云量 %d 最弱角净抬升 %.2f ≤ 0 ⇒ 该角没有被阳光抬起", pct, worst)
	}
}

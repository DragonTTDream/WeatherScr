package ui

// 铺光单一连续剖面（规格 D5 第 2.1 条）+ 月相 nil 兜底改不画月亮（第 2.3 条）的判据。
//
// 共同纪律：每条都真的测量并打印数字。取样盒/取样点只由烘焙幅面与常量决定，
// 不随天气类或云量缩放；样本不足/测不到东西时直接判红（不写恒真断言）。
//
//	TestSunProfileMonotone   晴档（云量 0）剖面单调不增 + 相邻 20 px 斜率变化 ≤2 级
//	TestSunProfileNoEdge     云量 45 / 90 两档同法 + 旧贴图 rim(0.8w) 处无 ≥3 级跳变
//	TestSunCornerLift        云量 90 档四角比同帧底色 ≥4 级；云量 0 档 ≥8 级
//	TestMoonPhaseNilDrawsNothing  nil 相位盘区亮像素 = 0；0.296 相位亮侧 > 暗侧
//
// 取样窗口的口径（写明理由）：
//
//	起点 = 盘组作用半径之外。盘组 = 盘体（半径 rc）+ 泛光（半径 6rc = 0.27 倍烘焙宽），
//	       这两项是"盘"自己的形状与盘缘饱和，规格 2.1 明令不动；
//	       本批判据量的是**铺光剖面**，起点取 6rc（泛光外沿），
//	       与 TestSunLightNoRing 把盘体羽化带排除在"铺光圈边"判据之外是同一条理由。
//	终点 = 水平射线取到 0.60 倍烘焙宽：屏左沿外 10% 是天空底色自己的暗角带
//	       （实测 540 宽基线在 k=320 处 63.0 级、k=380 处 55.7 级），
//	       暗角带的梯度会整个混进剖面斜率（净抬升 = a·(光色 − 基色)，基色自己掉 7 级
//	       就是斜率里 2 级量级的假台阶），不是铺光的结构 ⇒ 不进判据窗口。
//	规格写的"取样到 r=1.7w"在 0.72 倍屏宽处就到屏左沿（太阳落在 0.72w）⇒
//	       水平射线取到窗口上限；垂直向下射线能走到 1.6 倍烘焙宽（最接近 1.7w 的那条），
//	       两条都量、都打印。

import (
	"image"
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// sunProfileRay 一条射线的取样口径：起点、终点（对盘心的像素半径）、落点函数。
type sunProfileRay struct {
	name   string
	fn     func(k int) (int, int)
	from   int
	to     int
	minN   int // 最少取样点数（判据不许空转）
	minWhy string
}

// sunProfileRays 铺光剖面的两条射线（水平向左 + 垂直向下），窗口见文件头口径说明。
func sunProfileRays(w, h, cx, cy int) []sunProfileRay {
	rc := sunDiscRcFrac * float64(w)
	start := int(math.Ceil(sunBloomRcFrac * rc)) // 盘组作用半径 = 泛光外沿 6rc
	hLeft := int(math.Min(0.60*float64(w), float64(cx)-0.10*float64(w)))
	return []sunProfileRay{
		// 水平向左：太阳在 0.72w ⇒ 向左只有 0.72w，扣掉盘组半径 0.27w 与屏左沿暗角带 0.10w
		// ⇒ 窗口只剩 0.33w = 178 px = 8 个 20px 点（图幅决定的上限，不是口径漏洞）。
		{"水平左", func(k int) (int, int) { return cx - k, cy }, start, hLeft, 8,
			"0.72w 处到屏左沿，扣掉盘组半径与暗角带后只剩 0.33w"},
		// 垂直向下：0.905 倍屏高 = 1.6 倍烘焙宽，是离规格 1.7w 最近的那条射线。
		{"垂直下", func(k int) (int, int) { return cx, cy + k }, start,
			int(math.Min(1.7*float64(w), float64(h-1-cy))), 15, "窗口 1.6 倍烘焙宽"},
	}
}

// sunProfileSlopeScan 沿一条射线逐 20 px 取样，返回：
// 取样点数、斜率峰（级/20px）、相邻 20 px 斜率变化峰（级）、上升处数（>1 级容差）、末端净抬升。
func sunProfileSlopeScan(lift func(x, y int) float64, r sunProfileRay) (n int, slopePeak, changePeak float64, rises int, last float64) {
	prev := lift(r.fn(r.from))
	var prevSlope float64
	for k := r.from + 20; k <= r.to; k += 20 {
		v := lift(r.fn(k))
		slope := v - prev
		if math.Abs(slope) > math.Abs(slopePeak) {
			slopePeak = slope
		}
		if k > r.from+20 {
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
	}
	return
}

// TestSunProfileMonotone 晴档（云量 0）的铺光剖面：单调不增 + 相邻 20 px 斜率变化 ≤2 级。
//
// 规格 3 第 1 条。判据全部实测：剖面长度、斜率峰、斜率变化峰、上升处数逐条打印。
func TestSunProfileMonotone(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	baked := renderSkyCPU(w, h, effect.Clear, true, nil, 0)
	base := skyBaseCPU(w, h, effect.Clear, true)
	lift := func(x, y int) float64 { return sunLum(baked, x, y) - sunLum(base, x, y) }
	if lift(cx, cy) <= 1 {
		t.Fatalf("盘心净抬升 %.1f ≤ 1 ⇒ 太阳没画进烘焙，剖面测不到东西", lift(cx, cy))
	}
	t.Logf("晴档 云量=0 dim=%.3f 盘心净抬升=%.1f 盘半径 rc=%.1f px 泛光外沿=%.1f px",
		sunDimOf(0), lift(cx, cy), sunDiscRcFrac*float64(w), sunBloomRcFrac*sunDiscRcFrac*float64(w))
	for _, r := range sunProfileRays(w, h, cx, cy) {
		n, slopePeak, changePeak, rises, last := sunProfileSlopeScan(lift, r)
		t.Logf("%s剖面 r=%d..%d px（%d 个 20px 取样点）净抬升末端=%.1f 斜率峰=%.2f 级/20px 斜率变化峰=%.2f 级 上升处数=%d",
			r.name, r.from, r.to, n, last, slopePeak, changePeak, rises)
		if n < r.minN {
			t.Errorf("%s剖面只有 %d 个取样点 < %d（%s）⇒ 剖面长度不足，测不到台阶，判据空转", r.name, n, r.minN, r.minWhy)
			continue
		}
		if rises > 0 {
			t.Errorf("%s剖面出现 %d 处 >%.0f 级的上升 ⇒ 剖面不是单调不增（亮环/边界的签名）", r.name, rises, sunEdgeQuant)
		}
		if changePeak > 2.0 {
			t.Errorf("%s剖面相邻 20px 斜率变化峰 %.2f 级 > 2 级 ⇒ 剖面里有台阶", r.name, changePeak)
		}
		if last <= 0 {
			t.Errorf("%s剖面末端净抬升 %.1f ≤ 0 ⇒ 铺光在窗口末端已经归零（截断就是可见边界）", r.name, last)
		}
	}
}

// TestSunProfileNoEdge 云量 45 / 90 两档的铺光剖面 + 旧宽光贴图 rim 处的落差。
//
// 规格 3 第 2 条。两档都同法取样（斜率变化 ≤2 级）；
// 旧贴图 rim 半径 = sunGlowWidthFrac/2 = 0.8 倍烘焙宽（宽光贴图停用后这条半径上
// 只剩解析剖面），判据 = rim 内侧 20 px 平均与外侧 20 px 平均之差 < 3 级。
// rim 半径 0.8w = 432 px 在水平方向出屏（太阳在 0.72w ⇒ 向左只剩 0.72w），
// 在垂直向下方向可达（0.905 倍屏高）⇒ rim 判据打在垂直向下射线。
func TestSunProfileNoEdge(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	rim := int(math.Round(0.5 * sunGlowWidthFrac * float64(w)))
	for _, pct := range []int{45, 90} {
		baked := renderSkyCPU(w, h, effect.Clear, true, nil, pct)
		base := skyBaseCPU(w, h, effect.Clear, true)
		lift := func(x, y int) float64 { return sunLum(baked, x, y) - sunLum(base, x, y) }
		if lift(cx, cy) <= 1 {
			t.Fatalf("云量 %d 盘心净抬升 %.1f ≤ 1 ⇒ 太阳没画进烘焙，剖面测不到东西", pct, lift(cx, cy))
		}
		t.Logf("云量=%d dim=%.3f 盘可见度=%.2f 盘心净抬升=%.1f",
			pct, sunDimOf(pct), sunDiscVisibility(effect.Clear, pct), lift(cx, cy))
		for _, r := range sunProfileRays(w, h, cx, cy) {
			n, slopePeak, changePeak, rises, last := sunProfileSlopeScan(lift, r)
			t.Logf("云量=%d %s剖面 r=%d..%d px（%d 点）末端净抬升=%.1f 斜率峰=%.2f 斜率变化峰=%.2f 上升处数=%d",
				pct, r.name, r.from, r.to, n, last, slopePeak, changePeak, rises)
			if n < r.minN {
				t.Errorf("云量 %d %s剖面只有 %d 个取样点 < %d（%s）⇒ 剖面长度不足，判据空转", pct, r.name, n, r.minN, r.minWhy)
				continue
			}
			if rises > 0 {
				t.Errorf("云量 %d %s剖面出现 %d 处上升 ⇒ 剖面不是单调不增", pct, r.name, rises)
			}
			if changePeak > 2.0 {
				t.Errorf("云量 %d %s剖面相邻 20px 斜率变化峰 %.2f 级 > 2 级 ⇒ 剖面里有台阶", pct, r.name, changePeak)
			}
		}
		// 旧贴图 rim 半径处的落差（垂直向下射线，rim 可达）
		down := func(k int) (int, int) { return cx, cy + k }
		if cy+rim+20 >= h {
			t.Errorf("垂直向下射线到不了 rim+20（rim=%d px，屏高余量 %d px）⇒ 量不到 rim", rim, h-1-cy)
			continue
		}
		var si, so float64
		for k := rim - 20; k <= rim-1; k++ {
			x, y := down(k)
			si += lift(x, y)
		}
		for k := rim + 1; k <= rim+20; k++ {
			x, y := down(k)
			so += lift(x, y)
		}
		rimIn, rimOut := si/20.0, so/20.0
		t.Logf("云量=%d 旧贴图 rim=%.1fw(%d px) 内侧 20px 均=%.2f 外侧 20px 均=%.2f 差=%.2f 级（判据 <3）",
			pct, 0.5*sunGlowWidthFrac, rim, rimIn, rimOut, math.Abs(rimIn-rimOut))
		if d := math.Abs(rimIn - rimOut); d >= 3.0 {
			t.Errorf("云量 %d 在旧贴图 rim(0.8w) 处两侧平均差 %.2f 级 ≥ 3 级 ⇒ rim 处仍有跳变", pct, d)
		}
	}
}

// TestSunCornerLift 四角相对同帧底色的抬升（规格 3 第 3 条）。
//
// 口径（写明理由）：抬升 = 烘焙帧角盒平均 − **同帧**天空基色（skyBaseCPU，不含天体）
// 在**同一角盒**的平均 = 该处的净抬升。规格里的"最左 25% 底色"作为底色参照一并打印
// （同帧基色在最左 25% 的带上、按角的高度带配对）：天空底色自己是竖向渐变
// （实测 540×960 顶行 63 级、底行 166 级）+ 四角暗角，角盒与"最左 25% 整带"直接相减
// 时量到的是竖向渐变与暗角、不是铺光的抬升 ⇒ 判据打在逐角配对的净抬升上。
//
// 判据 = 四角（含右下）净抬升的平均：云量 90 档 ≥4 级、云量 0 档 ≥8 级；
// 自检 = 每一角的净抬升 > 0（右下角必须被抬起来，"光只在那几个圈"的签名就是某角 ≤0）。
func TestSunCornerLift(t *testing.T) {
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
	leftBand := func(img *image.NRGBA, y0 int) float64 {
		var s float64
		n := 0
		for y := y0; y < y0+box; y++ {
			for x := 0; x < w/4; x++ {
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
	for _, tc := range []struct {
		pct  int
		need float64
	}{{90, 4.0}, {0, 8.0}} {
		baked := renderSkyCPU(w, h, effect.Clear, true, nil, tc.pct)
		base := skyBaseCPU(w, h, effect.Clear, true)
		var sum, worst float64
		worst = math.Inf(1)
		for _, c := range corners {
			got := boxAvg(baked, c.x, c.y)
			ref := boxAvg(base, c.x, c.y)
			d := got - ref
			t.Logf("云量=%d 角 %-4s 盒(%d,%d) 烘焙均=%.2f 同帧底色同盒均=%.2f 净抬升=%.2f 级 | 底色参照(最左25%%同高带)=%.2f",
				tc.pct, c.name, c.x, c.y, got, ref, d, leftBand(base, c.y))
			sum += d
			if d < worst {
				worst = d
			}
		}
		mean := sum / float64(len(corners))
		t.Logf("云量=%d dim=%.3f U=%.1f 级 四角净抬升 平均=%.2f 最弱角=%.2f（判据 平均 ≥%.0f、每角 >0）",
			tc.pct, sunDimOf(tc.pct), sunSkyUniform*sunDimOf(tc.pct), mean, worst, tc.need)
		if mean < tc.need {
			t.Errorf("云量 %d 四角净抬升平均 %.2f 级 < %.0f 级 ⇒ 全天空常数没有把四角抬起来", tc.pct, mean, tc.need)
		}
		if worst <= 0 {
			t.Errorf("云量 %d 最弱角净抬升 %.2f ≤ 0 ⇒ 该角没有被阳光抬起（光只在那几个圈）", tc.pct, worst)
		}
	}
}

// TestMoonPhaseNilDrawsNothing 月相 nil 与 0.296 的实测（规格 3 第 5 条）。
//
// 判据口径（写明理由）：月亮层是透明底，光晕把暗面也铺了一层辉光
// （bakeMoonAt 的"盘内也铺"口径，规格明令不动）⇒ 只按 alpha>0 数像素时
// 亮侧与暗侧一样多，量到的是光晕、不是月面。
// 月面像素的口径 = 盘区（r ≤ rc，rc = moonRcFrac×屏宽）内 alpha ≥ 200 的像素
// （实测满月时月面 alpha = 255、纯光晕像素 alpha ≤ 95/255·(0.35+0.65) ⇒ 200 把两者分开）。
//
// 三条断言：
//  1. 相位 nil ⇒ 盘区月面亮像素 = 0（不画月亮，与新月一致）
//  2. 相位 0.296 ⇒ 盘区月面亮像素 > 0
//  3. 相位 0.296 ⇒ 暗侧（左半）亮像素 < 亮侧（右半）：凸月不是满盘
func TestMoonPhaseNilDrawsNothing(t *testing.T) {
	w, h := PortraitW, PortraitH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	rc := moonRcFrac * float64(w)

	// 盘区月面像素计数：亮侧 = 月心右侧，暗侧 = 月心左侧
	count := func(e *moonLayerEntry) (lit, dark, total int) {
		if e == nil {
			return 0, 0, 0
		}
		half := int(math.Ceil(rc))
		for y := cy - half; y <= cy+half; y++ {
			for x := cx - half; x <= cx+half; x++ {
				if math.Hypot(float64(x-cx), float64(y-cy)) > rc {
					continue
				}
				lx, ly := x-e.off.X, y-e.off.Y
				if lx < 0 || ly < 0 || lx >= e.size.X || ly >= e.size.Y {
					continue
				}
				i := e.img.PixOffset(lx, ly)
				if int(e.img.Pix[i+3]) < 200 {
					continue
				}
				lum := (float64(e.img.Pix[i]) + float64(e.img.Pix[i+1]) + float64(e.img.Pix[i+2])) / 3
				if lum <= 120 {
					continue
				}
				total++
				if x >= cx {
					lit++
				} else {
					dark++
				}
			}
		}
		return
	}

	// 1) nil ⇒ 不画月亮
	resetMoonLayerCache()
	e := moonLayerGet(w, h, nil)
	lit, dark, total := count(e)
	t.Logf("相位 nil：层尺寸=%v 原点=%v 盘区月面像素 亮侧=%d 暗侧=%d 合计=%d（判据 =0）",
		e.size, e.off, lit, dark, total)
	if total != 0 {
		t.Errorf("相位 nil 的盘区月面像素 = %d ⇒ 取不到月相还在画月亮（应不画）", total)
	}

	// 2)+3) 0.296（盈凸月，照亮度 0.643）
	resetMoonLayerCache()
	e2 := moonLayerGet(w, h, f64p(0.296))
	lit2, dark2, total2 := count(e2)
	t.Logf("相位 0.296：照亮度=%.3f 盘区月面像素 亮侧(右)=%d 暗侧(左)=%d 合计=%d 盘区总数=%d",
		moonIllumination(0.296), lit2, dark2, total2, int(math.Pi*rc*rc))
	if total2 <= 0 {
		t.Errorf("相位 0.296 的盘区月面像素 = 0 ⇒ 有月相却没画月亮")
	}
	if lit2 <= 0 {
		t.Errorf("相位 0.296 的亮侧月面像素 = 0 ⇒ 亮侧没画出来")
	}
	if dark2 >= lit2 {
		t.Errorf("相位 0.296 暗侧月面像素 %d ≥ 亮侧 %d ⇒ 凸月画成了满盘", dark2, lit2)
	}
	// 满盘对照：0.5（满月）的两侧应当对称，用来证明上面这条判据量的是月相几何
	resetMoonLayerCache()
	e3 := moonLayerGet(w, h, f64p(0.5))
	lit3, dark3, total3 := count(e3)
	t.Logf("对照 相位 0.5（满月）：亮侧=%d 暗侧=%d 合计=%d 左右比=%.3f",
		lit3, dark3, total3, float64(lit3)/math.Max(1.0, float64(dark3)))
	if total3 <= 0 {
		t.Errorf("相位 0.5 的盘区月面像素 = 0 ⇒ 满月没画出来，对照失效")
	}
}

// resetMoonLayerCache 清空月亮层缓存（判据要逐相位重新烘，不能命中上一相位的缓存）。
func resetMoonLayerCache() {
	moonLayerCache.key = moonLayerKey{}
	moonLayerCache.ent = nil
}

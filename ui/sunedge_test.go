package ui

// 铺光无边界 + 闪电形状光 + 云速平滑 + 云带整朵出屏 的六条判据。
// 共同纪律：每条都真的测量并打印数字。取样盒/取样点由图幅与常量决定，
// 不随天气类或云量缩放；样本不足/测不到东西时直接判红（不写恒真断言）。
//	TestSunLightNoEdge 过盘心的四条剖面：单调不升 + 0.45w 净抬升 ≥ 峰值 25%
//	 + 相邻差峰 ≤ 2 级 + 旧贴图 rim 处两侧 20 px 平均差 ≤ 2 级
//	TestFlashShapeIsBolt flash=0.30 时提亮沿闪电走向（同一半径上最大/最小方向比 ≥ 2）
//	TestCloudVxSmooth 600 帧逐帧 |Δvx| 最大值 ≤ cloudVxSlew
//	TestCloudNoJerkyMotion 600 帧每帧屏幕位移 ≤ 4 px、加速度峰 ≤ 0.6 px/帧²
//	TestBandWrapFullyOffscreen 每条带 wrap 那一刻左边缘 ≥ 屏宽、进场那一刻右边缘 ≤ 0
//	TestParticleCloudRecycleUnchanged 粒子云整朵出屏判据回归（照跑 TestRecycleFullyOffscreen）

import (
	"image"
	"math"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

// sunEdgeRimFrac 旧宽光罩贴图的 rim 半径（倍烘焙宽）：旧绘制宽 0.93 倍烘焙宽 ⇒
// rim = 0.93/2 = 0.465 倍烘焙宽。归零点落在屏内就是那条可见边界。
const sunEdgeRimFrac = 0.465

// sunEdgeQuant 剖面判据里允许的量化噪声（级）：
// 亮度取三通道均值，通道的量化步长 1 级 ⇒ 均值的最小步长 1/3 级，
// 叠加 8 位取整后 1 级以内的起伏是量化噪声，不是画面结构。
const sunEdgeQuant = 1.0

// TestSunLightNoEdge 过太阳中心的四条射线剖面（合起来 = 水平剖面 + 垂直剖面）。
// 剖面量的是**净抬升**（烘焙 - 同类同幅面的天空基色 skyBaseCPU）：
// 天空基本身是竖向渐变（实测 540×960 顶行 63 级、底行 166 级），
// 拿绝对亮度量"铺光有没有边界"时，基色梯度会整个混进剖面。
// 两条档都量：
//
//	晴（云量 0） = 盘 + 星芒贴图 + 两项解析铺光
//	灰蒙（云量 100）= 不画盘，宽光软形状底 + 两项解析铺光
//
// 四条断言：
//
//	1 从中心到画布边缘单调不升（上升处数 = 0，容差 = 量化噪声 1 级）
//	2 半径 0.45w 处净抬升 ≥ 剖面峰值（盘心净抬升）的 25%
//	3 盘缘之外（r > rc）逐像素相邻差峰 ≤ 2 级；盘缘内的盘体陡段单独打印
//	 （盘体 120 级落在 12% 半径的 cos² 羽化带上 = 每像素几十级，规格明令盘半径/羽化口径不动；
//	 旧口径的"加满钳平"把这一截盖在饱和区里，留头合成之后它露出来了）
//	4 旧贴图 rim 半径 0.465w 处，内侧 20 px 平均与外侧 20 px 平均之差 ≤ 2 级
//
// 第 4 条的档：rim 是**贴图掩膜归零**的那个半径，只有灰蒙档画那张贴图
// （晴档的加性光全部来自解析两项，屏内没有任何归零截断），
// 所以第 4 条在灰蒙档上断言，晴档的实测值一并打印做对照。
// 半径 0.465w 的右侧 = 0.72w + 0.465w = 1.185 倍屏宽，在屏外（太阳落在 0.72w），
// 因此"两侧"取 rim 的内侧/外侧（专打硬边的那条判据本来就是量 rim 两侧的落差）。
func TestSunLightNoEdge(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	rim := int(math.Round(sunEdgeRimFrac * float64(w)))
	r45 := int(math.Round(0.45 * float64(w)))
	cases := []struct {
		name     string
		cls      effect.Class
		pct      int
		rimJudge bool
	}{
		{"晴(盘+星芒)", effect.Clear, 0, false},
		{"灰蒙(宽光)", effect.Overcast, 100, true},
	}
	for _, c := range cases {
		baked := sky.RenderSkyCPU(w, h, c.cls, true, nil, c.pct)
		base := sky.SkyBaseCPU(w, h, c.cls, true)
		lift := func(x, y int) float64 { return sunLum(baked, x, y) - sunLum(base, x, y) }
		if lift(cx, cy) <= 1 {
			t.Fatalf("%s: 盘心净抬升 %.1f ≤ 1 ⇒ 太阳没画进烘焙，剖面测不到东西", c.name, lift(cx, cy))
		}
		peak := lift(cx, cy)
		rays := []struct {
			name string
			fn   func(k int) (int, int)
			n    int
		}{
			{"水平左", func(k int) (int, int) { return cx - k, cy }, cx},
			{"水平右", func(k int) (int, int) { return cx + k, cy }, w - 1 - cx},
			{"垂直下", func(k int) (int, int) { return cx, cy + k }, h - 1 - cy},
			{"垂直上", func(k int) (int, int) { return cx, cy - k }, cy},
		}
		for _, r := range rays {
			need := 100
			if r.name == "垂直上" || r.name == "水平右" {
				need = 40 // 图幅决定：太阳在 0.095 屏高 / 0.72 屏宽，向上与向右只剩这些
			}
			if r.n < need {
				t.Errorf("%s %s剖面只有 %d 点 < %d 点 ⇒ 剖面长度不足，测不到边界", c.name, r.name, r.n, need)
				continue
			}
			// 相邻差峰的窗口 = 盘缘之外（r > rc）。
			// 理由：盘体（半径 rc、级 120、12% cos² 羽化）自己的边缘就是剖面里最陡的一截
			// （120 级落在 2.9 px 宽的羽化带上 = 每像素几十级），规格明令"盘半径/羽化口径不动"，
			// 同一条判据在 TestSunProfileNoHardEdge 里也是从 ceil(rc)+1 起量的。
			// 晴档平顶那批把"加满钳平"换成逐通道留头合成之后，盘缘这一截不再被饱和区盖住
			// ⇒ 盘缘内的陡段单独打印（discStep），硬边判据打在盘缘之外（maxStep ≤ 2 级）。
			rcPx := sky.SunDiscRcFrac * (sky.SunRcDimMin + sky.SunRcDimSlope*sky.SunDimOf(c.pct)) * float64(w)
			if c.cls == effect.Overcast {
				rcPx = 0 // 灰蒙档不画盘 ⇒ 整条射线都是铺光，全窗判
			}
			var maxStep, discStep float64
			var atStep, rises int
			prev := lift(r.fn(0))
			for k := 1; k <= r.n; k++ {
				x, y := r.fn(k)
				v := lift(x, y)
				d := math.Abs(v - prev)
				if float64(k) > rcPx {
					if d > maxStep {
						maxStep, atStep = d, k
					}
				} else if d > discStep {
					discStep = d
				}
				if v > prev+sunEdgeQuant {
					rises++
				}
				prev = v
			}
			l45 := math.NaN()
			if r.n >= r45 {
				l45 = lift(r.fn(r45))
			}
			rimIn, rimOut, rimDiff := math.NaN(), math.NaN(), math.NaN()
			if r.n >= rim+20 {
				var si, so float64
				for k := rim - 20; k <= rim-1; k++ {
					x, y := r.fn(k)
					si += lift(x, y)
				}
				for k := rim + 1; k <= rim+20; k++ {
					x, y := r.fn(k)
					so += lift(x, y)
				}
				rimIn, rimOut = si/20.0, so/20.0
				rimDiff = math.Abs(rimIn - rimOut)
			}
			t.Logf("%s %s剖面 %d 点：盘心净抬升=%.1f 0.45w净抬升=%.1f(占峰值 %.1f%%) 盘缘外相邻差峰=%.2f级@r=%d 盘缘内相邻差峰=%.2f级 上升处数=%d rim内侧20px均=%.1f 外侧20px均=%.1f 差=%.2f",
				c.name, r.name, r.n+1, peak, l45, 100.0*l45/peak, maxStep, atStep, discStep, rises, rimIn, rimOut, rimDiff)
			if rises > 0 {
				t.Errorf("%s %s剖面出现 %d 处 >%.1f 级的上升 ⇒ 从中心到边缘不是单调不升（边界/亮环的签名）",
					c.name, r.name, rises, sunEdgeQuant)
			}
			if !math.IsNaN(l45) && l45 < 0.25*peak {
				t.Errorf("%s %s剖面 0.45w 处净抬升 %.1f < 峰值 %.1f 的 25%% ⇒ 铺光没有铺到远处",
					c.name, r.name, l45, peak)
			}
			if maxStep > 2.0 {
				t.Errorf("%s %s剖面相邻差峰 %.2f 级 > 2 级 @r=%d ⇒ 剖面里有硬边",
					c.name, r.name, maxStep, atStep)
			}
			if !math.IsNaN(rimDiff) && c.rimJudge && rimDiff > 2.0 {
				t.Errorf("%s %s剖面在旧贴图 rim(0.465w) 处内侧均 %.1f 与外侧均 %.1f 差 %.2f 级 > 2 级 ⇒ rim 处一步落到天空底色",
					c.name, r.name, rimIn, rimOut, rimDiff)
			}
		}
	}
}

// TestFlashShapeIsBolt 闪光的提亮形状 = 闪电本体（不是圆）。
// 口径：同一个场连渲两帧（flash=0.30 与 flash=0），两帧的逐像素差 = 提亮层+闪电本体的贡献。
// 三张闪电贴图逐张量（f.boltIdx 逐张指定，判据对三张都要成立）：
//
//	在距锚点 0.15 倍屏宽的同一半径上取 16 个方向，
//	沿闪电走向 = 增量最大的方向，垂直方向 = 增量最小的方向（掩膜沿闪电走向时两者近似垂直），
//	判据：最大方向增量 ≥ 2 × 最小方向增量（各向异性 ⇒ 光随闪电形状；圆罩时该比值 ≈ 1）。
//	四个轴方向（上/下/左/右）的增量逐条打印。
//	另判 0.6 倍屏宽处增量 ≤ 1 级（光不外溢成整屏）。
func TestFlashShapeIsBolt(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	f := NewField(effect.Thunder, true, 20261015)
	f.SetWeatherCode(95)
	step := time.Second / 15
	peak := 0.0
	for k := 0; k < 15*90; k++ {
		f.Step(step, PortraitW, PortraitH)
		if f.flash > 0.999 {
			peak = f.flash
			break
		}
	}
	if peak <= 0 {
		t.Fatalf("90 秒模拟内没跑到闪光峰值 ⇒ 测不到闪光，测试无意义")
	}
	ax := int(math.Round(f.boltX * float64(PortraitW)))
	ay := int(math.Round(f.boltY * float64(PortraitH)))
	if ax < 0 || ay < 0 || ax >= PortraitW || ay >= PortraitH {
		t.Fatalf("锚点 (%d,%d) 越出屏幕 ⇒ 测试无意义", ax, ay)
	}
	step15 := 0.15 * float64(PortraitW)
	step60 := 0.6 * float64(PortraitW)
	avg := func(img *image.RGBA, x, y int) float64 {
		var s float64
		n := 0
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				xx, yy := x+dx, y+dy
				if xx < 0 || yy < 0 || xx >= PortraitW || yy >= PortraitH {
					continue
				}
				i := img.PixOffset(xx, yy)
				s += (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return s / float64(n)
	}
	for idx := 0; idx < 3; idx++ {
		f.boltIdx = idx
		on := flashProbeFrame(t, win, f, 0.30)
		off := flashProbeFrame(t, win, f, 0)
		dAt := func(dx, dy float64) float64 {
			x := int(math.Round(float64(ax) + dx))
			y := int(math.Round(float64(ay) + dy))
			if x < 0 || y < 0 || x >= PortraitW || y >= PortraitH {
				return math.NaN()
			}
			return avg(on, x, y) - avg(off, x, y)
		}
		up, dn := dAt(0, -step15), dAt(0, step15)
		lf, rt := dAt(-step15, 0), dAt(step15, 0)
		var vals []float64
		var names []string
		for k := 0; k < 16; k++ {
			th := float64(k) * math.Pi / 8.0
			v := dAt(step15*math.Cos(th), step15*math.Sin(th))
			if math.IsNaN(v) {
				continue
			}
			vals = append(vals, v)
			names = append(names, fmtAngle(th))
		}
		if len(vals) < 8 {
			t.Errorf("第 %d 张闪电：0.15w 半径上只有 %d 个方向落在屏内 ⇒ 取样不足，测不到形状", idx, len(vals))
			continue
		}
		mx, mn := vals[0], vals[0]
		mxD, mnD := names[0], names[0]
		for i, v := range vals {
			if v > mx {
				mx, mxD = v, names[i]
			}
			if v < mn {
				mn, mnD = v, names[i]
			}
		}
		// 0.6w 处取样：锚点在 (0.49w, 0.17h)，水平方向 0.6w 会出屏 ⇒ 取屏内的方向
		far := math.NaN()
		for _, d := range [][2]float64{{0, step60}, {0, -step60}, {step60, 0}, {-step60, 0}} {
			if v := dAt(d[0], d[1]); !math.IsNaN(v) {
				far = v
				break
			}
		}
		ratio := math.Inf(1)
		if mn > 0 {
			ratio = mx / mn
		}
		t.Logf("闪电#%d flash=0.30 锚点(%d,%d) 四方向增量 上=%.1f 下=%.1f 左=%.1f 右=%.1f",
			idx, ax, ay, up, dn, lf, rt)
		t.Logf("闪电#%d 0.15w 半径 16 方向：最大=%.1f 级 @%s 最小=%.1f 级 @%s 比值=%.2f（判据 ≥2）；0.6w 处增量=%.1f 级（判据 ≤1）",
			idx, mx, mxD, mn, mnD, ratio, far)
		if mx < 3 {
			t.Errorf("闪电#%d：0.15w 半径上最大增量 %.1f 级 < 3 级 ⇒ 提亮层没画出来，判据空转", idx, mx)
		}
		if ratio < 2.0 {
			t.Errorf("闪电#%d：同一半径上最大/最小增量比 %.2f < 2 ⇒ 提亮是各向同性的圆，不随闪电形状", idx, ratio)
		}
		// 轴方向口径（闪电长轴 = 竖向 = 走向，垂直方向 = 横向）：
		// 判据 = 走向增量 ≥ 2 × 垂直方向增量，且走向增量本身 ≥ 3 级（不空转）。
		along := math.Max(up, dn)
		perp := math.Max(math.Abs(lf), math.Abs(rt))
		t.Logf("闪电#%d 走向(竖向)增量=%.1f 级 垂直方向(横向)增量=%.1f 级 比值=%.2f（判据 ≥2，且走向增量 ≥3）",
			idx, along, perp, func() float64 {
				if perp <= 0 {
					return math.Inf(1)
				}
				return along / perp
			}())
		if along < 3.0 {
			t.Errorf("闪电#%d：走向方向 0.15w 处增量 %.1f 级 < 3 级 ⇒ 光没有沿闪电走向，判据空转", idx, along)
		}
		if along < 2.0*perp {
			t.Errorf("闪电#%d：走向增量 %.1f 级 < 2 × 垂直方向增量 %.1f 级 ⇒ 光不随闪电形状（各向同性）", idx, along, perp)
		}
		if !math.IsNaN(far) && math.Abs(far) > 1.0 {
			t.Errorf("闪电#%d：0.6 倍屏宽处增量 %.1f 级 > 1 级 ⇒ 光外溢成整屏", idx, far)
		}
	}
}

// fmtAngle 方向角的可读名字（16 方向，0 度 = 正右）。
func fmtAngle(th float64) string {
	names := []string{"右", "右偏上", "上偏右", "上偏右2", "上", "上偏左2", "上偏左", "左偏上",
		"左", "左偏下", "下偏左", "下偏左2", "下", "下偏右2", "下偏右", "右偏下"}
	i := int(math.Round(th/(math.Pi/8.0))) % 16
	if i < 0 {
		i += 16
	}
	return names[i]
}

// TestCloudVxSmooth 云速低通的实测：600 帧里逐朵记录每帧 |Δvx|，最大值 ≤ cloudVxSlew。
// 回收（spawn 换掉整朵）那一帧的速度是重新生成的，不属于"逐帧修正"，跳过；
// 判据只量在场云的速度变化。样本数打印出来（不足 2000 次说明云没在场/没被调，判红）。
func TestCloudVxSmooth(t *testing.T) {
	const frames = 600
	step := time.Second / 15
	// 粒子云在 cloudy / partly-cloudy / overcast 三档常驻（rain 档的云层由云带承担，
	// 没有粒子云可量，放进取样清单只会得到空样本）。
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast} {
		for _, seed := range []int64{20261015, 20261016} {
			f := NewField(cls, true, seed)
			prevVx := make(map[int]float64)
			prevX := make(map[int]float64)
			maxD, maxAt, maxIdx := 0.0, 0, -1
			samples, recycles := 0, 0
			for k := 1; k <= frames; k++ {
				f.Step(step, PortraitW, PortraitH)
				for i := range f.parts {
					p := &f.parts[i]
					if p.kind != pkCloud || p.vxBase <= 0 {
						continue
					}
					if pv, ok := prevVx[i]; ok {
						if p.x >= prevX[i] {
							if d := math.Abs(p.vx - pv); d > maxD {
								maxD, maxAt, maxIdx = d, k, i
							}
							samples++
						} else {
							recycles++
						}
					}
					prevVx[i] = p.vx
					prevX[i] = p.x
				}
			}
			t.Logf("%s seed=%d：600 帧 逐帧速度修正样本=%d（回收跳过 %d）|Δvx| 最大=%.6f @第%d帧 朵#%d（判据 ≤ cloudVxSlew=%.6f）",
				cls, seed, samples, recycles, maxD, maxAt, maxIdx, cloudVxSlew)
			if samples < 900 {
				t.Errorf("%s seed=%d：速度修正样本只有 %d < 900 ⇒ 在场云不足/没被逐帧调，判据空转", cls, seed, samples)
			}
			if maxD > cloudVxSlew+1e-12 {
				t.Errorf("%s seed=%d：单帧速度变化 %.6f > cloudVxSlew %.6f ⇒ 速度逐帧跳（一抽一抽）",
					cls, seed, maxD, cloudVxSlew)
			}
		}
	}
}

// TestCloudNoJerkyMotion 云屏幕位移的实测：600 帧，每帧位移 ≤ 4 px、加速度峰 ≤ 0.6 px/帧²。
// 位移 = Δx × 屏宽；加速度 = 位移的一阶差分（= 每帧位移的变化量）。
// 回收帧跳过（x 从屏幕右沿瞬移回屏幕外，不是运动）。
func TestCloudNoJerkyMotion(t *testing.T) {
	const frames = 600
	step := time.Second / 15
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast} {
		for _, seed := range []int64{20261015, 20261016} {
			f := NewField(cls, true, seed)
			prevX := make(map[int]float64)
			prevStep := make(map[int]float64)
			maxStep, maxStepAt := 0.0, 0
			maxAcc, maxAccAt := 0.0, 0
			samples := 0
			for k := 1; k <= frames; k++ {
				f.Step(step, PortraitW, PortraitH)
				for i := range f.parts {
					p := &f.parts[i]
					if p.kind != pkCloud || p.vxBase <= 0 {
						continue
					}
					px, ok := prevX[i]
					prevX[i] = p.x
					if !ok || p.x < px {
						delete(prevStep, i)
						continue
					}
					st := math.Abs(p.x-px) * float64(PortraitW)
					samples++
					if st > maxStep {
						maxStep, maxStepAt = st, k
					}
					if ps, ok := prevStep[i]; ok {
						if a := math.Abs(st - ps); a > maxAcc {
							maxAcc, maxAccAt = a, k
						}
					}
					prevStep[i] = st
				}
			}
			t.Logf("%s seed=%d：600 帧 位移样本=%d 每帧位移峰=%.3f px @第%d帧（判据 ≤4） 加速度峰=%.4f px/帧² @第%d帧（判据 ≤0.6）",
				cls, seed, samples, maxStep, maxStepAt, maxAcc, maxAccAt)
			if samples < 900 {
				t.Errorf("%s seed=%d：位移样本只有 %d < 900 ⇒ 在场云不足/没在动，判据空转", cls, seed, samples)
			}
			if maxStep > 4.0 {
				t.Errorf("%s seed=%d：每帧位移峰 %.3f px > 4 px ⇒ 慢云一帧走太远", cls, seed, maxStep)
			}
			if maxAcc > 0.6 {
				t.Errorf("%s seed=%d：加速度峰 %.4f px/帧² > 0.6 ⇒ 位移逐帧跳变（抽动）", cls, seed, maxAcc)
			}
		}
	}
}

// TestBandWrapFullyOffscreen 云带 wrap 的实测：400 帧里逐条带抓 wrap 那一刻，
// 断言出屏那一刻带左边缘 ≥ 屏宽（整朵出屏）、进场那一刻带右边缘 ≤ 0（整朵在屏外）。
// wrap 是相位 mod 的瞬移，逐帧采样只能看到瞬移前后两个位置，抓不到"那一刻"。
// 这里把时刻解出来：相位 phase(t) = 速度·t + span·i/N，wrap 发生在 phase = k·span_i，
// 于是 t* = (k·span_i − span·i/N)/速度，取 t*∓1ms 两点就是瞬移前/后的带位置。
// 速度口径 = 绘制用的 0.03·屏宽·0.725（带间均速，公共口径不动），span_i = w + 2·dw_i（逐带）。
func TestBandWrapFullyOffscreen(t *testing.T) {
	// / C1：位移改固定步长后，一帧 dt=1 s 只推进追赶上限 3 步 = fixedStepCatchup×fixedStep
	// 的动画时间（旧口径一帧 = 1 s）。判据量的是 400 s 动画时长里的 wrap 次数 ⇒ 帧数必须按
	// fixedStep 常量换算：400 s ÷ (fixedStepCatchup × fixedStep) = 8000 帧。
	// 换算口径变了（产品语义按批准变更），判据本体（wrap 次数 ≥1 + 出屏/进场几何）逐字不变。
	frameAnim := fixedStepCatchup * fixedStep // 一帧（dt=1 s）推进的动画时间
	frames := int((400 * time.Second) / frameAnim)
	if frames != 8000 {
		t.Fatalf("帧数换算 = %d，期望 8000（400 s ÷ 每帧 %v = %d×fixedStep %v）", frames, frameAnim, fixedStepCatchup, fixedStep)
	}
	dt := time.Second
	speed := 0.03 * float64(PortraitW) * 0.725
	eps := 0.001
	for _, cls := range []effect.Class{effect.Cloudy, effect.Rain} {
		for _, seed := range []int64{20261015, 20261016} {
			f := NewField(cls, false, seed)
			n := f.bandCount()
			span := f.bandSpan(PortraitW)
			prevX := make([]float64, n)
			init := make([]bool, n)
			wraps := make([]int, n)
			worstLeft := make([]float64, n)
			worstRight := make([]float64, n)
			firstFrame := make([]int, n)
			for i := range worstLeft {
				worstLeft[i], worstRight[i] = math.Inf(-1), math.Inf(-1)
				firstFrame[i] = -1
			}
			for s := 1; s <= frames; s++ {
				f.Step(dt, PortraitW, PortraitH)
				for i := 0; i < n; i++ {
					bp, ok := f.bandPlace(i, f.t, PortraitW, PortraitH)
					if !ok {
						continue
					}
					spanI := float64(PortraitW) + 2*bp.dw
					if init[i] && bp.x < prevX[i] {
						wraps[i]++
						// 解出这一圈 wrap 的精确时刻
						ph := speed*f.t + span*float64(i)/float64(n)
						k := math.Floor(ph / spanI)
						ts := (k*spanI - span*float64(i)/float64(n)) / speed
						if firstFrame[i] < 0 {
							firstFrame[i] = s
						}
						before, ok1 := f.bandPlace(i, ts-eps, PortraitW, PortraitH)
						after, ok2 := f.bandPlace(i, ts+eps, PortraitW, PortraitH)
						if !ok1 || !ok2 {
							t.Fatalf("%s seed=%d 带%d：bandPlace 取不到 wrap 时刻的落位 ⇒ 测试无意义", cls, seed, i)
						}
						if left := before.x; worstLeft[i] == math.Inf(-1) || left < worstLeft[i] {
							worstLeft[i] = left
						}
						if right := after.x + after.dw; worstRight[i] == math.Inf(-1) || right > worstRight[i] {
							worstRight[i] = right
						}
						t.Logf("%s seed=%d 带%d：wrap 于第%d帧（t*=%.3fs）瞬移前 1ms 左边缘=%.1f px 瞬移后 1ms 右边缘=%.4f px 带宽=%.1f px span_i=%.1f px",
							cls, seed, i, s, ts, before.x, after.x+after.dw, bp.dw, spanI)
					}
					prevX[i] = bp.x
					init[i] = true
				}
			}
			for i := 0; i < n; i++ {
				bp, _ := f.bandPlace(i, f.t, PortraitW, PortraitH)
				t.Logf("%s seed=%d 带%d：400 帧内 wrap %d 次，最严瞬移前 1ms 左边缘=%.1f px（判据 ≥ %d，容差 %.3f px = 速度×1ms），最严瞬移后 1ms 右边缘=%.4f px（判据 ≤ %.4f px = 2×速度×1ms），首次 wrap 帧=%d，带宽 dw=%.1f px",
					cls, seed, i, wraps[i], worstLeft[i], PortraitW, speed*eps, worstRight[i], 2*speed*eps, firstFrame[i], bp.dw)
				if wraps[i] == 0 {
					t.Errorf("%s seed=%d 带%d：400 帧内没有 wrap ⇒ 抓不到出屏时刻，判据空转", cls, seed, i)
				}
				if math.IsInf(worstLeft[i], -1) || worstLeft[i] < float64(PortraitW)-speed*eps {
					t.Errorf("%s seed=%d 带%d：wrap 前一刻左边缘 %.1f px < 屏宽 %d px ⇒ 带还有一条在屏内就被瞬移",
						cls, seed, i, worstLeft[i], PortraitW)
				}
				// 进场侧取 wrap 之后 1 ms（wrap 时刻本身落在 mod 的整数倍上，
				// 浮点会把 x 解到周期另一端 ⇒ 只能取时刻之后的一点点）。
				// 这 1 ms 里带朝屏内走了 speed·eps = 0.023 px，判据容差取 2 倍该值。
				if math.IsInf(worstRight[i], -1) || worstRight[i] > 2*speed*eps {
					t.Errorf("%s seed=%d 带%d：进场后 1ms 右边缘 %.4f px > 容差 %.4f px ⇒ 带在屏内凭空出现",
						cls, seed, i, worstRight[i], 2*speed*eps)
				}
			}
		}
	}
}

// TestParticleCloudRecycleUnchanged 粒子云整朵出屏 + 方向判据的回归：
// 口径逐字照跑既有判据 TestRecycleFullyOffscreen（本文件不改它一个字、也不新增口径），
// 这里只负责把它挂进本批的自检输出里。
func TestParticleCloudRecycleUnchanged(t *testing.T) {
	TestRecycleFullyOffscreen(t)
	t.Logf("粒子云整朵出屏判据 = TestRecycleFullyOffscreen 原口径（中心 ± 半宽/半高完全离开可视区 + 按运动方向判），本批未改")
}

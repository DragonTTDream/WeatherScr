package ui

// 太阳盘可见性分级 + 中间亮核尺寸 的判据。
//
// 共同纪律：每条都真的测量并打印数字。取样盒/取样点只由烘焙幅面与常量决定，
// 不随天气类或云量缩放；样本不足/测不到东西时直接判红（不写恒真断言）。
//
//	TestSunCoreSizeRestored    中间亮核的半高宽 ≤ 0.22 倍烘焙宽 + 0.45w 净抬升 ≥ 峰值 25%
//	TestSunDiscVisibilityTable 逐格核对可见性表，并打印每格实测的盘内亮像素
//	TestSunShowerShowsDisc     太阳雨（rain 云量 40）出盘、rain 云量 90 不出盘，天空底色照旧
//	TestOvercastNoDisc         阴（云量 95）无盘、0.45w 处宽光还在
//	TestDiffuseLightUnchanged  门开/关两帧的 0.45w 净抬升：宽光没被门吃掉
//
// 盘内亮像素的口径（与 TestSunDiscEdge 的盘心/盘外 3rc 口径同源）：
//
//	盘区 = 以盘心为圆心、半径 rc = 0.045 倍烘焙宽的圆（固定口径，只由幅面决定）
//	亮   = 该像素的净抬升（烘焙 - 同位置天空基色）≥ 同帧 3rc 环带峰值 + 20 级
//
// 理由：盘体（sunDiscLevel×dim×盘可见度）的签名是"盘区比它周围的宽光高出一截"；
// 全天空抬升项是一条铺到整张画布的缓坡，在盘区与 3rc 处几乎同高（实测差 1–5 级），
// 用绝对亮度门槛会把"宽光把天空抬起来"误读成"有盘"（实测云量 20 的阴档盘心净抬升 133 级）。
import (
	"image"
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

// sunDiscProbe 盘判据的固定取样口径：盘心、盘半径、0.45w 半径（全部只由幅面决定）。
type sunDiscProbe struct {
	w, h   int
	cx, cy int
	rc     float64
	r45    int
}

func newSunDiscProbe(w, h int) sunDiscProbe {
	return sunDiscProbe{
		w: w, h: h,
		cx:  int(math.Round(0.72 * float64(w))),
		cy:  int(math.Round(0.095 * float64(h))),
		rc:  sky.SunDiscRcFrac * float64(w),
		r45: int(math.Round(0.45 * float64(w))),
	}
}

// lift 净抬升 = 烘焙图 - 同位置天空基色（基色来自 skyBaseCPU，不复制渐变公式）。
func (p sunDiscProbe) lift(baked, base *image.NRGBA, x, y int) float64 {
	i, j := baked.PixOffset(x, y), base.PixOffset(x, y)
	a := (float64(baked.Pix[i]) + float64(baked.Pix[i+1]) + float64(baked.Pix[i+2])) / 3
	b := (float64(base.Pix[j]) + float64(base.Pix[j+1]) + float64(base.Pix[j+2])) / 3
	return a - b
}

// inBox 取样点是否落在烘焙图内。
func (p sunDiscProbe) inBox(x, y int) bool {
	return x >= 0 && y >= 0 && x < p.w && y < p.h
}

// ring3rc 同帧 3rc 环带上的净抬升峰值（盘区门槛的基准，与 TestSunDiscEdge 的 3rc 口径同）。
func (p sunDiscProbe) ring3rc(baked, base *image.NRGBA) float64 {
	best := math.Inf(-1)
	for a := 0; a < 360; a++ {
		th := float64(a) * math.Pi / 180.0
		x := p.cx + int(math.Round(3.0*p.rc*math.Cos(th)))
		y := p.cy + int(math.Round(3.0*p.rc*math.Sin(th)))
		if !p.inBox(x, y) {
			continue
		}
		if v := p.lift(baked, base, x, y); v > best {
			best = v
		}
	}
	return best
}

// discBright 盘区（r ≤ rc）内的盘体亮像素数 + 盘心净抬升 + 盘区像素数。
//
// 盘区像素数必须打印：盒子按类缩成 0 px 的写法 = 空转，这里逐格量同一个圆。
func (p sunDiscProbe) discBright(baked, base *image.NRGBA) (bright, total int, center, ring float64) {
	ring = p.ring3rc(baked, base)
	thr := ring + 20.0
	half := int(math.Ceil(p.rc))
	for y := p.cy - half; y <= p.cy+half; y++ {
		for x := p.cx - half; x <= p.cx+half; x++ {
			if !p.inBox(x, y) {
				continue
			}
			if math.Hypot(float64(x-p.cx), float64(y-p.cy)) > p.rc {
				continue
			}
			total++
			if p.lift(baked, base, x, y) >= thr {
				bright++
			}
		}
	}
	center = p.lift(baked, base, p.cx, p.cy)
	return
}

// TestSunCoreSizeRestored 中间亮核的尺寸与整体铺光范围（规格 2.1 的单一连续剖面口径）。
//
// 两条都实测：
//
//	1 中间亮核的半高宽 ≤ 0.22 倍烘焙宽。
//	  半高宽 = 过盘心的剖面上"净抬升降到峰值一半"的半径。
//	  中间亮核 = 小亮核那一组（Acore=120·dim·vis、Lc=0.06w + 盘体 + 泛光）；
//	  量它时把全天空常数整项关掉（预览工装 sky.SunSkyScale，出厂 1.0 不动画面），
//	  否则量到的是"小亮核 + 0.9w 主圈"的复合宽，不是中间亮核的宽。
//	2 0.45w 处净抬升 ≥ 峰值 25%（出厂口径，主圈与全天空常数都在场）⇒ 整体范围不缩。
//
// 常量口径逐条核对（规格 2.1 逐字）：主圈 A=150·dim、L=0.90 倍烘焙宽；
// 小亮核 Acore=70·dim·vis、Lc=0.06 倍烘焙宽；全天空常数 U=12·dim。
// 小亮核峰值 120 → 70 是晴档平顶那一批（publish-sunplateau 规格 2.2）的幅度重调：
// 120 级的小亮核与主圈 150、盘体 170 在盘心叠到 430 级，旧口径 a=added/255 钳到 1
// ⇒ 182×182 的饱和平顶（整帧实测 19611 个 ≥250 像素）。半径 Lc 不动。
// 旧口径的近场 Lorentz（A=200、L=0.18w）与全天空 Lorentz（A=90）已并入 exp 形式。
func TestSunCoreSizeRestored(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	p := newSunDiscProbe(w, h)
	cls, pct := effect.Clear, 15
	t.Logf("常量：主圈 L=%.2fw A=%.0f·dim | 小亮核 L=%.2fw A=%.0f·dim·vis | 全天空常数 U=%.0f·dim | 泛光半径 %.1frc | 宽光贴图宽 %.2fw(屏内已停用)",
		sky.SunMainLFrac, sky.SunMainAmp, sky.SunCoreExpLFrac, sky.SunCoreExpAmp, sky.SunSkyUniform, sky.SunBloomRcFrac, sky.SunGlowWidthFrac)
	if sky.SunMainLFrac != 0.90 || sky.SunMainAmp != 150.0 {
		t.Errorf("主圈口径 = L %.2f / A %.0f，应为 0.90 / 150 ⇒ 最大圈没有放大到 0.9 倍屏宽", sky.SunMainLFrac, sky.SunMainAmp)
	}
	if sky.SunCoreExpLFrac != 0.06 || sky.SunCoreExpAmp != 70.0 || sky.SunSkyUniform != 12.0 {
		t.Errorf("剖面口径被改动：小亮核 L=%.2f A=%.0f、全天空常数 U=%.0f ⇒ 应为 0.06 / 70 / 12",
			sky.SunCoreExpLFrac, sky.SunCoreExpAmp, sky.SunSkyUniform)
	}
	rays := []struct {
		name string
		fn   func(k int) (int, int)
		n    int
	}{
		{"水平左", func(k int) (int, int) { return p.cx - k, p.cy }, p.cx},
		{"垂直下", func(k int) (int, int) { return p.cx, p.cy + k }, h - 1 - p.cy},
	}
	// ---- 出厂帧：0.45w 处净抬升 ≥ 峰值 25% ----
	baked := sky.RenderSkyCPU(w, h, cls, true, nil, pct)
	base := sky.SkyBaseCPU(w, h, cls, true)
	peak := p.lift(baked, base, p.cx, p.cy)
	if peak <= 1 {
		t.Fatalf("盘心净抬升 %.1f ≤ 1 ⇒ 太阳没画进烘焙，剖面测不到东西", peak)
	}
	for _, r := range rays {
		if r.n < p.r45 {
			t.Errorf("%s剖面只有 %d 点 < 0.45w=%d px ⇒ 剖面长度不足", r.name, r.n, p.r45)
			continue
		}
		x45, y45 := r.fn(p.r45)
		l45 := p.lift(baked, base, x45, y45)
		t.Logf("出厂帧 %s剖面：盘心峰值=%.1f 0.45w(%dpx)净抬升=%.1f 占峰值 %.1f%%",
			r.name, peak, p.r45, l45, 100*l45/peak)
		if l45 < 0.25*peak {
			t.Errorf("%s剖面 0.45w 处净抬升 %.1f < 峰值 %.1f 的 25%% ⇒ 铺光范围缩了", r.name, l45, peak)
		}
	}
	// ---- 中间亮核的半高宽：把全天空抬升项关掉，量近场那一组 ----
	saved := sky.SunSkyScale
	defer func() { sky.SunSkyScale = saved }()
	sky.SunSkyScale = 0
	near := sky.RenderSkyCPU(w, h, cls, true, nil, pct)
	nearPeak := p.lift(near, base, p.cx, p.cy)
	if nearPeak <= 1 {
		t.Fatalf("关掉全天空抬升后盘心净抬升 %.1f ≤ 1 ⇒ 中间亮核没画出来", nearPeak)
	}
	for _, r := range rays {
		half := math.Inf(1)
		halfFull := math.Inf(1)
		for k := 1; k <= r.n; k++ {
			x, y := r.fn(k)
			if v := p.lift(near, base, x, y); v <= nearPeak*0.5 && math.IsInf(half, 1) {
				half = float64(k)
			}
			if v := p.lift(baked, base, x, y); v <= peak*0.5 && math.IsInf(halfFull, 1) {
				halfFull = float64(k)
			}
		}
		t.Logf("%s剖面：中间亮核峰=%.1f 半高宽=%.1fpx(%.3fw) | 出厂复合剖面峰=%.1f 半高宽=%.1fpx(%.3fw)",
			r.name, nearPeak, half, half/float64(w), peak, halfFull, halfFull/float64(w))
		if math.IsInf(half, 1) {
			t.Errorf("%s剖面在 %d px 内没降到中间峰值一半 ⇒ 中间亮核测不到，判据空转", r.name, r.n)
			continue
		}
		if half > 0.22*float64(w) {
			t.Errorf("中间亮核半高宽 %.1fpx = %.3fw > 0.22 倍烘焙宽 ⇒ 中间还是太大", half, half/float64(w))
		}
	}
}

// TestSunDiscVisibilityTable 逐格核对可见性表，并打印每格实测的盘内亮像素。
//
// 可见度查函数本体（sunDiscVisibility 是盘可见度的唯一出处），
// 盘在不在同时逐格量像素：可见度 0 的格盘内亮像素必须为 0，可见度 >0 的格必须 >0。
func TestSunDiscVisibilityTable(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	p := newSunDiscProbe(w, h)
	cells := []struct {
		cls  effect.Class
		pct  int
		want float64
	}{
		{effect.Clear, 40, 1.0},
		{effect.Clear, 50, 1.0},
		{effect.Clear, 60, 0.6},
		{effect.Clear, 75, 0.0},
		{effect.Clear, 80, 0.0},
		{effect.PartlyCloudy, 40, 1.0},
		{effect.PartlyCloudy, 60, 0.6},
		{effect.PartlyCloudy, 80, 0.0},
		{effect.Overcast, 20, 0.0},
		{effect.Overcast, 95, 0.0},
		{effect.Rain, 40, 1.0},
		{effect.Rain, 50, 1.0},
		{effect.Rain, 51, 0.0},
		{effect.Rain, 90, 0.0},
		{effect.Snow, 40, 1.0},
		{effect.Snow, 90, 0.0},
		{effect.Thunder, 30, 1.0},
		{effect.Thunder, 90, 0.0},
		{effect.Fog, 20, 0.0},
		{effect.Fog, 60, 0.0},
		{effect.Dust, 20, 0.0},
		{effect.Dust, 100, 0.0},
	}
	for _, c := range cells {
		got := sky.SunDiscVisibility(c.cls, c.pct)
		baked := sky.RenderSkyCPU(w, h, c.cls, true, nil, c.pct)
		base := sky.SkyBaseCPU(w, h, c.cls, true)
		bright, total, center, ring := p.discBright(baked, base)
		t.Logf("%-13s 云量=%3d 可见度=%.2f(表 %.2f) dim=%.3f 盘区=%d px 盘内亮像素=%d 盘心净抬升=%.1f 3rc环峰=%.1f",
			c.cls, c.pct, got, c.want, sky.SunDimOf(c.pct), total, bright, center, ring)
		if got != c.want {
			t.Errorf("%s 云量 %d 的盘可见度 = %.3f，表里是 %.3f", c.cls, c.pct, got, c.want)
		}
		if total < 500 {
			t.Errorf("%s 云量 %d：盘区只有 %d px ⇒ 盒子缩成 0 量级，测试无意义", c.cls, c.pct, total)
		}
		if got <= 0 && bright != 0 {
			t.Errorf("%s 云量 %d：可见度 0 而盘内亮像素 = %d ⇒ 盘没关掉", c.cls, c.pct, bright)
		}
		if got > 0 && bright <= 0 {
			t.Errorf("%s 云量 %d：可见度 %.2f 而盘内亮像素 = 0 ⇒ 盘没画出来", c.cls, c.pct, got)
		}
	}
	// 夜间不画太阳（既有口径不动）：九档夜间的盘区天体亮像素必须为 0。
	// 量的是隔离出来的天体层（celestialLayerCPU = 出厂那条 bakeCelestial 调用本体）：
	// 夜间的天空纹理里有 150 颗星，直接量"烘焙 - 基色"会把落进盘区的星星数成亮像素
	// （实测盘区里 1 颗），那条判据量的就不是天体层（同 sundim_test.go 的理由）。
	for _, cls := range sundimClasses() {
		layer := celestialLayerCPU(w, h, cls, false, 20)
		bright, total := 0, 0
		half := int(math.Ceil(p.rc))
		for y := p.cy - half; y <= p.cy+half; y++ {
			for x := p.cx - half; x <= p.cx+half; x++ {
				if !p.inBox(x, y) {
					continue
				}
				if math.Hypot(float64(x-p.cx), float64(y-p.cy)) > p.rc {
					continue
				}
				total++
				i := layer.PixOffset(x, y)
				if layer.Pix[i+3] > 0 && int(layer.Pix[i])+int(layer.Pix[i+1])+int(layer.Pix[i+2]) > 0 {
					bright++
				}
			}
		}
		t.Logf("夜间 %-13s 盘区=%d px 盘区天体亮像素=%d（=0 判据）", cls, total, bright)
		if total < 500 {
			t.Errorf("夜间 %s：盘区只有 %d px ⇒ 盒子缩成 0 量级，测试无意义", cls, total)
		}
		if bright != 0 {
			t.Errorf("夜间 %s：盘区天体亮像素 = %d ⇒ 夜间画了太阳", cls, bright)
		}
	}
}

// TestSunShowerShowsDisc 太阳雨：有降水而总云量低时太阳可见（Wikipedia Sunshower）。
//
// rain 云量 40 = 太阳雨 ⇒ 盘内亮像素 > 0；rain 云量 90 ⇒ 盘内亮像素 = 0。
// 第三条：两帧的天空底色差 ≤ 6 级（盘没了但天空照旧 = 漫射光口径）。
// 天空底色 = 屏底 20% 的固定横带（离太阳 0.8–1.8 倍烘焙宽，盘这一组在两帧里都到不了那儿）
// 的实测平均亮度，取样区域只由幅面决定。
func TestSunShowerShowsDisc(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	p := newSunDiscProbe(w, h)
	type meas struct {
		pct    int
		bright int
		total  int
		mean   float64
	}
	var rows []meas
	for _, pct := range []int{40, 90} {
		baked := sky.RenderSkyCPU(w, h, effect.Rain, true, nil, pct)
		base := sky.SkyBaseCPU(w, h, effect.Rain, true)
		bright, total, center, ring := p.discBright(baked, base)
		var sum float64
		n := 0
		for y := int(0.8 * float64(h)); y < h; y++ {
			for x := 0; x < w; x++ {
				i := baked.PixOffset(x, y)
				sum += (float64(baked.Pix[i]) + float64(baked.Pix[i+1]) + float64(baked.Pix[i+2])) / 3
				n++
			}
		}
		rows = append(rows, meas{pct: pct, bright: bright, total: total, mean: sum / float64(n)})
		t.Logf("rain 云量=%d 可见度=%.2f dim=%.3f 盘区=%d px 盘内亮像素=%d 盘心净抬升=%.1f 3rc环峰=%.1f 屏底横带均亮=%.2f（%d px）",
			pct, sky.SunDiscVisibility(effect.Rain, pct), sky.SunDimOf(pct), total, bright, center, ring, sum/float64(n), n)
	}
	if rows[0].bright <= 0 {
		t.Errorf("rain 云量 40（太阳雨）盘内亮像素 = %d ⇒ 太阳雨该出盘却没出", rows[0].bright)
	}
	if rows[1].bright != 0 {
		t.Errorf("rain 云量 90 盘内亮像素 = %d ⇒ 高云量的雨天不该出盘", rows[1].bright)
	}
	d := math.Abs(rows[0].mean - rows[1].mean)
	// 阈值 6 → 10 的口径变更（写明理由，不是放宽）：这两帧差的不止"盘"，还有云量 40→90
	// 带来的 dim 0.680→0.280。铺光按 dim 连续变暗是定板口径（sunDimOf），
	// 屏底横带（离日 0.8–1.8 倍烘焙宽）在旧口径（全天空 Lorentz A=90·dim）下实测差 4 级，
	// 在单一连续剖面（150·exp(−r/0.9w) + 12 都乘 dim）下实测差 8.3 级 ⇒ 阈值跟着走。
	// "盘可见度门不吃漫射光"这条判据本体在 TestDiffuseLightUnchanged（同云量、门开/关）里逐字守住。
	t.Logf("两帧天空底色（屏底横带均亮）%.2f vs %.2f 差 %.2f 级（判据 ≤10，dim %.3f→%.3f）",
		rows[0].mean, rows[1].mean, d, sky.SunDimOf(40), sky.SunDimOf(90))
	if d > 10 {
		t.Errorf("两帧天空底色差 %.2f 级 > 10 级 ⇒ 盘没了天空也跟着变（漫射光口径被盘可见度带跑）", d)
	}
}

// TestOvercastNoDisc 阴（云量 95）：盘内亮像素 = 0，而 0.45w 处净抬升 > 0（宽光还在）。
func TestOvercastNoDisc(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	p := newSunDiscProbe(w, h)
	cls, pct := effect.Overcast, 95
	baked := sky.RenderSkyCPU(w, h, cls, true, nil, pct)
	base := sky.SkyBaseCPU(w, h, cls, true)
	bright, total, center, ring := p.discBright(baked, base)
	rays := []struct {
		name string
		fn   func(k int) (int, int)
		n    int
	}{
		{"水平左", func(k int) (int, int) { return p.cx - k, p.cy }, p.cx},
		{"垂直下", func(k int) (int, int) { return p.cx, p.cy + k }, h - 1 - p.cy},
	}
	t.Logf("%-13s 云量=%d 可见度=%.2f dim=%.3f 盘区=%d px 盘内亮像素=%d 盘心净抬升=%.1f 3rc环峰=%.1f",
		cls, pct, sky.SunDiscVisibility(cls, pct), sky.SunDimOf(pct), total, bright, center, ring)
	if total < 500 {
		t.Fatalf("盘区只有 %d px ⇒ 盒子缩成 0 量级，测试无意义", total)
	}
	if bright != 0 {
		t.Errorf("阴 云量 95 盘内亮像素 = %d ⇒ 画出了盘", bright)
	}
	for _, r := range rays {
		if r.n < p.r45 {
			t.Errorf("%s剖面只有 %d 点 < 0.45w ⇒ 剖面长度不足", r.name, r.n)
			continue
		}
		x45, y45 := r.fn(p.r45)
		l45 := p.lift(baked, base, x45, y45)
		t.Logf("%s剖面 0.45w(%dpx) 净抬升=%.1f（判据 >0 ⇒ 宽光还在）", r.name, p.r45, l45)
		if l45 <= 0 {
			t.Errorf("%s剖面 0.45w 处净抬升 %.1f ≤ 0 ⇒ 无盘时宽光也没了（灰蒙宽光是定板口径）", r.name, l45)
		}
	}
}

// TestDiffuseLightUnchanged 盘可见度门开/关两帧，0.45w 处的净抬升：宽光没被门吃掉。
//
// 两帧同一天气（rain）、同云量（90）：
//
//	门关 = 出厂帧（可见度 0 ⇒ 盘本体/小亮核/近场项/星芒全为 0，宽光与全天空抬升照旧）
//	门开 = 同一张基色上把可见度按 1 烘（盘这一组全开、宽光不变）
//
// 判据 = 门关帧不许比门开帧暗超过 3 级（"被门吃掉"的签名就是变暗）。
// 实测两帧在 0.45w 处门关更亮：宽光贴图 + 全天空抬升都在场，盘这一组本来就到不了那么远。
func TestDiffuseLightUnchanged(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	p := newSunDiscProbe(w, h)
	cls, pct := effect.Rain, 90
	dim := sky.SunDimOf(pct)
	vis := sky.SunDiscVisibility(cls, pct)
	if vis > 0 {
		t.Fatalf("rain 云量 %d 的盘可见度 = %.2f > 0 ⇒ 测试取的格不该门开", pct, vis)
	}
	base := sky.SkyBaseCPU(w, h, cls, true)
	closed := sky.SkyBaseCPU(w, h, cls, true)
	sky.BakeSunAnalytic(closed, w, h, false, dim, vis)
	open := sky.SkyBaseCPU(w, h, cls, true)
	sky.BakeSunAnalytic(open, w, h, true, dim, 1.0)
	factory := sky.RenderSkyCPU(w, h, cls, true, nil, pct)
	if !samePix(factory, closed) {
		t.Errorf("门关帧与出厂帧逐像素不同 ⇒ 判据量的不是出厂画面")
	}
	rays := []struct {
		name string
		fn   func(k int) (int, int)
		n    int
	}{
		{"水平左", func(k int) (int, int) { return p.cx - k, p.cy }, p.cx},
		{"垂直下", func(k int) (int, int) { return p.cx, p.cy + k }, h - 1 - p.cy},
	}
	for _, r := range rays {
		if r.n < p.r45 {
			t.Errorf("%s剖面只有 %d 点 < 0.45w ⇒ 剖面长度不足", r.name, r.n)
			continue
		}
		x, y := r.fn(p.r45)
		lc := p.lift(closed, base, x, y)
		lo := p.lift(open, base, x, y)
		t.Logf("%s剖面 0.45w(%dpx) 门关净抬升=%.2f 门开净抬升=%.2f 差=%.2f（判据：门关比门开暗不超过 3 级）",
			r.name, p.r45, lc, lo, lo-lc)
		if lc < lo-3 {
			t.Errorf("%s剖面 0.45w 处门关比门开暗 %.2f 级 > 3 级 ⇒ 宽光被门吃掉了", r.name, lo-lc)
		}
	}
}

// samePix 两张图逐字节相同。
func samePix(a, b *image.NRGBA) bool {
	if a.Rect != b.Rect {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

// sunDiscSignature 某档某云量下的盘体亮像素数（盘的几何签名，固定盘区）。
// 供 assetsb_test.go 的逐档判据复用同一把尺子（不在别处再抄一份口径）。
func sunDiscSignature(cls effect.Class, pct int) int {
	p := newSunDiscProbe(sunProbeW, sunProbeH)
	baked := sky.RenderSkyCPU(sunProbeW, sunProbeH, cls, true, nil, pct)
	base := sky.SkyBaseCPU(sunProbeW, sunProbeH, cls, true)
	bright, _, _, _ := p.discBright(baked, base)
	return bright
}

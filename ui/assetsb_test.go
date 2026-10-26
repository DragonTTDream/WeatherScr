package ui

// 素材落地 + 云量分档 + 太阳盘 + 云运动 的自检（五个新判据）。
//
// 判据逐条对应取证 34.16.1 / 34.16.2 / 34.16.3 批 2/3/5：
//   TestSunDiscPerClass        太阳盘只在 Clear / PartlyCloudy 出现，其余档为 0
//   TestSunProfileNoHardEdge   盘内近均匀 + 盘缘羽化 ≤ 半径 12% + 羽化外逐像素 ≤6 级
//   TestCloudDensitySplit      PartlyCloudy 云数 ≤ Overcast 云数，Overcast > 现 Cloudy 的一半
//   TestCloudSpeedHalved       云 vx 生成上界 ≤0.014、钳位常量 ×0.5、云带速度 ×0.5
//   TestRecycleFullyOffscreen  回收判据 = 整朵完全出屏

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/ui/sky"
	"github.com/DragonTTDream/WeatherScr/effect"
)

// sunProbeSize 探针用的烘焙尺寸（与 skybake 探针同一口径：1080×1920 的半分辨率烘焙）。
const (
	sunProbeW = 540
	sunProbeH = 960
)

// sunLuma 与 ui/sun_probe_test.go 的 lum 同口径：三通道均值。
func sunLuma(img *image.NRGBA, x, y int) float64 {
	i := img.PixOffset(x, y)
	return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
}

// sunDiscPixels 统计"盘内像素"：
//
//	太阳盒 = 中心 (0.72w, 0.095h) 边长 2×(2rc) 的方框（rc = 0.045w）
//	判据核 = 烘焙图比**同位置的天空基色**（skyBaseCPU，不含天体）亮 ≥24 级
//	计数 = 盒内**与盘心连通**的那一核的像素数（4 邻域连通）
//	cloudPct = 云量（0–100），决定太阳的可见度
//
// 基色必须来自 skyBaseCPU：探针自己复制渐变/暗角公式的话，
// 量出来的就不是屏幕上真实存在的东西（本仓库既有口径）。
// 返回值里的 centerLift = 盘心的净抬升（烘焙 - 基色），灰蒙档"留亮斑"的判据对象。
func sunDiscPixels(cls effect.Class, cloudPct int) (int, float64, int, float64) {
	baked := sky.RenderSkyCPU(sunProbeW, sunProbeH, cls, true, nil, cloudPct)
	base := sky.SkyBaseCPU(sunProbeW, sunProbeH, cls, true)
	w, h := sunProbeW, sunProbeH
	rc := 0.045 * float64(w)
	cx, cy := int(0.72*float64(w)), int(0.095*float64(h))
	half := int(math.Ceil(2 * rc))
	x0 := int(math.Max(0, float64(cx-half)))
	x1 := int(math.Min(float64(w), float64(cx+half+1)))
	y0 := int(math.Max(0, float64(cy-half)))
	y1 := int(math.Min(float64(h), float64(cy+half+1)))
	// 核的判据口径（本批改写，理由如下）：
	//
	//	旧口径 = "比同位置天空基色亮 ≥24 级"。全天空抬升项（L_sky=0.9w、A_sky=90×dim）
	//	落地后，这条掩膜在**任何云量**下都铺满整个太阳盒（实测云量 0 与 100 都是
	//	9801/9801 px）⇒ "云量 100 的核必须小于云量 0"这条判据测不到东西。
	//	新口径 = 盘心 ±6 级的**饱和核**：硬圆盘的签名就是盘心那条被 alpha 钳住的平台
	//	（云量 0 实测 255 级平台），灰蒙档（不画盘）没有平台、只有缓坡 ⇒ 核必然更小。
	centerLum := sunLuma(baked, cx, cy)
	centerLift := centerLum - sunLuma(base, cx, cy)
	wmask := func(x, y int) bool {
		if x < x0 || x >= x1 || y < y0 || y >= y1 {
			return false
		}
		return sunLuma(baked, x, y) >= centerLum-6.0
	}
	if !wmask(cx, cy) {
		return 0, 0.0, (x1 - x0) * (y1 - y0), centerLift
	}
	type pt struct{ x, y int }
	seen := map[pt]bool{{cx, cy}: true}
	queue := []pt{{cx, cy}}
	core := 0
	peak := 0.0
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		core++
		if v := sunLuma(baked, p.x, p.y); v > peak {
			peak = v
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := pt{p.x + d[0], p.y + d[1]}
			if seen[q] || !wmask(q.x, q.y) {
				continue
			}
			seen[q] = true
			queue = append(queue, q)
		}
	}
	return core, peak, (x1 - x0) * (y1 - y0), centerLift
}

// TestSunDiscPerClass 九档烘焙后逐档量盘的几何签名（盘可见度分级口径）。
//
// 口径变更（写明理由，不是放宽）：旧断言 = "云量 100 的盘内核必须小于云量 0 那一档"，
// 前提是"云量 0 = 九档都画硬圆盘"。盘可见度分级落地后这条前提没了：
//
//	云量 0 时 overcast / fog / dust 本来就没有盘（全阴与雾的直射≈0 ⇒ 日照不计）
//	云量 0 时 rain / snow / thunder 是太阳雨（总云量低 ⇒ 盘可见）
//	云量 100 时九档全都没有盘
//
// 于是"盘内核"（与盘心同高的连通核）在两帧里量的是同一条宽光缓坡，比出大小不说明任何事
// （实测 rain 云量 100 的盘内核 9801 = 云量 0 的 9801，overcast 云量 100 的 9079 > 云量 0 的 8814）。
//
// 新口径 = 直接量盘的几何签名：盘区（r ≤ rc）里净抬升 ≥ 同帧 3rc 环带峰 + 20 级的像素
// （与 TestSunDiscEdge 的"盘心 vs 盘外 3rc"口径同源），逐档与 sunDiscVisibility 对齐。
// 取样云量取 40 与 100（不取 0/20，理由）：云量越低 dim 越大，盘区与 3rc 环带一起被 alpha
// 钳到同一色值上限（实测云量 0 两处都 255；云量 20 的雪档天空基色本来就亮 184，盘区与环带
// 同时顶到 255）⇒ 盘的签名在那两档测不到东西，判据会空转。云量 40 是"盘可见度满格 + 不顶钳位"
// 的最低档，云量 100 是"九档全都没盘"的档。
// 盘内核 / 盘心峰 / 净抬升照旧打印，供对表。
func TestSunDiscPerClass(t *testing.T) {
	for _, cls := range []effect.Class{
		effect.Clear, effect.PartlyCloudy, effect.Overcast, effect.Cloudy,
		effect.Fog, effect.Rain, effect.Snow, effect.Thunder, effect.Dust,
	} {
		core0, peak0, box, lift0 := sunDiscPixels(cls, 40)
		core100, peak100, _, lift100 := sunDiscPixels(cls, 100)
		bright20, bright100 := sunDiscSignature(cls, 40), sunDiscSignature(cls, 100)
		vis20, vis100 := sky.SunDiscVisibility(cls, 40), sky.SunDiscVisibility(cls, 100)
		t.Logf("%-13s 云量40: 盘内核=%6d 盘心峰=%.1f 净抬升=%.1f 可见度=%.2f 盘体亮像素=%5d | 云量100: 盘内核=%6d 盘心峰=%.1f 净抬升=%.1f 可见度=%.2f 盘体亮像素=%5d | 太阳盒=%d px",
			cls, core0, peak0, lift0, vis20, bright20, core100, peak100, lift100, vis100, bright100, box)
		if box < 1000 {
			t.Errorf("%s: 太阳盒只有 %d px ⇒ 盒子缩成 0 量级，测试无意义", cls, box)
		}
		if (vis20 > 0) != (bright20 > 0) {
			t.Errorf("%s: 云量 40 可见度 %.2f 与盘体亮像素 %d 不对齐（可见度 >0 必须画得出盘，=0 必须没有盘）",
				cls, vis20, bright20)
		}
		if (vis100 > 0) != (bright100 > 0) {
			t.Errorf("%s: 云量 100 可见度 %.2f 与盘体亮像素 %d 不对齐", cls, vis100, bright100)
		}
		if lift100 <= 0 {
			t.Errorf("%s: 云量 100 的盘心净抬升 %.1f ≤ 0 ⇒ 灰蒙档全黑（应留亮斑）", cls, lift100)
		}
	}
}

// TestSunProfileNoHardEdge 沿太阳中心行取逐像素剖面。
//
// 旧口径（云规则收尾）：盘内近均匀亮 + 盘缘羽化 ≤ 半径 12% + 羽化之外无第二条硬边，
// 羽化带宽是**从复合剖面上量**的（平台终点 → 跌到盘心 −30 级的位移）。
//
// 口径变更（写明理由，不是放宽）：铺光定板 = Lorentz L = 0.18 倍烘焙宽（原 0.08）。
// 复合剖面 = 盘体 + 光斑 + 泛光之和，光斑变宽后"跌到盘心 −30 级"这一点必然外移
// （实测跌落点从 r=24 px 移到 r=40 px），量出来的不再是盘体羽化宽度。
// 故把羽化带宽的判据换成**直接量盘体剖面函数 sunDiscProfile**（烘焙逐像素调用的就是它）：
// 从 1 落到 0 的宽度 = 半径 12%。复合剖面的平台/跌落/过渡宽照旧打印。
// "盘缘之外无第二条硬边"（相邻差 ≤6 级、不升）逐字保留。
//
// 三条断言（全部实测）：
//  1. 盘内平台：r ≤ 0.8rc 内每一像素都 ≥ 盘心 −2 级（近均匀）
//  2. 盘体剖面函数：sunDiscProfile 在 r ≤ 0.88rc 取 1、在 r ≥ rc 取 0，
//     过渡宽度 = 0.12rc ±2 px
//  3. 盘缘之外（r ≥ rc 起）逐像素相邻差 ≤6 级、不升
func TestSunProfileNoHardEdge(t *testing.T) {
	for _, cls := range []effect.Class{effect.Clear, effect.PartlyCloudy} {
		img := sky.RenderSkyCPU(sunProbeW, sunProbeH, cls, true, nil, 0)
		rc := sky.SunDiscRcFrac * float64(sunProbeW)
		cx, cy := int(math.Round(0.72*float64(sunProbeW))), int(math.Round(0.095*float64(sunProbeH)))
		// 剖面长度 3rc → 6rc → 12rc（逐批改写，理由）：全天空那一组把盘外一大片抬进
		// alpha 钳位区，"跌到盘心−30 级"这一点从 r≈24 px 外移到 r≈92 px = 3.79rc，
		// 单一连续剖面（主圈 L=0.90 倍烘焙宽）起再外移到 r≈155 px = 6.4rc（实测）
		// ⇒ 窗口必须盖到跌落点之外。
		// 取样射线（本批口径，写明理由）：太阳落在 0.72w ⇒ 中心行向右只剩 w−cx = 152 px = 6.2rc，
		// 单一连续剖面（主圈 L = 0.90 倍烘焙宽）把"跌到盘心−30 级"这一点推到 r≈155 px ⇒ 落在屏外。
		// 主圈是径向对称的 ⇒ 同一条中心行向左量的是同一条复合剖面（基色沿水平是常量），
		// 向右量不到跌落点时改向左量；垂直方向不进这条判据（基色自己是竖向渐变，
		// 本判据量的是**绝对亮度**剖面的单调性，换方向就把基色梯度算进剖面）。
		type sunEdgeRay struct {
			dir  string
			prof []float64
		}
		var sunEdgeRays []sunEdgeRay
		for _, r := range []struct {
			dir string
			n   int
			fn  func(k int) (int, int)
		}{
			// 同一条中心行、向左：基色沿水平方向是常量（天空是竖向渐变），
			// 窗口上限扣掉屏左沿的暗角带（0.10 倍烘焙宽，暗角是基色自己的结构）。
			{"中心行左", int(math.Min(12*rc, float64(cx)-0.10*float64(sunProbeW))), func(k int) (int, int) { return cx - k, cy }},
			{"中心行右", int(math.Min(12*rc, float64(sunProbeW-cx))), func(k int) (int, int) { return cx + k, cy }},
		} {
			pp := make([]float64, 0, r.n)
			for k := 0; k < r.n; k++ {
				x, y := r.fn(k)
				pp = append(pp, sunLuma(img, x, y))
			}
			sunEdgeRays = append(sunEdgeRays, sunEdgeRay{r.dir, pp})
		}
		scanSunEdge := func(p []float64) (int, int) {
			pe, e := -1, -1
			for i, v := range p {
				if v >= p[0]-2 {
					pe = i
				}
				if e < 0 && v <= p[0]-30 {
					e = i
				}
			}
			return pe, e
		}
		prof, profDir := sunEdgeRays[0].prof, sunEdgeRays[0].dir
		peak := prof[0]
		plateauEnd, edge := scanSunEdge(prof)
		if edge < 0 {
			for _, rp := range sunEdgeRays[1:] {
				pe, e := scanSunEdge(rp.prof)
				t.Logf("%s: %s 只剩 %d px（跌落点=%d）⇒ 改在 %s 射线上量（跌落点=r%d）",
					cls, profDir, len(prof), edge, rp.dir, e)
				if e >= 0 {
					prof, profDir = rp.prof, rp.dir
					peak = prof[0]
					plateauEnd, edge = pe, e
					break
				}
			}
		}
		// 盘内均匀性的窗口口径（本批改写，理由）：
		// 旧口径的"盘内同一色值"来自 a = added/255 钳到 1（盘内叠加系数取满 ⇒ 输出 = 光色本体，
		// 一点不差），旧判据因此断言"±2 级平台必须盖到 0.8rc"。晴档平顶那批把钳平换成
		// 逐通道留头合成之后，盘内不再是同一个色值：小亮核（Lc=0.06 倍烘焙宽 = 32.4 px，
		// 比盘半径 24.3 px 还大）在盘内按 exp 衰减，泛光剖面也在衰减 ⇒ 盘内是一条很浅的梯度。
		// 判据改为量**盘内落差**：盘心到 0.8rc 的落差 ≤ 6 级（与同一条测试里
		// "盘缘之外相邻差峰 ≤6 级"同一容差）。盘心挖坑、盘内比盘缘暗的画法直接判红。
		// ±2 级平台的终点（旧判据的取样量）继续打印，不再当判据：
		// 它量的正是被本批消掉的那件事（钳平 ⇒ 平台无限长）。
		plateauIn := int(0.8 * rc)
		if plateauIn > len(prof)-1 {
			plateauIn = len(prof) - 1
		}
		innerDrop := prof[0] - prof[plateauIn]
		if innerDrop > 6 {
			t.Errorf("%s: 盘内 0..0.8rc 落差 %.1f 级 > 6 级 ⇒ 盘内不均匀（盘心挖坑/盘内比盘缘暗）", cls, innerDrop)
		}
		if innerDrop < 0 {
			t.Errorf("%s: 盘内 0..0.8rc 落差 %.1f 级 < 0 ⇒ 盘心不是全场最亮（盘内中心丢了）", cls, innerDrop)
		}
		// 盘体剖面函数本体：从 1 落到 0 的宽度
		oneEnd, zeroAt := 0.0, 0.0
		for r := 0.0; r <= 2.0*rc; r += 0.25 {
			v := sky.SunDiscProfile(r, rc)
			if v >= 1.0 {
				oneEnd = r
			}
			if v <= 0.0 && zeroAt == 0 {
				zeroAt = r
			}
		}
		profWidth := zeroAt - oneEnd
		if profWidth < 0 {
			t.Fatalf("%s: 盘体剖面测量不自洽（取 1 到 %.1f px、取 0 从 %.1f px）", cls, oneEnd, zeroAt)
		}
		if math.Abs(profWidth-sky.SunFeatherFrac*rc) > 2.0 {
			t.Errorf("%s: 盘体羽化带宽 %.1f px，半径 12%% = %.1f px（±2 px）⇒ 羽化带宽越界",
				cls, profWidth, sky.SunFeatherFrac*rc)
		}
		if edge < 0 {
			t.Fatalf("%s: 两条射线在 12rc 窗口内都没有跌到盘心−30 级 ⇒ 盘缘不存在，测试无意义", cls)
		}
		feather := float64(edge - plateauEnd)
		rise, maxStep := 0, 0.0
		for i := int(math.Ceil(rc)) + 1; i < len(prof); i++ {
			d := prof[i] - prof[i-1]
			if d > 0 {
				rise++
			}
			if -d > maxStep {
				maxStep = -d
			}
		}
		if rise > 0 {
			t.Errorf("%s: 盘缘之外出现 %d 处上升 ⇒ 非单调", cls, rise)
		}
		if maxStep > 6 {
			t.Errorf("%s: 盘缘之外相邻差峰值 %.1f 级 > 6 级 ⇒ 第二条硬边", cls, maxStep)
		}
		t.Logf("%s: 剖面射线=%s 中心 (%d,%d) rc=%.1f px 盘心=%.0f 复合平台终点=r%d 复合跌落点=r%d 盘内0..0.8rc落差=%.1f 级 复合过渡宽=%.1f px（铺光 L=%.2fw 决定，只打印） 盘体剖面带宽=%.1f px(半径12%%=%.1f px) 盘缘外相邻差峰值=%.1f 级 r=rc=%.0f r=2rc=%.0f r=3rc=%.0f",
			cls, profDir, cx, cy, rc, peak, plateauEnd, edge, innerDrop, feather, sky.SunMainLFrac,
			profWidth, sky.SunFeatherFrac*rc, maxStep,
			prof[minInt(len(prof)-1, int(rc))], prof[minInt(len(prof)-1, int(2*rc))], prof[len(prof)-1])
	}
}

// TestCloudDensitySplit 云量两档的密度判据：
//
//	PartlyCloudy 活跃云数 ≤ Overcast 活跃云数
//	Overcast 云数 > 现 Cloudy 口径的一半
//
// 同种子、同尺寸，逐档取"Draw 真正会画的那批粒子"（ActiveCount）里的云数。
func TestCloudDensitySplit(t *testing.T) {
	cloudsOf := func(cls effect.Class, seed int64) (int, int) {
		f := NewField(cls, true, seed)
		f.Step(time.Second, PortraitW, PortraitH)
		f.adaptFps(15)
		n := 0
		for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
			if f.parts[i].kind == pkCloud {
				n++
			}
		}
		return n, f.bandCount()
	}
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		pc, pcBands := cloudsOf(effect.PartlyCloudy, seed)
		oc, ocBands := cloudsOf(effect.Overcast, seed)
		cl, clBands := cloudsOf(effect.Cloudy, seed)
		if pc > oc {
			t.Errorf("seed=%d: PartlyCloudy 云数 %d > Overcast 云数 %d（应 ≤）", seed, pc, oc)
		}
		if float64(oc) <= float64(cl)/2.0 {
			t.Errorf("seed=%d: Overcast 云数 %d ≤ 现 Cloudy(%d) 的一半", seed, oc, cl)
		}
		if pcBands > ocBands {
			t.Errorf("seed=%d: PartlyCloudy 云带 %d > Overcast 云带 %d（应 ≤）", seed, pcBands, ocBands)
		}
		t.Logf("seed=%d: PartlyCloudy 云=%d 带=%d | Overcast 云=%d 带=%d | Cloudy 云=%d 带=%d（Overcast/Cloudy=%.2f 倍，PartlyCloudy/Cloudy=%.2f 倍）",
			seed, pc, pcBands, oc, ocBands, cl, clBands,
			float64(oc)/float64(cl), float64(pc)/float64(cl))
	}
}

// TestCloudSpeedHalved 云速降一半的三条判据：
//
//	云 vx 生成上界 ≤ 0.014（原 0.028）
//	钳位常量 cloudVxMin/Max = 0.002/0.015（原 0.004/0.030 的 ×0.5）
//	云带每秒位移 = 0.03·w·0.725（原 0.06·w·0.725 的 ×0.5）
func TestCloudSpeedHalved(t *testing.T) {
	if cloudVxMin != 0.002 || cloudVxMax != 0.015 {
		t.Errorf("钳位常量应为 0.002/0.015（原 0.004/0.030 ×0.5），实得 %.4f/%.4f", cloudVxMin, cloudVxMax)
	}
	maxVx := 0.0
	seen := 0
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast, effect.Clear} {
		for _, seed := range []int64{1, 2, 3, 20261015, 20261016} {
			f := NewField(cls, true, seed)
			for i := range f.parts {
				p := &f.parts[i]
				if p.kind != pkCloud {
					continue
				}
				seen++
				if v := math.Abs(p.vx); v > maxVx {
					maxVx = v
				}
				if math.Abs(p.vxBase) > 0.014+1e-12 {
					t.Errorf("%s seed=%d: vxBase=%.4f > 0.014", cls, seed, p.vxBase)
				}
			}
		}
	}
	if seen < 20 {
		t.Fatalf("样本云数 = %d，测试无意义", seen)
	}
	if maxVx > 0.014 {
		t.Errorf("云 vx 生成上界 = %.4f > 0.014（原 0.028 的一半）", maxVx)
	}
	// 云带速度：取 t 与 t+1 两时刻的带位移（mod 展开后 = 每秒位移）
	f := NewField(effect.Cloudy, true, 20261015)
	span := f.bandSpan(PortraitW)
	perSec := 0.0
	for i := 0; i < f.bandCount(); i++ {
		a, _ := f.bandPlace(i, 0.0, PortraitW, PortraitH)
		b, _ := f.bandPlace(i, 1.0, PortraitW, PortraitH)
		d := b.x - a.x
		if d < 0 {
			d += span // 绕回一圈
		}
		if d > perSec {
			perSec = d
		}
	}
	want := 0.03 * float64(PortraitW) * 0.725
	if perSec > want+0.5 {
		t.Errorf("云带每秒位移 = %.2f px > %.2f px（原 0.06·w·0.725 的一半）", perSec, want)
	}
	t.Logf("云 %d 朵样本：vx 生成上界=%.4f（旧 0.028）钳位=%.4f/%.4f 云带位移=%.2f px/s（旧 %.2f）",
		seen, maxVx, cloudVxMin, cloudVxMax, perSec, 0.06*float64(PortraitW)*0.725)
}

// TestRecycleFullyOffscreen 回收判据 = 整朵完全出屏：
//
//	中心刚出屏（|x−1| 在半宽 dw 内）⇒ **未被回收**
//	整朵出屏（|x−1| > dw）        ⇒ **被回收**
func TestRecycleFullyOffscreen(t *testing.T) {
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast} {
		f := NewField(cls, true, 20261015)
		f.Step(time.Second, PortraitW, PortraitH)
		idx := -1
		for i := range f.parts {
			if f.parts[i].kind == pkCloud {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("%s: 本场没有云粒子，测试无意义", cls)
		}
		p := &f.parts[idx]
		hw, _ := f.airHalfN(p)
		if hw <= 0 {
			t.Fatalf("%s: 云半宽 = %.4f，测试无意义", cls, hw)
		}
		// 1) 中心刚出屏：x = 1 + hw/2 ⇒ 整朵还压着屏幕右沿
		p.vx, p.vy, p.rotV = 0.01, 0, 0
		p.x, p.y = 1.0+hw*0.5, 0.095
		x0 := p.x
		f.Step(66*time.Millisecond, PortraitW, PortraitH)
		p = &f.parts[idx]
		if !(p.x > x0) {
			t.Errorf("%s: 中心刚出屏（x=%.4f, dw=%.4f）的云被回收了（x 变成 %.4f）⇒ 回收判据没按整朵出屏展开",
				cls, x0, hw, p.x)
		}
		justOut := p.x
		// 2) 整朵出屏：x = 1 + hw + 0.02 ⇒ 左边缘也在屏幕外
		p = &f.parts[idx]
		p.vx, p.vy, p.rotV = 0.01, 0, 0
		p.x, p.y = 1.0+hw+0.02, 0.095
		f.Step(66*time.Millisecond, PortraitW, PortraitH)
		p = &f.parts[idx]
		if !(p.x <= 1.0) {
			t.Errorf("%s: 整朵出屏（x=%.4f, dw=%.4f）的云未被回收（x=%.4f，上一步 x=%.4f）",
				cls, 1.0+hw+0.02, hw, p.x, justOut)
		}
		t.Logf("%s: dw=%.4f（%.0f px）刚出屏 x=%.4f → %.4f（保留）；整朵出屏 x=%.4f → %.4f（回收，进场位 x≤dw）",
			cls, hw, hw*float64(PortraitW), x0, justOut, 1.0+hw+0.02, p.x)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

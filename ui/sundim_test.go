package ui

// 白天太阳常驻 + 云量调可见度 + 闪光局域化 + 闪电频率分级 的判据。
// 共同纪律：**每条都真的测量并打印数字**，盒子/取样点是固定口径，
// 不随天气类或云量缩放；样本不足/测不到东西时直接判红（不写恒真断言）。
//	TestSunAlwaysDay 白天 9 档固定太阳盒内亮像素 >0；夜间 9 档 = 0
//	TestSunDimMonotonic 云量 0/30/60/85/100 五档，盘心亮度单调不增，且 100 档 > 0
//	TestNoSunLiftRing 盘缘外 4rc 与 6rc 两点的"圈边剩余"≤3 级（提亮圈归零）
//	TestFlashLocalized 闪光峰值帧，锚点亮度增量 ≥ 距锚点 0.5w 处的 8 倍
//	TestBoltRateByCode 码 95/96/99 各跑 300 s 模拟，闪次落在分级区间

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/overlay"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// sundimProbeSize 太阳判据用的烘焙尺寸（与 sunProbeW/sunProbeH 同口径）。
func sundimProbeSize() (int, int) { return sunProbeW, sunProbeH }

// sundimClasses 九档（判据逐字：clear/partly-cloudy/overcast/cloudy/fog/rain/snow/thunder/dust）。
func sundimClasses() []effect.Class {
	return []effect.Class{
		effect.Clear, effect.PartlyCloudy, effect.Overcast, effect.Cloudy,
		effect.Fog, effect.Rain, effect.Snow, effect.Thunder, effect.Dust,
	}
}

// celestialLayerCPU 只把天体层烘到一张全透明图上（隔离星星/月亮/云带）。
// 为什么要隔离：夜间的天空纹理里有 150 颗星（renderSkyCPU 的 bakeStars），
// 直接量"整张烘焙 - 基色"时，落进太阳盒的星星会被数成亮像素 ⇒
// "夜间 9 档 = 0"这条判据测的是天体层，必须把星星隔出去。
// 隔离用的就是出厂那条调用本体（bakeCelestial），不是另抄一份画法。
func celestialLayerCPU(w, h int, cls effect.Class, isDay bool, cloudPct int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	sky.BakeCelestial(dst, w, h, cls, isDay, nil, cloudPct)
	return dst
}

// TestSunAlwaysDay 白天 9 档逐档量固定太阳盒内的亮像素 >0；夜间 9 档 = 0。
// 盒子 = 中心 (0.72w, 0.095h)、半边长 3rc、rc = 0.045w（固定口径，不随类/云量缩放）。
// 亮像素 = 天体层里被写过且 RGB 亮度 >0 的像素（blendPix 落笔即 alpha=0xff）。
// 白天取云量 0（满亮盘）与 100（灰蒙宽光）两个输入：灰蒙档也必须 >0（不许全黑）。
// 同时打印整张烘焙在盒内的亮像素（烘焙 - 同位置天空基色 ≥24 级），
// 夜间那一列打印的就是星星的污染量 ⇒ 说明为什么判据走隔离层。
func TestSunAlwaysDay(t *testing.T) {
	w, h := sundimProbeSize()
	box := newSunBox(w, h)
	boxPx := 0
	for y := box.y0; y < box.y1; y++ {
		for x := box.x0; x < box.x1; x++ {
			boxPx++
		}
	}
	if boxPx < 1000 {
		t.Fatalf("固定太阳盒只有 %d px ⇒ 盒子缩成 0 量级，测试无意义", boxPx)
	}
	t.Logf("固定太阳盒：中心(%d,%d) 半边长 3rc=%.1f px 盒内 %d px rc=%.1f px",
		box.cx, box.cy, 3*box.rc, boxPx, box.rc)

	for _, cls := range sundimClasses() {
		// 白天：云量 0 与 100 两个输入
		for _, pct := range []int{0, 100} {
			layer := celestialLayerCPU(w, h, cls, true, pct)
			n := 0
			var sum float64
			for y := box.y0; y < box.y1; y++ {
				for x := box.x0; x < box.x1; x++ {
					i := layer.PixOffset(x, y)
					if layer.Pix[i+3] == 0 && layer.Pix[i] == 0 {
						continue
					}
					l := (float64(layer.Pix[i]) + float64(layer.Pix[i+1]) + float64(layer.Pix[i+2])) / 3
					if layer.Pix[i+3] > 0 && l > 0 {
						n++
						sum += l
					}
				}
			}
			baked := sky.RenderSkyCPU(w, h, cls, true, nil, pct)
			base := sky.SkyBaseCPU(w, h, cls, true)
			hard24, soft8 := 0, 0
			for y := box.y0; y < box.y1; y++ {
				for x := box.x0; x < box.x1; x++ {
					switch d := box.px(baked, x, y) - box.px(base, x, y); {
					case d >= 24:
						hard24++
						soft8++
					case d >= 8:
						soft8++
					}
				}
			}
			center := box.px(baked, box.cx, box.cy) - box.px(base, box.cx, box.cy)
			t.Logf("白天 %-13s 云量=%3d dim=%.3f 盒内天体亮像素=%d（>0 判据） 整屏盒内净抬升≥8 级=%d ≥24 级=%d 盘心净抬升=%.1f",
				cls, pct, sky.SunDimOf(pct), n, soft8, hard24, center)
			if n <= 0 {
				t.Errorf("白天 %s 云量=%d：固定太阳盒内亮像素 = %d ⇒ 白天没有画太阳", cls, pct, n)
			}
			if center <= 0 {
				t.Errorf("白天 %s 云量=%d：盘心净抬升 %.1f ≤ 0 ⇒ 太阳在屏幕上没有把天空抬起来", cls, pct, center)
			}
			// 整屏可见性：净抬升 ≥8 级的像素必须存在。
			// 阈值取 8 级而不是 24 级：24 级是"硬圆盘"的口径（TestSunDiscEdge 用），
			// 灰蒙档在亮底（雪天基色 184）上只抬 23 级，是看得见的宽光、不是硬盘。
			if soft8 <= 0 {
				t.Errorf("白天 %s 云量=%d：整屏盒内净抬升 ≥8 级的像素 = %d ⇒ 太阳在屏幕上不可见", cls, pct, soft8)
			}
		}
		// 夜间：9 档都必须 0
		layer := celestialLayerCPU(w, h, cls, false, 0)
		n := 0
		for y := box.y0; y < box.y1; y++ {
			for x := box.x0; x < box.x1; x++ {
				i := layer.PixOffset(x, y)
				if layer.Pix[i+3] > 0 && int(layer.Pix[i])+int(layer.Pix[i+1])+int(layer.Pix[i+2]) > 0 {
					n++
				}
			}
		}
		baked := sky.RenderSkyCPU(w, h, cls, false, nil, 0)
		base := sky.SkyBaseCPU(w, h, cls, false)
		starPx := 0
		for y := box.y0; y < box.y1; y++ {
			for x := box.x0; x < box.x1; x++ {
				if box.px(baked, x, y)-box.px(base, x, y) >= 24 {
					starPx++
				}
			}
		}
		t.Logf("夜间 %-13s 盒内天体亮像素=%d（=0 判据） 整张烘焙盒内≥24 级像素=%d（星星污染量，只打印）", cls, n, starPx)
		if n != 0 {
			t.Errorf("夜间 %s：固定太阳盒内亮像素 = %d，应为 0（夜间不画太阳）", cls, n)
		}
	}
}

// TestSunDimMonotonic 云量 0/30/60/85/100 五档的盘心亮度：单调不增，且 100 档 > 0。
// 同一档天空（Clear）+ 同一取样点（盘心），只变云量 ⇒ 变的就是 dim。
// 两个量都打印：盘心绝对亮度、盘心净抬升（烘焙 - 同位置天空基色）。
// 100 档的净抬升 >0 = "灰蒙档留亮斑"（不许全黑）。
func TestSunDimMonotonic(t *testing.T) {
	w, h := sundimProbeSize()
	box := newSunBox(w, h)
	base := sky.SkyBaseCPU(w, h, effect.Clear, true)
	pcts := []int{0, 30, 60, 85, 100}
	type row struct {
		pct        int
		dim        float64
		centerLum  float64
		centerLift float64
	}
	var rows []row
	for _, pct := range pcts {
		baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, pct)
		lum := box.px(baked, box.cx, box.cy)
		rows = append(rows, row{
			pct: pct, dim: sky.SunDimOf(pct),
			centerLum:  lum,
			centerLift: lum - box.px(base, box.cx, box.cy),
		})
	}
	for _, r := range rows {
		t.Logf("云量=%3d dim=%.3f 盘心亮度=%.1f 盘心净抬升=%.1f 盘半径=%.1f px（rc=%.1f×(0.75+0.25·dim)）",
			r.pct, r.dim, r.centerLum, r.centerLift, sky.SunDiscRcFrac*(sky.SunRcDimMin+sky.SunRcDimSlope*r.dim)*float64(w), box.rc)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].centerLum > rows[i-1].centerLum+0.5 {
			t.Errorf("盘心亮度在云量 %d→%d 处上升 %.1f 级 ⇒ 云量越多太阳越亮，单调性反了（%.1f→%.1f）",
				rows[i-1].pct, rows[i].pct, rows[i].centerLum-rows[i-1].centerLum, rows[i-1].centerLum, rows[i].centerLum)
		}
		if rows[i].centerLift > rows[i-1].centerLift+0.5 {
			t.Errorf("盘心净抬升在云量 %d→%d 处上升 %.1f 级 ⇒ 单调性反了（%.1f→%.1f）",
				rows[i-1].pct, rows[i].pct, rows[i].centerLift-rows[i-1].centerLift, rows[i-1].centerLift, rows[i].centerLift)
		}
	}
	last := rows[len(rows)-1]
	if last.pct != 100 {
		t.Fatalf("末档云量 = %d，期望 100 ⇒ 测试输入给错", last.pct)
	}
	if last.centerLift <= 0 {
		t.Errorf("云量 100 的盘心净抬升 = %.1f ≤ 0 ⇒ 灰蒙档全黑（应留亮斑）", last.centerLift)
	}
	if rows[0].centerLift <= last.centerLift {
		t.Errorf("云量 0 的净抬升 %.1f ≤ 云量 100 的 %.1f ⇒ 云量没有把太阳调暗，判据空转",
			rows[0].centerLift, last.centerLift)
	}
}

// sunBakePredictedLum 按烘焙用的解析式预测一个盘外点的亮度（天空基色 + 单一连续剖面 + 盘 + 泛光 + cos² 窗）。
// 用途：量"圈边剩余"。烘焙里的剖面/盘/泛光/窗四条都是既有解析项，
// 把它们逐像素重算一遍就得到"没有提亮圈时该有多亮"，实测值减掉它就是圈边剩余。
// 基色取 skyBaseCPU（屏幕上真实存在的那张），不复制渐变公式。
// 口径同步：
// 剖面 = A·exp(−r/L) + Acore·exp(−r/Lc) + U，写入 = 逐因子逐通道留头合成
// out = base + add·(1 − base/255)（screen 型，见 bakeSunAnalytic 与 screenKeep）。
// 星芒贴图与宽光贴图在屏内的加性贡献已停用 ⇒ 预测器里也不再叠星芒那一条。
// 盘可见度：调用方给的 disc 就是"盘在不在"，出厂口径下 disc=true 时 vis=1
// （sunDiscVisibility 在云量 0 处取 1），故预测器不单独乘 vis。
func sunBakePredictedLum(base *image.NRGBA, x, y int, cx, cy, rc, dim float64, w int, disc bool) float64 {
	dx, dy := float64(x)-cx, float64(y)-cy
	d := math.Hypot(dx, dy)
	L := sky.SunMainLFrac * float64(w)
	Lc := sky.SunCoreExpLFrac * float64(w)
	rad := 1.5 * float64(w)
	ul := d / L
	uc := d / Lc
	main := sky.SunMainAmp * dim * sky.SunSkyScale * math.Exp(-ul)
	U := sky.SunSkyUniform * dim * sky.SunSkyScale
	core, discLv, liftLv, bloomLv := 0.0, 0.0, 0.0, 0.0
	if d <= rad {
		if disc {
			core = sky.SunCoreExpAmp * dim * math.Exp(-uc)
		}
		if disc && d <= sky.SunLiftR2Frac*rc {
			discLv = sky.SunDiscLevel * sky.SunDiscProfile(d, rc)
			liftLv = sky.SunSkyLift(d, rc)
		}
		if disc && d <= sky.SunBloomRcFrac*rc {
			bloomLv = sky.SunBloomAt(dx, dy, rc) * dim
		}
	}
	w2 := sky.Wl2(d, rad)
	k := math.Exp(-ul)
	if disc && d <= rc {
		k = 1.0
	}
	col := color.NRGBA{
		R: clamp8(255.0),
		G: clamp8(232.0 + 20.0*k),
		B: clamp8(176.0 + 62.0*k),
		A: 0xff,
	}
	// 与 bakeSunAnalytic 逐字同一条链：因子表一项不多一项不少，逐因子连乘留头乘子
	kR, kG, kB := 1.0, 1.0, 1.0
	kR, kG, kB = sky.ScreenKeep(kR, kG, kB, main, col)
	kR, kG, kB = sky.ScreenKeep(kR, kG, kB, U, col)
	if d <= rad {
		kR, kG, kB = sky.ScreenKeep(kR, kG, kB, main*w2, col)
		kR, kG, kB = sky.ScreenKeep(kR, kG, kB, core*w2, col)
		kR, kG, kB = sky.ScreenKeep(kR, kG, kB, discLv*w2, col)
		kR, kG, kB = sky.ScreenKeep(kR, kG, kB, liftLv*w2, col)
		kR, kG, kB = sky.ScreenKeep(kR, kG, kB, bloomLv*w2, col)
	}
	i := base.PixOffset(x, y)
	// 逐通道算（与烘焙的写入同一口径），最后取三通道均值
	ch := func(c0 uint8, keep float64) float64 {
		v := 255.0 - (255.0-float64(c0))*keep
		if v > 255.0 {
			v = 255.0
		}
		if v < 0 {
			v = 0
		}
		return v
	}
	r, g, b := ch(base.Pix[i], kR), ch(base.Pix[i+1], kG), ch(base.Pix[i+2], kB)
	return (r + g + b) / 3.0
}

// TestNoSunLiftRing 盘缘外 4rc 与 6rc 两点的圈边剩余 ≤3 级（提亮圈归零）。
// 口径说明（与规格字面的差别写在这里）：
// 盘外亮度本身在 4rc→6rc 之间必然差几十级 —— Lorentz 光斑 A/(1+(r/L)²) 就是
// 一条单调衰减的铺光（铺光半径 L=0.18 倍烘焙宽），这不是"圈"。
// "圈"= 提亮层在 [1rc, 3rc] 平台上抬 12 级、3.8rc 处收口造成的**局部结构**，
// 所以判据量的是"实测亮度 - 光斑/泛光/窗三项预测"的差（圈边剩余），
// 两点各自的剩余都 ≤3 级、两点之差也 ≤3 级才算归零。
// 原始两点亮度差一并打印，便于对表。
// 提亮函数本身也逐点量：sunSkyLift 在 4rc 与 6rc 必须为 0。
func TestNoSunLiftRing(t *testing.T) {
	w, h := sundimProbeSize()
	box := newSunBox(w, h)
	baked := sky.RenderSkyCPU(w, h, effect.Clear, true, nil, 0)
	base := sky.SkyBaseCPU(w, h, effect.Clear, true)
	rc := box.rc
	dim := sky.SunDimOf(0)
	type pt struct {
		k    float64
		x, y int
		got  float64
		pred float64
		lift float64
	}
	var pts []pt
	for _, k := range []float64{4.0, 6.0} {
		x := box.cx + int(math.Round(k*rc))
		y := box.cy
		if x >= w || y >= h {
			t.Fatalf("取样点 (%d,%d) 越出烘焙图 ⇒ 测试无意义", x, y)
		}
		pts = append(pts, pt{
			k: k, x: x, y: y,
			got:  box.px(baked, x, y),
			pred: sunBakePredictedLum(base, x, y, float64(box.cx), float64(box.cy), rc, dim, w, !sky.SunNoDisc(0)),
			lift: sky.SunSkyLift(k*rc, rc),
		})
	}
	for _, p := range pts {
		t.Logf("r=%.0frc (%d,%d) 实测亮度=%.1f 光斑+泛光预测=%.1f 圈边剩余=%.1f 提亮函数=%.2f",
			p.k, p.x, p.y, p.got, p.pred, p.got-p.pred, p.lift)
	}
	d0, d1 := pts[0].got-pts[0].pred, pts[1].got-pts[1].pred
	t.Logf("两点实测亮度差 = %.1f 级（铺光本身的衰减，不是圈）；圈边剩余差 = %.1f 级",
		math.Abs(pts[0].got-pts[1].got), math.Abs(d0-d1))
	if math.Abs(d0) > 3 || math.Abs(d1) > 3 {
		t.Errorf("圈边剩余 %.1f / %.1f 级 ⇒ 盘周提亮没有归零（4rc/6rc 两点）", d0, d1)
	}
	if math.Abs(d0-d1) > 3 {
		t.Errorf("4rc 与 6rc 的圈边剩余差 %.1f 级 > 3 级 ⇒ 盘外有局部亮边", math.Abs(d0-d1))
	}
	for _, p := range pts {
		if p.lift != 0 {
			t.Errorf("sky.SunSkyLift(%.0frc) = %.2f ≠ 0 ⇒ 提亮函数没有归零", p.k, p.lift)
		}
	}
	// 判据必须量到东西：两点实测亮度全黑说明烘焙没画东西，测不到圈
	if pts[0].got <= 1 || pts[1].got <= 1 {
		t.Errorf("盘外两点实测亮度 %.1f / %.1f ⇒ 盘外没有光，测不到圈边，测试无意义", pts[0].got, pts[1].got)
	}
}

// flashProbeFrame 把雷暴场推进到闪光峰值帧，返回该帧的离屏图与锚点。
// 同一场连渲两帧（中间只改 f.flash），两帧的差就是闪光层的贡献。
func flashProbeFrame(t *testing.T, win *headless.Window, f *Field, flash float64) *image.RGBA {
	t.Helper()
	f.SetFlash(flash)
	s := New()
	tt := 22.4
	hu := 77.0
	pp := 12.0
	ws := 5.6
	fc := "Tomorrow: 20°-25°C/Thunder"
	wt := &weather.Sample{
		Condition: "Thunderstorm", Temperature: &tt, WeatherCode: 95, IsDay: 1,
		Forecast: &fc, Location: "Hangzhou, China", Humidity: &hu,
		PrecipitationProbability: &pp, WindSpeed: &ws, CloudCoverPct: 90,
	}
	var ops op.Ops
	th := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
			Now:         th,
		}
		s.Layout(gtx, Frame{
			Now: th, Clock: DefaultClockSpec(), Weather: wt,
			Class: effect.Thunder, IsDay: true, Field: f,
			Location: "Hangzhou, China", Panel: DefaultPanelStyle(),
		})
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("离屏帧提交失败: %v", err)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

// flashPeakDelta 在闪电本体那块矩形里逐格求"亮帧 - 灭帧"的最大值（提亮层的峰值增量）。
// 取样步长 3 px（矩形 550×540 ⇒ 约 3.4 万格），峰值落在枝干上。
// 盒子 = 以落点为中心的 bolt 绘制矩形（口径见 drawBolt / boltRectPx），不随类缩放。
func flashPeakDelta(on, off *image.RGBA, ax, ay int) (float64, [2]int) {
	// 形状掩膜本体逐字迁到 ui/overlay.BoltGlowMask（贴图查询口 = ui 侧注入），
	// 取数口径不变（同一张 bolt1、同一幅面 ⇒ 同一块矩形）。
	e := overlay.BoltGlowMask(0, PortraitW, PortraitH, boltSpriteImg)
	halfW, halfH := 300.0, 300.0
	if e.Ok {
		halfW, halfH = e.Dw/2, e.Dh/2
	}
	best, bestAt := 0.0, [2]int{ax, ay}
	for y := int(math.Max(0, float64(ay)-halfH)); y < int(math.Min(float64(PortraitH), float64(ay)+halfH)); y += 3 {
		for x := int(math.Max(0, float64(ax)-halfW)); x < int(math.Min(float64(PortraitW), float64(ax)+halfW)); x += 3 {
			i := on.PixOffset(x, y)
			j := off.PixOffset(x, y)
			d := (float64(on.Pix[i])+float64(on.Pix[i+1])+float64(on.Pix[i+2]))/3 -
				(float64(off.Pix[j])+float64(off.Pix[j+1])+float64(off.Pix[j+2]))/3
			if d > best {
				best, bestAt = d, [2]int{x, y}
			}
		}
	}
	return best, bestAt
}

// TestFlashLocalized 闪光峰值帧：提亮层的峰值增量 ≥ 距锚点 0.5 屏宽处的 8 倍。
// 口径：同一个场连渲两帧，中间只把 f.flash 从峰值改成 0 ⇒ 两帧的逐像素差
// 就是闪光层的贡献（粒子、云带、文字在两帧里逐字相同）。
// 取样点 = 提亮层在闪电本体矩形里的峰值点（锚点增量一并打印做参照）
// 与沿水平方向距锚点 0.5 屏宽处（提亮掩膜半径 0.35w 之外）。
// 判据：峰值增量 ≥ 10 级（量到东西）且 ≥ 8 × 远处增量。
func TestFlashLocalized(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()

	f := NewField(effect.Thunder, true, 20261015)
	f.SetWeatherCode(95)
	step := time.Second / 15
	peak := 0.0
	anchorX, anchorY := 0.0, 0.0
	for k := 0; k < 15*60; k++ {
		f.Step(step, PortraitW, PortraitH)
		if f.flash > 0.999 {
			peak = f.flash
			anchorX, anchorY = f.boltX, f.boltY
			break
		}
	}
	if peak <= 0 {
		t.Fatalf("60 秒模拟内没跑到闪光峰值 ⇒ 测不到闪光，测试无意义")
	}
	on := flashProbeFrame(t, win, f, peak)
	off := flashProbeFrame(t, win, f, 0)

	ax := int(math.Round(anchorX * float64(PortraitW)))
	ay := int(math.Round(anchorY * float64(PortraitH)))
	farX := ax - PortraitW/2
	if farX < 0 {
		farX = ax + PortraitW/2
	}
	if farX < 0 || farX >= PortraitW {
		t.Fatalf("远处取样点 x=%d 越出屏宽 %d ⇒ 测试无意义", farX, PortraitW)
	}
	// 锚点取 5×5 邻域均值（闪电贴图的中心是锐利的，单点会踩在贴图上）
	avg := func(img *image.RGBA, x, y int) float64 {
		var s float64
		n := 0
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
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
	dAnchor := avg(on, ax, ay) - avg(off, ax, ay)
	dFar := avg(on, farX, ay) - avg(off, farX, ay)
	if dFar < 0 {
		dFar = -dFar
	}
	// 提亮层的峰值增量（在闪电本体那块矩形里逐格取最大差）。
	// 口径变更（写明理由，不是放宽）：提亮形状从圆罩换成闪电本体烘出的形状掩膜后，
	// 掩膜的最亮处 = 闪电本体的枝干，而锚点 = 贴图中心（= 落点）在闪电贴图里
	// 常常是空的（实测 bolt3 的贴图中心 21×21 邻域里 alpha>128 的像素 = 0）。
	// 于是"锚点那一个像素可以不被照亮"是新形状的正常结果，
	// 原判据（锚点增量 ≥10）量的是圆罩中心，在现形状下测不到东西。
	// 现判据量提亮层的峰值增量（同一帧对、同一取样口径），局域性倍率逐字不动。
	dPeak, peakAt := flashPeakDelta(on, off, ax, ay)
	t.Logf("锚点(%d,%d) 峰值帧=%.1f 熄灭帧=%.1f 增量=%.1f 级；提亮峰=%.1f 级 @相对落点(%d,%d)；远处(%d,%d) 峰值帧=%.1f 熄灭帧=%.1f 增量=%.1f 级；倍率=%.2f（判据 ≥8）",
		ax, ay, avg(on, ax, ay), avg(off, ax, ay), dAnchor, dPeak, peakAt[0]-ax, peakAt[1]-ay,
		farX, ay, avg(on, farX, ay), avg(off, farX, ay), dFar,
		func() float64 {
			if dFar <= 0 {
				return math.Inf(1)
			}
			return dPeak / dFar
		}())
	if dPeak < 10 {
		t.Errorf("提亮层峰值增量只有 %.1f 级 ⇒ 闪光没有把周围照亮，判据空转", dPeak)
	}
	if dPeak < 8*dFar {
		t.Errorf("提亮峰值增量 %.1f 级 < 8×远处增量 %.1f 级 ⇒ 闪光没有局域化（整屏白洗时倍率≈1）", dPeak, dFar)
	}
	// 锚点必须在提亮半径内、远处必须在半径外（口径自检）
	dn := math.Hypot(float64(farX-ax), 0) / float64(PortraitW)
	if dn < flashRadiusFrac {
		t.Errorf("远处取样点距锚点 %.3f 屏宽 < 半径 %.2f 屏宽 ⇒ 两点都在半径内，判据无意义", dn, flashRadiusFrac)
	}
}

// TestSkyCacheCloudKey 云量进烘焙缓存键的实测：
//
//	同尺寸同类同云量连取两次 ⇒ 第二次是命中（Builds 不涨）
//	云量 0 → 100 ⇒ 必须重建（Builds +1），且两张图的盘心亮度不同
//
// 键里没有云量时的现象：云量变了仍沿用旧图 ⇒ 太阳亮度停在旧云量上。
// 这条判据量的是"云量变化会不会换图"，不是公式。
func TestSkyCacheCloudKey(t *testing.T) {
	size := image.Pt(PortraitW, PortraitH)
	got := sky.SkyCacheGet()
	e0a := sky.SkyCached(size, effect.Clear, true, nil, 0)
	e0b := sky.SkyCached(size, effect.Clear, true, nil, 0)
	if e0a == nil || e0b == nil {
		t.Fatalf("sky.SkyCached 返回 nil ⇒ 测试无意义")
	}
	if e0a != e0b {
		t.Errorf("同云量连取两次换了图 ⇒ 键里有多余的抖动项")
	}
	after := sky.SkyCacheGet()
	buildsForSame := after.Builds - got.Builds
	if buildsForSame != 1 {
		t.Errorf("同云量两次取用重建了 %d 次，期望 1 次（第二次应命中）", buildsForSame)
	}
	e100 := sky.SkyCached(size, effect.Clear, true, nil, 100)
	after2 := sky.SkyCacheGet()
	if e100 == e0a {
		t.Errorf("云量 0→100 没有重建 ⇒ 缓存键里没有云量，太阳亮度会停在旧云量上")
	}
	l0 := sunLuma(e0a.Img, e0a.Img.Bounds().Dx()*72/100, e0a.Img.Bounds().Dy()*95/1000)
	l100 := sunLuma(e100.Img, e100.Img.Bounds().Dx()*72/100, e100.Img.Bounds().Dy()*95/1000)
	t.Logf("重建次数：同云量两次=%d，云量 0→100 后=%d；盘心亮度 云量0=%.1f 云量100=%.1f（烘焙图 %dx%d 与 %dx%d）",
		buildsForSame, after2.Builds-after.Builds, l0, l100,
		e0a.Img.Bounds().Dx(), e0a.Img.Bounds().Dy(), e100.Img.Bounds().Dx(), e100.Img.Bounds().Dy())
	if after2.Builds-after.Builds != 1 {
		t.Errorf("云量 0→100 重建了 %d 次，期望 1 次", after2.Builds-after.Builds)
	}
	if !(l100 < l0) {
		t.Errorf("云量 100 的盘心亮度 %.1f ≥ 云量 0 的 %.1f ⇒ 换到的图没有变暗", l100, l0)
	}
}

// TestBoltRateByCode 闪电频率按码分级：码 95/96/99 各跑 300 s 模拟，闪次落进分级区间。
// 判据区间：95 ⇒ 300/40–300/15 = 7.5–20 次；96 ⇒ 12–30 次；99 ⇒ 20–50 次。
// 每个码跑 3 个种子（逐种子打印闪次与平均间隔），逐种子都要落进区间。
// 随机源 = 场自己的确定性种子（NewField 的 rand.New(rand.NewSource(seed))），
// 判据可复现。
func TestBoltRateByCode(t *testing.T) {
	const secs = 300
	step := time.Second / 15
	// / C1：一帧 1/15 s 的 dt 只推进追赶上限 3 步 = 0.05 s 动画时间
	// （旧口径一帧 = dt 本身）。"300 秒模拟"量的是动画秒数 ⇒ 帧数按
	// 300 s ÷ (3×fixedStep) = 6000 帧换算，判据区间（按秒分级）逐字不变。
	framesPerSec := int(1.0 / (float64(fixedStepCatchup) * fixedStep.Seconds()))
	if framesPerSec != 20 {
		t.Fatalf("每秒帧数换算 = %d，期望 20（每帧 3×fixedStep = 0.05 s）", framesPerSec)
	}
	type tier struct {
		code         int
		lo, hi       float64
		gapLo, gapHi float64
	}
	cases := []tier{
		{95, 300.0 / 40.0, 300.0 / 15.0, 15.0, 40.0},
		{96, 12.0, 30.0, 10.0, 25.0},
		{99, 20.0, 50.0, 6.0, 15.0},
	}
	for _, c := range cases {
		lo, hi, ok := boltGapForCode(c.code)
		if !ok {
			t.Fatalf("码 %d 没有分级 ⇒ 测试无意义", c.code)
		}
		if lo != c.gapLo || hi != c.gapHi {
			t.Errorf("码 %d 的间隔档 = %.1f–%.1f s，期望 %.1f–%.1f s", c.code, lo, hi, c.gapLo, c.gapHi)
		}
		for _, seed := range []int64{20261015, 23, 11} {
			f := NewField(effect.Thunder, true, seed)
			f.SetWeatherCode(c.code)
			for k := 0; k < secs*framesPerSec; k++ {
				f.Step(step, PortraitW, PortraitH)
			}
			n := f.boltCount
			meanGap := math.NaN()
			gLo, gHi := math.Inf(1), 0.0
			if len(f.boltLog) >= 2 {
				var sum float64
				for i := 1; i < len(f.boltLog); i++ {
					g := f.boltLog[i] - f.boltLog[i-1]
					sum += g
					if g < gLo {
						gLo = g
					}
					if g > gHi {
						gHi = g
					}
				}
				meanGap = sum / float64(len(f.boltLog)-1)
			}
			t.Logf("码=%d seed=%d：%d 秒模拟 闪次=%d（判据 %.1f–%.1f）平均间隔=%.2f s 间隔范围=%.2f–%.2f s（档内 %.1f–%.1f s）",
				c.code, seed, secs, n, c.lo, c.hi, meanGap, gLo, gHi, c.gapLo, c.gapHi)
			if float64(n) < c.lo || float64(n) > c.hi {
				t.Errorf("码 %d seed=%d：300 秒闪 %d 次，越出 [%.1f, %.1f]", c.code, seed, n, c.lo, c.hi)
			}
			if !math.IsNaN(meanGap) && (meanGap < c.gapLo-1.0 || meanGap > c.gapHi+1.0) {
				t.Errorf("码 %d seed=%d：平均间隔 %.2f s 越出档内 %.1f–%.1f s（容差 1 s）", c.code, seed, meanGap, c.gapLo, c.gapHi)
			}
		}
	}
	// 非雷暴码：300 秒 0 闪
	for _, code := range []int{1, 2, 3, 61} {
		f := NewField(effect.Thunder, true, 20261015)
		f.SetWeatherCode(code)
		for k := 0; k < secs*framesPerSec; k++ {
			f.Step(step, PortraitW, PortraitH)
		}
		t.Logf("码=%d：300 秒闪 %d 次（判据 0）", code, f.boltCount)
		if f.boltCount != 0 {
			t.Errorf("码 %d：300 秒闪 %d 次，应为 0（非雷暴码不闪）", code, f.boltCount)
		}
	}
}

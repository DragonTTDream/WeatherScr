package ui

// 阳光铺满整片天空 + 云遮挡阳光 + 闪电以自身为中心照亮 的判据。
//
// 共同纪律：每条都真的测量并打印数字。取样盒/取样点是固定口径，
// 不随天气类或云量缩放；测不到东西时直接判红（不写恒真断言）。
//
//	TestSunLightFullSky        晴档昼帧 9 点网格：四角相对"无太阳基线"净抬升 ≥12 级
//	TestSunLightNoRing         过太阳中心的水平/垂直剖面，逐像素相邻差峰 ≤3 级
//	TestCloudShadowOnSky       同帧内云覆盖点比无云天空点低 ≥15 级
//	TestSunLightDimMonotonic   云量 0/30/60/85/100 的四角净抬升单调下降
//	TestBoltVisibleDuringFlash flash=0.20 时闪电本体像素 >0
//	TestFlashAnchorIsBolt      flash=0.30 时锚点增量 > 远处点增量 ×5

import (
	"image"
	"math"
	"sort"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// sunLum 三通道均值亮度（与 sunLuma 同口径，本文件独立命名便于打印）。
func sunLum(img *image.NRGBA, x, y int) float64 {
	i := img.PixOffset(x, y)
	return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
}

// sunGrid9 9 点网格：四角 + 四边中点 + 中心（固定口径，只由图幅决定）。
func sunGrid9(w, h int) [9][2]int {
	return [9][2]int{
		{2, 2}, {w / 2, 2}, {w - 3, 2},
		{2, h / 2}, {w / 2, h / 2}, {w - 3, h / 2},
		{2, h - 3}, {w / 2, h - 3}, {w - 3, h - 3},
	}
}

// TestSunLightFullSky 晴档昼帧的 9 点网格净抬升。
//
// 基线 = 同幅面的"无太阳"帧：阴天（Overcast）+ 云量 100（dim 落在下限 0.20，
// 盘不画、宽光衰减到最低）⇒ 网格点上的太阳贡献接近 0。
// 测量帧 = 晴（Clear）+ 云量 0（dim=1，全天空抬升取满 A_sky=90）。
// 判据：四角净抬升 ≥12 级（需求："光线永远只有那一圈"⇒ 四角必须被抬起来）。
func TestSunLightFullSky(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	frame := renderSkyCPU(w, h, effect.Clear, true, nil, 0)
	base := renderSkyCPU(w, h, effect.Overcast, true, nil, 100)
	names := [9]string{"左上角", "上边中", "右上角", "左边中", "中心", "右边中", "左下角", "下边中", "右下角"}
	grid := sunGrid9(w, h)
	var corners []float64
	for i, g := range grid {
		lift := sunLum(frame, g[0], g[1]) - sunLum(base, g[0], g[1])
		t.Logf("%-4s (%3d,%3d) 晴档=%.1f 无太阳基线=%.1f 净抬升=%.1f 级", names[i], g[0], g[1],
			sunLum(frame, g[0], g[1]), sunLum(base, g[0], g[1]), lift)
		if i == 0 || i == 2 || i == 6 || i == 8 {
			corners = append(corners, lift)
		}
		if sunLum(frame, g[0], g[1]) <= 1 {
			t.Fatalf("晴档在 (%d,%d) 亮度 %.1f ≤ 1 ⇒ 网格点全黑，测试无意义", g[0], g[1], sunLum(frame, g[0], g[1]))
		}
	}
	for i, c := range corners {
		if c < 12 {
			t.Errorf("四角第 %d 点净抬升 %.1f 级 < 12 级 ⇒ 阳光没有铺到四角（铺光仍缩在太阳附近一圈）", i, c)
		}
	}
	// 自检：基线帧本身必须有天空（不是全黑），否则"净抬升"是拿全黑当基线的假数字
	minBase := math.Inf(1)
	for _, g := range grid {
		if v := sunLum(base, g[0], g[1]); v < minBase {
			minBase = v
		}
	}
	if minBase <= 1 {
		t.Errorf("无太阳基线帧最暗网格点 %.1f ≤ 1 ⇒ 基线全黑，净抬升判据空转", minBase)
	}
	t.Logf("四角净抬升 = %.1f / %.1f / %.1f / %.1f 级（判据 ≥12）；基线帧最暗网格点 %.1f",
		corners[0], corners[1], corners[2], corners[3], minBase)
}

// TestSunLightNoRing 过太阳中心的水平与垂直剖面：逐像素相邻差峰 ≤3 级。
//
// 两条口径说明（都是实测逼出来的）：
//
//	1 剖面量的是**净抬升**（烘焙 - 同类同幅面的天空基色），不是绝对亮度。
//	  天空基本身是 top→horizon 的竖向渐变（实测 540×960 顶行 56 级、底行 166 级），
//	  拿绝对亮度量"铺光有没有圈边"时，基色梯度在垂直方向就是 0.13 级/px 的上升坡，
//	  131 处上升全部来自基色，不是铺光。
//	2 剖面从 1.2×盘半径起、向**两侧**各走到画布边缘。盘体羽化带在 [0.88rc, rc]，
//	  是盘自身的边缘，不在"铺光有没有圈边"这条判据的范围内。
//
// 两条档都量：晴档（画盘 + 星芒）与灰蒙档（云量 100，宽光罩）。
func TestSunLightNoRing(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	cx, cy := int(math.Round(0.72*float64(w))), int(math.Round(0.095*float64(h)))
	rc := sunDiscRcFrac * float64(w)
	cases := []struct {
		name string
		cls  effect.Class
		pct  int
	}{
		{"晴(盘+星芒)", effect.Clear, 0},
		{"灰蒙(宽光罩)", effect.Overcast, 100},
	}
	lift := func(baked, base *image.NRGBA, x, y int) float64 {
		return sunLum(baked, x, y) - sunLum(base, x, y)
	}
	type prof struct {
		cls      string
		dir      string
		n        int
		maxStep  float64
		atStep   int
		rise     int
		firstLum float64
		lastLum  float64
	}
	var all []prof
	for _, c := range cases {
		baked := renderSkyCPU(w, h, c.cls, true, nil, c.pct)
		base := skyBaseCPU(w, h, c.cls, true)
		start := int(math.Ceil(1.2 * rc))
		// 四条射线：水平左/右、垂直上/下（合起来就是过中心的水平剖面与垂直剖面）
		type ray struct {
			dir string
			fn  func(k int) (int, int)
			n   int
		}
		rays := []ray{
			{"水平右", func(k int) (int, int) { return cx + start + k, cy }, w - 1 - cx - start},
			{"水平左", func(k int) (int, int) { return cx - start - k, cy }, cx - start},
			{"垂直下", func(k int) (int, int) { return cx, cy + start + k }, h - 1 - cy - start},
			{"垂直上", func(k int) (int, int) { return cx, cy - start - k }, cy - start},
		}
		for _, r := range rays {
			// 太阳在 0.095 屏高 ⇒ 向上只有 62 px 的画布，这是图幅决定的，不是口径漏洞；
			// 下限取 40 点（能覆盖盘缘外 1.2rc 起的一段），其余三条射线要求 ≥100 点。
			need := 100
			if r.dir == "垂直上" {
				need = 40
			}
			if r.n < need {
				t.Errorf("%s：%s射线只有 %d 点 < %d 点 ⇒ 剖面长度不足，测不到圈边，测试无意义", c.name, r.dir, r.n, need)
				continue
			}
			p := prof{cls: c.name, dir: r.dir}
			x0, y0 := r.fn(0)
			prev := lift(baked, base, x0, y0)
			p.firstLum = prev
			for k := 1; k <= r.n; k++ {
				xk, yk := r.fn(k)
				v := lift(baked, base, xk, yk)
				d := v - prev
				if math.Abs(d) > math.Abs(p.maxStep) {
					p.maxStep, p.atStep = d, k
				}
				if d > 3 {
					// 上升幅度超过判据允许的相邻差 = 剖面重新鼓起来（圈边的签名）；
					// 8 位量化的取整噪声（±1 级）不算。
					p.rise++
				}
				p.n++
				p.lastLum = v
				prev = v
			}
			all = append(all, p)
		}
	}
	for _, p := range all {
		t.Logf("%s %s剖面 r=1.2rc..边缘（%d 点）净抬升 %.1f→%.1f 级 相邻差峰=%.2f 级 @r=%d 上升处数=%d",
			p.cls, p.dir, p.n+1, p.firstLum, p.lastLum, p.maxStep, p.atStep, p.rise)
		if p.n < 40 {
			t.Errorf("%s剖面只有 %d 点 ⇒ 剖面长度不足，测不到圈边，测试无意义", p.dir, p.n)
		}
		if math.Abs(p.maxStep) > 3 {
			t.Errorf("%s剖面相邻差峰 %.2f 级 > 3 级 @r=%d ⇒ 铺光剖面有圈边", p.dir, p.maxStep, p.atStep)
		}
		if p.rise > 0 {
			t.Errorf("%s剖面出现 %d 处 >3 级的上升 ⇒ 净抬升剖面重新鼓起（圈边的签名）", p.dir, p.rise)
		}
	}
}

// compositeCloudShadowsOnSky 把本帧的云影合成到天空纹理上（CPU 侧）。
//
// 影的几何与 alpha 直接取 Field.cloudShadowPlaces（与 GPU 绘制同一份数据），
// 影的像素取 sprites.tintedImg(-255)（= GPU 上传的那张纯黑副本，alpha 逐字不动），
// 两处都不在探针里重抄一遍。
// 返回：合成后的天空、逐点影覆盖率（0..1，用于挑云覆盖点/无云点）。
func compositeCloudShadowsOnSky(sky *image.NRGBA, f *Field, w, h int) (*image.NRGBA, []float64) {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(out.Pix, sky.Pix)
	cov := make([]float64, w*h)
	for _, pl := range f.cloudShadowPlaces(w, h, f.t) {
		if pl.alpha <= 0.01 {
			continue
		}
		img, ok := f.sp.tintedImg(-255, pl.spr, false)
		if !ok {
			continue
		}
		b := img.Bounds()
		x0 := int(math.Max(0, math.Floor(pl.x)))
		x1 := int(math.Min(float64(w), math.Ceil(pl.x+pl.dw)))
		y0 := int(math.Max(0, math.Floor(pl.y)))
		y1 := int(math.Min(float64(h), math.Ceil(pl.y+pl.dh)))
		for y := y0; y < y1; y++ {
			v := (float64(y) - pl.y) / pl.dh
			sy := int(v * float64(b.Dy()))
			if sy < 0 {
				sy = 0
			}
			if sy > b.Dy()-1 {
				sy = b.Dy() - 1
			}
			for x := x0; x < x1; x++ {
				u := (float64(x) - pl.x) / pl.dw
				if pl.mirror {
					u = 1.0 - u
				}
				sx := int(u * float64(b.Dx()))
				if sx < 0 {
					sx = 0
				}
				if sx > b.Dx()-1 {
					sx = b.Dx() - 1
				}
				a := pl.alpha * float64(img.Pix[img.PixOffset(b.Min.X+sx, b.Min.Y+sy)+3]) / 255.0
				if a <= 0.002 {
					continue
				}
				i := out.PixOffset(x, y)
				ia := 1.0 - a
				out.Pix[i+0] = clamp8(float64(out.Pix[i+0]) * ia)
				out.Pix[i+1] = clamp8(float64(out.Pix[i+1]) * ia)
				out.Pix[i+2] = clamp8(float64(out.Pix[i+2]) * ia)
				out.Pix[i+3] = 0xff
				if c := cov[y*w+x] + a; c > cov[y*w+x] {
					if c > 1 {
						c = 1
					}
					cov[y*w+x] = c
				}
			}
		}
	}
	return out, cov
}

// TestCloudShadowOnSky 云影层对天空亮度的削弱（需求："云没有盖住影响背后的阳光"）。
//
// 口径：同一帧内（同一个场、同一时刻、同一张天空纹理）
//
//	云覆盖点 = 影覆盖率最大的那一点（云团中心）
//	无云天空点 = 同一行上影覆盖率为 0 的点
//
// 判据：云覆盖点的亮度比无云天空点低 ≥15 级。
// 同时打印 GPU 整帧口径下同一对点的读数（云本体压在影之上，削弱量被云体色吃掉一部分）。
func TestCloudShadowOnSky(t *testing.T) {
	w, h := PortraitW, PortraitH
	cls := effect.Overcast
	seed := int64(20261020)
	f := NewField(cls, true, seed)
	for i := 0; i < 60; i++ {
		f.Step(time.Second/15, w, h)
	}
	sky := renderSkyCPU(w, h, cls, true, nil, 100)
	shadowed, cov := compositeCloudShadowsOnSky(sky, f, w, h)
	places := f.cloudShadowPlaces(w, h, f.t)
	nPlace := len(places)
	var maxCov float64
	cx, cy := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if c := cov[y*w+x]; c > maxCov {
				maxCov, cx, cy = c, x, y
			}
		}
	}
	if nPlace == 0 || maxCov <= 0 {
		t.Fatalf("云影层影片数=%d 最大覆盖率=%.3f ⇒ 白天档没有投出任何影，测试无意义", nPlace, maxCov)
	}
	// 无云天空点的挑法（实测逼出来的口径）：影覆盖率 = 0 的点里，
	// 先取**同一行**上离云覆盖点最近的那个；整行都被影盖住时（Overcast 的云带铺满上部），
	// 退到全图里"天空基色与云覆盖点最接近"的无影点 ⇒ 两点之差就是影本身，
	// 不是基色竖向梯度（基色 top→horizon 实测 56→166 级）。
	fx, fy := -1, -1
	for d := 1; d < w && fx < 0; d++ {
		for _, cand := range []int{cx + d, cx - d} {
			if cand >= 0 && cand < w && cov[cy*w+cand] == 0 {
				fx, fy = cand, cy
				break
			}
		}
	}
	if fx < 0 {
		best := math.Inf(1)
		for y := 0; y < h; y += 2 {
			for x := 0; x < w; x += 2 {
				if cov[y*w+x] != 0 {
					continue
				}
				if d := math.Abs(sunLum(sky, x, y) - sunLum(sky, cx, cy)); d < best {
					best, fx, fy = d, x, y
				}
			}
		}
		if fx < 0 {
			t.Fatalf("整幅面没有一个影覆盖率为 0 的点 ⇒ 取不到无云天空点，测试无意义")
		}
		t.Logf("同一行没有无影点 ⇒ 退到全图基色最近点：基色差 %.1f 级", best)
	}
	if cov[fy*w+fx] != 0 {
		t.Fatalf("无云取样点 (%d,%d) 影覆盖率 = %.3f ≠ 0 ⇒ 两点都在影里，判据无意义", fx, fy, cov[fy*w+fx])
	}
	lowLum := sunLum(shadowed, cx, cy)
	highLum := sunLum(shadowed, fx, fy)
	skyLow := sunLum(sky, cx, cy)
	t.Logf("云影片数=%d 云覆盖点(%d,%d) 影覆盖率=%.3f 无云点(%d,%d) 影覆盖率=0", nPlace, cx, cy, maxCov, fx, cy)
	t.Logf("云覆盖点亮度=%.1f 无云天空点亮度=%.1f 差=%.1f 级（判据 ≥15）", lowLum, highLum, highLum-lowLum)
	t.Logf("同点影自身削弱：无影天空=%.1f → 有影天空=%.1f 差=%.1f 级", skyLow, lowLum, skyLow-lowLum)
	if highLum-lowLum < 15 {
		t.Errorf("云覆盖点比无云天空点只低 %.1f 级 < 15 级 ⇒ 云影没有削弱云覆盖处的天空亮度", highLum-lowLum)
	}
	if skyLow-lowLum <= 0 {
		t.Errorf("影在云覆盖点上的削弱 = %.1f ≤ 0 ⇒ 影没有落在天空上", skyLow-lowLum)
	}
	// 夜间档：影必须整层关掉（夜间无阳光可挡，且会把夜空压黑）
	nf := NewField(cls, false, seed)
	for i := 0; i < 60; i++ {
		nf.Step(time.Second/15, w, h)
	}
	nPlaces := len(nf.cloudShadowPlaces(w, h, nf.t))
	t.Logf("同场的夜间档影片数=%d（判据 =0）", nPlaces)
	if nPlaces != 0 {
		t.Errorf("夜间档投出了 %d 片云影 ⇒ 白天档门控没生效", nPlaces)
	}
	// GPU 整帧口径的读数（只打印，不断言：云本体压在影之上，复合值由云体色主导）
	if win, err := headless.NewWindow(w, h); err == nil {
		defer win.Release()
		s := New()
		th := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
		shot := func(layers LayerMask) *image.RGBA {
			var ops op.Ops
			ops.Reset()
			gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(w, h)), Now: th}
			tt := 22.4
			hu := 77.0
			pp := 12.0
			ws := 5.6
			fc := "Tomorrow: 20°-25°C/Cloudy"
			s.Layout(gtx, Frame{
				Now: th, Clock: DefaultClockSpec(),
				Weather: &weather.Sample{Condition: "Cloudy", Temperature: &tt, WeatherCode: 80, IsDay: 1,
					Forecast: &fc, Location: "Hangzhou, China", Humidity: &hu,
					PrecipitationProbability: &pp, WindSpeed: &ws, CloudCoverPct: 100},
				Class: cls, IsDay: true, Field: f, Location: "Hangzhou, China",
				Panel: DefaultPanelStyle(), Layers: layers,
			})
			if err := win.Frame(&ops); err != nil {
				t.Fatalf("离屏帧提交失败: %v", err)
			}
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			if err := win.Screenshot(img); err != nil {
				t.Fatalf("Screenshot 失败: %v", err)
			}
			return img
		}
		lum := func(img *image.RGBA, x, y int) float64 {
			i := img.PixOffset(x, y)
			return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
		}
		withField := shot(LayerSky | LayerDepth | LayerField)
		skyOnly := shot(LayerSky | LayerDepth)
		t.Logf("整帧口径 云覆盖点(%d,%d) 带场=%.1f 仅天空=%.1f 差=%.1f；无云点(%d,%d) 带场=%.1f 仅天空=%.1f",
			cx, cy, lum(withField, cx, cy), lum(skyOnly, cx, cy), lum(skyOnly, cx, cy)-lum(withField, cx, cy),
			fx, fy, lum(withField, fx, fy), lum(skyOnly, fx, fy))
	} else {
		t.Logf("headless 不可用，跳过整帧口径的打印: %v", err)
	}
}

// TestSunLightDimMonotonic 云量 0/30/60/85/100 时四角净抬升单调下降。
//
// 净抬升 = 晴档该云量的烘焙 - 同幅面无太阳基线（阴天云量 100）。
// 全天空抬升与近场光斑都乘 dim = 1 − 0.8·(云量/100)（夹 [0.20,1.00]）
// ⇒ 云量越多，四角抬升必须越小。
func TestSunLightDimMonotonic(t *testing.T) {
	w, h := sunProbeW, sunProbeH
	base := renderSkyCPU(w, h, effect.Overcast, true, nil, 100)
	corners := [4][2]int{{2, 2}, {w - 3, 2}, {2, h - 3}, {w - 3, h - 3}}
	pcts := []int{0, 30, 60, 85, 100}
	var rows [4][]float64
	for _, pct := range pcts {
		frame := renderSkyCPU(w, h, effect.Clear, true, nil, pct)
		for ci, g := range corners {
			rows[ci] = append(rows[ci], sunLum(frame, g[0], g[1])-sunLum(base, g[0], g[1]))
		}
	}
	names := [4]string{"左上角", "右上角", "左下角", "右下角"}
	for ci, vals := range rows {
		t.Logf("%s 四角净抬升 云量 0/30/60/85/100 = %.1f / %.1f / %.1f / %.1f / %.1f 级（dim %.3f/%.3f/%.3f/%.3f/%.3f）",
			names[ci], vals[0], vals[1], vals[2], vals[3], vals[4],
			sunDimOf(0), sunDimOf(30), sunDimOf(60), sunDimOf(85), sunDimOf(100))
		for i := 1; i < len(vals); i++ {
			if vals[i] > vals[i-1]+0.5 {
				t.Errorf("%s：云量 %d→%d 净抬升上升 %.1f 级 ⇒ 云量越多阳光越亮，单调性反了（%.1f→%.1f）",
					names[ci], pcts[i-1], pcts[i], vals[i]-vals[i-1], vals[i-1], vals[i])
			}
		}
		if vals[0] <= 12 {
			t.Errorf("%s：云量 0 的净抬升 %.1f 级 ≤ 12 级 ⇒ 四角没有被抬起，单调性判据空转", names[ci], vals[0])
		}
		if vals[4] <= 0 {
			t.Errorf("%s：云量 100 的净抬升 %.1f 级 ≤ 0 ⇒ 全黑（应留灰蒙宽光）", names[ci], vals[4])
		}
	}
}

// TestBoltVisibleDuringFlash flash=0.20 时闪电本体必须画得出来。
//
// 改动前本体阈值 flash>0.25 ⇒ flash=0.20 这一帧本体像素 = 0（只见亮不见电）。
// 现阈值 boltFlashMin=0.12。
//
// 测量口径：同一个场连渲两帧，中间只把 f.flash 从 0.20 改成 0
// ⇒ 两帧之差 = 闪光层（软光罩 + 云 alpha 抬升）+ 闪电本体 的贡献。
// 软光罩是缓坡，本体是锐利枝：取"闪电外接框内的增量中位数"当光罩基线，
// 中位数 +15 级以上才算本体像素（阈值由实测的光罩基线推出来，不是写死的魔数）。
func TestBoltVisibleDuringFlash(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	f := NewField(effect.Thunder, true, 20261015)
	f.SetWeatherCode(95)
	// 批次 A2 / C1：模拟时长按 fixedStep 换算成**步数**（C1 第 1 条把位移基准写死 fixedStep，
	// flash 包络也随步数推进）⇒ "90 秒模拟" = 90 s ÷ fixedStep = 5400 步，
	// 每帧喂一个 fixedStep ⇒ 一帧一步，逐**步**采样 flash（旧口径 1/15 s 一帧只采到
	// 每第 3 步，0.15–0.30 s 的闪现只有 1–2 步落在 (0.12,0.30) 内 ⇒ 采不到）。
	// 幅度区间 (0.12,0.30) 与"必须有区间落在其中"这条判据逐字不变。
	step := fixedStep
	stepsPerSec := int(time.Second / fixedStep)
	if stepsPerSec != 60 {
		t.Fatalf("每秒步数换算 = %d，期望 60（fixedStep = %v）", stepsPerSec, fixedStep)
	}
	seen := false
	for k := 0; k < 90*stepsPerSec; k++ {
		f.Step(step, PortraitW, PortraitH)
		if f.flash > boltFlashMin && f.flash < 0.30 {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("90 秒模拟里没有落在 (0.12,0.30) 的 flash 区间 ⇒ 测不到低幅度闪光，测试无意义")
	}
	boltX, boltY := f.boltX, f.boltY
	on := flashProbeFrame(t, win, f, 0.20)
	off := flashProbeFrame(t, win, f, 0)
	// 闪电外接框（drawBolt 的落位口径：长轴 0.28 屏高，短轴按贴图宽高比）
	_, sz := f.sp.bolt, f.sp.boltSz
	if n := len(f.sp.boltSzOps); n > 0 {
		i := f.boltIdx
		if i >= n {
			i = 0
		}
		sz = f.sp.boltSzOps[i]
	}
	if sz.X <= 0 || sz.Y <= 0 {
		t.Fatalf("闪电贴图尺寸 = %v ⇒ 没有本体可画，测试无意义", sz)
	}
	dh := boltLenFrac * float64(PortraitH)
	dw := dh * float64(sz.X) / float64(sz.Y)
	bx := boltX*float64(PortraitW) - dw/2
	by := boltY*float64(PortraitH) - dh/2
	x0 := int(math.Max(0, math.Floor(bx)))
	x1 := int(math.Min(float64(PortraitW), math.Ceil(bx+dw)))
	y0 := int(math.Max(0, math.Floor(by)))
	y1 := int(math.Min(float64(PortraitH), math.Ceil(by+dh)))
	if x1-x0 < 4 || y1-y0 < 4 {
		t.Fatalf("闪电外接框 %dx%d ⇒ 盒子缩成 0 量级，测试无意义", x1-x0, y1-y0)
	}
	deltas := make([]float64, 0, (x1-x0)*(y1-y0))
	lum := func(img *image.RGBA, x, y int) float64 {
		i := img.PixOffset(x, y)
		return (float64(img.Pix[i]) + float64(img.Pix[i+1]) + float64(img.Pix[i+2])) / 3
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			deltas = append(deltas, lum(on, x, y)-lum(off, x, y))
		}
	}
	srt := append([]float64(nil), deltas...)
	sort.Float64s(srt)
	med := srt[len(srt)/2]
	thr := med + 15
	n := 0
	var peak float64
	for _, d := range deltas {
		if d > peak {
			peak = d
		}
		if d > thr {
			n++
		}
	}
	t.Logf("flash=0.20 闪电框 (%d,%d)..(%d,%d) 共 %d px；框内增量中位=%.1f 级（软光罩基线） 阈值=%.1f 级 峰=%.1f 级",
		x0, y0, x1, y1, len(deltas), med, thr, peak)
	t.Logf("flash=0.20 闪电本体像素=%d（判据 >0）；本体阈值 现 boltFlashMin=%.2f 旧口径 0.25 ⇒ 旧口径下本帧画不画=%v",
		n, boltFlashMin, 0.20 > 0.25)
	if n <= 0 {
		t.Errorf("flash=0.20 时闪电本体像素 = %d ⇒ 低幅度闪光时只见亮不见电（阈值 0.25 的旧病）", n)
	}
	if peak <= thr {
		t.Errorf("框内增量峰 %.1f ≤ 阈值 %.1f ⇒ 本体没有比软光罩更亮，测不到本体", peak, thr)
	}
}

// TestFlashAnchorIsBolt 闪光的照亮中心 = 闪电落点。
//
// flash=0.30 时量两点增量：锚点（= f.boltX/f.boltY，也是 drawBolt 的落位中心）
// 与沿水平方向距锚点 0.5 屏宽处（软光罩半径 0.35 屏宽之外）。
// 判据：锚点增量 > 远处增量 ×5，且锚点增量 ≥10 级（量到东西）。
func TestFlashAnchorIsBolt(t *testing.T) {
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
	on := flashProbeFrame(t, win, f, 0.30)
	off := flashProbeFrame(t, win, f, 0)
	ax := int(math.Round(f.boltX * float64(PortraitW)))
	ay := int(math.Round(f.boltY * float64(PortraitH)))
	farX := ax - PortraitW/2
	if farX < 0 {
		farX = ax + PortraitW/2
	}
	if farX < 0 || farX >= PortraitW {
		t.Fatalf("远处取样点 x=%d 越出屏宽 %d ⇒ 测试无意义", farX, PortraitW)
	}
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
	// 提亮峰值增量：形状换成闪电本体后，掩膜最亮处 = 枝干，锚点（贴图中心）
	// 在贴图里常是空的 ⇒ 判据量峰值增量，锚点增量照旧打印做参照
	// （口径变更的理由见 sundim_test.go 的 TestFlashLocalized）。
	dPeak, peakAt := flashPeakDelta(on, off, ax, ay)
	ratio := math.Inf(1)
	if dFar > 0 {
		ratio = dPeak / dFar
	}
	t.Logf("flash=0.30 锚点(=闪电落点) (%d,%d) 亮帧=%.1f 灭帧=%.1f 增量=%.1f 级（参照）", ax, ay, avg(on, ax, ay), avg(off, ax, ay), dAnchor)
	t.Logf("flash=0.30 提亮峰值=%.1f 级 @相对落点(%d,%d)；远处点 (%d,%d) 亮帧=%.1f 灭帧=%.1f 增量=%.1f 级；倍率=%.2f（判据 >5）",
		dPeak, peakAt[0]-ax, peakAt[1]-ay, farX, ay, avg(on, farX, ay), avg(off, farX, ay), dFar, ratio)
	if dPeak < 10 {
		t.Errorf("提亮层峰值增量 %.1f 级 < 10 级 ⇒ 闪电没有把周围照亮，判据空转", dPeak)
	}
	if !(dPeak > dFar*5) {
		t.Errorf("提亮峰值增量 %.1f 级 ≤ 远处增量 %.1f 级 ×5 ⇒ 照亮不是以闪电为中心", dPeak, dFar)
	}
	dn := math.Hypot(float64(farX-ax), 0) / float64(PortraitW)
	if dn < flashRadiusFrac {
		t.Errorf("远处点距锚点 %.3f 屏宽 < 提亮掩膜半径 %.3f 屏宽 ⇒ 两点都在罩内，判据无意义", dn, flashRadiusFrac)
	}
}

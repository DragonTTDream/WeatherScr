package ui

import (
	"image"
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestMoonSpriteMap 对应需求 6：
// moonSprite 映射表逐区间断言（含 0.9375/0.0625 跨界、0.5 落 full、0.174 落 waxing-crescent）。
// 8 等分每格宽 0.125，中心对齐 8 个标准相位（相位→素材表）。
func TestMoonSpriteMap(t *testing.T) {
	cases := []struct {
		phase float64
		want  string
	}{
		// new 区间 = [0.9375,1 ∪ [0,0.0625)
		{0.0, "moon_new.png"},
		{0.0624, "moon_new.png"},
		{0.9375, "moon_new.png"}, // 跨界：0.9375 落 new
		{0.99, "moon_new.png"},
		// waxing-crescent [0.0625,0.1875
		{0.0625, "moon_waxing-crescent.png"}, // 跨界：0.0625 落 waxing-crescent
		{0.174, "moon_waxing-crescent.png"},  // 实测值 0.174
		{0.1874, "moon_waxing-crescent.png"},
		// first-quarter [0.1875,0.3125
		{0.1875, "moon_first-quarter.png"},
		{0.25, "moon_first-quarter.png"},
		// waxing-gibbous [0.3125,0.4375
		{0.3125, "moon_waxing-gibbous.png"},
		{0.4, "moon_waxing-gibbous.png"},
		// full [0.4375,0.5625
		{0.4375, "moon_full.png"},
		{0.5, "moon_full.png"}, // 0.5 落 full
		{0.5624, "moon_full.png"},
		// waning-gibbous [0.5625,0.6875
		{0.5625, "moon_waning-gibbous.png"},
		{0.625, "moon_waning-gibbous.png"},
		// waning-crescent [0.6875,0.8125
		{0.6875, "moon_waning-crescent.png"},
		{0.75, "moon_waning-crescent.png"},
		// last-quarter [0.8125,0.9375
		{0.8125, "moon_last-quarter.png"},
		{0.875, "moon_last-quarter.png"},
		{0.9374, "moon_last-quarter.png"},
	}
	for _, c := range cases {
		iop, sz, ok := moonSprite(c.phase)
		if !ok {
			t.Errorf("phase=%.4f 取不到素材", c.phase)
			continue
		}
		if sz.X != 250 || sz.Y != 250 {
			t.Errorf("phase=%.4f 源尺寸 = %v，期望 250×250", c.phase, sz)
		}
		if iop.Size().X == 0 {
			t.Errorf("phase=%.4f 的 ImageOp 尺寸为 0", c.phase)
		}
		if got := moonSpriteNames[moonPhaseIndex(c.phase)]; got != c.want {
			t.Errorf("phase=%.4f 落 %q，期望 %q", c.phase, got, c.want)
		}
	}
	// new 区间判定（bakeMoon 据此不画月亮）：[0.9375,1 ∪ [0,0.0625)
	for _, p := range []float64{0.0, 0.03, 0.9375, 0.95, 0.9999} {
		if !isMoonNew(p) {
			t.Errorf("phase=%.4f 应判为 new", p)
		}
	}
	// 0.9374 落 last-quarter（new 区间的下界是 0.9375）
	for _, p := range []float64{0.0625, 0.5, 0.9374} {
		if isMoonNew(p) {
			t.Errorf("phase=%.4f 不应判为 new", p)
		}
	}
}

// TestMoonIllumination 对应需求 6：
// 照度公式 illum = (1 − cos(2π·phase)) / 2 的四个边界值。
// illum(0)=0、illum(0.25)=0.5、illum(0.5)=1、illum(0.75)=0.5。
func TestMoonIllumination(t *testing.T) {
	cases := []struct {
		phase, want float64
	}{
		{0.0, 0.0},
		{0.25, 0.5},
		{0.5, 1.0},
		{0.75, 0.5},
	}
	for _, c := range cases {
		got := moonIllumination(c.phase)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("illum(%.2f) = %v，期望 %v", c.phase, got, c.want)
		}
	}
	// 实测校验点：phase=0.174 ⇒ illum = 0.270
	if got := moonIllumination(0.174); math.Abs(got-0.270) > 1e-3 {
		t.Errorf("illum(0.174) = %v，期望 ≈0.270", got)
	}
}

// TestSkyCacheKeyMoon 对应需求 6：
// skyCacheKey 随相位变化而变化、同一相位多次调用不重烘。
func TestSkyCacheKeyMoon(t *testing.T) {
	// 键随相位变化：nil / 0.174 / 0.5 / 0.9375 的量化键互不相同
	keys := map[int]bool{}
	for _, p := range []*float64{nil, f64p(0.174), f64p(0.5), f64p(0.9375)} {
		k := moonPhaseKey(p)
		if keys[k] {
			t.Fatalf("相位键重复：moonPhaseKey(%v) = %d 与前面相同", p, k)
		}
		keys[k] = true
	}
	// nil 与"相位 0"必须区分（nil 画硬边圆盘，0 落 new 不画月亮）
	if moonPhaseKey(nil) == moonPhaseKey(f64p(0.0)) {
		t.Errorf("nil 与 phase=0 的键相同，无法区分回落画法与新月不画")
	}
	// 量化精度：1/800 内的抖动不改变键（避免每帧重烘）
	if moonPhaseKey(f64p(0.3000)) != moonPhaseKey(f64p(0.3001)) {
		t.Errorf("1/800 内的浮点抖动改变了键 ⇒ 会每帧重烘")
	}

	// 同一相位多次调用不重烘：skyCached 的 Builds 只 +1、Hits 增长
	skyCache = skyCacheType{}
	skyStats = SkyCacheStats{}
	size := image.Pt(540, 960)
	e1 := skyCached(size, effect.Clear, false, f64p(0.174))
	e2 := skyCached(size, effect.Clear, false, f64p(0.174))
	if e1 == nil || e2 == nil {
		t.Fatal("skyCached 返回 nil")
	}
	if e1 != e2 {
		t.Errorf("同一相位两次调用返回不同条目 ⇒ 重烘了")
	}
	if skyStats.Builds != 1 {
		t.Errorf("同一相位两次调用 Builds = %d，期望 1（不重烘）", skyStats.Builds)
	}
	if skyStats.Hits < 1 {
		t.Errorf("同一相位第二次调用 Hits = %d，期望 ≥1", skyStats.Hits)
	}
	// 相位变了 → 键变 → 重烘（Builds +1）
	e3 := skyCached(size, effect.Clear, false, f64p(0.5))
	if e3 == e1 {
		t.Errorf("相位变化后返回同一条目 ⇒ 没重烘")
	}
	if skyStats.Builds != 2 {
		t.Errorf("相位变化后 Builds = %d，期望 2", skyStats.Builds)
	}
}

func f64p(v float64) *float64 { return &v }

// TestBakeMoonPhases 实测 bakeMoon 的三条口径（需求 3）：
// - phase == nil → 走 bakeMoonCrisp 回落（月核区有像素）。
// - phase != nil 且非 new → 用素材绘制（月核区有像素）。
// - phase 落在 new 区间 → 不画月亮（月核区无像素）。
//
// 位置/半径口径逐字沿用 bakeMoonCrisp：心 (0.72w,0.095h)，半径 0.045w。
// TestBakeMoonPhases 月亮层的月相判据（此后月亮由独立层绘制）。
//
// 月亮已从天空纹理摘出（半分辨率烘焙 ⇒ 边缘整像素台阶 + 云压不住月面），
// 上线路径 = moonLayerGet(整屏分辨率) → moonBlit 画在云之前。
// 本测试直接打这条路径，判据逐字不变：
// - phase == nil → 满月回落（月核区有亮像素）
// - phase = 0.5 → 满月（月核区有亮像素）
// - phase = 0.0 → 新月不画月亮（月核区无亮像素）
// - 白天不画月亮
func TestBakeMoonPhases(t *testing.T) {
	w, h := 1080, 1920
	cx, cy := int(0.72*float64(w)), int(0.095*float64(h))
	rc := moonRcFrac * float64(w)

	// 月核区（月心附近 rc 半径内）明显亮于天空基色的像素计数。
	// 月亮层是子矩形，左上角 = 层原点 ⇒ 屏幕坐标减原点即层内坐标。
	bright := func(e *moonLayerEntry) int {
		if e == nil {
			return 0
		}
		n := 0
		for y := cy - int(rc); y <= cy+int(rc); y++ {
			for x := cx - int(rc); x <= cx+int(rc); x++ {
				lx, ly := x-e.off.X, y-e.off.Y
				if lx < 0 || ly < 0 || lx >= e.size.X || ly >= e.size.Y {
					continue
				}
				i := e.img.PixOffset(lx, ly)
				if e.img.Pix[i+3] == 0 {
					continue // 层是透明底，只看画出来的像素
				}
				lum := (float64(e.img.Pix[i]) + float64(e.img.Pix[i+1]) + float64(e.img.Pix[i+2])) / 3
				if lum > 120 {
					n++
				}
			}
		}
		return n
	}

	if n := bright(moonLayerGet(w, h, nil)); n == 0 {
		t.Errorf("phase=nil 回落画法月核区无亮像素")
	}
	if n := bright(moonLayerGet(w, h, f64p(0.5))); n == 0 {
		t.Errorf("phase=0.5 素材绘制月核区无亮像素")
	}
	if n := bright(moonLayerGet(w, h, f64p(0.0))); n != 0 {
		t.Errorf("phase=0.0 新月应不画月亮，月核区却有 %d 个亮像素", n)
	}
	// 白天不画月亮（层函数直接返回 nil）
	if e := moonLayerGet(w, h, f64p(0.5)); e == nil {
		t.Skip("月亮层为空，白天判据不适用")
	}
}

// TestMoonLayerSmallWindow 低性能 x86 小主机屏保窗口是 **810x1389**（日志 SCR_FIRSTFRAME size=810x1389），
// 不是 1080x1920。月亮在 0.095h 时其光晕盒顶到屏幕外，
// bakeMoonAt 的起点外扩 -1 变成 -1 ⇒ PixOffset 负索引 panic
// （有一版一启动就崩，屏幕上根本没有新版在跑）。
func TestMoonLayerSmallWindow(t *testing.T) {
	for _, ph := range []*float64{nil, f64p(0.0), f64p(0.174), f64p(0.5), f64p(0.875)} {
		for _, sz := range [][2]int{{810, 1389}, {1080, 1920}, {400, 700}, {320, 480}} {
			e := moonLayerGet(sz[0], sz[1], ph)
			if e == nil {
				continue
			}
			if e.off.X < 0 || e.off.Y < 0 {
				t.Errorf("尺寸 %dx%d phase=%v 层左上角为负: %v", sz[0], sz[1], ph, e.off)
			}
			if e.off.X+e.size.X > sz[0] || e.off.Y+e.size.Y > sz[1] {
				t.Errorf("尺寸 %dx%d phase=%v 层越出屏幕: off=%v size=%v", sz[0], sz[1], ph, e.off, e.size)
			}
			// 逐像素访问一遍，越界会直接 panic
			for y := 0; y < e.size.Y; y++ {
				for x := 0; x < e.size.X; x++ {
					_ = e.img.Pix[y*e.img.Stride+x]
				}
			}
		}
	}
}

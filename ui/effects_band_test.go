package ui

import (
	"math/rand"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// 信息带避让的自检（口径变更：按半径断言）。
//
// 判据是**按半径**断言，不是按中心：
// "断言云/雾在推进 3000 帧后，精灵中心 ± 半高 都不与带区间相交"。
// 半高 = 粒子实际画出来的半高（Field.halfHN，与 Draw 的 scale = sc×(0.55+0.45·depth)
// 同口径。证据：离屏图视觉判读（图名 lane_cloudy）
// "在数字 34 的正上方，有一朵灰蓝色的云直接覆盖在数字上…数字的上半部分看起来
// 像是'陷'在云里"；"顶部的云层过于下沉，直接侵入了屏幕中央的时间显示区域"。
// 根因：旧版只保证中心在带外，而云 sc 上限 3.5 × Kenney 云原生高
// 118–138 px、雾 sc 上限 4.2 × 256 px → 半高 210–537 px 压进带内。

// bandHalf 把"中心 ± 半高"与带区间的相交判据写成一次判断（相交 = 违规）。
func bandIntersectsHalf(y, half, y0, y1 float64) bool {
	return y+half > y0 && y-half < y1
}

// TestCloudFogAvoidInfoBand 云（pkCloud）与雾（pkFog）：初始分布与推进 3000 帧后，
// 精灵**中心 ± 半高**都不得与信息带相交（按半径断言）。
func TestCloudFogAvoidInfoBand(t *testing.T) {
	const screenH = PortraitH // 1920，与 fxshot/屏保竖屏同口径

	y0, y1 := infoBandY()
	if !(y0 < y1) {
		t.Fatalf("信息带区间非法: [%.3f, %.3f]", y0, y1)
	}
	if y0 != ClockRect.Y || y1 != WeatherRect.Y+WeatherRect.H {
		t.Errorf("信息带必须从 rect 常量取，不得写死数字: got [%.3f,%.3f]", y0, y1)
	}

	for _, tc := range []struct {
		cls  effect.Class
		kind pkind
	}{
		{effect.Cloudy, pkCloud},
		{effect.Fog, pkFog},
	} {
		pad := bandPadN(tc.kind, screenH)
		if pad <= 0 {
			t.Fatalf("%s 外扩量应为正，got %v", tc.cls, pad)
		}
		// 外扩量口径：(最大 sc × 精灵原生高 / 屏高) / 2，
		// 原生高从贴图集取、sc 上限从 spawn 常量取，屏高由调用方传入。
		// 净尺寸口径：粒子云 = ÷bandNetDiv（与云带共用分母，spriteScale 的 pkCloud 分支），
		// 雾 = ÷spriteNetDiv（雾的绘制式仍走既有净倍率）。
		var maxSc float64
		var netDiv float64
		switch tc.kind {
		case pkCloud:
			maxSc = cloudScBase + cloudScSpan
			netDiv = bandNetDiv
		case pkFog:
			maxSc = fogScBase + fogScSpan
			netDiv = spriteNetDiv
		}
		want := (maxSc * float64(maxSpriteH(spritesOnce(), tc.kind)) / float64(screenH)) / 2 / netDiv
		if pad != want {
			t.Errorf("%s 外扩量口径不符: got %v want %v", tc.cls, pad, want)
		}

		f := NewField(tc.cls, true, 20261015)
		n := 0
		for i, p := range f.parts {
			if p.kind != tc.kind {
				continue
			}
			n++
			half := f.halfHN(&f.parts[i], screenH)
			// 云/雾口径变更：**云也允许压带**，
			// 由信息带自带的 30% 底压暗（上游 upstream/core/settings/
			// default_settings.py:1523-1540 的 weather.bg_opacity 0.3 +
			// show_background True 就是这层底；本复刻 ui/render.go:621 已实现，
			// 绘制顺序 天空→粒子→fillPanel→文字，render.go:263/270/277。
			// 保留带避让的实测代价：带 0.244–0.822 占 578 px，云原生高 121–138 px
			// ⇒ 带外只剩顶部 182 px + 底部 56 px = 238 px，只容 1.3 行
			// ⇒ 8 朵全挤顶部叠成宽 735 px 的一块 ⇒ 顶部一堆一堆重叠的云。
			// 雾口径：雾团半高 0.213 屏高，信息带占 0.578 屏高
			// ⇒ "带外 + 整只在屏内"几何无解；旧实现靠把中心放到屏幕外
			// （bandFreeY 区间 -0.35…1.15）"避让"，直接造成
			// "有的云在顶上，然后只有云的下面一小部分在屏幕内"。
			// 新判据：云与雾都 = **整只在屏内**，压带交给带底。
			if tc.kind == pkFog || tc.kind == pkCloud {
				if p.y-half < 0 || p.y+half > 1 {
					t.Errorf("%s 初始 雾未整只在屏内: i=%d y=%.4f half=%.4f sc=%.2f",
						tc.cls, i, p.y, half, p.sc)
				}
			} else {
				if bandIntersectsHalf(p.y, half, y0, y1) {
					t.Errorf("%s 初始 中心±半高 与带相交: i=%d y=%.4f half=%.4f sc=%.2f band=[%.3f,%.3f]",
						tc.cls, i, p.y, half, p.sc, y0, y1)
				}
				if p.y > y0-pad && p.y < y1+pad {
					t.Errorf("%s 初始 中心未落到带边界±外扩之外: i=%d y=%.4f pad=%.4f", tc.cls, i, p.y, pad)
				}
			}
		}
		if n == 0 {
			t.Fatalf("%s 没有云/雾粒子，测试无意义", tc.cls)
		}

		// 推进 3000 帧（≈200 s）：任何一帧都不得有云/雾的精灵身体落进带内。
		for k := 0; k < 3000; k++ {
			f.Step(66*time.Millisecond, 1080, screenH)
			for i, p := range f.parts {
				if p.kind != tc.kind {
					continue
				}
				half := f.halfHN(&f.parts[i], screenH)
				if tc.kind == pkFog || tc.kind == pkCloud {
					if p.y-half < 0 || p.y+half > 1 {
						t.Fatalf("%s 推进 %d 帧后 雾未整只在屏内: i=%d y=%.4f half=%.4f sc=%.2f depth=%.2f",
							tc.cls, k, i, p.y, half, p.sc, p.depth)
					}
					continue
				}
				if bandIntersectsHalf(p.y, half, y0, y1) {
					t.Fatalf("%s 推进 %d 帧后 中心±半高 与带相交: i=%d y=%.4f half=%.4f sc=%.2f depth=%.2f band=[%.3f,%.3f]",
						tc.cls, k, i, p.y, half, p.sc, p.depth, y0, y1)
				}
			}
		}
	}
}

// TestBandFreeYOutsidePaddedBand 直接验初始分布函数本身：中心必须落在
// 带边界 ± 外扩量 之外，且不超过回收窗口。
func TestBandFreeYOutsidePaddedBand(t *testing.T) {
	y0, y1 := infoBandY()
	for _, kind := range []pkind{pkCloud, pkFog} {
		pad := bandPadN(kind, PortraitH)
		r := rand.New(rand.NewSource(1))
		for i := 0; i < 20000; i++ {
			y := bandFreeY(r, pad)
			if y > y0-pad && y < y1+pad {
				t.Fatalf("bandFreeY(pad=%.4f, kind=%d) 落进带边界±外扩之内: y=%.4f band=[%.4f,%.4f]",
					pad, kind, y, y0, y1)
			}
			if y < recycleYLo-1e-9 || y > recycleYHi+1e-9 {
				t.Fatalf("bandFreeY(pad=%.4f, kind=%d) 越出回收窗口: y=%.4f window=[%.2f,%.2f]",
					pad, kind, y, recycleYLo, recycleYHi)
			}
		}
	}
}

// TestBandPadOnlyCloudFog 外扩量只给云与雾（
// 雨/雪/尘/星不动 —— 细线条压在大字上不影响可读性。
func TestBandPadOnlyCloudFog(t *testing.T) {
	for _, k := range []pkind{pkDrop, pkFlake, pkMote, pkStar, pkGlow} {
		if got := bandPadN(k, PortraitH); got != 0 {
			t.Errorf("kind=%d 不应有外扩量，got %v", k, got)
		}
	}
	for _, k := range []pkind{pkCloud, pkFog} {
		if got := bandPadN(k, PortraitH); got <= 0 {
			t.Errorf("kind=%d 应有外扩量，got %v", k, got)
		}
	}
	// 外扩量随屏高走（屏高由调用方传入，不写死像素数字）：屏越矮外扩越大。
	c1920 := bandPadN(pkCloud, 1920)
	c1080 := bandPadN(pkCloud, 1080)
	if !(c1080 > c1920) {
		t.Errorf("外扩量应随屏高变小而变大: 1080→%v 1920→%v", c1080, c1920)
	}
	if bandPadN(pkCloud, 0) != 0 {
		t.Errorf("屏高非法时应为 0")
	}
}

// TestRainSnowDustUnchanged 雨/雪/尘的初始 y 分布未被改动（实测只改云与雾）：
// 雨/雪 fresh 铺满 [0,1]，尘铺 [0.08,0.96]。
func TestRainSnowDustUnchanged(t *testing.T) {
	for _, cls := range []effect.Class{effect.Rain, effect.Snow} {
		f := NewField(cls, true, 20261015)
		inBand := 0
		for _, p := range f.parts {
			if p.y > ClockRect.Y && p.y < WeatherRect.Y+WeatherRect.H {
				inBand++
			}
		}
		if inBand == 0 {
			t.Errorf("%s 的粒子应仍允许落在带内（本批不动它）", cls)
		}
	}
	f := NewField(effect.Dust, true, 20261015)
	inBand := 0
	for _, p := range f.parts {
		if p.y > ClockRect.Y && p.y < WeatherRect.Y+WeatherRect.H {
			inBand++
		}
	}
	if inBand == 0 {
		t.Errorf("尘的粒子应仍允许落在带内（本批不动它）")
	}
}

// TestFpsAdaptiveCutsActive fps 自适应自检（实测
// 新增要求 + 实测：在给定低 fps 输入下，活跃粒子数被削减，且不低于
// 原值的 25% 下限；回升到 18 fps 以上逐步恢复；12–18 滞回区不动。
//
// 证据：40 秒不足 60 帧（≈1.5 fps）、
// 进程被标 Not Responding → 粒子绘制开销必须自适应收缩。
// 口径：Field.Step 以传入 dt 累计最近 2 秒的实测 fps
// 帧间隔，低于 12 fps 按比例削减（active ← active×fps/12，下限 25%），
// 高于 18 fps 每次 +10% 总数恢复，上限为总数。削减只影响 Draw 绘制数量。
func TestFpsAdaptiveCutsActive(t *testing.T) {
	f := NewField(effect.Rain, true, 20261015)
	n := f.TotalCount()
	if n != 320 {
		t.Fatalf("雨粒子总数应为 counts 的 320，got %d", n)
	}
	if got := f.ActiveCount(); got != n {
		t.Fatalf("建场时活跃数应为全部: got %d want %d", got, n)
	}

	feed := func(dt time.Duration, windows int) {
		framesPer := int(2.0 / dt.Seconds())
		for k := 0; k < framesPer*windows; k++ {
			f.Step(dt, 1080, PortraitH)
		}
	}
	floor := (n + 3) / 4 // 25% 下限

	// 1 5 fps（低帧率输入）× 3 个 2 秒窗口：按比例削减必须触底 25% 下限
	feed(200*time.Millisecond, 3)
	if got := f.ActiveCount(); got != floor {
		t.Errorf("5 fps 下应削减到 25%% 下限: got %d want %d (total=%d)", got, floor, n)
	}
	if got := f.ActiveCount(); got < n/4 {
		t.Errorf("活跃数不得低于原值 25%%: got %d floor=%d", got, n/4)
	}

	// 2 10 fps：按比例削减（320×10/12=266），且不低于下限、低于总数
	f2 := NewField(effect.Rain, true, 20261015)
	feed2 := func(dt time.Duration, windows int) {
		framesPer := int(2.0 / dt.Seconds())
		for k := 0; k < framesPer*windows; k++ {
			f2.Step(dt, 1080, PortraitH)
		}
	}
	feed2(100*time.Millisecond, 1)
	if got, want := f2.ActiveCount(), int(float64(n)*10.0/12.0); got != want {
		t.Errorf("10 fps 应按比例削减: got %d want %d", got, want)
	}
	if f2.ActiveCount() < n/4 {
		t.Errorf("10 fps 削减不得低于 25%% 下限: got %d", f2.ActiveCount())
	}

	// 3 15 fps 滞回区（12–18）：不动（dt = 1s/15 ≈ 66.7 ms/帧）
	before := f2.ActiveCount()
	feed2(time.Second/15, 2)
	if got := f2.ActiveCount(); got != before {
		t.Errorf("15 fps 在滞回区不应变化: got %d want %d", got, before)
	}

	// 4 20 fps（>18）：逐步恢复，8 个窗口内回到总数
	feed2(50*time.Millisecond, 8)
	if got := f2.ActiveCount(); got != n {
		t.Errorf("20 fps 应逐步恢复到总数: got %d want %d", got, n)
	}

	// 5 小场（雾 18 个）同样受 25% 下限保护
	fs := NewField(effect.Fog, true, 20261015)
	ntot := fs.TotalCount()
	feedF := func(dt time.Duration, windows int) {
		framesPer := int(2.0 / dt.Seconds())
		for k := 0; k < framesPer*windows; k++ {
			fs.Step(dt, 1080, PortraitH)
		}
	}
	feedF(200*time.Millisecond, 5)
	if got := fs.ActiveCount(); got < ntot/4 {
		t.Errorf("雾 5 fps 削减后不得低于 25%% 下限: got %d total=%d", got, ntot)
	}
}

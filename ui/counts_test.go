package ui

// / C5a（降图元上限）的钉死单测 + 必查项（idx% 分组依赖）取证落点。
// 必查项逐条（grep 全树 idx% 的口径，非测试代码里的全部取模分组）：
//	ui/effects.go:counts(Snow) 注释指认（幕层取法，P3 起 = idx%86==85 的 3 团）
//	ui/effects.go:981 idx%5 == 0 → 只在 spawn 的 Clear + isDay 分支（白天补云）
//	ui/effects.go:idx%86 == 85 → 只在 spawn 的 Snow 分支（P3 起 3 团灰幕）
//	ui/effects.go:1209 cfi[idx%n] → n = 云形态贴图表长度（cloudFormOf.idx），与总数无关
//	ui/effects.go:1229 idx%2 == 1 → mirror，仅 pkCloud / pkFog 生效
//	ui/effects.go:1934 i%2 == 1 → 云带 mirror，自变量 = 带序号 i（bandCount 由类别定）
// ⇒ Rain / Thunder / Dust 三类的 idx% 依赖 = **无**：
// Rain、Thunder 在 spawn 里整档走 `case effect.Rain, effect.Thunder:` ⇒ 全 pkDrop，
// 贴图按景深挑档（p.spr = int(depth×6.999)，effects.go:1193-1201），全程不吃 idx 取模；
// Dust 走 `case effect.Dust:` ⇒ 全 pkMote，贴图 p.spr = r.Intn(len(sp.mote))（随机，不吃 idx）。
// 三类的 mirror 恒 false（idx%2 只给 pkCloud/pkFog），也没有灰幕那样的整档分组。
// 下面每条都钉成断言（改数后分组数量 = 预期）。

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestCountsC5a 钉死三个改数与六个不动的档。
func TestCountsC5a(t *testing.T) {
	// 改的三档（C5a 定档 a）
	if got := counts(effect.Rain, false); got != 220 {
		t.Errorf("counts(Rain,false) = %d，期望 220（320 → 220）", got)
	}
	if got := counts(effect.Rain, true); got != 220 {
		t.Errorf("counts(Rain,true) = %d，期望 220（昼夜同档）", got)
	}
	if got := counts(effect.Thunder, false); got != 300 {
		t.Errorf("counts(Thunder,false) = %d，期望 300（420 → 300）", got)
	}
	if got := counts(effect.Thunder, true); got != 300 {
		t.Errorf("counts(Thunder,true) = %d，期望 300（昼夜同档）", got)
	}
	if got := counts(effect.Dust, false); got != 320 {
		t.Errorf("counts(Dust,false) = %d，期望 320（460 → 320）", got)
	}
	if got := counts(effect.Dust, true); got != 320 {
		t.Errorf("counts(Dust,true) = %d，期望 320（昼夜同档）", got)
	}
	// 一律不动的六档
	if got := counts(effect.Snow, false); got != 260 {
		t.Errorf("counts(Snow,false) = %d，期望 260（P3 不许动：与 idx%%86 灰幕分组强耦合，判据 3 逐字钉 260）", got)
	}
	if got := counts(effect.Snow, true); got != 260 {
		t.Errorf("counts(Snow,true) = %d，期望 260（禁改）", got)
	}
	for _, isDay := range []bool{true, false} {
		// P3 判据 B：counts(Fog) 20 → 2（判据 3 钉 counts(Fog)==2；原断言钉 20）
		if got := counts(effect.Fog, isDay); got != 2 {
			t.Errorf("counts(Fog,%v) = %d，期望 2（P3 判据 B：20 → 2）", isDay, got)
		}
		// P3 判据 B：counts(Overcast) 5 → 2（判据 3 钉 Overcast counts()==2；原断言钉 5）
		if got := counts(effect.Overcast, isDay); got != 2 {
			t.Errorf("counts(Overcast,%v) = %d，期望 2（P3 判据 B：5 → 2）", isDay, got)
		}
		if got := counts(effect.Cloudy, isDay); got != 2 {
			t.Errorf("counts(Cloudy,%v) = %d，期望 2（B3/R4：同带 ≤2 朵，kinetics.BandCapacity 封顶；原断言钉 3）", isDay, got)
		}
		if got := counts(effect.PartlyCloudy, isDay); got != 2 {
			t.Errorf("counts(PartlyCloudy,%v) = %d，期望 2（禁改）", isDay, got)
		}
	}
	if got := counts(effect.Clear, true); got != 2 {
		t.Errorf("counts(Clear,true) = %d，期望 2（B3/R4：云档封顶 2 朵，Clear 白天 = 1 云 + 1 光晕；原断言钉 4）", got)
	}
	if got := counts(effect.Clear, false); got != 110 {
		t.Errorf("counts(Clear,false) = %d，期望 110（夜间是星，禁改）", got)
	}
}

// TestCountsTotalCountMatches 钉死 counts → 全场粒子数（NewField 消费端）。
func TestCountsTotalCountMatches(t *testing.T) {
	want := map[effect.Class]int{
		effect.Rain: 220, effect.Thunder: 300, effect.Dust: 320,
		effect.Snow: 260, effect.Fog: 2, effect.Cloudy: 2,
		effect.PartlyCloudy: 2, effect.Overcast: 2, // P3 判据 B：Fog 20→2、Overcast 5→2；Cloudy 3→2（同带 ≤2 朵）
	}
	for cls, n := range want {
		for _, isDay := range []bool{true, false} {
			f := NewField(cls, isDay, 20261023)
			if got := f.TotalCount(); got != n {
				t.Errorf("%s isDay=%v：TotalCount = %d，期望 %d", cls, isDay, got, n)
			}
		}
	}
}

// TestIdxGroupRainThunderDustNone 必查项：Rain / Thunder / Dust 三类无 idx% 分组依赖。
// 钉法：改数后**逐粒子种类计数**必须等于"整档一类"的预期（0 个分组），
// 且 mirror（idx%2 的分组）在三类里恒 false（idx%2 只给 pkCloud/pkFog）。
func TestIdxGroupRainThunderDustNone(t *testing.T) {
	type want struct {
		total           int
		drop, mote, fog int
		flake, cloud    int
		mirror          int
		sprByDepthOnly  bool
	}
	cases := []struct {
		cls effect.Class
		w   want
	}{
		{effect.Rain, want{total: 220, drop: 220, sprByDepthOnly: true}},
		{effect.Thunder, want{total: 300, drop: 300, sprByDepthOnly: true}},
		{effect.Dust, want{total: 320, mote: 320}},
	}
	for _, c := range cases {
		for _, seed := range []int64{20261023, 23, 11, 20261015} {
			f := NewField(c.cls, true, seed)
			n := f.TotalCount()
			if n != c.w.total {
				t.Fatalf("%s seed=%d：总数 = %d，期望 %d", c.cls, seed, n, c.w.total)
			}
			cnt := map[pkind]int{}
			mirror := 0
			for i := range f.parts {
				p := &f.parts[i]
				cnt[p.kind]++
				if p.mirror {
					mirror++
				}
				if c.w.sprByDepthOnly {
					// pkDrop 的贴图档必须 = 景深导出（与 idx 取模无关）
					want := int(p.depth * 6.999)
					if want < 0 {
						want = 0
					}
					if m := len(f.sp.rain); m > 0 && want >= m {
						want = m - 1
					}
					if p.spr != want {
						t.Fatalf("%s seed=%d i=%d：贴图档 %d ≠ 景深档 %d ⇒ 贴图挑选吃了 idx",
							c.cls, seed, i, p.spr, want)
					}
				}
			}
			for k, exp := range map[pkind]int{pkDrop: c.w.drop, pkMote: c.w.mote,
				pkFog: c.w.fog, pkFlake: c.w.flake, pkCloud: c.w.cloud} {
				if cnt[k] != exp {
					t.Errorf("%s seed=%d：%v 粒子数 = %d，期望 %d（该类无 idx%% 分组 ⇒ 只许整档一类）",
						c.cls, seed, k, cnt[k], exp)
				}
			}
			if mirror != c.w.mirror {
				t.Errorf("%s seed=%d：mirror 粒子数 = %d，期望 0（idx%%2 只给 pkCloud/pkFog）", c.cls, seed, mirror)
			}
			// 云带 mirror 的自变量 = 带序号 i（effects.go bandPlace 的 i%2==1），与粒子总数无关：
			// P6 设计第 1 条后三类的带数 = 3 ⇒ mirror 的带数 = 1（i 为奇数那一条），改数不动。
			if bc := f.bandCount(); bc != 3 {
				t.Errorf("%s：云带数 = %d，期望 3（P6：bandCount 由类别定，与 counts 无关）", c.cls, bc)
			}
		}
	}
}

// TestIdxGroupSnowIntact 钉死 Snow 的灰幕分组取法（P3 判据 B：idx%13==12 的 20 团 →
// **idx%86==85 的 3 团**，命中 idx = 85 / 171 / 257；240 片雪口径不动）。
func TestIdxGroupSnowIntact(t *testing.T) {
	for _, seed := range []int64{20261023, 23, 11, 20261015} {
		for _, isDay := range []bool{true, false} {
			f := NewField(effect.Snow, isDay, seed)
			if n := f.TotalCount(); n != 260 {
				t.Fatalf("Snow seed=%d isDay=%v：总数 = %d，期望 260", seed, isDay, n)
			}
			fog, flake, other := 0, 0, 0
			for i := range f.parts {
				switch f.parts[i].kind {
				case pkFog:
					fog++
					if i%86 != 85 {
						t.Fatalf("Snow seed=%d i=%d：灰幕落在 idx%%86 = %d，期望 85", seed, i, i%86)
					}
				case pkFlake:
					flake++
					if i%86 == 85 {
						t.Fatalf("Snow seed=%d i=%d：雪花落进了灰幕分组 idx%%86 = 85", seed, i)
					}
				default:
					other++
				}
			}
			// 判据 3 逐字："Snow：雾团数 == 3（数 pkFog 的粒子数）"；原断言钉 20 ⇒ 新断言钉 3，断言式不变。
			if fog != 3 {
				t.Fatalf("Snow seed=%d isDay=%v：灰幕团数 = %d，期望 3（idx%%86==85 ⇒ 260 个里正好 3 团）", seed, isDay, fog)
			}
			// 原断言：雪花 240 片（= 260 − 20 团灰幕）。新断言：257 片 = 260 − 3 团灰幕。
			// 规格"不许动"钉的是 counts(Snow) = **260**（上面第一条 Fatalf 照钉 260）；
			// 灰幕从同一池 260 里取 ⇒ 幕层 20 → 3 时雪粒 240 → 257 是同一池的算术结果，
			// 保 240 就得把总数降到 243 ⇒ 撞判据 3 的 counts(Snow)==260（推导写进）。
			if flake != 257 {
				t.Fatalf("Snow seed=%d isDay=%v：雪花瓣数 = %d，期望 257（260 − 3 团灰幕）", seed, isDay, flake)
			}
			if other != 0 {
				t.Errorf("Snow seed=%d：出现第三种粒子 %d 个", seed, other)
			}
		}
	}
}

// TestIdxGroupCloudTiersIntact 钉死其余 idx% 分组在 C5a 后不变：
// Clear 白天 idx%5==0 ⇒ 4 个里 1 朵云；云量三档全云；Snow 之外无灰幕。
func TestIdxGroupCloudTiersIntact(t *testing.T) {
	f := NewField(effect.Clear, true, 20261023)
	cloud, other := 0, 0
	for i := range f.parts {
		if f.parts[i].kind == pkCloud {
			cloud++
			if i%5 != 0 {
				t.Fatalf("Clear 白天：云落在 idx=%d（idx%%5 == %d），期望 0", i, i%5)
			}
		} else {
			other++
		}
	}
	if cloud != 1 {
		t.Errorf("Clear 白天云数 = %d，期望 1（idx%%5==0 在 counts=2 里取 1 个）", cloud)
	}
	if other != 1 {
		t.Errorf("Clear 白天非云粒子 = %d，期望 1（B3/R4：counts 4→2 ⇒ 1 云 + 1 光晕；原断言钉 3）", other)
	}
	// P3 判据 B：Overcast 粒子数 5 → 2（原断言钉 5）；B3/：Cloudy 3 → 2（同带 ≤2 朵）。
	// 本测试的另一半（"这些档的粒子必须全是 pkCloud"）一条没动。
	for cls, n := range map[effect.Class]int{effect.Cloudy: 2, effect.PartlyCloudy: 2, effect.Overcast: 2} {
		gf := NewField(cls, true, 20261023)
		if gf.TotalCount() != n {
			t.Fatalf("%s：总数 = %d，期望 %d", cls, gf.TotalCount(), n)
		}
		for i := range gf.parts {
			if gf.parts[i].kind != pkCloud {
				t.Errorf("%s：粒子 %d 种类 = %v，期望全 pkCloud", cls, i, gf.parts[i].kind)
			}
		}
	}
}

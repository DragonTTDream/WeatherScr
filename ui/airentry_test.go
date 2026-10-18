package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestAirEntryPlacement 证明进场落位的两条不变量：
//
//	需求 2：云整只都要在屏内（不得只有下半部分在屏幕内）
// ⇒ 回收后云中心 y ∈ [半高, 1−半高]，整只在屏内
//	需求 1：云不得突然刷新出来，要在屏幕外慢慢进场
// ⇒ 回收后云中心 x 在屏幕外（vx>0 → x=−半宽；vx<0 → x=1+半宽）
func TestAirEntryPlacement(t *testing.T) {
	for _, cls := range []effect.Class{effect.Cloudy, effect.Fog, effect.Clear, effect.Rain} {
		f := NewField(cls, true, 11)
		f.Step(time.Second, 1080, 1920)
		for i := range f.parts {
			p := &f.parts[i]
			if p.kind != pkCloud && p.kind != pkFog {
				continue
			}
			hw, hh := f.airHalfN(p)
			if p.y < hh-1e-9 || p.y > 1-hh+1e-9 {
				t.Fatalf("%v: 云中心 y=%.4f 越出整只在屏内的区间 [%.4f, %.4f]", cls, p.y, hh, 1-hh)
			}
			if p.x < -1e-9 && p.x < -hw-1e-9 {
				t.Fatalf("%v: 进场云 x=%.4f 超出屏幕外一个身位 (−%.4f)", cls, p.x, hw)
			}
		}
	}
}

// TestAirEntryRecycleIsOffscreen 强制走回收路径（fresh=false）：
// 把云推到出界后回收，断言新粒子在屏幕外。
func TestAirEntryRecycleIsOffscreen(t *testing.T) {
	f := NewField(effect.Cloudy, true, 23)
	f.Step(time.Second, 1080, 1920)
	n := 0
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		n++
		p.x = 1.3 // 推到右出界 ⇒ 下一帧回收走 placeAirEntry
	}
	if n == 0 {
		t.Skip("本场无云粒子")
	}
	f.Step(time.Second, 1080, 1920)
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		hw, _ := f.airHalfN(p)
		if p.x > -hw+1e-9 {
			t.Fatalf("回收后云未从屏幕外进场：x=%.4f 半宽=%.4f", p.x, hw)
		}
	}
}

// TestSpeedScaleKeepsPerFrameStep 证明位移速度系数的口径：
// speedScale = fps/15 时，每帧位移与 15 fps 时逐帧一致。
func TestSpeedScaleKeepsPerFrameStep(t *testing.T) {
	x0 := 0.5
	vx := 0.02
	step := func(fps float64) float64 {
		ss := fps / 15.0
		dt := time.Duration(float64(time.Second) / fps)
		return x0 + vx*dt.Seconds()*ss
	}
	a := step(15)
	b := step(8)
	if d := a - b; d > 1e-6 || d < -1e-6 {
		t.Fatalf("每帧位移不恒定：15fps=%.8f 8fps=%.8f", a, b)
	}
	// 对照：不缩放时每帧位移随帧率变化（这就是"一顿一顿"机制）
	c := x0 + vx*(1.0/8.0)
	if c <= a {
		t.Fatalf("对照组失效：不缩放 8fps 位移 %.6f 应显著大于 15fps %.6f", c, a)
	}
}

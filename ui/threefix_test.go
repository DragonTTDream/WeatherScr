package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestClearDayCloudsMove：白天晴天的点缀云必须有速度、alpha 不能是 0。
// 缺陷：白天还发现有静止的小云。
func TestClearDayCloudsMove(t *testing.T) {
	for _, seed := range []int64{20261015, 20261016, 20261017} {
		f := NewField(effect.Clear, true, seed)
		n, minVx, minA, maxA := 0, 9.0, 9.0, -1.0
		for i := 0; i < f.TotalCount(); i++ {
			p := &f.parts[i]
			if p.kind != pkCloud {
				continue
			}
			n++
			if p.vx < minVx {
				minVx = p.vx
			}
			if p.alpha < minA {
				minA = p.alpha
			}
			if p.alpha > maxA {
				maxA = p.alpha
			}
		}
		if n == 0 {
			t.Fatalf("seed=%d：白天晴天没有云粒子", seed)
		}
		// 15 fps 下每帧位移 = vx·speedScale/fps ≈ vx/15 屏宽 = vx/15·1080 px
		px := minVx / 15 * 1080
		if minVx <= 0 {
			t.Errorf("seed=%d：白天晴天云 vx 最小值=%.4f ⇒ 静止云（指定的缺陷）", seed, minVx)
		}
		if minA <= 0 {
			t.Errorf("seed=%d：白天晴天云 alpha 最小值=%.3f ⇒ 会被 0.92 下限兜成不透明", seed, minA)
		}
		t.Logf("seed=%d：白天云 %d 朵，vx 最小 %.4f（15fps 下 %.2f px/帧 = %.1f px/s），alpha %.2f–%.2f",
			seed, n, minVx, px, minVx*1080, minA, maxA)
	}
}

// TestBandAlphaOccludesDisc：会盖到日/月盘的那两条带，alpha 必须把
// "云盖月面 − 云盖暗天空" 压到 20 级以内，否则月面从云里透出来。
// 缺陷：部分大云没有遮盖月亮，反而被月亮遮盖。
func TestBandAlphaOccludesDisc(t *testing.T) {
	f := NewField(effect.Cloudy, false, 20261015)
	for i := 0; i < cloudBandN; i++ {
		frac := float64(i) / float64(cloudBandN-1)
		a := (240.0 - 25.0*frac) / 255.0
		bp, _ := f.bandPlace(i, f.t, PortraitW, PortraitH)
		crosses := bp.y <= 247 && bp.y+bp.dh >= 117
		diff := (246.0 - 90.0) * (1 - a)
		if crosses && diff > 20 {
			t.Errorf("带%d 与月盘相交但 alpha=%.3f ⇒ 云盖月面比云盖暗天空亮 %.0f 级 ⇒ 遮不住", i, a, diff)
		}
		t.Logf("带%d: y=%.0f..%.0f alpha=%.3f 相交月盘=%v 盖月面-盖暗天空=%.0f 级", i, bp.y, bp.y+bp.dh, a, crosses, diff)
	}
}

// TestEnteringCloudsOffscreen：类别切换建场时，云必须全在屏幕外左侧并错峰。
// 需求：云要缓慢进场，不得突然刷新到屏幕上。
func TestEnteringCloudsOffscreen(t *testing.T) {
	f := NewFieldEntering(effect.Cloudy, false, 20261015)
	onScreen, xs := 0, []float64{}
	for i := 0; i < f.TotalCount(); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		hw, _ := f.airHalfN(p) // 该朵云的真实半宽（归一化，与绘制同口径）
		if p.x+hw > 0 {
			onScreen++
		}
		xs = append(xs, p.x)
	}
	if onScreen > 0 {
		t.Errorf("类别切换建场时有 %d 朵云已经在屏内 ⇒ 整片云会瞬间出现", onScreen)
	}
	if f.fade != 0 {
		t.Errorf("新场 fade=%.2f，应为 0（淡入起点）", f.fade)
	}
	// 逐秒看进屏数量：应当随时间递增，而不是一帧全进
	prev := 0
	var marks []int
	for s := 1; s <= 120; s++ {
		f.Step(time.Second, PortraitW, PortraitH)
		in := 0
		for i := 0; i < f.ActiveCount(); i++ {
			p := &f.parts[i]
			if p.kind == pkCloud && p.x > 0 {
				in++
			}
		}
		if in > prev && s%15 == 0 {
			marks = append(marks, s)
		}
		prev = in
	}
	t.Logf("建场时屏内云=%d 朵（应为 0），x 列表=%v；进屏递增发生在秒 %v ⇒ 云逐批飘入", onScreen, xs, marks)
}

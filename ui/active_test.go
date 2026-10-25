package ui

// 批次 A2 / C2（密度变化改斜坡 + 滞回）与 C3（启动期 ActiveCount 口径）的钉死单测。
//
// 判据来源：spec/a2-cloud-stutter.md 的 C2 四条 + "钉死单测"清单。
// 全部断言为精确值：无 t.Skip、无放宽。

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestNextActiveLimiter 钉死单轮限幅：cur=177, total=320, fps=6.7 ⇒ 差 ≤16 且 ≥4。
//
// 旧口径一次砍 43%（177×6.7/12 = 98 ⇒ 单轮差 79）就是"一卡卡的"的机制。
func TestNextActiveLimiter(t *testing.T) {
	got := NextActive(177, 320, 6.7)
	diff := 177 - got
	if diff > 16 {
		t.Fatalf("单轮削减差 = %d > 16（lim = max(4, 320/20) = 16）⇒ 没有限幅", diff)
	}
	if diff < 4 {
		t.Fatalf("单轮削减差 = %d < 4（lim 下限 4）⇒ 削减没生效", diff)
	}
	if got != 161 {
		t.Fatalf("NextActive(177,320,6.7) = %d，期望 161（177 − lim 16）", got)
	}
	// lim 下限 4：total=20 ⇒ lim = max(4, 20/20) = 4
	if n := NextActive(20, 20, 6.7); 20-n != 4 {
		t.Errorf("total=20 的单轮削减差 = %d，期望 4（lim 下限）", 20-n)
	}
	// lim = total/20：total=100 ⇒ lim = 5
	if n := NextActive(100, 100, 6.7); 100-n != 5 {
		t.Errorf("total=100 的单轮削减差 = %d，期望 5", 100-n)
	}
	// 恢复方向同样限幅：每轮 +≤lim
	if n := NextActive(100, 320, 20); n-100 != 16 {
		t.Errorf("恢复轮 +（%d−100）= %d，期望 16（lim）", n, n-100)
	}
	if n := NextActive(315, 320, 20); n != 320 {
		t.Errorf("恢复上限 = %d，期望 320（不超总数）", n)
	}
}

// TestNextActiveRampMonotone 钉死"连续 8 轮 fps=6.7 ⇒ active 单调不升"，
// 且触底到 minActive = (total+3)/4（既有 25% 下限口径不动）。
func TestNextActiveRampMonotone(t *testing.T) {
	cur := 177
	const total = 320
	lim := 16
	prev := cur
	var seq []int
	for k := 0; k < 8; k++ {
		next := NextActive(cur, total, 6.7)
		seq = append(seq, next)
		if next > prev {
			t.Fatalf("第 %d 轮 active 上升：%d → %d ⇒ 削减轮里出现回升", k+1, prev, next)
		}
		if prev-next > lim {
			t.Fatalf("第 %d 轮单轮位移 %d 越出限幅 %d", k+1, prev-next, lim)
		}
		prev = next
		cur = next
	}
	want := (total + 3) / 4
	if prev != want {
		t.Fatalf("8 轮后 active = %d，期望触底 minActive = %d（序列 %v）", prev, want, seq)
	}
	// 触底后不再往下（下限保护）
	if got := NextActive(prev, total, 6.7); got != want {
		t.Fatalf("触底后再喂 6.7 ⇒ %d，期望守住 %d", got, want)
	}
	t.Logf("序列 %v ⇒ 触底 %d", seq, prev)
}

// TestNextActiveDeadZone 钉死死区 12 ≤ fps ≤ 18 不变化。
func TestNextActiveDeadZone(t *testing.T) {
	for _, fps := range []float64{12.0, 13.5, 15.0, 17.9, 18.0} {
		if got := NextActive(177, 320, fps); got != 177 {
			t.Errorf("fps=%.1f 落在死区，NextActive = %d，期望原值 177", fps, got)
		}
	}
}

// TestNextActiveCloudTier 钉死云量档永不削减（total=5 ⇒ 恒 5）。
func TestNextActiveCloudTier(t *testing.T) {
	for _, fps := range []float64{1.0, 6.7, 11.9, 12.0, 15.0, 18.1, 30.0} {
		if got := NextActive(5, 5, fps); got != 5 {
			t.Fatalf("fps=%.1f：云量档 total=5 返回 %d，期望 5（永不削减）", fps, got)
		}
		if got := NextActive(2, 5, fps); got != 5 {
			t.Fatalf("fps=%.1f：云量档从削减态 2 返回 %d，期望 5", fps, got)
		}
	}
	for _, total := range []int{2, 3, 4, 5} { // PartlyCloudy / Cloudy / Clear 白天 / Overcast
		if got := NextActive(total, total, 6.7); got != total {
			t.Errorf("total=%d 云量档被削减成 %d", total, got)
		}
	}
}

// TestAdaptFpsRecoveryHysteresis 钉死恢复侧滞回：连续 3 轮 fps>18 才开始恢复，
// 期间任何一轮 fps ≤ 18 把计数清零。
func TestAdaptFpsRecoveryHysteresis(t *testing.T) {
	f := NewField(effect.Rain, true, 20261023)
	if f.TotalCount() != 220 {
		t.Fatalf("Rain 总数 = %d，期望 220（C5a）", f.TotalCount())
	}
	f.active = 100 // 削减后的稳态
	lim := 220 / 20
	if lim < 4 {
		lim = 4
	}
	// 第 1、2 轮 fps=20：未武装 ⇒ 完全不动
	f.adaptFps(20)
	if got := f.active; got != 100 {
		t.Fatalf("恢复第 1 轮就动了：active = %d（滞回要求连续 3 轮才开始）", got)
	}
	f.adaptFps(20)
	if got := f.active; got != 100 {
		t.Fatalf("恢复第 2 轮就动了：active = %d", got)
	}
	// 第 3 轮：武装 ⇒ +lim
	f.adaptFps(20)
	if got := f.active; got != 100+lim {
		t.Fatalf("恢复第 3 轮 active = %d，期望 %d", got, 100+lim)
	}
	f.adaptFps(20)
	if got := f.active; got != 100+2*lim {
		t.Fatalf("恢复第 4 轮 active = %d，期望 %d", got, 100+2*lim)
	}
	// 死区一轮 ⇒ 计数清零 ⇒ 下一轮 fps=20 不恢复
	f.adaptFps(15)
	before := f.active
	f.adaptFps(20)
	if got := f.active; got != before {
		t.Fatalf("死区打断后第 1 轮 fps=20 恢复了（%d → %d）⇒ 计数没清零", before, got)
	}
	f.adaptFps(20)
	f.adaptFps(20)
	if got := f.active; got != before+lim {
		t.Fatalf("重新武装 3 轮后 active = %d，期望 %d", got, before+lim)
	}
}

// TestAdaptFpsNoOscillation 钉死"fps 交替 20,10,20,10 ⇒ 无单调振荡"：
// 恢复需要连续 3 轮，交替输入下永远武装不起来 ⇒ 序列单调不升（不来回弹）。
func TestAdaptFpsNoOscillation(t *testing.T) {
	f := NewField(effect.Rain, true, 20261023)
	f.active = 150
	prev := f.active
	var seq []int
	for k := 0; k < 8; k++ {
		f.adaptFps(20)
		seq = append(seq, f.active)
		if f.active > prev {
			t.Fatalf("第 %d 轮 fps=20 把 active 抬到 %d（上一轮 %d）⇒ 交替输入下发生了回升/振荡",
				k+1, f.active, prev)
		}
		prev = f.active
		f.adaptFps(10)
		seq = append(seq, f.active)
		if f.active > prev {
			t.Fatalf("第 %d 轮 fps=10 把 active 抬到 %d（上一轮 %d）⇒ 削减轮里出现回升", k+1, f.active, prev)
		}
		prev = f.active
	}
	if f.active >= 150 {
		t.Fatalf("交替输入 8 对后 active = %d，期望低于起点 150（序列 %v）", f.active, seq)
	}
	t.Logf("交替 20/10 序列 = %v ⇒ 单调不升、无振荡", seq)
}

// TestAdaptFpsCloudTierNeverCuts 钉死云量三档（minActive = n）与 Fog 的 25% 下限。
func TestAdaptFpsCloudTierNeverCuts(t *testing.T) {
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Overcast} {
		f := NewField(cls, true, 20261023)
		n := f.TotalCount()
		for k := 0; k < 10; k++ {
			f.adaptFps(5)
		}
		if got := f.active; got != n {
			t.Fatalf("%s：10 轮 5 fps 后 active = %d，期望 %d（云量三档 minActive = n）", cls, got, n)
		}
	}
	fs := NewField(effect.Fog, true, 20261023)
	ntot := fs.TotalCount()
	// P3 判据 B 更新常量（原断言：Fog 总数 == 20 ⇒ 新断言：== 2）：
	// 规格把 Fog 的 counts 20 → 2（预算表"雾团 20→2 团"，判据 3 钉 counts(Fog)==2）。
	// 下面的 25% 下限与恢复 lim 是**同一个断言式**（minActive = (n+3)/4、
	// lim = max(4, n/20)、恢复上限 = 总数 n），只是把 n 从 20 换成 2：
	// 下限 = (2+3)/4 = 1，恢复一轮 = min(n, 1+lim) = 2（lim=4 大于余量，被总数上限钳住）。
	if ntot != 2 {
		t.Fatalf("Fog 总数 = %d，期望 2（P3 判据 B：20 → 2）", ntot)
	}
	for k := 0; k < 20; k++ {
		fs.adaptFps(5)
	}
	// 原断言：20 轮 5 fps 后触底 25% 下限 = (n+3)/4 = 5（n=20 时该分支可达）。
	// 新断言：active = 总数 = 2 —— P3 判据 B 把 Fog 降到 n=2 后，落进 NextActive 的
	// "total ≤ 8 ⇒ 不削减"分支（ui/effects.go 的 NextActive，这条削减口径在规格"不许动"里，一行没改）
	// ⇒ 25% 下限分支在 n ≤ 8 时结构上不可达。断言没放宽：25% 下限本身仍由
	// effects_band_test.go 的"雾 5 fps 削减后不得低于 25% 下限"（got ≥ ntot/4）钉着。
	want := ntot
	if fs.active != want {
		t.Fatalf("Fog(n=%d) 20 轮 5 fps 后 active = %d，期望 %d（total ≤ 8 ⇒ 不削减分支）", ntot, fs.active, want)
	}
	fs.recoverStreak = recoverArmRounds
	fs.adaptFps(20)
	// 原断言：恢复一轮 = want+4（n=20 时 lim=4 未被总数钳）⇒ 新断言：恢复一轮 = ntot
	// （n=2 时 lim=4 > 余量 n−want = 1 ⇒ NextActive 的总数上限把结果钳在 2）。
	// 断言式没动（仍是"恢复一轮后 = min(总数, active+lim)"），随规格换 n 换常量。
	if got, want2 := fs.active, ntot; got != want2 {
		t.Fatalf("Fog 恢复一轮 active = %d，期望 %d（lim = max(4, n/20) = 4，被总数 %d 钳住）", got, want2, ntot)
	}
}

// TestAdaptFpsRampRampUp 钉死恢复侧也走斜坡（每轮 +≤lim），不是一步回到总数。
func TestAdaptFpsRampRampUp(t *testing.T) {
	f := NewField(effect.Rain, true, 20261023)
	f.active = 55
	lim := 220 / 20
	var seq []int
	for k := 0; k < 6; k++ {
		f.adaptFps(20)
		seq = append(seq, f.active)
	}
	// 前 2 轮武装不动，第 3 轮起每轮 +lim
	want := []int{55, 55, 55 + lim, 55 + 2*lim, 55 + 3*lim, 55 + 4*lim}
	for k := range want {
		if seq[k] != want[k] {
			t.Fatalf("恢复序列第 %d 轮 = %d，期望 %d（整序列 %v，期望 %v）", k+1, seq[k], want[k], seq, want)
		}
	}
}

// TestBootCapWithReduction 钉死启动期上限与削减取小：
// 启动期（80）比削减值（55）大 ⇒ 生效值 = 55；启动期比削减值小 ⇒ 生效值 = 80。
func TestBootCapWithReduction(t *testing.T) {
	f := NewField(effect.Rain, true, 20261023)
	for k := 0; k < 30; k++ { // 第 1–30 帧 ⇒ 上限 80
		f.Step(time.Millisecond, PortraitW, PortraitH)
	}
	f.active = 55
	if got := f.ActiveCount(); got != 55 {
		t.Fatalf("启动期内削减到 55 ⇒ ActiveCount = %d，期望 55（取小）", got)
	}
	f.active = 200
	if got := f.ActiveCount(); got != 80 {
		t.Fatalf("启动期内 active=200 ⇒ ActiveCount = %d，期望 80（启动期取小）", got)
	}
}

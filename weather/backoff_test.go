package weather

import (
	"testing"
	"time"
)

// S2/S3 钉死单测（钉死清单第 4、5 条）。

// NextRefreshInterval 的阶梯：1→1m、2→2m、3→5m、4→10m、5→10m（第 4 次及以后封顶）。
func TestNextRefreshIntervalLadder(t *testing.T) {
	const base = time.Minute
	want := []time.Duration{
		time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
		10 * time.Minute,
	}
	for i, w := range want {
		fails := i + 1
		if got := NextRefreshInterval(fails, base); got != w {
			t.Errorf("failCount=%d ⇒ %v，期望 %v", fails, got, w)
		}
	}
}

// failCount<=0 ⇒ 复位回 base（成功一轮就回到设定的节奏）。
func TestNextRefreshIntervalResets(t *testing.T) {
	for _, base := range []time.Duration{
		time.Minute, 3 * time.Minute, 20 * time.Minute,
	} {
		for _, fails := range []int{0, -1, -7} {
			if got := NextRefreshInterval(fails, base); got != base {
				t.Errorf("failCount=%d base=%v ⇒ %v，期望复位为 base", fails, base, got)
			}
		}
	}
}

// 结果**不得小于 base**：base=5m 时，任何 failCount 都得 ≥ 5m。
func TestNextRefreshIntervalNeverBelowBase(t *testing.T) {
	const base = 5 * time.Minute
	for fails := -2; fails <= 30; fails++ {
		got := NextRefreshInterval(fails, base)
		if got < base {
			t.Errorf("failCount=%d base=%v ⇒ %v，小于 base", fails, base, got)
		}
	}
}

// OfficialJitter：同一 seed 两次调用必须相等（可复现 ⇒ 实机对拍能指名）。
func TestOfficialJitterIsDeterministic(t *testing.T) {
	seeds := []int64{0, 1, -1, 7, 123456789, -987654321, 1 << 40}
	for _, seed := range seeds {
		a := OfficialJitter(seed)
		for k := 0; k < 5; k++ {
			if b := OfficialJitter(seed); b != a {
				t.Fatalf("seed=%d 两次调用不等：%v vs %v", seed, a, b)
			}
		}
	}
}

// OfficialJitter：跨 1000 个 seed 全部落在 [0, 30s] 内。
func TestOfficialJitterInBand(t *testing.T) {
	for seed := int64(0); seed < 1000; seed++ {
		d := OfficialJitter(seed)
		if d < 0 || d > 30*time.Second {
			t.Fatalf("seed=%d ⇒ %v，越出 [0,30s]", seed, d)
		}
	}
}

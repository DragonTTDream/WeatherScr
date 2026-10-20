package effect

import (
	"sort"
	"testing"
)

// TestWMO：判据 —— 28 码全枚举，类名逐一断言（实测逐行）。
func TestWMO(t *testing.T) {
	want := map[int]string{
		0:  "clear",
		1:  "clear",
		2:  "partly-cloudy",
		3:  "overcast",
		45: "fog",
		48: "fog",

		51: "rain-light",
		53: "rain-light",
		56: "rain-light",
		61: "rain-light",
		66: "rain-light",
		80: "rain-light",

		55: "rain-heavy",
		57: "rain-heavy",
		63: "rain-heavy",
		65: "rain-heavy",
		67: "rain-heavy",
		81: "rain-heavy",
		82: "rain-heavy",

		71: "snow-light",
		77: "snow-light",
		85: "snow-light",

		73: "snow-heavy",
		75: "snow-heavy",
		86: "snow-heavy",

		95: "thunder",
		96: "thunder",
		99: "thunder",
	}
	if len(want) != 28 {
		t.Fatalf("枚举的码数 = %d，期望 28", len(want))
	}
	for code, wv := range want {
		if got := Variant(code); got != wv {
			t.Errorf("码 %d → %q，期望 %q", code, got, wv)
		}
	}

	// CoreCodes 必须恰好是这 28 个
	codes := CoreCodes()
	if len(codes) != 28 {
		t.Fatalf("CoreCodes 数量 = %d，期望 28", len(codes))
	}
	got := map[int]bool{}
	for _, c := range codes {
		got[c] = true
		if c > 99 {
			t.Errorf("CoreCodes 混入了扩展码 %d", c)
		}
	}
	for c := range want {
		if !got[c] {
			t.Errorf("CoreCodes 缺码 %d", c)
		}
	}
	if !sort.IntsAreSorted(codes) {
		t.Errorf("CoreCodes 未升序: %v", codes)
	}
}

// TestDustNotInCore：诚实性判据 —— 核心码段（0–99）下 dust 永不触发。
func TestDustNotInCore(t *testing.T) {
	for _, c := range CoreCodes() {
		if cl, _ := Classify(c); cl == Dust {
			t.Fatalf("核心码 %d 落到了 dust，违反实测口径", c)
		}
	}
	// 扩展码段才出 dust
	for _, c := range []int{101, 105, 110} {
		if cl, _ := Classify(c); cl != Dust {
			t.Errorf("扩展码 %d 应得 dust，实得 %q", c, cl)
		}
	}
}

// TestSevenClasses：类总数必须是 9（原 7 类 + 云量两档 PartlyCloudy/Overcast）。
// 判据从 7 改到 9 的依据 = 云量分档把 Cloudy 拆成两档（取证 34.16.2）。
// 枚举口径 = 核心码 + 扩展码经 ClassifyByCloudCover（带云量与不带云量各一次）：
// Cloudy 只由逐小时云量分档产出（WMO 2/3 已让给 PartlyCloudy/Overcast），
// 只枚举 Classify 会漏掉它。
func TestSevenClasses(t *testing.T) {
	set := map[Class]bool{}
	cc30, cc90 := 30, 90
	for _, c := range append(CoreCodes(), 101, 110) {
		cl, _ := Classify(c)
		set[cl] = true
		cl, _ = ClassifyByCloudCover(c, &cc30)
		set[cl] = true
		cl, _ = ClassifyByCloudCover(c, &cc90)
		set[cl] = true
	}
	if len(set) != 9 {
		names := make([]string, 0, len(set))
		for k := range set {
			names = append(names, string(k))
		}
		sort.Strings(names)
		t.Fatalf("类数 = %d（%v），期望 9", len(set), names)
	}
}

// TestUnknownCodeFallsBackClear：未知码不得空白/崩。
func TestUnknownCodeFallsBackClear(t *testing.T) {
	for _, c := range []int{-1, 4, 44, 50, 94, 100, 111, 9999} {
		if cl, _ := Classify(c); cl != Clear {
			t.Errorf("未知码 %d 应回落 clear，实得 %q", c, cl)
		}
	}
}

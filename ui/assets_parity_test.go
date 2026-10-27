//go:build !vulkan

// TestAssetsParity（钉死测试 · ui 侧）：
// cloudFormOf 组装结果逐条对拍（含宽度 wLo/wHi 与白名单下标，数字/下标写死）。
// 期望值 = 迁移前旧实现打印的基线（
// TestParityDumpB6 在旧代码上跑出的 9 行 TIER + 1 行 WHITELIST）。
// 宽度口径 = kinetics.WidthFor（规则侧，留 ui/effects.go，见 边界说明）。
package ui

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

func TestAssetsParity(t *testing.T) {
	// ── 白名单集合逐字对拍（ui 侧消费口 cloudIdxAllowed = assets 同一底层数组）──
	wantWL := []int{0, 1, 3}
	if got := len(cloudIdxAllowed); got != len(wantWL) {
		t.Fatalf("白名单长度 = %d，期望 %d", got, len(wantWL))
	}
	for i, w := range wantWL {
		if cloudIdxAllowed[i] != w {
			t.Fatalf("白名单下标集合 [%d]=%d，期望 %d（逐字：0,1,3）", i, cloudIdxAllowed[i], w)
		}
	}

	// ── 档表逐条对拍（wLo/wHi/lift/aLo/aHi/pressTop/idx 全列，旧实现基线写死）──
	want := []struct {
		cls  effect.Class
		name string
		wLo  float64
		wHi  float64
		lift float64
		aLo  float64
		aHi  float64
		top  bool
	}{
		{effect.Clear, "clear", 480, 529, 140, 0.85, 0.95, false},
		{effect.PartlyCloudy, "partly-cloudy", 480, 529, 115, 0.80, 0.92, false},
		{effect.Cloudy, "cloudy", 480, 529, 45, 0.75, 0.90, false},
		{effect.Overcast, "overcast", 480, 529, 10, 0.85, 0.95, true},
		{effect.Rain, "rain", 768, 1178, -32, 0.88, 0.96, true},
		{effect.Thunder, "thunder", 768, 1178, -32, 0.88, 0.96, true},
		{effect.Fog, "fog", 480, 529, 0, 0.92, 1.0, false},
		{effect.Snow, "snow", 768, 1178, 0, 0.92, 1.0, false},
		{effect.Dust, "dust", 768, 1178, 0, 0.92, 1.0, false},
	}
	for _, w := range want {
		got := cloudFormOf(w.cls)
		if got.WLo != w.wLo || got.WHi != w.wHi || got.Lift != w.lift ||
			got.ALo != w.aLo || got.AHi != w.aHi || got.PressTop != w.top {
			t.Fatalf("档表 %v = wLo=%v wHi=%v lift=%v aLo=%v aHi=%v pressTop=%v，期望 %v/%v/%v/%v/%v/%v",
				w.name, got.WLo, got.WHi, got.Lift, got.ALo, got.AHi, got.PressTop,
				w.wLo, w.wHi, w.lift, w.aLo, w.aHi, w.top)
		}
		if len(got.Idx) != 3 || got.Idx[0] != 0 || got.Idx[1] != 1 || got.Idx[2] != 3 {
			t.Fatalf("档表 %v 的 idx = %v，期望逐字 [0 1 3]", w.name, got.Idx)
		}
	}
}

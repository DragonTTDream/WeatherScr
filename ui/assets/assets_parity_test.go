//go:build !vulkan

// TestAssetsParity（钉死测试 · assets 侧）：
// 白名单集合逐字对拍 + 云贴图档表逐条对拍。
// 期望值 = 迁移前旧实现（ui/effects.go:636 白名单 + cloudFormOf 表体）
// 打印的基线（TestParityDumpB6 旧代码复跑），数字/下标写死。
package assets

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

func TestAssetsParity(t *testing.T) {
	// ── 白名单逐字对拍： = cloud1/cloud2/cloud4 ⇒ 0 基下标 {0,1,3} ──
	wantWL := []int{0, 1, 3}
	if got := len(CloudIdxAllowed); got != len(wantWL) {
		t.Fatalf("白名单长度 = %d，期望 %d", got, len(wantWL))
	}
	for i, w := range wantWL {
		if CloudIdxAllowed[i] != w {
			t.Fatalf("白名单下标集合 [%d]=%d，期望 %d（逐字：0,1,3）", i, CloudIdxAllowed[i], w)
		}
	}

	// ── 档表逐条对拍（数字写死，来源 = 旧实现打印基线）──
	type row struct {
		lift     float64
		aLo, aHi float64
		pressTop bool
	}
	want := []struct {
		cls  effect.Class
		name string
		row  row
	}{
		{effect.Clear, "clear", row{140, 0.85, 0.95, false}},
		{effect.PartlyCloudy, "partly-cloudy", row{115, 0.80, 0.92, false}},
		{effect.Cloudy, "cloudy", row{45, 0.75, 0.90, false}},
		{effect.Overcast, "overcast", row{10, 0.85, 0.95, true}},
		{effect.Rain, "rain", row{-32, 0.88, 0.96, true}},
		{effect.Thunder, "thunder", row{-32, 0.88, 0.96, true}},
		{effect.Fog, "fog", row{0, 0.92, 1.0, false}},
		{effect.Snow, "snow", row{0, 0.92, 1.0, false}}, // 旧 cloudFormOf fall-through 默认档
		{effect.Dust, "dust", row{0, 0.92, 1.0, false}}, // 同上
	}
	for _, w := range want {
		got := CloudTier(w.cls)
		if got.Lift != w.row.lift || got.ALo != w.row.aLo || got.AHi != w.row.aHi || got.PressTop != w.row.pressTop {
			t.Fatalf("档表 %v = lift=%v aLo=%v aHi=%v pressTop=%v，期望 lift=%v aLo=%v aHi=%v pressTop=%v",
				w.name, got.Lift, got.ALo, got.AHi, got.PressTop, w.row.lift, w.row.aLo, w.row.aHi, w.row.pressTop)
		}
		if len(got.Idx) != 3 || got.Idx[0] != 0 || got.Idx[1] != 1 || got.Idx[2] != 3 {
			t.Fatalf("档表 %v 的 idx = %v，期望逐字 [0 1 3]", w.name, got.Idx)
		}
	}
}

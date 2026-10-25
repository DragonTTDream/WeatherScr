package kinetics

import "testing"

// TestKineticsConstants 钉死 R1 速度档与 R4 口径值（spec/b3-kinetics.md §2 逐字）。
func TestKineticsConstants(t *testing.T) {
	if VxMax != 0.0140 {
		t.Fatalf("VxMax = %v，期望逐字 0.0140（R1 最快的云不能比现在更快）", VxMax)
	}
	if VxTier0 != 0.0140 || VxTier1 != 0.0085 || VxTier2 != 0.0040 || VxTier3 != 0.0022 {
		t.Fatalf("速度档 = %v/%v/%v/%v，期望逐字 0.0140/0.0085/0.0040/0.0022",
			VxTier0, VxTier1, VxTier2, VxTier3)
	}
	if WidthFloor != 0.444 || GapRatio != 0.0194 || MaxPerBand != 2 {
		t.Fatalf("口径 = %v/%v/%v，期望逐字 0.444/0.0194/2", WidthFloor, GapRatio, MaxPerBand)
	}
	if !(VxTier0 >= VxTier1 && VxTier1 >= VxTier2 && VxTier2 >= VxTier3) {
		t.Fatalf("速度档必须单调不增（越大越慢）：%v/%v/%v/%v", VxTier0, VxTier1, VxTier2, VxTier3)
	}
}

// TestKineticsBandCapacity 钉死 BandCapacity(1080)==(2,480,21)、BandCapacity(1920)==(2,853,37)。
func TestKineticsBandCapacity(t *testing.T) {
	n, minPx, gapPx := BandCapacity(1080)
	if n != 2 || minPx != 480 || gapPx != 21 {
		t.Fatalf("BandCapacity(1080) = (%d,%d,%d)，期望 (2,480,21)", n, minPx, gapPx)
	}
	n, minPx, gapPx = BandCapacity(1920)
	if n != 2 || minPx != 853 || gapPx != 37 {
		t.Fatalf("BandCapacity(1920) = (%d,%d,%d)，期望 (2,853,37)", n, minPx, gapPx)
	}
	// 同带 2 朵首尾相连恰好铺满：2×529 + 21 = 1079 ≤ 1080。
	if hi := FloorHi(1080); hi != 529 {
		t.Fatalf("FloorHi(1080) = %d，期望 529", hi)
	}
	if 2*FloorHi(1080)+GapPx(1080) > 1080 {
		t.Fatalf("两朵 %d px + 间隙 %d px 越出 1080", FloorHi(1080), GapPx(1080))
	}
}

// TestKineticsWidthFor 钉死逐类最终宽度区间：lo 全档 ≥ 480（@1080），Rain/Thunder 保 768–1178。
func TestKineticsWidthFor(t *testing.T) {
	lo, _ := WidthFor(ClassClear, 1080)
	if lo < 480 {
		t.Fatalf("Clear lo = %d，期望 ≥ 480", lo)
	}
	want := map[Class][2]int{
		ClassClear:        {480, 529},
		ClassPartlyCloudy: {480, 529},
		ClassCloudy:       {480, 529},
		ClassOvercast:     {480, 529},
		ClassRainThunder:  {768, 1178},
		ClassFogCloud:     {480, 529},
	}
	for cls, w := range want {
		gotLo, gotHi := WidthFor(cls, 1080)
		if gotLo != w[0] || gotHi != w[1] {
			t.Errorf("WidthFor(cls=%d,1080) = (%d,%d)，期望 (%d,%d)", cls, gotLo, gotHi, w[0], w[1])
		}
		if gotLo > gotHi {
			t.Errorf("WidthFor(cls=%d,1080) = (%d,%d)：lo > hi，区间无解", cls, gotLo, gotHi)
		}
	}
	// 横屏口径（1920）：下限 853、上限 (1920−37)/2 = 941。
	if lo1920, hi1920 := WidthFor(ClassOvercast, 1920); lo1920 != 853 || hi1920 != 941 {
		t.Errorf("WidthFor(Overcast,1920) = (%d,%d)，期望 (853,941)", lo1920, hi1920)
	}
}

// TestKineticsEntryOK 钉死进场排队判据的两条用例（spec/b3-kinetics.md §2 逐字）。
func TestKineticsEntryOK(t *testing.T) {
	if EntryOK(100, 50, 120, 50, 1080) {
		t.Fatalf("EntryOK(100,50,120,50,1080) = true，期望 false（|Δx|=20 < 上一条半宽 50 ⇒ 同出同进）")
	}
	if !EntryOK(100, 50, 600, 50, 1080) {
		t.Fatalf("EntryOK(100,50,600,50,1080) = false，期望 true（|Δx|=500 ≥ 50）")
	}
	// 边界：|Δx| 恰等于上一条半宽 ⇒ 放行（判据是 "< 半宽 ⇒ false"）。
	if !EntryOK(100, 50, 150, 50, 1080) {
		t.Fatalf("EntryOK(100,50,150,50,1080) = false，期望 true（|Δx|=50 = 上一条半宽 ⇒ 放行）")
	}
}

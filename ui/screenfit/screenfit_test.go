package screenfit

import "testing"

// TestScreenfitRefWidth 钉死：运行时 float64 与常量表达式 1178.0/1080.0 逐字相等。
func TestScreenfitRefWidth(t *testing.T) {
	if RefWidth != 1178.0/1080.0 {
		t.Fatalf("RefWidth=%v，期望逐字等于 1178.0/1080.0", RefWidth)
	}
}

// TestScreenfitPortrait 钉死：PortraitW/PortraitH 的 float64 逐字值。
func TestScreenfitPortrait(t *testing.T) {
	if PortraitW != 1080.0 {
		t.Fatalf("PortraitW=%v，期望 1080.0", PortraitW)
	}
	if PortraitH != 1920.0 {
		t.Fatalf("PortraitH=%v，期望 1920.0", PortraitH)
	}
}

// TestScreenfitFallback 钉死零屏/负屏/正常屏三分支。
func TestScreenfitFallback(t *testing.T) {
	cases := []struct {
		w, h         int
		wantW, wantH int
	}{
		{0, 0, 1080, 1920},
		{-5, -9, 1080, 1920},
		{1920, 1080, 1920, 1080},
	}
	for _, c := range cases {
		gw, gh := Fallback(c.w, c.h)
		if gw != c.wantW || gh != c.wantH {
			t.Fatalf("Fallback(%d,%d)=(%d,%d)，期望 (%d,%d)", c.w, c.h, gw, gh, c.wantW, c.wantH)
		}
	}
}

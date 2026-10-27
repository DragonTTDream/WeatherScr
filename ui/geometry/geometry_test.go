package geometry

// TestGeometryParity —— 钉死对拍（active.md:209 判据 3）。
// 期望数字 = **迁移前旧实现**对同一批固定输入的打印值（取数件 ui/zz_pre_geometry_test.go，
// 迁移前实跑，存档）：
// · 屏向常量对 旧 ui/layout.go:47-48（B2 薄壳）
// · 屏向判定 旧 ui/layout.go:67
// · px↔归一换算 旧 ui/layout.go:26-31（Rect.Px）、ui/effects.go:2333、:3380-3381、:3774、:3884、:3982-3983、:4235、:1408
// · 带宽分母 旧 ui/effects.go:2214-2227（bandWidthPx，P6"等宽"）
// 浮点一律按 `%v` 字符串逐字比较（末位差一比特也判红）。两制边界 = 1080×1920 与 1920×1080。

import (
	"fmt"
	"testing"
)

func eqF(t *testing.T, tag string, got float64, want string) {
	t.Helper()
	if s := fmt.Sprintf("%v", got); s != want {
		t.Errorf("%s: got %v want %v", tag, s, want)
	}
}

func TestGeometryParity(t *testing.T) {
	// ---- 屏向常量对（竖/横）----
	if PortraitW != 1080 || PortraitH != 1920 {
		t.Errorf("Portrait: got %vx%v want 1080x1920", PortraitW, PortraitH)
	}
	if LandscapeW != 1920 || LandscapeH != 1080 {
		t.Errorf("Landscape: got %vx%v want 1920x1080", LandscapeW, LandscapeH)
	}

	// ---- 屏向判定（逐字 w > h）----
	for _, c := range []struct {
		w, h int
		want bool
	}{
		{1920, 1080, true},
		{1080, 1920, false},
		{1080, 1080, false},
		{0, 0, false},
		{1920, 1920, false},
		{1080, 1081, false},
		{1079, 1080, false},
		{-5, -9, true},
	} {
		if got := IsLandscape(c.w, c.h); got != c.want {
			t.Errorf("IsLandscape(%v,%v): got %v want %v", c.w, c.h, got, c.want)
		}
	}

	// ---- 归一 → 像素（截断）：旧 Rect.Px 的四行口径 ----
	for _, c := range []struct {
		tag  string
		n    float64
		sz   int
		want int
	}{
		{"clock.X@1080", 0.013, 1080, 14},
		{"clock.Y@1920", 0.244, 1920, 468},
		{"clock.W@1080", 0.973, 1080, 1050},
		{"clock.H@1920", 0.210, 1920, 403},
		{"clock.X@1920", 0.013, 1920, 24},
		{"clock.Y@1080", 0.244, 1080, 263},
		{"clock.W@1920", 0.973, 1920, 1868},
		{"clock.H@1080", 0.210, 1080, 226},
		{"weather.X@1080", 0.027, 1080, 29},
		{"weather.Y@1920", 0.531, 1920, 1019},
		{"weather.W@1080", 0.946, 1080, 1021},
		{"weather.H@1920", 0.291, 1920, 558},
		{"weather.X@1920", 0.027, 1920, 51},
		{"weather.Y@1080", 0.531, 1080, 573},
		{"weather.W@1920", 0.946, 1920, 1816},
		{"weather.H@1080", 0.291, 1080, 314},
	} {
		n, sz := c.n, c.sz
		if got := NormToPx(n, sz); got != c.want {
			t.Errorf("NormToPx %s: got %v want %v", c.tag, got, c.want)
		}
	}

	// ---- 归一 → 像素（浮点）：旧 bandPlace 的 y 式与 spreadAir 的 minGap 式 ----
	// 带纵向口径（旧 ui/effects.go:738/746 非压顶 0.02/0.24，:754/755 压顶 -0.12/0.10）
	for _, c := range []struct {
		tag        string
		top, spred float64
		frac       float64
		h          int
		want       string
	}{
		{"press@1920 f=0", -0.12, 0.10, 0, 1920, "-230.39999999999998"},
		{"press@1920 f=.5", -0.12, 0.10, 0.5, 1920, "-134.39999999999998"},
		{"press@1920 f=1", -0.12, 0.10, 1, 1920, "-38.39999999999998"},
		{"press@1080 f=0", -0.12, 0.10, 0, 1080, "-129.6"},
		{"press@1080 f=.5", -0.12, 0.10, 0.5, 1080, "-75.6"},
		{"press@1080 f=1", -0.12, 0.10, 1, 1080, "-21.59999999999999"},
		{"plain@1920 f=0", 0.02, 0.24, 0, 1920, "38.4"},
		{"plain@1920 f=.5", 0.02, 0.24, 0.5, 1920, "268.79999999999995"},
		{"plain@1920 f=1", 0.02, 0.24, 1, 1920, "499.20000000000005"},
		{"plain@1080 f=0", 0.02, 0.24, 0, 1080, "21.6"},
		{"plain@1080 f=.5", 0.02, 0.24, 0.5, 1080, "151.2"},
		{"plain@1080 f=1", 0.02, 0.24, 1, 1080, "280.8"},
	} {
		top, spred, frac := c.top, c.spred, c.frac
		eqF(t, "NormPx(band) "+c.tag, NormPx(top+spred*frac, c.h), c.want)
	}
	eqF(t, "NormPx(minGap@1080)", NormPx(0.10, 1080), "108")
	eqF(t, "NormPx(minGap@1920)", NormPx(0.10, 1920), "192")

	// ---- 像素 → 归一：旧 airHalfN / bandSpan / CelestialCover / driftLayers / 云速 dw 口径 ----
	// 组合量一律在循环里用变量算（与旧代码同为运行时算术；常量表达式会被高精度折叠，末位可能差 1 ulp）。
	for _, c := range []struct {
		tag   string
		px    float64
		y, dh float64 // 旧 driftLayers：bp.y + bp.dh/2（给出时用它算 px）
		szX   int     // 旧 airHalfN：float64(sz.X) * scale / 2
		scale float64
		sz    int
		want  string
	}{
		{"T=1000@1080", 1000, 0, 0, 0, 0, 1080, "0.9259259259259259"},
		{"span=3080@1080", 3080, 0, 0, 0, 0, 1080, "2.8518518518518516"},
		{"span=3728@1920", 3728, 0, 0, 0, 0, 1920, "1.9416666666666667"},
		{"hi=904@1920", 904, 0, 0, 0, 0, 1920, "0.4708333333333333"},
		{"bandCy@1920 i0", 0, -230.39999999999998, 742.1875, 0, 0, 1920, "0.07327799479166668"},
		{"bandCy@1920 i1", 0, -134.39999999999998, 791.015625, 0, 0, 1920, "0.13599365234375002"},
		{"bandCy@1920 i2", 0, -38.39999999999998, 705.078125, 0, 0, 1920, "0.16361409505208335"},
		{"bandCy@1080 i0", 0, -129.6, 670.9375, 0, 0, 1080, "0.19061921296296297"},
		{"bandCy@1080 i1", 0, -75.6, 715.078125, 0, 0, 1080, "0.2610546875"},
		{"bandCy@1080 i2", 0, -21.59999999999999, 637.390625, 0, 0, 1080, "0.2750882523148149"},
		{"cloudDrawW@1080", 481.09087613869275, 0, 0, 0, 0, 1080, "0.445454514943234"},
		{"cloudDrawW@1920", 873.735817468271, 0, 0, 0, 0, 1920, "0.45507073826472444"},
		{"airHalfHW@1080", 0, 0, 0, 177, 1.2063673343422403, 1080, "0.09885510100860025"},
	} {
		sz, y, dh, scale := c.sz, c.y, c.dh, c.scale
		px := c.px
		if c.szX > 0 {
			px = float64(c.szX) * scale / 2
		} else if c.dh != 0 {
			px = y + dh/2
		}
		eqF(t, "PxToNorm "+c.tag, PxToNorm(px, sz), c.want)
	}

	// ---- 带宽分母：旧 bandWidthPx（T = round(w×1000/1080)，硬上限 0.95·w）----
	for _, c := range []struct {
		w    int
		want string
	}{
		{1080, "1000"},
		{1920, "1778"},
		{100, "93"},
		{0, "0"},
		{2000, "1852"},
		{540, "500"},
		{38, "35"},
		{-20, "-19"},
		{1081, "1001"},
	} {
		eqF(t, fmt.Sprintf("BandWidthPx(%v)", c.w), BandWidthPx(c.w), c.want)
	}
}

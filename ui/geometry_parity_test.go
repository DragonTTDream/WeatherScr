package ui

// TestGeometryMigrationParity —— R5 卡 5（屏向与几何基准迁进 ui/geometry）的 ui 侧对拍。
//
// 两条口径同批量：
//  1. 旧式原件逐字抄在本文件里（old* 函数 = 迁移前的 ui/layout.go、ui/effects.go 函数体），
//     与 ui/geometry 的新函数同输入比 `%v` 字符串（末位差一比特即红）。
//  2. 线上通路（Field 的带几何与云落位）的取数：期望数字 = 迁移前探针
//     ui/zz_pre_geometry_test.go 的打印值（存档 /tmp/geometry.pre.txt，2026-10-26 实跑），
//     数字写死在本文件里 ⇒ 迁移后复跑必须逐字一致。

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/geometry"
	"github.com/DragonTTDream/WeatherScr/ui/screenfit"
)

// ---- 旧式原件（迁移前的函数体，逐字抄录，不参与生产）----

func oldNormToPx(n float64, size int) int { return int(n * float64(size)) }

func oldNormPx(n float64, size int) float64 { return float64(size) * n }

func oldPxToNorm(px float64, size int) float64 { return px / float64(size) }

func oldBandWidthPx(w int) float64 {
	t := math.Round(float64(w) * 1000.0 / 1080.0)
	if cap := 0.95 * float64(w); t > cap {
		t = cap
	}
	return t
}

func oldIsLandscape(w, h int) bool { return w > h }

func eqS(t *testing.T, tag, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %s want %s", tag, got, want)
	}
}

func TestGeometryMigrationParity(t *testing.T) {
	// 1) 换算式与带宽式：新函数 == 旧式原件
	for _, c := range []struct {
		n  float64
		sz int
	}{
		{0.013, 1080}, {0.244, 1920}, {0.973, 1080}, {0.210, 1920},
		{0.027, 1920}, {0.531, 1080}, {0.946, 1920}, {0.291, 1080},
	} {
		n, sz := c.n, c.sz
		eqS(t, fmt.Sprintf("NormToPx(%v,%v)", n, sz),
			fmt.Sprintf("%v", geometry.NormToPx(n, sz)), fmt.Sprintf("%v", oldNormToPx(n, sz)))
		eqS(t, fmt.Sprintf("NormPx(%v,%v)", n, sz),
			fmt.Sprintf("%v", geometry.NormPx(n, sz)), fmt.Sprintf("%v", oldNormPx(n, sz)))
	}
	for _, c := range []struct {
		px float64
		sz int
	}{
		{1000, 1080}, {3080, 1080}, {3728, 1920}, {904, 1920},
		{481.09087613869275, 1080}, {873.735817468271, 1920}, {742.1875, 1920},
	} {
		px, sz := c.px, c.sz
		eqS(t, fmt.Sprintf("PxToNorm(%v,%v)", px, sz),
			fmt.Sprintf("%v", geometry.PxToNorm(px, sz)), fmt.Sprintf("%v", oldPxToNorm(px, sz)))
	}
	for _, w := range []int{1080, 1920, 100, 0, 2000, 540, 38, -20, 1081, 1079} {
		eqS(t, fmt.Sprintf("BandWidthPx(%v)", w),
			fmt.Sprintf("%v", geometry.BandWidthPx(w)), fmt.Sprintf("%v", oldBandWidthPx(w)))
		eqS(t, fmt.Sprintf("bandWidthPx shell(%v)", w),
			fmt.Sprintf("%v", bandWidthPx(w)), fmt.Sprintf("%v", oldBandWidthPx(w)))
	}
	for _, c := range [][2]int{{1920, 1080}, {1080, 1920}, {1080, 1080}, {0, 0}, {1920, 1920}, {1080, 1081}, {1079, 1080}, {-5, -9}} {
		if geometry.IsLandscape(c[0], c[1]) != oldIsLandscape(c[0], c[1]) || IsLandscape(c[0], c[1]) != oldIsLandscape(c[0], c[1]) {
			t.Errorf("IsLandscape(%v,%v): got %v/%v want %v", c[0], c[1],
				geometry.IsLandscape(c[0], c[1]), IsLandscape(c[0], c[1]), oldIsLandscape(c[0], c[1]))
		}
	}
	// 屏向常量对：旧薄壳口径 int(screenfit.PortraitW/H) 与横竖对调
	eqS(t, "PortraitW", fmt.Sprintf("%v", PortraitW), fmt.Sprintf("%v", int(screenfit.PortraitW)))
	eqS(t, "PortraitH", fmt.Sprintf("%v", PortraitH), fmt.Sprintf("%v", int(screenfit.PortraitH)))
	eqS(t, "PortraitW/geometry", fmt.Sprintf("%v", PortraitW), fmt.Sprintf("%v", geometry.PortraitW))
	eqS(t, "Landscape 对调", fmt.Sprintf("%v %v", geometry.LandscapeW, geometry.LandscapeH),
		fmt.Sprintf("%v %v", PortraitH, PortraitW))

	// 2) 线上通路（Field 带几何/云落位）数字 = 迁移前探针打印值（写死）
	for _, c := range []struct {
		w, h int
		want map[string]string
	}{
		{1080, 1920, map[string]string{
			"span":           "3080",
			"bandWidthFor":   "1000",
			"bandCount":      "3",
			"bandPlace0":     "-60.40003758399996 -230.39999999999998 1000 742.1875 false",
			"bandPlace1":     "966.2666290826669 -134.39999999999998 1000 791.015625 true",
			"bandPlace2":     "1992.9332957493334 -38.39999999999998 1000 705.078125 false",
			"bandCy0":        "0.07327799479166668",
			"DriftOverlap":   "0.974025974025974 0",
			"CelestialCover": "1 24.107142857142858",
			"BandRegionPx":   "-230 667",
			"airHalfN0":      "0.222727257471617 0.0929842798526526 1.2063673343422403 0.7788926230920439 0.009180923312023283 481.09087613869275",
			"airHalfN1":      "0.22226407009880614 0.09889557318239116 1.0121013437922168 0.9264650733650663 0.00919094514543432 480.0903914134213",
			"landscapeRange": "480 508",
		}},
		{1920, 1080, map[string]string{
			"span":           "3728",
			"bandWidthFor":   "904",
			"bandCount":      "3",
			"bandPlace0":     "766.399933184 -129.6 904 670.9375 false",
			"bandPlace1":     "2009.0665998506665 -75.6 904 715.078125 true",
			"bandPlace2":     "-476.2667334826665 -21.59999999999999 904 637.390625 false",
			"bandCy0":        "0.19061921296296297",
			"DriftOverlap":   "0.7274678111587983 0",
			"CelestialCover": "1 100",
			"BandRegionPx":   "-130 639",
			"airHalfN0":      "0.22753536913236222 0.3002202787163113 2.1909506110331374 0.7788926230920439 0.009076891775513032 873.735817468271",
			"airHalfN1":      "0.2264872684340097 0.3184977212353261 1.8334792774057778 0.9264650733650663 0.009099569186887913 869.7111107865973",
			"landscapeRange": "853 904",
		}},
	} {
		f := NewField(effect.Overcast, false, 7)
		for i := 0; i < 800; i++ {
			f.Step(time.Second, c.w, c.h)
		}
		eqS(t, fmt.Sprintf("span@%vx%v", c.w, c.h), fmt.Sprintf("%v", f.bandSpan(c.w)), c.want["span"])
		eqS(t, fmt.Sprintf("bandWidthFor@%vx%v", c.w, c.h), fmt.Sprintf("%v", f.bandWidthFor(c.w, c.h)), c.want["bandWidthFor"])
		eqS(t, fmt.Sprintf("bandCount@%vx%v", c.w, c.h), fmt.Sprintf("%v", f.bandCount()), c.want["bandCount"])
		for i := 0; i < f.bandCount(); i++ {
			bp, _ := f.bandPlace(i, f.t, c.w, c.h)
			eqS(t, fmt.Sprintf("bandPlace%v@%vx%v", i, c.w, c.h),
				fmt.Sprintf("%v %v %v %v %v", bp.x, bp.y, bp.dw, bp.dh, bp.mirror), c.want[fmt.Sprintf("bandPlace%v", i)])
		}
		bp, _ := f.bandPlace(0, f.t, c.w, c.h)
		eqS(t, fmt.Sprintf("bandCy0@%vx%v", c.w, c.h), fmt.Sprintf("%v", (bp.y+bp.dh/2)/float64(c.h)), c.want["bandCy0"])
		l, dp := f.DriftOverlapBandOnly()
		eqS(t, fmt.Sprintf("DriftOverlap@%vx%v", c.w, c.h), fmt.Sprintf("%v %v", l, dp), c.want["DriftOverlap"])
		n, cov := f.CelestialCover(c.w, c.h)
		eqS(t, fmt.Sprintf("CelestialCover@%vx%v", c.w, c.h), fmt.Sprintf("%v %v", n, cov), c.want["CelestialCover"])
		top, bottom := f.BandRegionPx(c.w, c.h)
		eqS(t, fmt.Sprintf("BandRegionPx@%vx%v", c.w, c.h), fmt.Sprintf("%v %v", top, bottom), c.want["BandRegionPx"])
		shown := 0
		for i := 0; i < f.ActiveCount() && shown < 2; i++ {
			p := &f.parts[i]
			if p.kind != pkCloud {
				continue
			}
			hw, hh := f.airHalfN(p)
			eqS(t, fmt.Sprintf("airHalfN%v@%vx%v", shown, c.w, c.h),
				fmt.Sprintf("%v %v %v %v %v %v", hw, hh, p.sc, p.scaleMul, p.vx, cloudDrawWidthPx(p)),
				c.want[fmt.Sprintf("airHalfN%v", shown)])
			shown++
		}
		lo, hi := landscapeCloudWidthRangePx(c.w)
		eqS(t, fmt.Sprintf("landscapeRange@%vx%v", c.w, c.h), fmt.Sprintf("%v %v", lo, hi), c.want["landscapeRange"])
	}
}

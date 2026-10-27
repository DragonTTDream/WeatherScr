// Package sky 的天空层对拍（判据 3）。
// 常量来源 = 迁移**前**的 ui 旧实现跑同一固定输入打印出来的数字（逐字抄录、不改位）：
//
//	取数件 = ui/zz_pre_sky_test.go（临时，取数后删除），命令
//	WS_PRE_SKY=1 go test -tags=novulkan,nox11 -count=1 -run TestZZPreSkyNumbers ./ui -v
//	数字 = 该命令 stdout 的 PRE 行（%v 全精度打印），本文件逐字抄录进 skyWant
//
// 判据 = 迁移后的每个函数在固定输入下与旧实现**逐位相等**（比较 %v 字符串，
// float64 的 %v 是最短往返表示 ⇒ 末位不同也会红）。
// 本文件只覆盖**不触贴图**的那一半（公式与常量、CPU 混合小工具、纯程序化烘焙）。
// 走贴图口的烘焙（天空底图覆盖 / 月亮贴图 / 太阳泛光与光线剖面）在 ui 侧对拍：
// 贴图目录归 ui，消费者 ui 注入 assets 查询口后才有线上口径 ⇒ 见 ui/sky_parity_baked_test.go
// 的 TestSkyParityBaked（同一份取数件、同一批常量）。
// 没有数字对拍的迁移件 = 只在 gioui 操作流里绘制的函数（SkyGradient / SkyDepth / GradAlpha /
// SkyBlit / MoonBlit / DrawMoon / Disc / DiscColor）：它们没有可脱离 GPU 的返回值，
// 判据 = 44 张基线出图逐像素 diff=0（第 5 步口径）。
package sky

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// skyWant = 迁移前旧实现在固定输入下的输出（%v 逐字抄录）。
var skyWant = map[string]string{
	"SkyColor/clear/true":          "[{42 109 184 255}]",
	"SkyColor/clear/false":         "[{10 20 40 255}]",
	"SkyColor/partly-cloudy/true":  "[{107 119 132 255}]",
	"SkyColor/partly-cloudy/false": "[{28 36 48 255}]",
	"SkyColor/cloudy/true":         "[{107 119 132 255}]",
	"SkyColor/cloudy/false":        "[{28 36 48 255}]",
	"SkyColor/overcast/true":       "[{86 95 106 255}]",
	"SkyColor/overcast/false":      "[{22 29 38 255}]",
	"SkyColor/rain/true":           "[{55 74 94 255}]",
	"SkyColor/rain/false":          "[{16 26 38 255}]",
	"SkyColor/thunder/true":        "[{43 47 68 255}]",
	"SkyColor/thunder/false":       "[{12 14 26 255}]",
	"SkyColor/fog/true":            "[{154 162 168 255}]",
	"SkyColor/fog/false":           "[{42 48 54 255}]",
	"SkyColor/snow/true":           "[{168 184 200 255}]",
	"SkyColor/snow/false":          "[{46 56 70 255}]",
	"SkyColor/dust/true":           "[{168 140 94 255}]",
	"SkyColor/dust/false":          "[{58 48 32 255}]",
	"Consts/scale":                 "[2 4 4 2 4 0.06 2.2 256]",
	"Consts/sun":                   "[0.045 0.12 120 0 1 3 3.8 sunbloom.png 80 6 72 0.8 0.2 85 0.75 0.25]",
	"Consts/sun2":                  "[200 0.18 0.9 90 150 0.9 70 0.06 12 2 sunrays.png sunglow.png 1.4 1.6 22 46 0.09 90]",
	"GlowTab":                      "[256 1 0.9779040227602094 0.058562761715430754 0.0033538167611646937]",
	"MoonPhaseKey":                 "[-1 0 139 400 720 0]",
	"SunDimOf":                     "[1 false 0.96 false 0.84 false 0.64 false 0.52 false 0.4 false 0.32799999999999996 false 0.31999999999999995 true 0.28 true 0.2 true]",
	"SunDiscVisibility":            "[1 1 0.6 0 0 1 1 0.6 0 0 1 1 0.6 0 0 0 0 0 0 0 1 1 0 0 0]",
	"MathHelpers":                  "[0 1 -3 0 1 3 0.5 0 6 1 0 9 1 0 15 0.8826450353803235 0.8219991858335028 0.7606349540834042 1.1 1 0.9]",
	"BlendOps":                     "[40 60 1331329221589601074 [0 1 2 3] [105 149 110 255] [58 59 60 61] [88 165 130 255] [60 85 80 255]]",
	"BakeStars":                    "[200 300 468759310747061804 [0 0 0 0] [0 0 0 0] [0 0 0 0] [0 0 0 0] [0 0 0 0]]",
	"BakeMoonCrisp":                "[200 300 10667624561290460733 [0 0 0 0] [0 0 0 0] [0 0 0 0] [0 0 0 0] [0 0 0 0]]",
	"SkyBaseCPU":                   "[270 480 13127579207462348909 [13 36 73 255] [47 107 173 255] [54 119 177 255] [41 95 158 255] [37 88 149 255] 270 480 7071179427390409733 [3 7 16 255] [18 27 43 255] [23 32 46 255] [15 23 39 255] [13 21 36 255] 270 480 13792444617545093737 [35 38 52 255] [105 116 126 255] [117 129 129 255] [94 104 115 255] [86 95 108 255] 270 480 6200032602355522253 [9 12 19 255] [34 42 50 255] [41 49 54 255] [30 36 45 255] [27 33 42 255] 270 480 13792444617545093737 [35 38 52 255] [105 116 126 255] [117 129 129 255] [94 104 115 255] [86 95 108 255] 270 480 6200032602355522253 [9 12 19 255] [34 42 50 255] [41 49 54 255] [30 36 45 255] [27 33 42 255] 270 480 6042438557220073057 [27 31 42 255] [86 94 103 255] [97 106 106 255] [76 84 94 255] [70 77 88 255] 270 480 17459795445282154245 [7 9 14 255] [29 35 41 255] [35 42 45 255] [25 31 37 255] [22 28 34 255] 270 480 17970700616715845073 [17 24 37 255] [58 76 92 255] [66 85 95 255] [51 67 83 255] [47 61 78 255] 270 480 15895958130118474409 [5 8 14 255] [24 32 41 255] [29 38 45 255] [20 28 37 255] [18 25 34 255] 270 480 8943111581489104281 [14 15 26 255] [48 51 68 255] [55 59 71 255] [42 45 62 255] [38 41 58 255] 270 480 6607445008837984949 [3 4 10 255] [20 22 31 255] [25 26 34 255] [17 18 27 255] [15 16 25 255] 270 480 16240158129464941925 [50 52 66 255] [148 155 158 255] [163 172 163 255] [132 138 145 255] [121 127 136 255] 270 480 12654160128251045617 [13 15 21 255] [47 52 56 255] [54 60 60 255] [41 46 50 255] [37 42 47 255] 270 480 4076869526833943017 [54 60 80 255] [160 169 183 255] [177 185 185 255] [143 151 168 255] [132 140 158 255] 270 480 6851908532004677441 [14 18 27 255] [50 60 70 255] [58 68 74 255] [44 53 64 255] [40 48 59 255] 270 480 7688372268970603601 [54 46 37 255] [160 135 92 255] [177 150 95 255] [143 121 83 255] [132 111 78 255] 270 480 4843124293947705721 [19 15 12 255] [61 52 36 255] [70 60 39 255] [54 46 32 255] [49 42 29 255]]",
}

// skyFnv FNV-64a：整张图逐字节的校验和（任一像素差都会改变它）。
func skyFnv(img *image.NRGBA) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range img.Pix {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

// skySmp 固定坐标取样（越界钳位）。
func skySmp(img *image.NRGBA, x, y int) [4]uint8 {
	b := img.Bounds()
	if x < b.Min.X {
		x = b.Min.X
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if x > b.Max.X-1 {
		x = b.Max.X - 1
	}
	if y > b.Max.Y-1 {
		y = b.Max.Y - 1
	}
	i := img.PixOffset(x, y)
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

func skyImgVals(img *image.NRGBA) []any {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	return []any{w, h, skyFnv(img), skySmp(img, 0, 0), skySmp(img, w/2, h/2),
		skySmp(img, w-1, h-1), skySmp(img, w/3, (h*2)/5), skySmp(img, (w*4)/5, h/3)}
}

func colorOf(r, g, b int) color.NRGBA {
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}

func skyCheck(t *testing.T, key string, vals []any) {
	t.Helper()
	got := fmt.Sprint(vals)
	want, ok := skyWant[key]
	if !ok {
		t.Fatalf("对拍常量表里没有 %s", key)
	}
	if got != want {
		t.Errorf("%s 对拍失败\n got = %s\nwant = %s", key, got, want)
	}
}

func TestSkyParity(t *testing.T) {
	cls := []effect.Class{effect.Clear, effect.PartlyCloudy, effect.Cloudy,
		effect.Overcast, effect.Rain, effect.Thunder, effect.Fog, effect.Snow, effect.Dust}
	ph174, ph5, ph9 := 0.174, 0.5, 0.9

	// ---- 天空底色 ----
	for _, c := range cls {
		for _, day := range []bool{true, false} {
			skyCheck(t, fmt.Sprintf("SkyColor/%s/%v", string(c), day), []any{SkyColor(c, day)})
		}
	}
	// ---- 尺度与常量 ----
	skyCheck(t, "Consts/scale", []any{SkyCacheScale, SkyCacheBootScale, BakeScale(true), BakeScale(false),
		SkyCacheScale * 2, MoonRcFrac, MoonHaloSpread, GlowTabN})
	skyCheck(t, "Consts/sun", []any{SunDiscRcFrac, SunFeatherFrac, SunDiscLevel, SunLiftLevel,
		SunLiftR0Frac, SunLiftR1Frac, SunLiftR2Frac, SunBloomAsset, SunBloomLevel,
		SunBloomRcFrac, SunBloomAngSteps, SunDimSlope, SunDimFloor, SunNoDiscPct,
		SunRcDimMin, SunRcDimSlope})
	skyCheck(t, "Consts/sun2", []any{SunNearAmp, SunNearLFrac, SkyLiftLFrac, SunSkyLiftAmp, SunMainAmp,
		SunMainLFrac, SunCoreExpAmp, SunCoreExpLFrac, SunSkyUniform, SunScreenLayers,
		SunRaysAsset, SunGlowAsset, SunRaysWidthFrac, SunGlowWidthFrac, SunRaysAmp,
		SunCoreAmp, SunCoreLFrac, SunGlowAmp})
	skyCheck(t, "GlowTab", []any{len(GlowTab), GlowTab[0], GlowTab[1], GlowTab[127], GlowTab[255]})
	skyCheck(t, "MoonPhaseKey", []any{MoonPhaseKey(nil), MoonPhaseKey(&[]float64{0}[0]),
		MoonPhaseKey(&ph174), MoonPhaseKey(&ph5), MoonPhaseKey(&ph9), MoonPhaseKey(&[]float64{1.0}[0])})
	// ---- 日照渐变解析函数 ----
	var out []any
	for _, pct := range []int{0, 5, 20, 45, 60, 75, 84, 85, 90, 100} {
		out = append(out, SunDimOf(pct), SunNoDisc(pct))
	}
	skyCheck(t, "SunDimOf", out)
	out = nil
	for _, c := range []effect.Class{effect.Clear, effect.PartlyCloudy, effect.Cloudy, effect.Overcast, effect.Rain} {
		for _, pct := range []int{0, 30, 60, 85, 100} {
			out = append(out, SunDiscVisibility(c, pct))
		}
	}
	skyCheck(t, "SunDiscVisibility", out)
	// 注：SunDiscProfile / SunSkyLift / SunBloomAt / SunRaysAt / SunCoreAt / SunAdditiveAt /
	// SunGlowAt / SunMaskAt / BakeSunGlow 会读太阳贴图剖面（sunBloomRadial / sunRadialMask），
	// 属贴图口半区 ⇒ 在注入真实贴图口的 ui 侧对拍（ui/sky_parity_baked_test.go），本文件不重复量。
	out = nil
	for _, v := range []float64{-1, 0, 0.5, 1, 2} {
		out = append(out, Clamp01(v), Wl2(v, 0.25), LerpF(3, 9, v))
	}
	a1, b1, c1 := ScreenKeep(0.9, 0.85, 0.8, 18, colorOf(70, 120, 180))
	a2, b2, c2 := ScreenKeep(1.1, 1.0, 0.9, -6, colorOf(200, 40, 10))
	skyCheck(t, "MathHelpers", append(out, a1, b1, c1, a2, b2, c2))
	// ---- CPU 混合小工具 ----
	dst := image.NewNRGBA(image.Rect(0, 0, 40, 60))
	for i := range dst.Pix {
		dst.Pix[i] = uint8(i % 251)
	}
	BlendAlphaV(dst, image.Rect(2, 3, 30, 50), colorOf(200, 120, 40), 10, 200)
	BlendAlphaH(dst, image.Rect(1, 5, 39, 55), colorOf(30, 200, 160), 200, 5)
	BlendPix(dst, 7, 11, colorOf(255, 255, 255), 0.37)
	AddPix(dst, 13, 19, colorOf(10, 20, 30), 0.61)
	skyCheck(t, "BlendOps", skyImgVals(dst))
	// ---- 纯程序化烘焙 ----
	stars := image.NewNRGBA(image.Rect(0, 0, 200, 300))
	BakeStars(stars, 200, 300)
	skyCheck(t, "BakeStars", skyImgVals(stars))
	crisp := image.NewNRGBA(image.Rect(0, 0, 200, 300))
	BakeMoonCrisp(crisp, 200, 300)
	skyCheck(t, "BakeMoonCrisp", skyImgVals(crisp))
	out = nil
	for _, c := range cls {
		for _, day := range []bool{true, false} {
			out = append(out, skyImgVals(SkyBaseCPU(270, 480, c, day))...)
		}
	}
	skyCheck(t, "SkyBaseCPU", out)
}

//go:build !vulkan

// B4/ 横屏版式钉死单测（26-33'硬判据 1'，四条逐字）：
// 对 1920×1080（取证照片 flipSCR.png 口径）
//	① 面板顶沿 y ≥ 云带下沿（云带下沿从 Field 的带区间接口 BandRegionPx 取实际值）
//	② 数据行左右边距 = round(0.0194×1920) = 37 px ±1
//	③ 时钟宽 ≤ 0.30×1920 = 576 px
//	④ 云宽区间 lo ≥ 853、hi ≤ 932（kinetics 屏宽分制，:11-15）
// 竖屏口径的回归由既有版式测试钉死：TestLayout / TestLayoutPx / TestCloudWidthMatchesBand
// （480/529/21 px @1080 逐字不许变，:38）。

package ui

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/kinetics"
)

const (
	landW    = 1920
	landH    = 1080
	landTol  = 1 // px，spec 硬判据 2 的 ±1
	landSeed = 20261015
)

func landNow() time.Time { return time.Date(2026, 10, 24, 15, 4, 17, 0, time.UTC) }

// landField 横屏稳态场：与基线出图同一口径（66 ms 固定步，横屏 1920×1080 步进）。
func landField(cls effect.Class, isDay bool, steps int) *Field {
	f := NewField(cls, isDay, landSeed)
	for i := 0; i < steps; i++ {
		f.Step(66*time.Millisecond, landW, landH)
	}
	return f
}

// 判据 ①：面板顶沿 y ≥ 云带下沿（压顶三档：阴/雨/雷 = 取证照片与横屏基线里的厚云层）。
func TestLandscapeLayoutPanelBelowCloudBand(t *testing.T) {
	s := New()
	for _, cls := range []effect.Class{effect.Overcast, effect.Rain, effect.Thunder} {
		f := landField(cls, false, 800)
		bandTop, bandBottom := f.BandRegionPx(landW, landH)
		if bandBottom <= 0 {
			t.Fatalf("%s: 云带下沿 = %d，取证照片里阴/雨/雷横屏必有云带", cls, bandBottom)
		}
		px := s.weatherPxRect(image.Pt(landW, landH), Frame{Field: f})
		if px.Y < bandBottom {
			t.Errorf("%s: 面板顶沿 y=%d < 云带下沿 y=%d（缺陷 L2 未修：面板压进云带）", cls, px.Y, bandBottom)
		}
		if px.Y+px.H > landH {
			t.Errorf("%s: 面板越出屏底 y=%d h=%d（横屏版式必须整块在屏内）", cls, px.Y, px.H)
		}
		t.Logf("L2 %s: 云带 [%d,%d] 面板顶沿=%d 间隙=%d px 面板高=%d", cls, bandTop, bandBottom, px.Y, px.Y-bandBottom, px.H)
	}

	// 非压顶档（云带从 0.02 铺到 0.26 屏高 ⇒ 横屏 1080 高下带下沿贴近屏底）：
	// "顶沿 = 云带下沿 + 间隙"与"面板整块在屏内"在 1080 高下互斥，
	// 实现取"面板完整优先"（贴屏底、保持 landscapePanelMinH）。
	// 冲突逐条打印交主会话判读，已记 。TODO(未取证)：横屏下非压顶档的带纵向口径。
	for _, cls := range []effect.Class{effect.Cloudy, effect.PartlyCloudy, effect.Fog} {
		f := landField(cls, true, 800)
		bandTop, bandBottom := f.BandRegionPx(landW, landH)
		px := s.weatherPxRect(image.Pt(landW, landH), Frame{Field: f})
		t.Logf("L2 %s(非压顶): 云带 [%d,%d] 面板顶沿=%d 间隙=%d px 面板高=%d 屏底=%d",
			cls, bandTop, bandBottom, px.Y, px.Y-bandBottom, px.H, landH)
		if px.Y+px.H > landH {
			t.Errorf("%s: 面板越出屏底 y=%d h=%d", cls, px.Y, px.H)
		}
	}
}

// 判据 ②：数据行左右边距 = round(0.0194×1920) = 37 px ±1（缺陷 L3）。
// 数据行 = 面板内的三列行（rowDetails 的 rowW = 面板宽，列间距 = rowW/3 ⇒ 同 W 分制）。
func TestLandscapeLayoutDataRowMargins(t *testing.T) {
	s := New()
	f := landField(effect.Overcast, false, 800)
	px := s.weatherPxRect(image.Pt(landW, landH), Frame{Field: f})
	want := kinetics.GapPx(landW) // round(0.0194×1920) = 37
	left, right := px.X, landW-(px.X+px.W)
	if math.Abs(float64(left-want)) > landTol {
		t.Errorf("左边距 = %d，期望 %d±%d（0.0194W 同口径）", left, want, landTol)
	}
	if math.Abs(float64(right-want)) > landTol {
		t.Errorf("右边距 = %d，期望 %d±%d（缺陷 L3：右列贴右边距）", right, want, landTol)
	}
	cell := px.W / 3
	wantCell := (landW - 2*want) / 3 // 列间距 = 面板宽/3，面板宽 = W − 2×边距 ⇒ 同 W 分制
	if math.Abs(float64(cell-wantCell)) > 1 {
		t.Errorf("列间距 = %d，期望 (W−2×边距)/3 = %d（按 W 分制）", cell, wantCell)
	}
	t.Logf("L3 边距 左=%d 右=%d 列间距=%d（期望 %d / %d）", left, right, cell, want, wantCell)
}

// 判据 ③：时钟宽 ≤ 0.30×1920 = 576 px（缺陷 L5，字号按屏高比 ⇒ 横屏占屏宽 40%）。
// 实画宽用与绘制同一路径的测量（measureText + material 主题）：先取横屏封顶后的字号，
// 再量该字号下的行宽。
func TestLandscapeLayoutClockWidthCap(t *testing.T) {
	s := New()
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(landW, landH)),
		Now:         landNow(),
	}
	spec := DefaultClockSpec()
	timeStr := spec.TimeLine(landNow())
	dateStr := spec.DateLine(landNow())
	timeSp, dateSp := s.clockLineSp(gtx, image.Pt(landW, landH), timeStr, dateStr)
	capPx := int(landscapeClockWidthFrac * float64(landW)) // 576
	for _, c := range []struct {
		name string
		txt  string
		sp   int
	}{{"时间行", timeStr, timeSp}, {"日期行", dateStr, dateSp}} {
		w := measureText(gtx, s.label(c.txt, unit.Sp(c.sp), color.NRGBA{R: 255, G: 255, B: 255, A: 255}, text.Middle)).Size.X
		if w <= 0 {
			t.Fatalf("%s 宽度测量 = %d（文字测量路径不可用）", c.name, w)
		}
		if w > capPx {
			t.Errorf("%s 宽 = %d px > 0.30×W = %d px（字号 %d sp，缺陷 L5 未修）", c.name, w, capPx, c.sp)
		}
		t.Logf("L5 %s: 字号=%d 实画宽=%d px（上限 %d）", c.name, c.sp, w, capPx)
	}
	// 竖屏口径不许变：1080×1920 下时钟字号逐字 = 现口径 174/40
	ps, ds := s.clockLineSp(gtx, image.Pt(PortraitW, PortraitH), timeStr, dateStr)
	if ps != ClockFontSizeShown || ds != WeatherFontSize {
		t.Errorf("竖屏时钟字号 = %d/%d，期望 %d/%d（竖屏口径逐字不许变）", ps, ds, ClockFontSizeShown, WeatherFontSize)
	}
}

// 判据 ④：云宽区间 lo ≥ 853、hi ≤ 932 @1920（kinetics 屏宽分制；云永不铺满整屏）。
func TestLandscapeLayoutCloudWidth(t *testing.T) {
	lo, hi := landscapeCloudWidthRangePx(landW)
	if lo < 853 {
		t.Errorf("云宽下限 = %d，期望 ≥ 853（0.444×1920，kinetics 下限）", lo)
	}
	if hi > 932 {
		t.Errorf("云宽上限 = %d，期望 ≤ 932（横屏 (1−0.0194)W/2 口径，留左右边距）", hi)
	}
	if hi < lo {
		t.Fatalf("云宽区间非法 lo=%d hi=%d", lo, hi)
	}
	// 云带实际落位宽（bandPlace 同源口径）与粒子云实画宽都必须落在区间内
	for _, cls := range []effect.Class{effect.Overcast, effect.Rain, effect.Cloudy, effect.PartlyCloudy} {
		f := landField(cls, false, 800)
		n := f.bandCount()
		var minDw, maxDw float64 = 1e9, -1
		for i := 0; i < n; i++ {
			bp, ok := f.bandPlace(i, f.t, landW, landH)
			if !ok {
				t.Fatalf("%s: 带落位取不到（云贴图缺失？）", cls)
			}
			if bp.dw < minDw {
				minDw = bp.dw
			}
			if bp.dw > maxDw {
				maxDw = bp.dw
			}
		}
		if minDw < float64(lo)-1 || maxDw > float64(hi)+1 {
			t.Errorf("%s: 云带宽 = [%.0f,%.0f]，越出 [%d,%d]", cls, minDw, maxDw, lo, hi)
		}
		if maxDw >= float64(landW) {
			t.Errorf("%s: 云带宽 %.0f ≥ 屏宽 ⇒ 铺满整屏（缺陷 L1 未修）", cls, maxDw)
		}
		t.Logf("L1/L4 %s: 云带宽 [%.0f,%.0f] 朵数=%d 区间=[%d,%d]", cls, minDw, maxDw, n, lo, hi)
	}
	// 粒子云实画宽（生成侧最终显示宽度，同一 kinetics 区间）
	for _, cls := range []effect.Class{effect.Overcast, effect.Cloudy, effect.PartlyCloudy} {
		f := landField(cls, false, 800)
		minW, _ := airWidthProbe(f)
		if minW > 0 && minW < float64(lo)-1 {
			t.Errorf("%s: 粒子云最窄实画宽 = %.0f px < 下限 %d（R4 小云横屏复活）", cls, minW, lo)
		}
		t.Logf("L4 %s: 粒子云最窄实画宽 = %.0f px（下限 %d）", cls, minW, lo)
	}
}

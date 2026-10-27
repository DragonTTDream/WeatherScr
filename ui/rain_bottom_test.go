//go:build linux

package ui

// 雨滴落点自检（离屏实测）。
// 判据：雨丝本身照旧画到屏幕下边缘，但**下边缘不出现水花印记**。
// 水花的形态口径（删除前的实现）：贴底 10 px 的实心矩形，宽 8 px，
// 同一滴还在 x±(age·15·1.2) 处各复制一份 ⇒ 同一滴在一条行上连成 8–24 px 的实心段。
// 雨丝本身是描边线（宽 1–2 px、带倾角），在同一行上只占 2–3 px。
// ⇒ 用"粒子层在底边造成的最长水平实心段"把两者分开：
// 有水花 ⇒ 段长 ≥ 8；无水花 ⇒ 段长 ≤ 5。
// 两帧差分：同一场粒子、同一时间，只切 LayerField 图层开关，
// 差分像素 = 粒子层贡献，天空/文字不参与。

import (
	"image"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// rainBottomShot 出一帧雨（粒子层按 layers 给），返回整屏像素。
func rainBottomShot(t *testing.T, steps int, layers LayerMask) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()

	f := NewField(effect.Rain, true, 20261015)
	for i := 0; i < steps; i++ {
		f.Step(66*time.Millisecond, PortraitW, PortraitH)
	}
	th := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	s := New()
	var ops op.Ops
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
			Now:         th,
		}
		s.Layout(gtx, Frame{
			Now:    th,
			Clock:  DefaultClockSpec(),
			Class:  effect.Rain,
			IsDay:  true,
			Field:  f,
			Panel:  DefaultPanelStyle(),
			Layers: layers,
		})
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("离屏帧提交失败: %v", err)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

// maxParticleRun 在 r 内逐行扫描，返回最长水平连续段、变化像素合计、该段的落点。
func maxParticleRun(withField, noField *image.RGBA, r image.Rectangle) (runMax, changed int, at image.Point) {
	best, total := 0, 0
	at = image.Point{X: -1, Y: -1}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		run := 0
		for x := r.Min.X; x < r.Max.X; x++ {
			ia := withField.PixOffset(x, y)
			ib := noField.PixOffset(x, y)
			d := 0
			for k := 0; k < 3; k++ {
				v := int(withField.Pix[ia+k]) - int(noField.Pix[ib+k])
				if v < 0 {
					v = -v
				}
				if v > d {
					d = v
				}
			}
			if d > 24 {
				total++
				run++
				if run > best {
					best, at = run, image.Point{X: x, Y: y}
				}
			} else {
				run = 0
			}
		}
	}
	return best, total, at
}

func TestRainBottomEdgeNoSplash(t *testing.T) {
	const steps = 90
	withField := rainBottomShot(t, steps, LayerSky|LayerField)
	noField := rainBottomShot(t, steps, LayerSky)

	// 底边带：水花最高到 2×10 = 20 px，取 24 px 带（含其上方溅起段）
	band := image.Rect(0, PortraitH-24, PortraitW, PortraitH)
	run, changed, at := maxParticleRun(withField, noField, band)
	t.Logf("底边 24 px 带：最长实心段 = %d px @行 %d x=%d；带内粒子层变化像素 = %d", run, at.Y, at.X, changed)

	// 雨丝本身必须还在（判据不许放宽）：底边带内必须存在粒子层造成的变化
	if changed == 0 {
		t.Fatalf("底边带内没有任何粒子层变化 ⇒ 雨丝没画到下边缘，本判据失去意义")
	}
	// 水花印记必须没有：单颗水花宽 8 px，同滴复制后连成 ≥8 px 的实心段
	if run >= 6 {
		t.Errorf("底边出现 %d px 的实心段（@行 %d x=%d）⇒ 落点印记仍在（水花宽 8 px）", run, at.Y, at.X)
	}
}

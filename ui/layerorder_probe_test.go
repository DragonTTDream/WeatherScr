package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestMoonLayerOrder 判定月亮到底在哪一层
// "没发现是云的颜色改变了，月亮依旧是自己的颜色，是云被月亮盖住了！"。
// 同一帧渲染两次：一次带粒子层、一次不带。月核像素若完全相同
// ⇒ 粒子层对月亮零影响 ⇒ 月亮画在粒子之上。
func TestMoonLayerOrder(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skip("headless 不可用:", err)
	}
	defer win.Release()
	s := New()
	th := time.Date(2026, 10, 17, 3, 20, 0, 0, time.UTC)

	render := func(f *Field, layers LayerMask) *image.RGBA {
		var ops op.Ops
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)), Now: th}
		s.Layout(gtx, Frame{
			Now: th, Clock: DefaultClockSpec(), Weather: blitSample(),
			Class: effect.Cloudy, IsDay: false, Location: "Hangzhou, China",
			Panel: DefaultPanelStyle(), MoonPhase: f64p(0.174), Field: f,
			Layers: layers,
		})
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("frame: %v", err)
		}
		img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		return img
	}

	count := func(img *image.RGBA) int {
		fw, fh := float64(PortraitW), float64(PortraitH)
		cx, cy := int(0.72*fw), int(0.095*fh)
		rc := int(moonRcFrac * fw)
		n := 0
		for y := cy - rc; y <= cy+rc; y++ {
			for x := cx - rc; x <= cx+rc; x++ {
				i := img.PixOffset(x, y)
				if (int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2]))/3 > 235 { // 云体最亮 ~225，进不来
					n++
				}
			}
		}
		return n
	}

	f := NewField(effect.Cloudy, false, 20261015)
	for i := 0; i < 40; i++ {
		f.Step(time.Second, PortraitW, PortraitH)
	}
	// 把激活的云全部钉到月亮上、完全不透明
	for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
		if f.parts[i].kind == pkCloud {
			f.parts[i].x, f.parts[i].y = 0.72, 0.095
			f.parts[i].vx = 0
			f.parts[i].alpha = 1.0
		}
	}
	kinds := map[pkind]int{}
	for i := range f.parts {
		kinds[f.parts[i].kind]++
	}
	t.Logf("全部 %d 个粒子种类=%v（pkCloud=4 pkGlow=6 pkStar=0）ActiveCount=%d",
		len(f.parts), kinds, f.ActiveCount())
	withField := count(render(f, LayerSky|LayerDepth|LayerField))
	noField := count(render(nil, LayerSky|LayerDepth))
	t.Logf("月核亮像素(>235)：带粒子层=%d  无粒子层=%d", withField, noField)
	if withField == noField {
		t.Errorf("粒子层对月亮零影响（%d == %d）⇒ 月亮画在云之上，图层顺序错", withField, noField)
	}
}

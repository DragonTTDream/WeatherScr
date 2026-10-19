// setshot：离线渲染设置面板并量化其可见性。
//
// 用途：不开设置窗口，用 gioui 的 headless GPU 把 ui.SettingsPanel 渲染成
// PNG，逐帧打印
//
//	PANEL w=… h=… rows=N layoutW=… layoutH=… listOffset=… nonbg=…%
//
// 把"面板到底画没画出来、画出来的东西占不占窗口"变成可核对的数字。
// 走 cmd/fxshot 同一条 headless 路径（headless.NewWindow → Frame → Screenshot）。
//
// GOOS=linux 专用（headless 走 EGL/OSMesa），构建带 -tags=novulkan。
//
// 底色：gioui 的 Windows 后端每帧把窗口清成不透明白
// （gioui.org v0.10.3 app/window.go:216，color.NRGBA{A:ff,R:ff,G:ff,B:ff}），
// 设置窗口自身不画背景 ⇒ 离屏图先铺同一层清屏色，
// 这样量出来的"非背景占比"与 Windows 上设置窗口的观感同口径。
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/DragonTTDream/WeatherScr/ui"
)

// windowClear 是 gioui Windows 后端的每帧清屏色（app/window.go:216）。
var windowClear = color.NRGBA{A: 0xff, R: 0xff, G: 0xff, B: 0xff}

// panelModel 是面板夹具，字段与 config.Defaults() 的面板字段一一对应。
//
// 内联构造：config 已 import ui，量化工具不 import config，
// 免得把 config 拖进这条离屏渲染链路（与 ui 包不 import config 同一条规矩）。
func panelModel() ui.PanelModel {
	return ui.PanelModel{
		Location:            "Hangzhou, China",
		FontSize:            25,
		IconSize:            96,
		Margin:              30,
		Units:               "c",
		RefreshMinutes:      1,
		ShowBackground:      false,
		BgOpacity:           0.0,
		ClockBgOpacity:      0.0,
		ShowConditionIcon:   true,
		ShowDetailsRow:      true,
		ShowForecast:        true,
		ShowFiveDayForecast: false,
		EffectsOn:           true,
		WallpaperDir:        `C:\Users\Public\Pictures`,
		WallpaperIntervalS:  40,
		ClockYOffset:        0,
		WeatherYOffset:      0,
		InteractionMode:     false,
		AlertEnabled:        true,
		AlertThresholdPct:   30,
		AlertYOffset:        -60,
		AlertPulse:          true,
		AlertWindowHours:    3,
		LocationLat:         0,
		LocationLon:         0,
		LocationNameZh:      "",
		LocationNameEn:      "",
		DisplayLanguage:     "en",
	}
}

func main() {
	sizes := [][2]int{{640, 1100}, {810, 1389}, {1080, 1920}}
	if v := os.Getenv("SET_SIZES"); v != "" {
		var got [][2]int
		for _, tok := range strings.Split(v, ",") {
			wh := strings.SplitN(strings.TrimSpace(tok), "x", 2)
			if len(wh) != 2 {
				continue
			}
			w, e1 := strconv.Atoi(strings.TrimSpace(wh[0]))
			h, e2 := strconv.Atoi(strings.TrimSpace(wh[1]))
			if e1 != nil || e2 != nil || w <= 0 || h <= 0 {
				continue
			}
			got = append(got, [2]int{w, h})
		}
		if len(got) > 0 {
			sizes = got
		}
	}
	frames := 3
	if v := os.Getenv("SET_FRAMES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			frames = n
		}
	}
	base := os.Getenv("SET_OUT")
	if base == "" {
		base = "setshot"
	}
	// 缩放口径：目标竖屏 1080×1920 上 1 dp = 1 px（density 1.0），
	// 设置窗口的 app.Size(640, 1100) 即 640×1100 px。SET_DENSITY 可改。
	density := float32(1)
	if v := os.Getenv("SET_DENSITY"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			density = float32(f)
		}
	}

	th := ui.New().Theme
	fmt.Printf("THEME textsize=%.1f fg=%s bg=%s contrastbg=%s contrastfg=%s clear=%s density=%.2f\n",
		th.TextSize, fmtColor(th.Palette.Fg), fmtColor(th.Palette.Bg),
		fmtColor(th.Palette.ContrastBg), fmtColor(th.Palette.ContrastFg),
		fmtColor(windowClear), density)

	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		win, err := headless.NewWindow(w, h)
		if err != nil {
			fmt.Println("HEADLESS_FAIL=" + err.Error())
			return
		}
		panel := ui.NewSettingsPanel(panelModel())
		fmt.Printf("SIZE w=%d h=%d rows=%d\n", w, h, panel.RowCount())
		var ops op.Ops
		for f := 0; f < frames; f++ {
			ops.Reset()
			gtx := layout.Context{
				Ops:         &ops,
				Constraints: layout.Exact(image.Point{X: w, Y: h}),
				Metric:      unit.Metric{PxPerDp: density, PxPerSp: density},
				Now:         time.Now(),
			}
			paint.FillShape(gtx.Ops, windowClear, clip.Rect{Max: image.Point{X: w, Y: h}}.Op())
			dims := panel.Layout(gtx, th)
			if err := win.Frame(&ops); err != nil {
				fmt.Println("FRAME_FAIL=" + err.Error())
				win.Release()
				return
			}
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			if err := win.Screenshot(img); err != nil {
				fmt.Println("SHOT_FAIL=" + err.Error())
				win.Release()
				return
			}
			name := fmt.Sprintf("%s.%dx%d.f%d.png", base, w, h, f)
			if err := writePNG(name, img); err != nil {
				fmt.Println("WRITE_FAIL=" + err.Error())
				win.Release()
				return
			}
			fmt.Printf("PANEL w=%d h=%d rows=%d layoutW=%d layoutH=%d listOffset=%d nonbg=%.1f%%\n",
				w, h, panel.RowCount(), dims.Size.X, dims.Size.Y, panel.ListOffset(), nonbg(img))
			x0, y0, x1, y1, bands := contentBox(img)
			fmt.Printf("SHOT file=%s bbox=[%d,%d,%d,%d] bands=%d visible=%d/%d rows\n",
				name, x0, y0, x1, y1, bands, panel.ListVisibleCount(), panel.RowCount())
		}
		win.Release()
	}
}

func fmtColor(c color.NRGBA) string {
	return fmt.Sprintf("(%d,%d,%d,%d)", c.R, c.G, c.B, c.A)
}

func writePNG(name string, img *image.RGBA) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// nonbg 返回与背景色不同的像素占比（%）。
//
// 背景色 = 出现最多的颜色（与 cmd/fxshot 的 quantify 同一口径）；
// 逐 2 px 采样，三通道差之和 > 40 记为"非背景"。
func nonbg(img *image.RGBA) float64 {
	bd := img.Bounds()
	type key struct{ r, g, b uint8 }
	cnt := map[key]int{}
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			p := img.Pix[(y*bd.Dx()+x)*4:]
			cnt[key{p[0], p[1], p[2]}]++
		}
	}
	var bg key
	best := -1
	for k, n := range cnt {
		if n > best {
			best, bg = n, k
		}
	}
	tot, diff := 0, 0
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			p := img.Pix[(y*bd.Dx()+x)*4:]
			tot++
			d := abs(int(p[0]) - int(bg.r))
			e := abs(int(p[1]) - int(bg.g))
			f := abs(int(p[2]) - int(bg.b))
			if d+e+f > 40 {
				diff++
			}
		}
	}
	if tot == 0 {
		return 0
	}
	return 100.0 * float64(diff) / float64(tot)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// contentBox 返回非背景像素的包围盒与"行带"数（连续有内容的 y 段）。
//
// 行带数 = 首屏真正画出来的行数口径：面板每行之间都有 inset 空隙，
// 一行有内容就是一段连续的 y，行与行之间空开 ⇒ 一段对应一行。
func contentBox(img *image.RGBA) (int, int, int, int, int) {
	bd := img.Bounds()
	type key struct{ r, g, b uint8 }
	cnt := map[key]int{}
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			p := img.Pix[(y*bd.Dx()+x)*4:]
			cnt[key{p[0], p[1], p[2]}]++
		}
	}
	var bg key
	best := -1
	for k, n := range cnt {
		if n > best {
			best, bg = n, k
		}
	}
	has := func(x, y int) bool {
		p := img.Pix[(y*bd.Dx()+x)*4:]
		d := abs(int(p[0]) - int(bg.r))
		e := abs(int(p[1]) - int(bg.g))
		f := abs(int(p[2]) - int(bg.b))
		return d+e+f > 40
	}
	x0, y0, x1, y1 := bd.Max.X, bd.Max.Y, bd.Min.X, bd.Min.Y
	bands := 0
	prev := false
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		rowHas := false
		for x := bd.Min.X; x < bd.Max.X; x++ {
			if has(x, y) {
				rowHas = true
				if x < x0 {
					x0 = x
				}
				if x > x1 {
					x1 = x
				}
			}
		}
		if rowHas {
			if y < y0 {
				y0 = y
			}
			if y > y1 {
				y1 = y
			}
			if !prev {
				bands++
			}
		}
		prev = rowHas
	}
	if x1 < x0 {
		return 0, 0, 0, 0, 0
	}
	return x0, y0, x1, y1, bands
}

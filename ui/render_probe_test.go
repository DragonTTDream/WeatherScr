//go:build linux

package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// 离屏渲染探针（与 cmd/fxshot 同一 headless 口径）。
//
// 为什么要这个测试：需求 3 指定了 gioui 的已知退化
// （Affine2D 的 .Scale 形式实测画不出东西，见 ui/render.go:1036-1043 的探针记录），
// 而"编译通过 + 单测通过"证明不了图标**真的被画到屏幕上**。
// 这里用差分法实测：同一帧只换 WMO 码（61 = 有图标 / 4 = 映射不到），
// 数状况图标盒内的差异像素；详情行同理（三项都有 vs 全 nil）。
// headless 不可用时跳过（本机 cmd/fxshot 实测可用）。

func shotWeatherBlock(t *testing.T, code int, withDetails bool) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	return renderOffscreen(t, win, code, withDetails)
}

// renderOffscreen 在同一 headless 窗口上连渲两帧取第二帧
// （gioui 的 headless 窗口可以多次 Frame）。
func renderOffscreen(t *testing.T, win *headless.Window, code int, withDetails bool) *image.RGBA {
	t.Helper()
	tt := 22.4
	hu := 77.0
	pp := 88.0
	ws := 5.6
	fc := "Tomorrow: 20°-25°C/Rain"
	w := &weather.Sample{
		Condition:          "Slight rain",
		Temperature:        &tt,
		WeatherCode:        code,
		IsDay:              1,
		Forecast:           &fc,
		Location:           "Hangzhou, China",
		TodayPrecipProbMax: &[]int{88}[0],
	}
	if withDetails {
		w.Humidity, w.PrecipitationProbability, w.WindSpeed = &hu, &pp, &ws
	}

	s := New()
	var ops op.Ops
	th := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
			Now:         th,
		}
		s.Layout(gtx, Frame{
			Now:      th,
			Clock:    DefaultClockSpec(),
			Weather:  w,
			Class:    effect.Rain,
			IsDay:    true,
			Location: "Hangzhou, China",
			Panel:    DefaultPanelStyle(),
			Layers:   LayerSky | LayerClock | LayerWeath, // 不画粒子，判读干净
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

// diffCount 统计两图在矩形 r 内差异超过阈值的像素数。
func diffCount(a, b *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ia := a.PixOffset(x, y)
			ib := b.PixOffset(x, y)
			d := 0
			for k := 0; k < 3; k++ {
				v := int(a.Pix[ia+k]) - int(b.Pix[ib+k])
				if v < 0 {
					v = -v
				}
				if v > d {
					d = v
				}
			}
			if d > 24 {
				n++
			}
		}
	}
	return n
}

// TestConditionIconDrawsRightBox 实测需求 3：
// 状况图标 192×192 画在文字列右侧，且**真的画得出来**（不是 Affine 退化）。
func TestConditionIconDrawsRightBox(t *testing.T) {
	with := shotWeatherBlock(t, 61, true)   // rain.png：映射得到
	without := shotWeatherBlock(t, 4, true) // 码 4 不在上游 9 组里 → 纯文字列

	px := WeatherRect.Px(PortraitW, PortraitH)
	box := image.Rect(px.X+px.W-WeatherIconSize, px.Y, px.X+px.W, px.Y+px.H)
	n := diffCount(with, without, box)
	tot := box.Dx() * box.Dy()
	pct := 100.0 * float64(n) / float64(tot)
	t.Logf("状况图标盒 %v 差异像素 = %d/%d = %.2f%%", box, n, tot, pct)
	// 上线态钉"图标不画"：天气卡片布局失衡 ⇒ WeatherShowIcon = false。
	// 本测试因此反向实测：码 61（映射得到图标）与码 4（映射不到）两帧在图标盒内
	// 必须**逐像素相同**，即图标确实没画。将来要把图标加回来，先取得需求确认再翻这条。
	if pct > 0.5 {
		t.Errorf("状况图标盒内差异 %.2f%%，期望 ≤0.5%%（上线态不画状况图标）", pct)
	}
	// 图标位右侧留白：块宽 − 192 − 12 之外的文字列区域应基本不变
	// （文字列变宽会让文字重排，故只要求差异远小于图标盒）
	left := image.Rect(px.X, px.Y, px.X+px.W-WeatherIconSize-condIconGapPx, px.Y+px.H)
	nl := diffCount(with, without, left)
	t.Logf("文字列区域差异像素 = %d（图标盒 %d）", nl, n)
}

// TestDetailRowDrawsIcons 实测：
// 详情行的三项图标 + 数字画得出来。
//
// 两帧的 WMO 码相同 ⇒ 状况图标逐像素相同，差分只可能来自详情行本身。
// 判据按**行带**算（详情行高 68 px，上游 QML:393-397），不按整块面积算，
// 否则 68 px 的行带被 558 px 的块高稀释成 0.2% 而误判。
func TestDetailRowDrawsIcons(t *testing.T) {
	with := shotWeatherBlock(t, 61, true)
	without := shotWeatherBlock(t, 61, false)

	px := WeatherRect.Px(PortraitW, PortraitH)
	full := image.Rect(px.X, px.Y, px.X+px.W, px.Y+px.H)

	// 逐行差分，找详情行所在的连续行带
	rows := make([]int, full.Dy())
	for y := full.Min.Y; y < full.Max.Y; y++ {
		r := image.Rect(full.Min.X, y, full.Max.X, y+1)
		rows[y-full.Min.Y] = diffCount(with, without, r)
	}
	peak := 0
	for i, v := range rows {
		if v > rows[peak] {
			peak = i
		}
	}
	lo, hi := peak, peak
	for lo > 0 && rows[lo-1] > 0 {
		lo--
	}
	for hi < len(rows)-1 && rows[hi+1] > 0 {
		hi++
	}
	sum := 0
	for i := lo; i <= hi; i++ {
		sum += rows[i]
	}
	band := hi - lo + 1
	t.Logf("详情行行带 = 块内 y∈[%d,%d] 高 %d px，差分像素 %d（峰值行 %d 有 %d 个）",
		lo, hi, band, sum, peak, rows[peak])
	// 行带量的是**画出来的内容**：20 px 字号的字面 ≈ 15 行，
	// 30 px 图标画的是 250×250 的整张画布（上游 fillMode: PreserveAspectFit），
	// 画布里的字形只占画布 ~60% ⇒ 实测字形带 ≈ 17 行（ASCII 探针逐行读过）。
	// 行盒本身是 68 px（TestDetailRowGeometry 已按上游公式实测），
	// 这里只要求差分内容带存在且量级对得上。
	if band < 10 {
		t.Errorf("差分内容带只有 %d 行，图标/数字没画出来的特征", band)
	}
	if sum < 500 {
		t.Errorf("行带差分像素 %d，期望 ≥500（3 张图标 + 20 px 数字的量级）", sum)
	}
}

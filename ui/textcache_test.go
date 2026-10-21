//go:build linux

package ui

// 文字块绘制缓存的自检。
//
// 三条判据，全部实测：
//  1. 逐像素一致：同一帧内容，"逐帧直接绘制"与"缓存重放"两张截图
//     必须**逐像素完全相同**（差异像素 = 0，不是"接近"）。
//  2. 条数上限 2、键变整条替换、不累积。
//  3. 重排频率：15 fps 连跑 4 秒，时钟块重建次数 = 秒数（每秒 1 次），
//     天气块在数据不变时只建 1 次。
//
// headless 不可用时跳过（与 ui/render_probe_test.go 同一口径）。

import (
	"fmt"
	"image"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// textShot 在同一 headless 窗口上渲染一帧并截图。
func textShot(t *testing.T, win *headless.Window, s *Screen, f Frame) *image.RGBA {
	t.Helper()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
		Now:         f.Now,
	}
	s.Layout(gtx, f)
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("帧提交失败: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

// textFrame 造一帧固定内容。withAlert = 带上降雨提醒层（提醒的文字画在
// 帧缓冲区里，与时钟/天气两块的重放混在同一帧 ⇒ 覆盖"混合缓冲区"这一情形）。
func textFrame(now time.Time, full bool, cond string, withAlert ...bool) Frame {
	tt := 22.4
	hu := 84.0
	pp := 12.0
	ws := 4.6
	fc := "Tomorrow: 20C-25C/Rain"
	w := &weather.Sample{
		Condition:   cond,
		Temperature: &tt,
		WeatherCode: 61,
		IsDay:       1,
		Forecast:    &fc,
	}
	if full {
		w.Humidity = &hu
		w.PrecipitationProbability = &pp
		w.WindSpeed = &ws
	}
	f := Frame{
		Now:       now,
		Clock:     DefaultClockSpec(),
		Weather:   w,
		Class:     effect.Rain,
		IsDay:     false,
		Location:  "Hangzhou, China",
		Panel:     DefaultPanelStyle(),
		MoonPhase: nil,
		Layers:    LayerSky | LayerDepth | LayerClock | LayerWeath | LayerShadow,
	}
	if len(withAlert) > 0 && withAlert[0] {
		f.Layers |= LayerAlert
		f.Alert = &Alert{State: AlertDisaster, TierVal: 3, MM: 1.2, DisasterHours: 3, Pulse: 0.25, Lang: "zh"}
	}
	return f
}

// exactDiff 逐像素比对，返回任一通道不等的像素数（严格相等判据）。
func exactDiff(a, b *image.RGBA) int {
	n := 0
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			n++
		}
	}
	return n
}

// TestTextCachePixelIdentical 硬判据：缓存重放与逐帧直接绘制逐像素相同。
func TestTextCachePixelIdentical(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	textCacheResetForTest()

	s := New()
	now := time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC)
	cases := []struct {
		name  string
		frame Frame
	}{
		{"时钟+天气", textFrame(now, true, "Light rain")},
		{"含提醒层", textFrame(now, true, "Light rain", true)},
	}
	for _, c := range cases {
		f := c.frame

		// 1) 关掉缓存：逐帧直接绘制（改动前的路径）
		textCacheBypass = true
		direct := textShot(t, win, s, f)
		direct2 := textShot(t, win, s, f)
		textCacheBypass = false

		// 2) 开缓存：首帧重建，第二帧命中
		cached := textShot(t, win, s, f)
		cached2 := textShot(t, win, s, f)

		if n := exactDiff(direct, direct2); n != 0 {
			t.Errorf("%s 基线自身不稳定：两次逐帧直接绘制有 %d 个字节不等 ⇒ 判据本身不可用", c.name, n)
		}
		if n := exactDiff(direct, cached); n != 0 {
			t.Errorf("%s 缓存首帧与直接绘制不一致：%d 个字节不等", c.name, n)
		}
		if n := exactDiff(direct, cached2); n != 0 {
			t.Errorf("%s 缓存命中帧与直接绘制不一致：%d 个字节不等", c.name, n)
		}
		t.Logf("%s: 逐像素比对差异字节 = 0", c.name)
	}
	st := s.TextCacheStats()
	t.Logf("缓存 entries=%d clock hits/builds=%d/%d weather hits/builds=%d/%d",
		st.Entries, st.ClockHits, st.ClockBuilds, st.WeatherHits, st.WeatherBuilds)
	if st.Entries != 2 {
		t.Errorf("缓存条数 = %d，期望 2（时钟 1 条 + 天气 1 条）", st.Entries)
	}
}

// TestTextCacheReplaceNotAccumulate 键变整条替换、条数上限 2、不累积。
func TestTextCacheReplaceNotAccumulate(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	textCacheResetForTest()

	s := New()
	base := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	// 同一内容连打 20 帧：只建 1 次，其余全命中
	for i := 0; i < 20; i++ {
		textShot(t, win, s, textFrame(base, true, "Light rain"))
	}
	st := s.TextCacheStats()
	if st.ClockBuilds != 1 || st.WeatherBuilds != 1 {
		t.Errorf("同内容 20 帧重建次数 = clock %d / weather %d，期望各 1", st.ClockBuilds, st.WeatherBuilds)
	}
	if st.Entries != 2 {
		t.Errorf("条数 = %d，期望 2", st.Entries)
	}

	// 换 30 次天气内容：天气块重建 30 次，条数仍是 2 ⇒ 整条替换、不累积
	before := s.TextCacheStats()
	for i := 0; i < 30; i++ {
		textShot(t, win, s, textFrame(base, i%2 == 0, fmt.Sprintf("Cond %d", i)))
	}
	st = s.TextCacheStats()
	if st.WeatherBuilds != before.WeatherBuilds+30 {
		t.Errorf("30 次换内容后天气重建 = %d，期望 %d（前值 + 30）", st.WeatherBuilds, before.WeatherBuilds+30)
	}
	if st.Entries != 2 {
		t.Errorf("换内容 30 次后条数 = %d，期望 2（不许累积）", st.Entries)
	}
	if d := st.WeatherHits - before.WeatherHits; d != 0 {
		t.Errorf("30 次内容各不相同 ⇒ 命中增量 = %d，期望 0", d)
	}
	t.Logf("替换口径：weather builds %d→%d、hits %d→%d、entries=%d",
		before.WeatherBuilds, st.WeatherBuilds, before.WeatherHits, st.WeatherHits, st.Entries)
}

// TestTextCacheRebuildRate 重排频率：15 fps 连跑 4 秒。
func TestTextCacheRebuildRate(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用，跳过离屏实测: %v", err)
	}
	defer win.Release()
	textCacheResetForTest()

	s := New()
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	frames := 60 // 60 × 66 ms ≈ 4 s
	for i := 0; i < frames; i++ {
		now := start.Add(time.Duration(i) * 66 * time.Millisecond)
		textShot(t, win, s, textFrame(now, true, "Light rain"))
	}
	st := s.TextCacheStats()
	secs := 4 // 12:00:00.000 → 12:00:03.934 跨 4 个整秒
	if st.ClockBuilds != secs {
		t.Errorf("时钟块重建次数 = %d，期望 %d（每秒 1 次，15 fps 下 60 帧只重建 4 次）", st.ClockBuilds, secs)
	}
	if st.WeatherBuilds != 1 {
		t.Errorf("天气块重建次数 = %d，期望 1（数据不变 ⇒ 只建一次）", st.WeatherBuilds)
	}
	if st.ClockHits+st.ClockBuilds != frames {
		t.Errorf("时钟 hits+builds = %d+%d，期望 %d（每帧都要出内容）", st.ClockHits, st.ClockBuilds, frames)
	}
	t.Logf("重排频率：%d 帧 / %d 秒 ⇒ 时钟重建 %d 次、命中 %d 次；天气重建 %d 次、命中 %d 次",
		frames, secs, st.ClockBuilds, st.ClockHits, st.WeatherBuilds, st.WeatherHits)
	textCacheResetForTest()
}

// BenchmarkTextBlocks 量"每帧重排文字"的 CPU 成本：
// 只画时钟块 + 天气块（掩掉天空/纵深/粒子），逐帧直接绘制 vs 缓存重放。
// 这是 op 构建 + 字形 shape + Flex 重排这一维的直接对比（不含 GPU 填充）。
func BenchmarkTextBlocksDirect(b *testing.B) {
	textCacheResetForTest()
	textCacheBypass = true
	defer func() { textCacheBypass = false }()
	benchTextBlocks(b, New())
}

func BenchmarkTextBlocksCached(b *testing.B) {
	textCacheResetForTest()
	benchTextBlocks(b, New())
}

func benchTextBlocks(b *testing.B, s *Screen) {
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	f := textFrame(start, true, "Light rain")
	f.Layers = LayerClock | LayerWeath | LayerShadow
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
		Now:         start,
	}
	s.Layout(gtx, f) // 预热：缓存先建好
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		// 同一秒内的帧：时钟键不变 ⇒ 命中；逐帧直接绘制则每帧重排
		gtx.Now = start
		s.Layout(gtx, f)
	}
}

// TestTextCacheFootprint 记录缓存的常驻内存量级（只打印，不作判据）。
// 产物是"录好的绘制指令"，体积 = 块内字形路径与 op 的字节数，
// 与整屏位图（1080×1920×4 = 8.3 MB/条）不是一个量级。
//
// 量法：同一进程、同一 headless 窗口里跑两遍同样的 3 帧，
// 一遍关缓存（逐帧直接绘制）、一遍开缓存，各自 GC 后读 HeapAlloc，
// 两者之差才是缓存自己占的量。
func TestTextCacheFootprint(t *testing.T) {
	s := New()
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用: %v", err)
	}
	defer win.Release()
	now := time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC)
	f := textFrame(now, true, "Light rain")

	alloc := func() uint64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}

	textCacheResetForTest()
	textCacheBypass = true
	base := alloc()
	for i := 0; i < 3; i++ {
		textShot(t, win, s, f)
	}
	direct := alloc()

	textCacheResetForTest()
	alloc()
	for i := 0; i < 3; i++ {
		textShot(t, win, s, f)
	}
	cached := alloc()
	st := s.TextCacheStats()

	t.Logf("同内容 3 帧：关缓存 HeapAlloc 增量 %.1f KB，开缓存 %.1f KB，缓存自身 ≈ %.1f KB（entries=%d）",
		float64(int64(direct)-int64(base))/1024.0,
		float64(int64(cached)-int64(base))/1024.0,
		float64(int64(cached)-int64(direct))/1024.0,
		st.Entries)
	textCacheResetForTest()
}

// TestBitmapBlitDeviation 实测"位图 blit 路线"的像素代价（这条路线没有上线，
// 上线的是指令宏重放；这里量的是规格字面方案的实际偏差，给记录用）。
//
// 做法：
// A = 现状帧（天空 + 纵深 + 时钟块 + 天气块）
// B = 只画两块文字（透明底）⇒ 当作"已绘制好的位图"
// C = 天空 + 纵深 + 把 B 的两块位图按绝对偏移 blit 上去
// 判据：A 与 C 逐像素比对，统计差异像素数与最大通道差。
//
// 量点：gioui 的绘制端在输出时把非预乘颜色乘一次 alpha（gioui.org/shader
// 的 blit.frag：fragColor = opacity * color），而 paint.ImageOp 把贴图按
// **非预乘**语义再乘一次 alpha ⇒ 回读到的预乘位图直接 blit 会变成 alpha 平方，
// 半透明边缘（字形抗锯齿）必然偏淡；要修就得在 CPU 上反预乘（除以 alpha），
// 小 alpha 会把 8 bit 量化误差放大。
func TestBitmapBlitDeviation(t *testing.T) {
	win, err := headless.NewWindow(PortraitW, PortraitH)
	if err != nil {
		t.Skipf("headless 不可用: %v", err)
	}
	defer win.Release()
	textCacheResetForTest()
	s := New()
	now := time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC)
	f := textFrame(now, true, "Light rain")
	f.Layers = LayerSky | LayerDepth | LayerClock | LayerWeath | LayerShadow

	a := textShot(t, win, s, f)

	b := textFrame(now, true, "Light rain")
	b.Layers = LayerClock | LayerWeath | LayerShadow
	bm := textShot(t, win, s, b)

	// 两块位图：覆盖面板矩形（面板底比块矩形外扩 margin，必须一起进位图）
	cp := shiftY(ClockRect.Px(PortraitW, PortraitH), f.ClockYOff)
	wp := shiftY(WeatherRect.Px(PortraitW, PortraitH), f.WeatherYOff)
	cr := panelRect(cp, f.Panel.Margin)
	wr := panelRect(wp, f.Panel.Margin)

	c := shotWithBlits(t, win, s, f, bm, cr, wr)

	n, mx, sum := diffStats(a, c, cr.Union(wr))
	tot := cr.Union(wr).Dx() * cr.Union(wr).Dy()
	t.Logf("位图 blit 路线在两块盒内：差异像素 %d/%d = %.2f%%，最大通道差 %d，平均通道差 %.3f",
		n, tot, 100.0*float64(n)/float64(tot), mx, float64(sum)/float64(tot*3))
	if n == 0 {
		t.Logf("逐像素一致（这条路线可直接上线）")
	}
	textCacheResetForTest()
}

// shotWithBlits 渲染"天空 + 纵深"帧，再把 bm 的两块矩形按绝对偏移 blit 上去。
// blit 写法照 ui/render.go 的 moonBlit：clip.Rect.Push + Affine(带平移).Push +
// ImageOp + PaintOp + 两次 Pop。
func shotWithBlits(t *testing.T, win *headless.Window, s *Screen, f Frame, bm *image.RGBA, cr, wr image.Rectangle) *image.RGBA {
	t.Helper()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
		Now:         f.Now,
	}
	gf := f
	gf.Layers = LayerSky | LayerDepth
	s.Layout(gtx, gf)
	blitSub(t, gtx, bm, cr)
	blitSub(t, gtx, bm, wr)
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("帧提交失败: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, PortraitW, PortraitH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

func blitSub(t *testing.T, gtx layout.Context, src *image.RGBA, r image.Rectangle) {
	t.Helper()
	sz := r.Size()
	sub := image.NewRGBA(image.Rect(0, 0, sz.X, sz.Y))
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			sub.Set(x, y, src.At(r.Min.X+x, r.Min.Y+y))
		}
	}
	iop := paint.NewImageOp(sub)
	cl := clip.Rect(r).Push(gtx.Ops)
	tr := op.Affine(f32.NewAffine2D(1, 0, float32(r.Min.X), 0, 1, float32(r.Min.Y))).Push(gtx.Ops)
	iop.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
}

// diffStats 统计矩形 r 内的差异像素数、最大通道差、通道差之和。
func diffStats(a, b *image.RGBA, r image.Rectangle) (int, int, int) {
	n, mx, sum := 0, 0, 0
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
				sum += v
				if v > d {
					d = v
				}
			}
			if d > 0 {
				n++
			}
			if d > mx {
				mx = d
			}
		}
	}
	return n, mx, sum
}

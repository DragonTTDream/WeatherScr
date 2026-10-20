package ui

// ── 文字块绘制缓存（时钟块 + 天气块）─────────────────────────
//
// 实测依据（分层阶梯实测，180 s 窗口、单层掩码，取证记录见 out/notes-textcache.md）：
//
//	sky 6.6 s  depth 7.0 s  field 126 s  clock 10.7 s  weather 14 s
//
// clock + weather 两层合计 24.7 s/180 s = 单核 13.7%，全部花在**每帧重新排版
// 文字**上：15 fps 下每帧都重新 shape 字形、把字形轮廓写成 clip.Path 段、
// 重排 Flex 行盒。而时钟块的数字每秒只变一次、天气块默认一分钟才刷新一次
// （cmd/scr/main.go 的后台刷新），一帧里这些输入与上一帧逐字相同。
//
// 缓存做法：整块内容的绘制指令**录制**成一份宏（op.Record → op.CallOp），
// 键不变时逐帧重放（CallOp.Add），不再走排版与路径构建。
// 单条替换、不累积，写法照 ui/render.go:943 的 skyCached
// （key 相等且条目非空 ⇒ 命中；否则整条替换）。条数上限 2 = 时钟 1 条 + 天气 1 条。
//
// 为什么缓存产物是"指令宏"而不是位图：
// gioui v0.10.3 的文字是**矢量路径**，不是位图字形 ——
// text/gotext.go 把字形轮廓逐段写成 clip.Path 的 Move/Line/Quad/Cube
// （gioui.org@v0.10.3/text/gotext.go:575-615 的 SegmentOp 分支），
// 抗锯齿由 GPU 侧的路径光栅化完成；gioui 没有"把已录制指令渲染成 CPU 位图"
// 的公开 API（能读像素的只有 app.Window / headless.Window 的 Screenshot，
// 那是整窗回读，且要求先提交一帧）。要在 CPU 上烘出这张位图，
// 就得复刻 GPU 的路径光栅化与抗锯齿/合成公式 ⇒ 画面必然变化，
// 与"像素与现状一致"这条硬判据直接冲突。
// 取指令宏：重放的是**同一串绘制指令**，落点/裁剪/变换/颜色全部原样，
// 输出逐像素相同（ui/textcache_test.go 逐像素实测）。
// 这条与规格字面（image.RGBA + blit）的偏差记录在 out/notes-textcache.md。
//
// 附带收益：gioui 的 GPU 侧按 op 位置缓存已构建的路径顶点
// （gioui.org@v0.10.3/gpu/gpu.go:913-921 的 pathCache，键 = 指令所在缓冲区+偏移+版本）。
// 录制缓冲区不重建时这些键逐帧稳定 ⇒ 字形路径的顶点构建也命中缓存。

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// textCacheKey 是渲染一个文字块所需的**全部**输入。
// 任一项变化 ⇒ 整条缓存作废重建；全部相同 ⇒ 不重排。
type textCacheKey struct {
	// 落点与度量：块像素矩形（含面板 y 偏移）、块宽高（决定可用宽度与分隔线长）、
	// 以及 gtx.Metric 的两个换算系数（Sp/Dp → 像素，阴影偏移也走它）。
	x, y, w, h int
	pxPerDp    float32
	pxPerSp    float32
	// 主字色与阴影开关（阴影 = Panel.Shadow 且 LayerShadow 开着）。
	ink    color.NRGBA
	shadow bool
	// 时钟块输入：时间行、日期行。
	timeStr string
	dateStr string
	// 天气块输入：地名、条件、温度、预报、详情三项文本。
	location  string
	condition string
	temp      string
	forecast  string
	details   [3]string // rain / humidity / wind，空串 = 该格不画
}

// textCacheEntry 是缓存产物：录好的绘制宏 + 它在块内的布局尺寸。
type textCacheEntry struct {
	call op.CallOp
	dims layout.Dimensions
}

// textCacheSlot 是一个块的缓存槽：**单条**替换，不累积。
type textCacheSlot struct {
	key    textCacheKey
	ent    *textCacheEntry
	ops    *op.Ops // 专用录制缓冲区，重建时 Reset 后重录
	lastK  string
	lastMs float64
	hits   int
	builds int
}

var (
	// 条数上限 2：时钟 1 条 + 天气 1 条。
	clockTextCache   textCacheSlot
	weatherTextCache textCacheSlot

	// textCacheBypass 只给包内测试用：置 true 时 get 一律返回 nil，
	// 调用方回落到"逐帧直接绘制"的老路径。生产路径不读它、不读环境变量、
	// 不是设置项。用途 = 同一进程内做"缓存开/关"的逐像素对拍。
	textCacheBypass bool
)

// textCacheKeyBase 取两个块共用的输入。
func textCacheKeyBase(gtx layout.Context, px PxRect, f Frame) textCacheKey {
	return textCacheKey{
		x: px.X, y: px.Y, w: px.W, h: px.H,
		pxPerDp: gtx.Metric.PxPerDp,
		pxPerSp: gtx.Metric.PxPerSp,
		ink:     f.Panel.TextColor,
		shadow:  f.Panel.Shadow && f.Layers.on(LayerShadow),
	}
}

// String 给出可读的键摘要（取证用）。
func (k textCacheKey) String() string {
	return fmt.Sprintf("pos=%d,%d %dx%d dp=%.3f sp=%.3f ink=%v shadow=%v clock=%q|%q wx=%q|%q|%q|%q|%q",
		k.x, k.y, k.w, k.h, k.pxPerDp, k.pxPerSp, k.ink, k.shadow,
		k.timeStr, k.dateStr,
		k.location, k.condition, k.temp, k.forecast, k.details)
}

// get 返回该键对应的缓存条目；键变化时整条替换（重建）。
// 返回 nil = 不缓存（textCacheBypass），调用方按老路径逐帧直接绘制。
//
// 重建 = 在专用录制缓冲区里 Reset + 重录一遍块内容。Reset 会让**上一帧**
// 里引用它的 CallOp 失效：屏保与出图都在同一线程里"布局 → e.Frame(ops) 同步提交"，
// 下一次布局开始时上一帧已经画完，故 Reset 不会踩到在飞的帧。
func (c *textCacheSlot) get(k textCacheKey, gtx layout.Context, px PxRect, content layout.Widget) *textCacheEntry {
	if c.ent != nil && c.key == k {
		c.hits++
		return c.ent
	}
	if textCacheBypass {
		return nil
	}
	t0 := time.Now()
	if c.ops == nil {
		c.ops = new(op.Ops)
	}
	c.ops.Reset()
	g := gtx
	g.Ops = c.ops
	// 与 inPx 完全相同的定位与约束：Center 布局 + Exact(块宽高)。
	g.Constraints = layout.Exact(image.Point{X: px.W, Y: px.H})
	mac := op.Record(c.ops)
	dims := layout.Center.Layout(g, content)
	call := mac.Stop()
	c.key = k
	c.ent = &textCacheEntry{call: call, dims: dims}
	c.lastK = k.String()
	c.lastMs = float64(time.Since(t0).Microseconds()) / 1000.0
	c.builds++
	return c.ent
}

// inPxReplay 把缓存好的宏贴回块内，外层结构与 inPx 逐字相同
// （op.Offset 定位 + clip.Rect 限定 + Center 布局 + 两次 Pop），
// 只把"当场绘制内容"换成"重放录好的宏"。
func inPxReplay(gtx layout.Context, px PxRect, ent *textCacheEntry) {
	mac := op.Offset(image.Pt(px.X, px.Y)).Push(gtx.Ops)
	cl := clip.Rect(image.Rectangle{Max: image.Pt(px.W, px.H)}).Push(gtx.Ops)
	gtx2 := gtx
	gtx2.Constraints = layout.Exact(image.Point{X: px.W, Y: px.H})
	layout.Center.Layout(gtx2, func(g layout.Context) layout.Dimensions {
		ent.call.Add(g.Ops)
		return ent.dims
	})
	cl.Pop()
	mac.Pop()
}

// TextCacheStats 是文字块缓存的只读快照（取证用，不改变任何行为）。
type TextCacheStats struct {
	ClockHits, ClockBuilds     int
	WeatherHits, WeatherBuilds int
	Entries                    int // 现存条目数，上限 2
	ClockKey, WeatherKey       string
	ClockMs, WeatherMs         float64 // 最近一次重建耗时
}

// TextCacheStats 读出快照。只读，不新增设置项。
func (s *Screen) TextCacheStats() TextCacheStats {
	n := 0
	if clockTextCache.ent != nil {
		n++
	}
	if weatherTextCache.ent != nil {
		n++
	}
	return TextCacheStats{
		ClockHits: clockTextCache.hits, ClockBuilds: clockTextCache.builds,
		WeatherHits: weatherTextCache.hits, WeatherBuilds: weatherTextCache.builds,
		Entries:    n,
		ClockKey:   clockTextCache.lastK,
		WeatherKey: weatherTextCache.lastK,
		ClockMs:    clockTextCache.lastMs,
		WeatherMs:  weatherTextCache.lastMs,
	}
}

// textCacheResetForTest 清空两个槽与计数，只给包内测试用。
func textCacheResetForTest() {
	clockTextCache = textCacheSlot{}
	weatherTextCache = textCacheSlot{}
	textCacheBypass = false
}

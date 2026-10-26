//go:build !vulkan

// 基线快照（R5 拆分前的零行为基线 / R8 效果图交付固定口径 / R10 横屏口径）：
// 11 个天气场景 × 两态（铺放态 steps=0、满密度 steps=800）× 两制（竖 1080×1920、横 1920×1080）
// = 44 张出图 + 一张数据表（表内含度量行，图与度量同批交付，见经验库 ## 36）。
//
//	WS_BASELINE_DIR=/dir go test -tags=novulkan -count=1 -run TestBaselineDump ./ui
//
// 目录未设 ⇒ 跳过。本文件只做取证出图，不改任何产品行为。

package ui

import (
	"fmt"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"image"
	"image/png"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/metrics"
)

type baselineCase struct {
	class effect.Class
	name  string
	isDay bool
	steps int
	w, h  int
}

func baselineCases() []baselineCase {
	_cls := []struct {
		c     effect.Class
		name  string
		isDay bool
	}{
		{effect.Clear, "clear-day", true},
		{effect.Clear, "clear-night", false},
		{effect.PartlyCloudy, "partlycloudy-day", true},
		{effect.PartlyCloudy, "partlycloudy-night", false},
		{effect.Cloudy, "cloudy", true},
		{effect.Overcast, "overcast", false},
		{effect.Rain, "rain", true},
		{effect.Thunder, "thunder", false},
		{effect.Fog, "fog", true},
		{effect.Snow, "snow", true},
		{effect.Dust, "dust", true},
	}
	var out []baselineCase
	for _, dims := range [][2]int{{PortraitW, PortraitH}, {PortraitH, PortraitW}} {
		for _, c := range _cls {
			for _, st := range []int{0, 800} {
				out = append(out, baselineCase{
					class: c.c, name: c.name, isDay: c.isDay, steps: st, w: dims[0], h: dims[1],
				})
			}
		}
	}
	return out
}

func TestBaselineDump(t *testing.T) {
	dir := os.Getenv("WS_BASELINE_DIR")
	if dir == "" {
		t.Skip("WS_BASELINE_DIR 未设置，跳过基线出图")
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	now := time.Date(2026, 10, 23, 12, 34, 56, 0, time.UTC)
	s := New()
	rows := []string{"场景\t步数\t画布\t带-only%\t含粒子%\t平均层数\t最终最窄云px\t云贴图下标\t每帧绘制\t帧节拍 p50-p95-max"}

	for _, cs := range baselineCases() {
		field := NewField(cs.class, cs.isDay, 20261015)
		for i := 0; i < cs.steps; i++ {
			field.Step(66*time.Millisecond, cs.w, cs.h)
		}
		layers, dualIncl := field.DriftOverlapIncl()
		_, dualBand := field.DriftOverlapBandOnly()
		minW, sprs := airWidthProbe(field)

		win, err := headless.NewWindow(cs.w, cs.h)
		if err != nil {
			t.Skipf("headless 不可用: %v", err)
		}
		var ops op.Ops
		gtx := layout.Context{
			Ops: &ops, Constraints: layout.Exact(image.Pt(cs.w, cs.h)), Now: now,
		}
		fr := Frame{
			Now: now, Clock: DefaultClockSpec(), Weather: dumpSample(true),
			Class: cs.class, IsDay: cs.isDay, Field: field,
			Location: "Ziliujing, China", Panel: DefaultPanelStyle(), Layers: 0,
		}
		s.Layout(gtx, fr)
		// 每帧绘制（R8 判据 plans/active.md:92「逐类 … 每帧绘制 …」）口径 = 该步这一帧提交的
		// op.Ops 序列化长度：取 ui/metrics/frames.go:38 OpsLen 的 data（gioui v0.10.3 把子 op
		// 随块展平进同一条 data 流 ⇒ "含子 op 展平"；refs 只记块引用个数，不计入本列）。
		// 必须在 win.Frame 之前读（gpu.Update 末尾 ops.Reset()），反射只读、不改 op.Ops 内容。
		draws, _ := metrics.OpsLen(&ops)
		if err := win.Frame(&ops); err != nil {
			win.Release()
			t.Fatalf("帧提交失败 %s: %v", cs.name, err)
		}
		img := image.NewRGBA(image.Rect(0, 0, cs.w, cs.h))
		if err := win.Screenshot(img); err != nil {
			win.Release()
			t.Fatalf("Screenshot 失败 %s: %v", cs.name, err)
		}
		// 帧节拍（R8 判据 plans/active.md:92「… 帧节拍 p50-p95-max」）：截图已在这之前
		// 取完 ⇒ 下面的连渲完全不在图像路径上（同一窗口、同一个场，30 帧；窗口须先渲完
		// 再 Release，gioui headless 的上下文随 Release 失效）。
		// 步进 = Field.Step 的现有机制（66 ms 模拟步长），帧间零休眠（帧间隔 0 连渲）；
		// 每帧墙钟 = Layout + win.Frame 提交全程。p50 复用 ui/cloudrules_test.go:31 medianF
		// 现成口径，p95 = 同一插值口径取 q=0.95，max = 样本最大值；毫秒、保留 1 位小数。
		p50, p95, mx := cadenceCell(baselineCadence(t, win, s, fr, field, cs, now))
		win.Release()
		fname := fmt.Sprintf("%s_%d_%dx%d", cs.name, cs.steps, cs.w, cs.h)
		f, err := os.Create(dir + "/" + fname + ".png")
		if err != nil {
			t.Fatalf("建文件失败: %v", err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("编码失败: %v", err)
		}
		f.Close()
		rows = append(rows, fmt.Sprintf("%s\t%d\t%dx%d\t%.2f\t%.2f\t%.2f\t%.0f\t%s\t%d\t%.1f/%.1f/%.1f",
			cs.name, cs.steps, cs.w, cs.h, dualBand, dualIncl, layers, minW, sprs,
			draws, p50, p95, mx))
	}
	if err := os.WriteFile(dir+"/table.tsv", []byte(joinRows(rows)), 0o666); err != nil {
		t.Fatalf("写表失败: %v", err)
	}
	t.Logf("基线 %d 张 + 表已落 %s", len(baselineCases()), dir)
}

// cadenceFrames 帧节拍样本数（R8 判据：连续 30 帧）。
const cadenceFrames = 30

// baselineCadence 在同一个 headless 窗口上连渲 cadenceFrames 帧，返回逐帧墙钟毫秒。
// 步进走 Field.Step(66 ms, w, h)（基线出图既有的步进机制），帧间不睡眠；
// 每帧墙钟 = s.Layout + win.Frame 提交全程；不改任何绘制参数。
func baselineCadence(t *testing.T, win *headless.Window, s *Screen, fr Frame, field *Field, cs baselineCase, now time.Time) []float64 {
	ms := make([]float64, 0, cadenceFrames)
	for i := 0; i < cadenceFrames; i++ {
		field.Step(66*time.Millisecond, cs.w, cs.h)
		var oop op.Ops
		gtx := layout.Context{
			Ops: &oop, Constraints: layout.Exact(image.Pt(cs.w, cs.h)), Now: now,
		}
		t0 := time.Now()
		s.Layout(gtx, fr)
		if err := win.Frame(&oop); err != nil {
			win.Release()
			t.Fatalf("帧节拍渲染失败 %s: %v", cs.name, err)
		}
		ms = append(ms, float64(time.Since(t0).Microseconds())/1000.0)
	}
	return ms
}

// cadenceCell 帧节拍三值：p50 = medianF（现成口径），p95 = quantileF(v, 0.95)，max = 样本最大值。
func cadenceCell(v []float64) (p50, p95, mx float64) {
	p50 = medianF(v)
	p95 = quantileF(v, 0.95)
	mx = math.NaN()
	for _, x := range v {
		if math.IsNaN(mx) || x > mx {
			mx = x
		}
	}
	return
}

// quantileF 分位数（与 ui/cloudrules_test.go:31 medianF 同一插值口径：排序副本 + 位置
// q·(n−1) 的线性插值；q=0.5 时逐字等于 medianF）。本表只用它导 p95，p50 直接走 medianF
// ⇒ 全仓一套分位口径，不新造第二套算法。
func quantileF(v []float64, q float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n == 0 {
		return math.NaN()
	}
	if n == 1 {
		return c[0]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return c[lo]
	}
	f := pos - float64(lo)
	return c[lo]*(1-f) + c[hi]*f
}

func joinRows(rows []string) string {
	out := ""
	for _, r := range rows {
		out += r + "\n"
	}
	return out
}

// airWidthProbe 逐云量最终屏宽（px）与云贴图下标清单（R4 小云 / R3 白名单 的取证口径）。
func airWidthProbe(f *Field) (float64, string) {
	minW := -1.0
	seen := ""
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud && p.kind != pkFog {
			continue
		}
		w := cloudDrawWidthPx(p)
		if w <= 0 {
			continue
		}
		if minW < 0 || w < minW {
			minW = w
		}
		if p.kind == pkCloud {
			seen += fmt.Sprintf("%d,", p.spr)
		}
	}
	if minW < 0 {
		minW = 0
	}
	return minW, seen
}

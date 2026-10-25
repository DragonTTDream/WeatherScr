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
	"os"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
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
	rows := []string{"场景\t步数\t画布\t带-only%\t含粒子%\t平均层数\t最终最窄云px\t云贴图下标"}

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
		s.Layout(gtx, Frame{
			Now: now, Clock: DefaultClockSpec(), Weather: dumpSample(true),
			Class: cs.class, IsDay: cs.isDay, Field: field,
			Location: "Ziliujing, China", Panel: DefaultPanelStyle(), Layers: 0,
		})
		if err := win.Frame(&ops); err != nil {
			win.Release()
			t.Fatalf("帧提交失败 %s: %v", cs.name, err)
		}
		img := image.NewRGBA(image.Rect(0, 0, cs.w, cs.h))
		if err := win.Screenshot(img); err != nil {
			win.Release()
			t.Fatalf("Screenshot 失败 %s: %v", cs.name, err)
		}
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
		rows = append(rows, fmt.Sprintf("%s\t%d\t%dx%d\t%.2f\t%.2f\t%.2f\t%.0f\t%s",
			cs.name, cs.steps, cs.w, cs.h, dualBand, dualIncl, layers, minW, sprs))
	}
	if err := os.WriteFile(dir+"/table.tsv", []byte(joinRows(rows)), 0o666); err != nil {
		t.Fatalf("写表失败: %v", err)
	}
	t.Logf("基线 %d 张 + 表已落 %s", len(baselineCases()), dir)
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

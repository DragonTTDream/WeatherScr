package ui

// 落点印记（水花）删除后的自检：状态层 + 雨丝本身。
// 判据两条：
// 1. 承载落点印记的状态必须整体不存在 —— Field / particle 都不得再有相关字段
// （删除前 Field 有 splashes、particle 有 splashed，本判据在删除前必红）。
// 2. 雨丝本身不许被一起删掉：近层雨滴越过下边缘后照常回收、照常铺回屏内。

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestFieldCarriesNoLandingMarks 落点印记的状态字段必须为零。
func TestFieldCarriesNoLandingMarks(t *testing.T) {
	for _, tc := range []struct {
		name string
		rt   reflect.Type
	}{
		{"Field", reflect.TypeOf(Field{})},
		{"particle", reflect.TypeOf(particle{})},
	} {
		for i := 0; i < tc.rt.NumField(); i++ {
			f := tc.rt.Field(i)
			if strings.Contains(strings.ToLower(f.Name), "splash") {
				t.Errorf("%s 仍带落点印记状态字段 %s(%s) ⇒ 状态没删净", tc.name, f.Name, f.Type)
			}
		}
	}
}

// TestNearLayerDropsStillRecycle 近层雨滴（旧口径 depth>0.6 才会留印记）
// 越过下边缘后必须照常回收并铺回屏内：删印记不能把雨丝的回收路径一起删掉。
func TestNearLayerDropsStillRecycle(t *testing.T) {
	f := NewField(effect.Rain, true, 20261015)
	n := f.TotalCount()
	if n == 0 {
		t.Fatalf("雨场没有粒子，测试无意义")
	}
	idx := -1
	for i := range f.parts {
		if f.parts[i].kind == pkDrop && f.parts[i].depth > 0.6 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("雨场没有近层雨滴（depth>0.6），测试无意义")
	}
	f.parts[idx].x, f.parts[idx].y = 0.5, 1.05
	respawned := false
	for k := 0; k < 5 && !respawned; k++ {
		f.Step(66*time.Millisecond, 1080, PortraitH)
		if p := f.parts[idx]; p.y < 1.0 {
			respawned = true
		}
	}
	p := f.parts[idx]
	if !respawned {
		t.Errorf("越过下边缘的近层雨滴没有被回收: y=%.3f", p.y)
	}
	if p.kind != pkDrop {
		t.Errorf("回收后粒子类型变了: kind=%d", p.kind)
	}
	if got := f.TotalCount(); got != n {
		t.Errorf("粒子总数应不变: got %d want %d", got, n)
	}

	// 连续推进：雨滴必须一直在下落、一直在回收，不出现"停在屏外"的堆积
	drops := 0
	moving := 0
	for i := range f.parts {
		if f.parts[i].kind == pkDrop {
			drops++
			if f.parts[i].vy > 0 {
				moving++
			}
		}
	}
	if drops == 0 {
		t.Fatalf("雨场没有 pkDrop 粒子")
	}
	if moving != drops {
		t.Errorf("雨滴必须全部向下运动: moving=%d drops=%d", moving, drops)
	}
	for k := 0; k < 300; k++ {
		f.Step(66*time.Millisecond, 1080, PortraitH)
	}
	stuck := 0
	for i := range f.parts {
		p := f.parts[i]
		if p.kind != pkDrop {
			continue
		}
		if p.y > 1.15 || p.y < -0.35 {
			stuck++
		}
	}
	if stuck > 0 {
		t.Errorf("推进 300 帧后仍有 %d 滴停在回收窗口外（回收路径被改动）", stuck)
	}
	t.Logf("近层雨滴 idx=%d 回收后 y=%.3f；雨滴 %d 颗全部向下，300 帧后窗口外 %d 颗", idx, p.y, drops, stuck)
}

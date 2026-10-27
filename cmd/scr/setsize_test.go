package main

// 钉死表+ 全表 w>h 断言。
// 表 = settingsSizeFor(land, workW, workH) → (w, h, minW, minH)。

import "testing"

func TestSettingsSizeForTable(t *testing.T) {
	cases := []struct {
		land             bool
		workW, workH     int
		w, h, minW, minH int
	}{
		{true, 1920, 1080, 1920, 1080, 900, 540},
		{false, 1080, 1920, 1080, 640, 720, 420},
		{false, 800, 600, 800, 600, 720, 420},
		{false, 700, 900, 700, 640, 700, 420},
		{false, 480, 1920, 480, 420, 480, 420},
	}
	for _, c := range cases {
		w, h, minW, minH := settingsSizeFor(c.land, c.workW, c.workH)
		if w != c.w || h != c.h || minW != c.minW || minH != c.minH {
			t.Errorf("settingsSizeFor(%v,%d,%d) = (%d,%d,%d,%d)，要求 (%d,%d,%d,%d)",
				c.land, c.workW, c.workH, w, h, minW, minH, c.w, c.h, c.minW, c.minH)
		}
		if !(w > h) {
			t.Errorf("settingsSizeFor(%v,%d,%d) 全表 w>h 断言：w=%d h=%d 不满足宽>高",
				c.land, c.workW, c.workH, w, h)
		}
	}
}

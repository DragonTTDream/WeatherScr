package main

import "testing"

// 铺满口径钉死（批次 b-cover-v1，2026-10-22 实机取证：
// 截图底部 y=1880–1919 共 40 px 纯白 = 任务栏；帧日志 size=1080x1880 共 400 帧）。

// Win32 官方值逐字钉死（闸门口径：常量必须对官方表核）。
func TestCoverWin32ConstantsOfficial(t *testing.T) {
	want := map[string]uint64{
		"wsPopup":       uint64(wsPopup),
		"wsVisible":     uint64(wsVisible),
		"swShowNormal":  uint64(swShowNormal),
		"swShow":        uint64(swShow),
		"swHide":        uint64(swHide),
		"swShowNa":      uint64(swShowNa),
		"swpShowWindow": uint64(swpShowWindow),
	}
	for k, v := range want {
		if got, exp := v, officialVals[k]; got != exp {
			t.Errorf("%s = 0x%X，官方值 0x%X", k, got, exp)
		}
	}
	if uintptr(hwndTopmost) != ^uintptr(0) {
		t.Errorf("HWND_TOPMOST 必须是 -1，实得 %v", hwndTopmost)
	}
	if uintptr(hwndTop) != 0 {
		t.Errorf("HWND_TOP 必须是 0，实得 %v", hwndTop)
	}
}

// winuser.h 官方表值（抄录，不派生）。
var officialVals = map[string]uint64{
	"wsPopup":       0x80000000, // WS_POPUP
	"wsVisible":     0x10000000, // WS_VISIBLE
	"swShowNormal":  1,          // SW_SHOWNORMAL
	"swShow":        5,          // SW_SHOW
	"swHide":        0,          // SW_HIDE
	"swShowNa":      8,          // SW_SHOWNA
	"swpShowWindow": 0x0040,     // SWP_SHOWWINDOW
}

// 判据口径：铺满目标样式里 WS_OVERLAPPEDWINDOW(0x00CF0000) 的六个位必须全为 0。
func TestCoverStyleTargetHasNoOverlappedWindowBits(t *testing.T) {
	const wsOverlappedwindow = 0x00CF0000
	if got := coverStyleTarget(); got&wsOverlappedwindow != 0 {
		t.Errorf("coverStyleTarget = 0x%X 含 OVERLAPPEDWINDOW 位（必须为 0）", got)
	}
	if coverStyleTarget() != wsPopup|wsVisible {
		t.Errorf("铺满目标样式必须是 WS_POPUP|WS_VISIBLE，实得 0x%X", coverStyleTarget())
	}
}

// DEBUG 小窗几何逐字钉死（数值取自实机日志 13:11:46 的 270x480 @405,720）。
// 口径：小盒在大矩形内居中 ⇒ 左上角 = 大矩形原点 + 半个余量（**必须带原点偏移**，
// 我第一版把期望写成 (200,250) 就是漏了这个偏移，闸门当场拦下）。
func TestDbgQuarterRect(t *testing.T) {
	cases := []struct {
		x, y, w, h     int32
		ex, ey, ew, eh int32
	}{
		{0, 0, 1080, 1920, 405, 720, 270, 480},            // 实机口径：1/4 居中
		{0, 0, 1079, 1919, 405, 720, 269, 479},            // 奇数向下取整
		{100, 200, 800, 400, 400, 350, 200, 100},          // 带原点偏移（100+300, 200+150）
		{-1920, -1080, 1920, 1080, -1200, -675, 480, 270}, // 负原点（副屏在左上）
		{0, 0, 3, 3, 1, 1, 0, 0},                          // 3/4 = 0 ⇒ 零尺寸盒
		{7, 9, 0, 5, 7, 9, 0, 0},                          // 退化：w<=0 ⇒ 零盒，原点不动
		{7, 9, 5, 0, 7, 9, 0, 0},                          // 退化：h<=0
	}
	for _, c := range cases {
		x, y, w, h := dbgQuarterRect(c.x, c.y, c.w, c.h)
		if x != c.ex || y != c.ey || w != c.ew || h != c.eh {
			t.Errorf("dbgQuarterRect(%d,%d,%d,%d) = (%d,%d,%d,%d)，期望 (%d,%d,%d,%d)",
				c.x, c.y, c.w, c.h, x, y, w, h, c.ex, c.ey, c.ew, c.eh)
		}
	}
}

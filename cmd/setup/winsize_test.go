package main

import "testing"

// 按钮底边 ≤ 客户区高度，客户区高度 = 窗口高度 − 标题栏 − 边框
// （与 AdjustWindowRectEx 同一套数值：标题栏 31 + 上下边框 8 = 39，左右边框 16）。
func TestButtonRowInsideClientArea(t *testing.T) {
	cases := []struct {
		name       string
		winW, winH int32
	}{
		{"实机窗口 620x460（旧口径的窗口尺寸）", 620, 460},
		{"新口径：期望客户区 620x460 ⇒ 窗口 636x499", 636, 499},
		{"最小窗口 480x420", 480, 420},
		{"小工作区窗口 480x420（工作区 90% 夹到下限）", 480, 420},
	}
	for _, c := range cases {
		cw, ch := clientSizeFromWindow(c.winW, c.winH)
		by := buttonTopY(ch)
		bottom := by + btnH
		t.Logf("%s: 窗口 %dx%d ⇒ 客户区 %dx%d，按钮行 y=%d..%d，客户区高 %d，底边留白 %d",
			c.name, c.winW, c.winH, cw, ch, by, bottom, ch, ch-bottom)
		if bottom > ch {
			t.Errorf("%s: 按钮底边 %d 超出客户区高度 %d（超 %d px）", c.name, bottom, ch, bottom-ch)
		}
		if by < 0 {
			t.Errorf("%s: 按钮行顶边 %d 为负", c.name, by)
		}
	}
}

// 证据 E3 的复现：旧代码把**窗口**高度当客户区高度布局 ⇒ 按钮被裁 27px。
func TestOldLayoutClipsButtons(t *testing.T) {
	const winW, winH int32 = 620, 460 // 旧代码：CreateWindowExW 用 620×460，布局也用 620×460
	_, ch := clientSizeFromWindow(winW, winH)
	by := winH - 46 // 旧代码的 by
	bottom := by + btnH
	t.Logf("旧口径：窗口 %d×%d ⇒ 真实客户区高 %d；by=%d，底边=%d ⇒ 被裁 %d px", winW, winH, ch, by, bottom, bottom-ch)
	if ch != 421 {
		t.Errorf("客户区高度 = %d，期望 421（460 − 标题栏 31 − 上下边框 8）", ch)
	}
	if bottom-ch != 27 {
		t.Errorf("被裁 %d px，期望 27", bottom-ch)
	}
}

// 客户区 ⇄ 窗口尺寸换算（AdjustWindowRectEx 的加法语义）。
func TestClientWindowRoundTrip(t *testing.T) {
	ww, wh := windowSizeFromClient(620, 460)
	if ww != 636 || wh != 499 {
		t.Errorf("客户区 620×460 ⇒ 窗口应为 636×499，实得 %d×%d", ww, wh)
	}
	cw, ch := clientSizeFromWindow(ww, wh)
	if cw != 620 || ch != 460 {
		t.Errorf("窗口 636×499 ⇒ 客户区应为 620×460，实得 %d×%d", cw, ch)
	}
	if by := buttonTopY(ch); by+btnH > ch {
		t.Errorf("by=%d + 按钮高 %d = %d 超出客户区高 %d", by, btnH, by+btnH, ch)
	}
	t.Logf("客户区 620×460 ⇒ 窗口 %d×%d；by=%d，底边=%d ≤ 客户区高 %d", ww, wh, buttonTopY(ch), buttonTopY(ch)+btnH, ch)
}

package main

// 客户区 ⇄ 窗口尺寸的纯算术（无 build tag ⇒ 可在非 windows 平台跑测试）。
//
// 证据 E3（spec §0.3）：控件坐标是**客户区**坐标，CreateWindowExW 的 w/h 是**窗口**尺寸。
// 实机读数：窗口 620×460 ⇒ 客户区 = 460 − 标题栏(≈31) − 上下边框(≈8) = 421，
// 而旧代码按 460 布局按钮（y=414、高 34 ⇒ 414..448）⇒ 超出 421，被裁约 27px。
//
// WS_OVERLAPPEDWINDOW 在 96 DPI 下的非客户区（user32!AdjustWindowRectEx 对空矩形的返回值）：
// 宽 = 左+右边框 = 8+8 = 16；高 = 标题栏 31（SM_CYCAPTION 23 + 顶部边框 8）+ 底部边框 8 = 39。
// 实机对照：窗口 620×460 ⇒ 客户区 604×421（spec §0.3 的 460−31−8=421）。
// 下面这组常量与 AdjustWindowRectEx 是**同一套数值**（cmd/setup/main.go 的 nonClientSize
// 用 AdjustWindowRectEx 取实际样式的真实值，两者不一致时按真实值重算窗口尺寸）。
const (
	captionPx  = 31 // 标题栏（SM_CYCAPTION 23 + 顶部边框 8）
	frameVPx   = 8  // 上+下边框（各 8）
	frameHPx   = 16 // 左+右边框（各 8）
	ncWidthPx  = frameHPx
	ncHeightPx = captionPx + frameVPx // 39

	btnW        = 150 // 三个按钮并排的宽度（窗口最小宽度 480 的由来）
	btnH        = 34  // 按钮高度
	btnBottomPx = 12  // 按钮底边到客户区底边的留白
)

// windowSizeFromClient 客户区尺寸 ⇒ 窗口尺寸（AdjustWindowRectEx 的加法语义）。
func windowSizeFromClient(cw, ch int32) (int32, int32) {
	return cw + ncWidthPx, ch + ncHeightPx
}

// clientSizeFromWindow 窗口尺寸 ⇒ 客户区尺寸（同一套数值的减法）。
func clientSizeFromWindow(ww, wh int32) (int32, int32) {
	return ww - ncWidthPx, wh - ncHeightPx
}

// buttonTopY 页内按钮行的顶边 y（客户区坐标）。
// 底边 = clientH − btnBottomPx ⇒ 恒 ≤ 客户区高度（T3 判据）。
func buttonTopY(clientH int32) int32 {
	return clientH - (btnH + btnBottomPx)
}

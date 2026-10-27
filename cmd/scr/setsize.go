package main

// 设置窗强制横向矩形。
// 需求：设置页改成横向矩形——没有软件把设置页做成竖向长条，
// 设置窗必须是横向矩形。
// 旧竖屏档 640×1100（竖向长条）由本函数的钳制口径取代 ⇒ 朝向跟随逻辑的
// "竖屏取高>宽尺寸"这条根因被排除（见下面的钳制式与阈值 900）。

// settingsSizeFor 按朝向与工作区算设置窗口的 (w, h, minW, minH)（px）。
// 纯函数：不查系统、无副作用 ⇒ 供钉死单测 cmd/scr/setsize_test.go。
// dp/px 口径沿用 run_lifecycle.go 既有注释：100% 缩放下 1 dp = 1 px。
// 钳制式：
//
//	横屏实机（workArea 宽>高）：保持现值 —— 请求 1920×1080、MinSize 900×540
//	 （现有行为零回归）。
//	竖屏实机：请求 1100×640、MinSize 720×420，按工作区钳制：
//	 sizeW = min(1100, workW)
//	 sizeH = min(640, workH)
//	 sizeH = min(sizeH, sizeW - 60) // 保宽>高
//	 minW = min(720, sizeW)
//	 minH = min(420, sizeH)
//	竖屏 1080×1920 ⇒ (1080, 640) 宽>高。
//
// w>h 对钉死表全部行恒成立（setsize_test.go 全表断言）；工作区尺寸非正
// （异常查询结果）时按请求值口径不钳制，保证输出仍满足 w>h。
func settingsSizeFor(land bool, workW, workH int) (w, h, minW, minH int) {
	if land {
		// 横屏实机：现有行为零回归。
		return 1920, 1080, 900, 540
	}
	if workW <= 0 {
		workW = 1100 // 工作区尺寸不可问 ⇒ 不钳制（请求值直出）
	}
	if workH <= 0 {
		workH = 640
	}
	w = min(1100, workW)
	h = min(640, workH)
	h = min(h, w-60) // 保宽>高
	minW = min(720, w)
	minH = min(420, h)
	return w, h, minW, minH
}

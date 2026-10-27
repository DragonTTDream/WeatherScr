package main

// 职责（app/scr）：朝向切换：workAreaIsLandscape 消费点、横竖二值选择
// 搬移来源：cmd/scr/main.go:（本卡无可搬移声明）（逐字，含注释）
// 规格：—— app/scr = 屏保生命周期；本卡零行为变化。
// TODO(未取证)：拆分点名的 workAreaIsLandscape 消费点与横竖二值选择在现树里不是独立声明，唯一消费点在
// run_lifecycle.go:runSettings 体内（旧 main.go:495 land := workAreaIsLandscape()、:496-500 尺寸二值、
// 517 ui.NewPanelTheme(land)、:554 normalizeDialog(…, land)）。
// 纯搬移纪律（函数体逐字不动、只挪文件）下不可把消费点从 runSettings 里切出 ⇒ 本文件暂无可搬移声明。
// 朝向判定本体：ui 包 ui.PanelIsLandscape 与 cmd/scr/foreground_windows.go:workAreaIsLandscape（本卡未动）。

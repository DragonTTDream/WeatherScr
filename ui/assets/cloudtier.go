package assets

import "github.com/DragonTTDream/WeatherScr/effect"

// ── 云贴图档表 + R3 贴图白名单（R5 卡 6 自 ui/effects.go 逐字迁入）──────────
//
// 职责口径 = "只装配，不做规则"：本文件只做逐类云形态**表**与贴图**白名单**
// 的装配数据；宽度 lo/hi 由 ui 侧运动规则（kinetics.WidthFor）算出后传入
// CloudForm，本包不算宽度（见 ui/effects.go 的 cloudFormOf）。
//
// 注释里的字段名 wLo/lift/aLo/pressTop/idx 为旧实现（ui/effects.go:610-617）
// 原文用名；迁入后导出字段为 WLo/Lift/ALo/AHi/PressTop/Idx，数字逐字不动。

// ---- 逐类云形态口径（2026-10-20 云规则改动，规格 2 的表逐字落地）----
//
// 屏宽 = 屏上像素宽（1080 px 屏口径，与云带同一 px 口径）。粒子云屏宽 =
// 源图 512 px × p.sc × scaleMul ÷ bandNetDiv ⇒ p.sc 由**目标屏宽反解**
// （sc 是纯倍率，屏宽是口径本身，抖动 scaleMul 只改形状不改屏宽）。
//
//	类                屏宽 px     体色（运行时着色）       alpha        纵向位置
//	晴 clear          300–600     白（提亮 +140）         0.85–0.95    视差：小=远=靠上
//	多云见日 partly   300–700     白偏亮（+115）          0.80–0.92    视差
//	多云 cloudy      500–900     中灰（+45）             0.75–0.90    视差：大=近=靠下
//	阴 overcast      768–1178    中暗灰（+10）           0.85–0.95    压顶
//	雨/雷 rain/thunder 768–1178  暗灰（−32 ⇒ 降 25–40）  0.88–0.96    压顶
//	雪 snow          均匀灰幕（雾精灵 1024 px 源，lift −70） 现 fogAlphaMul  满屏均匀不堆团
//	尘 dust          现口径                             现口径        现口径
//
// 检索依据（出处逐条，规格 0 要求写进注释）：
//
//	雨/雷 = 又厚又黑的云："大雨来临前，天空通常会变得阴沉沉，云也越来越黑"
//	"造成降水的云最主要有两种，一种叫积雨云，一种叫雨层云"
//	  https://www.weather.com.cn/hainan/zyqxxx/05/2869945.shtml
//	晴天云 = 白棉团、边缘清楚："积云…通常具有如同棉花的形状…颜色为白色到浅灰色，
//	常见于大晴天"
//	  https://xinwen.bjd.com.cn/content/s64a8e0f1e4b03d11a64d1566.html
//	视差口径（远=上=小=慢、近=下=大=快）："background images move past the camera
//	more slowly than foreground"
//	  https://wikiwand.com/en/Parallax_scrolling
//	云速随高度（薄高云快、厚低云慢）："higher clouds typically moving much faster
//	than those near the surface"/"high thin cirrus streaks fly fast while low
//	stratus creeps along"
//	  https://popsci.blog/fast-moving-clouds-meaning
//	  https://calendar-holidays.com/how-fast-do-clouds-travel
//
// 体色 = **运行时着色**（在贴图 RGB 上加一个常量，ui/embed/*.png 逐字节不动）：
// 实测 9 张云精灵实心像素（alpha>200）反预乘均色 = (92,93,95)…(114,115,119)，
// 现档云体就是这层深灰 ⇒ 晴/多云见日的云读成"与雾天没区别"的淡灰大幕。
// lift 逐档：晴 +140（→ 232…255 白）、多云见日 +115（→ 207…234 白偏亮）、
// 多云 +45（→ 137…164 中灰）、阴 +10（→ 102…129 中暗灰）、
// 雨/雷 −32（→ 60…87，比现档降 32 级，落在规格要求的 25–40 区间内）。
type CloudForm struct {
	WLo, WHi float64 // 屏宽区间（px，1080 屏口径）
	Lift     float64 // 体色提亮/压暗量（加到贴图 RGB，运行时着色）
	ALo, AHi float64 // alpha 区间
	PressTop bool    // true = 压顶（云中心 y 落在画面上缘带）
	Idx      []int   // 按类挑选的云精灵下标（0 基）
}

// 云精灵按类挑选（按 9 张贴图的**实测内容**分配，不是按文件名顺序）：
//
//	实测口径 = alpha>40 的实心占比 fill、实心区平均列高 meanColH、bbox、最大 alpha
//	cloud1 378×255 fill .639 meanColH 242 maxA  95  → 紧凑厚团（边缘软）
//	cloud2 259×214 fill .461 meanColH 120 maxA  61  → 最小的小棉团
//	cloud3 459×321 fill .655 meanColH 301 maxA 252  → 最厚最实（积云）
//	cloud4 356×245 fill .669 meanColH 238 maxA  81  → 紧凑厚团
//	cloud5 468×468 fill .286 meanColH 134 maxA 229  → 大块散点积云
//	cloud6 498×420 fill .403 meanColH 201 maxA 255  → 顶厚（上/下质量比 2.18）
//	cloud7 425×288 fill .075 meanColH  32 maxA  77  → 细条薄卷云（上/下比 5.11）
//	cloud8 466×449 fill .193 meanColH  90 maxA 137  → 散点薄云
//	cloud9 507×499 fill .320 meanColH 162 maxA 244  → 大朵散积云
//
// ⇒ 薄小那几张 = cloud7/2/8（fill ≤ .193、meanColH ≤ 120）；
//
//	中档 = cloud1/4/9；厚大那几张（积云系）= cloud3/6/5（fill ≥ .286 且 maxA ≥ 229）。
//
// CloudIdxAllowed = **R3 贴图白名单**（B3，evidence/p8-numbers-20261023.md:26-33）：
// 可用 = cloud1 / cloud2 / cloud4 ⇒ 0 基索引 **{0,1,3}**；
// 需求逐字"cloud3-5-6-7-8-9不使用" ⇒ 原三档表 cloudIdxThin{6,1,7} / cloudIdxMid{0,3,8} /
// cloudIdxThick{2,5,4} 合并为单一表，三处 cloudForm.idx 全部指它（上面那段 9 张实测内容表保留做停用依据）。
var CloudIdxAllowed = []int{0, 1, 3}

// CloudTierSpec = 云贴图档表的逐档装配口径（不含屏宽：屏宽是 kinetics 运动规则
// 的输出，由 ui 侧 cloudFormOf 算出后填进 CloudForm 的 WLo/WHi，本包不算）。
type CloudTierSpec struct {
	Lift     float64 // 体色提亮/压暗量
	ALo, AHi float64 // alpha 区间
	PressTop bool    // 压顶判据
	Idx      []int   // 白名单下标表
}

// CloudTier 逐类取档表条目（自 ui/effects.go:660-684 cloudFormOf 的表体逐字迁入；
// 旧函数里的宽度计算 = kinetics 规则调用，留在 ui 侧，见 cloudFormOf）。
// 未列类（snow/dust/未知）= 旧 cloudFormOf 的 fall-through 默认档，逐字保留。
func CloudTier(cls effect.Class) CloudTierSpec {
	switch cls {
	case effect.Clear:
		// B3：原 300–420（"薄快厚慢"排序用的薄档）整档低于 R4 下限 ⇒ 抬到 480–529。
		// 速度排序不再由档间宽度差承担（各档同带），由 kinetics 的档内速度式 + 进场排队承担。
		return CloudTierSpec{Lift: 140, ALo: 0.85, AHi: 0.95, PressTop: false, Idx: CloudIdxAllowed}
	case effect.PartlyCloudy:
		return CloudTierSpec{Lift: 115, ALo: 0.80, AHi: 0.92, PressTop: false, Idx: CloudIdxAllowed}
	case effect.Cloudy:
		return CloudTierSpec{Lift: 45, ALo: 0.75, AHi: 0.90, PressTop: false, Idx: CloudIdxAllowed}
	case effect.Overcast:
		return CloudTierSpec{Lift: 10, ALo: 0.85, AHi: 0.95, PressTop: true, Idx: CloudIdxAllowed}
	case effect.Rain, effect.Thunder:
		return CloudTierSpec{Lift: -32, ALo: 0.88, AHi: 0.96, PressTop: true, Idx: CloudIdxAllowed}
	case effect.Fog:
		// B3：云档 480–529；雾团封顶 clampFogWidthPx 不动（不动清单）。
		return CloudTierSpec{Lift: 0, ALo: 0.92, AHi: 1.0, PressTop: false, Idx: CloudIdxAllowed}
	}
	return CloudTierSpec{Lift: 0, ALo: 0.92, AHi: 1.0, PressTop: false, Idx: CloudIdxAllowed}
}

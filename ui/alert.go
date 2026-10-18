package ui

import (
	"image"
	"image/color"
	"math"
	"strconv"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
)

// 降雨提醒（需求 1）。
//
// 上游事实：上游**没有任何天气预警/提醒功能**（grep 到的 warning 全是 Steam/音频日志），
// 上游 daily 只取 temperature_2m_max/min + weathercode（open_meteo_provider.py:332）
// ⇒ 本条是需求驱动的新增，不是上游对齐项。
//
// 阈值与配色按**中国标准**，不是随手拍的数：
// - 中国气象局令第 16 号《气象灾害预警信号发布与传播办法》（2007-06-12 施行）：
// 暴雨预警分蓝/黄/橙/红四级，颜色对应**一般 / 较重 / 严重 / 特别严重**；
// 蓝 = 12 小时内降雨量将达 50 mm 以上，黄 = 6 小时内 50 mm 以上，红 = 3 小时内 100 mm 以上。
// - 中国降水概率分级（11 级，每 10% 一级）：<20% 基本不会发生、
// **30%–50% 有机会出现降水**、60%–70% 可能性较大、>70% 认为有降水发生、80%–90% 可能性非常大。
// ⇒ 触发线取官方"有机会出现降水"下限 **30%**。
//
// 数据源（逐字定为）：
// **未来 N 小时滚动窗口**（weather.WindowProb），不再是"一天的最大值"；
// N = config.alert_window_hours（默认 3，可设 6） ⇒ Alert.WindowHours。
// 阈值口径与档位配色**不变**。

// AlertThresholdPct = 触发线（官方"有机会出现降水"下限）。
const AlertThresholdPct = 30

// Alert 一条降雨提醒。
type Alert struct {
	// Pct = 预警概率（%）。低于 AlertThresholdPct 时不画。
	// 现行口径 = **未来 WindowHours 小时滚动窗口里的最大概率**
	// （weather.WindowProb）；
	// 不再是 daily.precipitation_probability_max（一天的最大值）。
	Pct int
	// MM = 窗口内的预计降雨量（mm，逐小时 precipitation 求和）。<=0 = 未知，正文不显示该项。
	MM float64
	// WindowHours = 滚动窗口长度（小时），**逐字出现在文案里**。
	// 合法值只有 3 与 6（config.alert_window_hours，默认 3）；其它值按 3 出文案。
	WindowHours int
	// Lang 文案语言：取值 "en"/"zh"，
	// **只有恰好 "zh" 走中文，其余一律英文** —— 与 cmd/scr/main.go 的 displayLang
	// 口径一致（同文件 :339-344）。由 buildAlert 从配置传进来。
	Lang string
	// Tomorrow = 文案说的是明日而非今日（需求 3 的"明日"模板）。
	// 生产路径不填（buildAlert 只用今日数据，口径）。
	Tomorrow bool
	// Pulse 0..1 脉动相位；PulseOn 是否做缓慢呼吸。
	Pulse   float64
	PulseOn bool
	// YOff 提醒行的垂直偏移（像素，可调，与 ClockYOff / WeatherYOff 同族）。
	YOff int
}

// Tier 标准四级：1=蓝 2=黄 3=橙 4=红。
func (a *Alert) Tier() int {
	switch {
	case a.Pct >= 90:
		return 4
	case a.Pct >= 70:
		return 3
	case a.Pct >= 50:
		return 2
	default:
		return 1
	}
}

// TierName 四级名。
//
// 语言口径（需求 1）：**只有恰好 "zh" 走中文，
// 其余一律英文** —— 与 cmd/scr/main.go 的 displayLang 口径一致。
// 档位名映射（需求 2）：蓝/黄/橙/红 → Blue/Yellow/Orange/Red。
func (a *Alert) TierName(lang string) string {
	if lang != "zh" {
		switch a.Tier() {
		case 4:
			return "Red"
		case 3:
			return "Orange"
		case 2:
			return "Yellow"
		default:
			return "Blue"
		}
	}
	switch a.Tier() {
	case 4:
		return "红"
	case 3:
		return "橙"
	case 2:
		return "黄"
	default:
		return "蓝"
	}
}

// TierLabel 档位标签 = 档位名 + 后缀
// 中文 "橙" "警"，英文 "Orange" " Alert"。
func (a *Alert) TierLabel(lang string) string {
	if lang == "zh" {
		return a.TierName("zh") + "警"
	}
	return a.TierName("en") + " Alert"
}

// TierColor 预警四级配色。
func (a *Alert) TierColor() color.NRGBA {
	switch a.Tier() {
	case 4:
		return color.NRGBA{R: 217, G: 38, B: 38, A: 255} // 红
	case 3:
		return color.NRGBA{R: 240, G: 120, B: 24, A: 255} // 橙
	case 2:
		return color.NRGBA{R: 242, G: 187, B: 5, A: 255} // 黄
	default:
		return color.NRGBA{R: 30, G: 111, B: 217, A: 255} // 蓝
	}
}

// windowHoursLabel 文案里的窗口小时数：合法值只有 3 与 6
// （config.alert_window_hours 的合法集）；
// 其它值（含零值）按 3 出文案 = 配置的默认值口径。
func windowHoursLabel(h int) int {
	if h == 6 {
		return 6
	}
	return 3
}

// Text 提醒正文（需求 3、4 的语言口径不变：
// 只有恰好 "zh" 走中文，其余（含空串一律英文）。
//
// 正文内容 = **未来 N 小时滚动窗口**
// 需求：正文取动态窗口，不取一天的最大值；默认 3 小时，可设 6 小时。
//	有毫米数 en：Rain in next 3 h 62% · 0.3 mm expected
//	无毫米数 en：Rain in next 3 h 62%
//	有毫米数 zh：未来 3 小时降雨 62% · 预计 0.3 mm
//	无毫米数 zh：未来 3 小时降雨 62%
//	明日分支：  保留现状不动（最后一句）
//
// 窗口小时数来自 Alert.WindowHours（3 或 6），逐字出现在文案里。
func (a *Alert) Text(lang string) string {
	if a.Tomorrow {
		// 明日分支保留现状（明日分支不动）。
		if lang == "zh" {
			// 注意：不能用 formatTemp —— 它带 °C 后缀（实测渲染出 "预计 2.4°C mm"）。
			if a.MM > 0 {
				return "今日降雨 " + strconv.Itoa(a.Pct) + "% · 预计 " + strconv.FormatFloat(a.MM, 'f', 1, 64) + " mm"
			}
			return "今日降雨 " + strconv.Itoa(a.Pct) + "%"
		}
		if a.MM > 0 {
			return "Rain tomorrow " + strconv.Itoa(a.Pct) + "% · " +
				strconv.FormatFloat(a.MM, 'f', 1, 64) + " mm expected"
		}
		return "Rain tomorrow " + strconv.Itoa(a.Pct) + "%"
	}
	h := strconv.Itoa(windowHoursLabel(a.WindowHours))
	if lang == "zh" {
		if a.MM > 0 {
			return "未来 " + h + " 小时降雨 " + strconv.Itoa(a.Pct) + "% · 预计 " +
				strconv.FormatFloat(a.MM, 'f', 1, 64) + " mm"
		}
		return "未来 " + h + " 小时降雨 " + strconv.Itoa(a.Pct) + "%"
	}
	if a.MM > 0 {
		return "Rain in next " + h + " h " + strconv.Itoa(a.Pct) + "% · " +
			strconv.FormatFloat(a.MM, 'f', 1, 64) + " mm expected"
	}
	return "Rain in next " + h + " h " + strconv.Itoa(a.Pct) + "%"
}

// Line 完整一行 = 档位标签 " · " + 正文。
//
// 拼接口径照出厂行（drawPanelRow / alertRow）：**只有 tier>=3 才带
// 档位前缀**，tier 1–2 只有正文。
// zh 例："橙警 · 未来 3 小时降雨 88% · 预计 2.6 mm"
// en 例："Orange Alert · Rain in next 3 h 88% · 2.6 mm expected"
func (a *Alert) Line(lang string) string {
	txt := a.Text(lang)
	if a.Tier() >= 3 {
		txt = a.TierLabel(lang) + " · " + txt
	}
	return txt
}

// alphaScale 缓慢呼吸：0.75..1.0 的正弦（Pulse 给相位）。
func (a *Alert) alphaScale() float64 {
	if !a.PulseOn {
		return 1
	}
	v := 0.875 + 0.125*math.Sin(a.Pulse*2*math.Pi)
	if v < 0.75 {
		v = 0.75
	}
	if v > 1 {
		v = 1
	}
	return v
}

// fill 按给定 alpha 生成提醒色（乘上脉动系数）。
func (a *Alert) fill(alpha uint8) color.NRGBA {
	t := a.TierColor()
	return color.NRGBA{R: t.R, G: t.G, B: t.B, A: uint8(float64(alpha) * a.alphaScale())}
}

var alertWhite = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
var alertWhiteSoft = color.NRGBA{R: 255, G: 255, B: 255, A: 235}

// DrawAlert 画在最上层（sky → field → clock → weather → **alert**）。
//
// 出厂版式 panelrow 的语义 = **本函数不画任何东西**：提醒行画在天气面板下沿之内，
// 由 DrawAlertPanelRow 出。证据：ui/render.go:309-312 的 LayerAlert 分支 —— 出厂
// panelrow 一直走 DrawAlert3（现名 DrawAlertPanelRow，绘制代码逐字未动），
// 从不走 DrawAlert。
func DrawAlert(gtx layout.Context, size image.Point, a *Alert, s *Screen, f Frame) {
	// 出厂版式不在这里画（见上）。选型期的通栏/卡片/竖条/大字/融合分支已删。
	return
}

// rectFill 实心矩形。
func rectFill(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	if r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y {
		return
	}
	cl := clip.Rect(r).Push(gtx.Ops)
	paint.Fill(gtx.Ops, c)
	cl.Pop()
}

// rrectFill 圆角矩形。
func rrectFill(gtx layout.Context, r image.Rectangle, rad int, c color.NRGBA) {
	if r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y {
		return
	}
	rr := gtx.Dp(unit.Dp(rad))
	outer := clip.RRect{Rect: r, SE: rr, SW: rr, NW: rr, NE: rr}.Push(gtx.Ops)
	paint.Fill(gtx.Ops, c)
	outer.Pop()
}

// drawInPanel 天气面板内加一行（贴在面板下沿之内）。
func drawInPanel(gtx layout.Context, size image.Point, a *Alert, s *Screen, f Frame) {
	px := shiftY(WeatherRect.Px(size.X, size.Y), f.WeatherYOff)
	band := 74
	r := image.Rect(px.X-20, px.Y+px.H-band-14, px.X+px.W+20, px.Y+px.H-14)
	rrectFill(gtx, r, 14, a.fill(215))
	inPx(gtx, PxRect{X: r.Min.X, Y: r.Min.Y, W: r.Dx(), H: r.Dy()}, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceEvenly}.Layout(gtx,
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(s.label(a.TierLabel(a.Lang), unit.Sp(26), alertWhiteSoft, text.Start)),
			layout.Flexed(1, s.label(a.Text(a.Lang), unit.Sp(28), alertWhite, text.Middle)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
		)
	})
}

// measureText 用一次性 op 缓冲区量文字尺寸（gioui 的字形缓存是线程级的，重复布局很便宜）。
func measureText(gtx layout.Context, w layout.Widget) layout.Dimensions {
	var tmp op.Ops
	g := gtx
	g.Ops = &tmp
	// 不用 layout.Infinite（本机 mod 缓存未解包 constraints.go，编译不到） ⇒ 给足够大的有限约束
	g.Constraints = layout.Constraints{Min: image.Point{}, Max: image.Point{X: 100000, Y: 100000}}
	return w(g)
}

// softGlow 分阶叠加的柔和光晕：n 层圆角矩形，每层 alpha 相同 ⇒ 
// 中心 ≈ n×alpha、边缘 ≈ 1×alpha，圆角让边缘不出现硬边。
func softGlow(gtx layout.Context, r image.Rectangle, c color.NRGBA, rad int, n int, per uint8, scale float64) {
	for i := 0; i < n; i++ {
		inset := i * 6
		rr := image.Rect(r.Min.X+inset, r.Min.Y+inset, r.Max.X-inset, r.Max.Y-inset)
		a := float64(per) * (1.0 - float64(i)/float64(n*2)) * scale
		if a < 0 {
			a = 0
		}
		if a > 255 {
			a = 255
		}
		rrectFill(gtx, rr, rad+i*4, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(a)})
	}
}

type glowCtx struct {
	TierColor   color.NRGBA
	alphaScaleF float64
}

// alertRow 天气面板内的提醒行（融合版）。
//
// 低性能 x86 小主机反馈：色块太大、没色块又不显眼、六款"视觉上都不太平衡"。
// 根因实测：提醒画在 0.86h，天气块底在 0.822h ⇒ 中间空 100+ px，提醒像浮在空区里。
// ⇒ 做成面板 flex 里的一行，与地名/条件/明细/预报同组，颜色融合进文字。
func (s *Screen) alertRow(f Frame) layout.Widget {
	a := f.Alert
	if a == nil || a.Pct < AlertThresholdPct {
		return func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
	}
	tier := a.TierColor()
	as := a.alphaScale()
	txt := a.Line(a.Lang)
	return func(gtx layout.Context) layout.Dimensions {
		lbl := s.label(txt, unit.Sp(WeatherFontSize), tier, text.Middle)
		d := measureText(gtx, lbl)
		tw, th := d.Size.X, d.Size.Y
		if tw <= 0 || th <= 0 {
			return layout.Dimensions{}
		}
		// 行尺寸固定 = 文字高 + 上下留白；光晕与文字**都按这个行盒居中**
		// （首版用 Stack 的 Expanded 画光晕 + Rigid 画文字，两者定位基准不同
		// ⇒ 实测文字贴左、光晕在行中心，中间还多出一块空光斑
		padX, padY := 26, 12
		rowW := gtx.Constraints.Max.X
		rowH := th + 2*padY
		cx, cy := rowW/2, rowH/2
		softGlow(gtx, image.Rect(cx-tw/2-padX, cy-th/2-padY, cx+tw/2+padX, cy+th/2+padY), tier, 20, 5, 22, as)
		inPx(gtx, PxRect{X: cx - tw/2, Y: cy - th/2, W: tw, H: th}, lbl)
		return layout.Dimensions{Size: image.Point{X: rowW, Y: rowH}}
	}
}

// ── 出厂版式 panelrow 的胶囊：画在天气面板下沿之内，绝对屏幕坐标水平居中 ──
//
// 出厂画面口径（逐字保持，一个像素都不动）：
//
//	1. 无光晕。softGlow 是嵌套圆角矩形分阶叠加，暗底盖住内层后，可见的只剩
//	   一圈**向外扩 26 px 的矩形环**，不贴文字 ⇒ 读成偏移的脏影 ⇒ 去掉。
//	2. 左右居中走**绝对屏幕坐标**，不走 flex 行盒：行盒在 flex 内的坐标基准
//	   与绝对绘制对不上（实测 chip 中心 x=766、屏幕中心 540 ⇒ 右偏 226 px）。
//	3. 白字 + 只包住文字的圆角暗底 + 左侧预警色小色块 + 文字下细线。
func drawPanelRow(gtx layout.Context, size image.Point, a *Alert, s *Screen, cy int) {
	tier := a.TierColor()
	as := a.alphaScale()
	txt := a.Line(a.Lang)
	sp := unit.Sp(34)
	lbl := s.label(txt, sp, alertWhite, text.Middle)
	d := measureText(gtx, lbl)
	tw, th := d.Size.X, d.Size.Y
	if tw <= 0 || th <= 0 {
		return
	}
	padX, padY := 26, 14
	bw, bh := tw+2*padX, th+2*padY
	bx := size.X/2 - bw/2 // **绝对屏幕水平居中**
	box := image.Rect(bx, cy-bh/2, bx+bw, cy+bh/2)
	// 无光晕。暗底 + 左侧预警色小色块 + 文字下细线。
	rrectFill(gtx, box, 16, color.NRGBA{R: 12, G: 12, B: 16, A: uint8(170 * as)})
	tk := 14
	rrectFill(gtx, image.Rect(bx+12, cy-tk/2, bx+12+tk, cy+tk/2), 4,
		color.NRGBA{R: tier.R, G: tier.G, B: tier.B, A: uint8(240 * as)})
	ruleH := 4
	rrectFill(gtx, image.Rect(bx+padX, box.Max.Y-11, bx+bw-padX, box.Max.Y-11+ruleH), ruleH/2,
		color.NRGBA{R: tier.R, G: tier.G, B: tier.B, A: uint8(220 * as)})
	inPx(gtx, PxRect{X: bx, Y: cy - bh/2, W: bw, H: bh}, lbl)
}

// DrawAlertPanelRow 画出厂 panelrow 的提醒行：天气面板下沿之内（绝对坐标，水平居中）。
// panelBottom = 天气面板矩形的下边界（像素）；行心 = panelBottom-52+Alert.YOff。
//
// 这就是出厂预警画面的全部绘制代码（原 fusion3 分支，改名 panelrow 胶囊，绘制逐字未动）。
func DrawAlertPanelRow(gtx layout.Context, size image.Point, a *Alert, s *Screen, panelBottom int) {
	if a == nil || a.Pct < AlertThresholdPct {
		return
	}
	drawPanelRow(gtx, size, a, s, panelBottom-52+a.YOff)
}

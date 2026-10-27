package ui

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
)

// 降雨提醒（需求 1）。
// 上游事实：上游**没有任何天气预警/提醒功能**（grep 到的 warning 全是 Steam/音频日志），
// 上游 daily 只取 temperature_2m_max/min + weathercode（open_meteo_provider.py:332）
// ⇒ 本条是需求驱动的新增，不是上游对齐项。
// 阈值与配色按**中国标准**，不是随手拍的数：
// - 中国气象局令第 16 号《气象灾害预警信号发布与传播办法》（2007-06-12 施行）：
// 暴雨预警分蓝/黄/橙/红四级，颜色对应**一般 / 较重 / 严重 / 特别严重**；
// 蓝 = 12 小时内降雨量将达 50 mm 以上，黄 = 6 小时内 50 mm 以上，红 = 3 小时内 100 mm 以上。
// - 中国降水概率分级（11 级，每 10% 一级）：<20% 基本不会发生、
// **30%–50% 有机会出现降水**、60%–70% 可能性较大、>70% 认为有降水发生、80%–90% 可能性非常大。
// ⇒ 触发线取官方"有机会出现降水"下限 **30%**。
// 数据源与判据（定案）：
// 灾害层 = 官方四级**配对**判据（红 R(3h)≥100 / 橙 R(3h)≥50 / 黄 R(6h)≥50 / 蓝 R(12h)≥50）；
// 日常层 = 视野内第一个 ≥ alert_min_mm 的 15 分钟格（锚定型，默认 0.05 mm/15min）；
// 滞回 = 亮 = 达门槛，灭 = 连续 2 个采样周期低于门槛 70%；
// 数据闸门 = 概率条数不足 ⇒ '数据不可用'态。实现：weather.Evaluate（weather/window.go）。
// **窗口不再是可调项**（旧 alert_window_hours 作废，由量级配对决定），
// **概率不再单独决定亮灭**（旧 AlertThresholdPct 触发线口径作废，只进措辞）。

// AlertThresholdPct = 官方"有机会出现降水"下限（30%）。
// 之后**不再作触发线**（亮灭由判据 + 滞回决定），
// 保留 = 配置键 alert_threshold_pct 与设置面板那行的既有口径不动。
const AlertThresholdPct = 30

// AlertState 提醒形态（的四态，本批无①官方信号）。
// 四态互斥、同一时刻只显示一行，优先级 灾害 > 日常 > 不可用。
type AlertState int

const (
	// AlertNone = 不画（视野内无达门槛降水且无灾害、数据可用）。
	AlertNone AlertState = iota
	// AlertDisaster = 态② 自算灾害（官方配对判据命中）。
	AlertDisaster
	// AlertDaily = 态③ 带伞提醒（视野内第一个达门槛的 15 分钟格）。
	AlertDaily
	// AlertUnavailable = 态④ 数据不可用（第 4 步数据闸门）。
	AlertUnavailable
	// AlertOfficial = 态① 官方信号（
	// 中国区 NMC 返回有效信号 ⇒ 优先显示，取不到回退自算层 ②/③）。
	// 追加在枚举末尾 ⇒ 既有三态的取值不动（既有钉死单测不受影响）。
	AlertOfficial
)

// Alert 一条降雨提醒。
type Alert struct {
	// State 四态之一；只有 AlertNone 不画。
	State AlertState
	// Pct = 日常层那一格的降水概率（%）。
	// 口径（第 6 步）：概率只决定**措辞与深浅**，不单独决定亮灭
	// （'概率只进措辞与深浅'）。
	Pct int
	// MM = 灾害层命中那一级窗口的累计预报雨量（mm，第 5 步 R(Δ)）。
	MM float64
	// MMPerH = 日常层那格的折算强度（mm/h = mm × 4，第 6 步）。
	MMPerH float64
	// DisasterHours = 灾害层命中那一级配对的窗口小时数（3/6/12，逐字进文案）。
	DisasterHours int
	// Start = 日常层第一个达门槛格子的起始时刻（当地墙钟，逐字进文案 "22:15 起"）。
	Start time.Time
	// AsOf = 数据不可用文案的"截至"时刻（当地墙钟，逐字进文案 "截至 21:11"）。
	AsOf time.Time
	// TierVal 灾害层档位：1=蓝 2=黄 3=橙 4=红（第 5 步判据给，取满足的最高级）。
	TierVal int

	// Lang 文案语言：取值 "en"/"zh"，
	// **只有恰好 "zh" 走中文，其余一律英文** —— 与 cmd/scr/main.go 的 displayLang
	// 口径一致（同文件）。由 buildAlert 从配置传进来。
	Lang string
	// Pulse 0..1 脉动相位；PulseOn 是否做缓慢呼吸。
	Pulse   float64
	PulseOn bool
	// YOff 提醒行的垂直偏移（像素，可调，与 ClockYOff / WeatherYOff 同族）。
	YOff int

	// ---- ： 态① 官方信号 ----
	// 文案逐字（第 6 步）：
	// zh `暴雨 黄色预警 · 22:44 发布` en `Rainstorm Yellow · issued 22:44`
	OfficialTypeZh string // 中文类型名（标题正则取）
	OfficialTypeEn string // 英文类型名
	OfficialLevel  int    // 官方等级 1=蓝 2=黄 3=橙 4=红（Tier 由它给，设计口径）
	OfficialIssued time.Time
}

// Tier 档位：1=蓝 2=黄 3=橙 4=红。
// **由灾害层给**。
// 日常层 = 态③ 的"蓝色小色块" ⇒ 恒 1；不可用态不参与配色分级（走灰）。
func (a *Alert) Tier() int {
	switch a.State {
	case AlertDisaster:
		if a.TierVal >= 1 && a.TierVal <= 4 {
			return a.TierVal
		}
		return 1
	case AlertOfficial:
		// 态① 的档位**由官方等级给**。
		if a.OfficialLevel >= 1 && a.OfficialLevel <= 4 {
			return a.OfficialLevel
		}
		return 1
	default:
		return 1
	}
}

// TierName 四级名。
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

// TierColor 预警四级配色；不可用态走灰（态④'灰，恒定'）。
func (a *Alert) TierColor() color.NRGBA {
	if a.State == AlertUnavailable {
		return color.NRGBA{R: 128, G: 128, B: 132, A: 255} // 灰
	}
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

// fmtMM 毫米数出文案：1 位小数，整数位省掉 ".0"
// 。
func fmtMM(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if strings.HasSuffix(s, ".0") {
		return s[:len(s)-2]
	}
	return s
}

// ProbWord 官方降水概率分级词（逐字口径，:1663-1665 引中国气象局）。
//
//	<20 基本不会 / 30–50 有机会 / 60–70 可能性较大 / >70 认为有降水 / 80–90 可能性非常大
//
// 官方**未定义** 20–30 与 50–60 两档 ⇒ 本批自定为'可能性很小'/'可能性中等'
// 。
// 80–90 是官方给的更窄档 ⇒ 覆盖 ">70 认为有降水"；>90 回到"认为有降水"。
func ProbWord(pct int) string {
	switch {
	case pct < 20:
		return "基本不会"
	case pct < 30:
		return "可能性很小" // 自定：官方 11 级里 20–30 档未定义
	case pct <= 50:
		return "有机会"
	case pct < 60:
		return "可能性中等" // 自定：官方 11 级里 50–60 档未定义
	case pct <= 70:
		return "可能性较大"
	case pct < 80:
		return "认为有降水"
	case pct <= 90:
		return "可能性非常大"
	default:
		return "认为有降水"
	}
}

// officialLevelZh 官方等级名（1=蓝 2=黄 3=橙 4=红），逐字对应 的颜色词。
func officialLevelZh(level int) string {
	switch level {
	case 4:
		return "红色"
	case 3:
		return "橙色"
	case 2:
		return "黄色"
	default:
		return "蓝色"
	}
}

// officialLevelEn 官方等级英文名（态① 文案 `Rainstorm Yellow` 的那一段）。
func officialLevelEn(level int) string {
	switch level {
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

// Visible 报告这一行画不画。
// 旧口径 `Pct < AlertThresholdPct ⇒ 不画` 作废：第 6/11 步定案
// **概率不单独决定亮灭**，亮灭由判据 + 滞回（第 7 步）决定 ⇒ 改由 State 决定。
func (a *Alert) Visible() bool { return a != nil && a.State != AlertNone }

// Text 提醒正文（第 9 步三态文案，逐字照规格样例）：
//
//	日常 zh `22:15 起 有雨 · 有机会 45% · 0.6 mm/h` en `Rain from 22:15 · 45% likely · 0.6 mm/h`
//	灾害 zh `暴雨风险 蓝 · 12h 内 62 mm` en `Rainstorm risk Blue · 62 mm within 12 h`
//	不可用 zh `预警数据不可用 · 截至 21:11` en `Alert data unavailable · as of 21:11`
//
// 语言口径不变：只有恰好 "zh" 走中文，其余（含空串）一律英文。
func (a *Alert) Text(lang string) string {
	switch a.State {
	case AlertOfficial:
		// 态① 文案逐字（第 6 步）：
		// zh `暴雨 黄色预警 · 22:44 发布` en `Rainstorm Yellow · issued 22:44`
		t := a.OfficialIssued.Format("15:04")
		if lang == "zh" {
			return a.OfficialTypeZh + " " + officialLevelZh(a.OfficialLevel) + "预警 · " + t + " 发布"
		}
		return a.OfficialTypeEn + " " + officialLevelEn(a.OfficialLevel) + " · issued " + t
	case AlertDisaster:
		h := strconv.Itoa(a.DisasterHours)
		if lang == "zh" {
			return "暴雨风险 " + a.TierName("zh") + " · " + h + "h 内 " + fmtMM(a.MM) + " mm"
		}
		return "Rainstorm risk " + a.TierName("en") + " · " + fmtMM(a.MM) + " mm within " + h + " h"
	case AlertUnavailable:
		if lang == "zh" {
			return "预警数据不可用 · 截至 " + a.AsOf.Format("15:04")
		}
		return "Alert data unavailable · as of " + a.AsOf.Format("15:04")
	case AlertDaily:
		t := a.Start.Format("15:04")
		mh := strconv.FormatFloat(a.MMPerH, 'f', 1, 64)
		if lang == "zh" {
			return t + " 起 有雨 · " + ProbWord(a.Pct) + " " + strconv.Itoa(a.Pct) + "% · " + mh + " mm/h"
		}
		return "Rain from " + t + " · " + strconv.Itoa(a.Pct) + "% likely · " + mh + " mm/h"
	}
	return ""
}

// Line 完整一行。
// 旧口径"tier>=3 才带档位前缀"作废：灾害态正文自带档位名（`暴雨风险 蓝 · …`），
// 日常态 = 蓝色不带前缀，不可用态无前缀 ⇒ 三态都直接出正文。
func (a *Alert) Line(lang string) string { return a.Text(lang) }

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
// 出厂版式 panelrow 的语义 = **本函数不画任何东西**：提醒行画在天气面板下沿之内，
// 由 DrawAlertPanelRow 出。证据：ui/render.go:309-312 的 LayerAlert 分支 —— 出厂
// panelrow 一直走 DrawAlert3（现名 DrawAlertPanelRow，绘制代码逐字未动），
// 从不走 DrawAlert。
func DrawAlert(gtx layout.Context, size image.Point, a *Alert, s *Screen, f Frame) {
	// 出厂版式不在这里画（见上）。选型期的通栏//竖条/大字/融合分支已删。
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
// 低性能 x86 实机反馈：色块太大、没色块又不显眼、六款"视觉上都不太平衡"。
// 根因实测：提醒画在 0.86h，天气块底在 0.822h ⇒ 中间空 100+ px，提醒像浮在空区里。
// ⇒ 做成面板 flex 里的一行，与地名/条件/明细/预报同组，颜色融合进文字。
func (s *Screen) alertRow(f Frame) layout.Widget {
	a := f.Alert
	if !a.Visible() {
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
// 出厂画面口径（逐字保持，一个像素都不动）：
// 1. 无光晕。softGlow 是嵌套圆角矩形分阶叠加，暗底盖住内层后，可见的只剩
// 一圈**向外扩 26 px 的矩形环**，不贴文字 ⇒ 读成偏移的脏影 ⇒ 去掉。
// 2. 左右居中走**绝对屏幕坐标**，不走 flex 行盒：行盒在 flex 内的坐标基准
// 与绝对绘制对不上（实测 chip 中心 x=766、屏幕中心 540 ⇒ 右偏 226 px）。
// 3. 白字 + 只包住文字的圆角暗底 + 左侧预警色小色块 + 文字下细线。
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
// 这就是出厂预警画面的全部绘制代码（原 fusion3 分支，改名 panelrow 胶囊，绘制逐字未动）。
// 例外（定案）：**态④'数据不可用'不占整行胶囊**
// —— 它不是预警，是盲区提示 ⇒ 改画左下角小信号（`drawCornerBadge`，ping 风格）。
// 态①②③ 的胶囊版式逐字不动。
func DrawAlertPanelRow(gtx layout.Context, size image.Point, a *Alert, s *Screen, panelBottom int) {
	if !a.Visible() {
		return
	}
	if !usesPanelRow(a) {
		drawCornerBadge(gtx, size, a, s)
		return
	}
	drawPanelRow(gtx, size, a, s, panelBottom-52+a.YOff)
}

// ── 态④ 左下角小信号──
// 版式语言取自游戏里的网络异常/ping 高指示：三段递高信号条 + 极短文字 + 极小圆角暗底。
// 与胶囊行的差别（逐条对应"简易标识"这条需求）：
//
//	无细下划线、无光晕、不呼吸（恒定）、不居中、不随 YOff 动、字号降到 Sp(18)。
const (
	badgeMarginX = 28 // 左边距（px）
	badgeMarginY = 28 // 底边距（px）
	badgeBarW    = 7  // 信号条宽
	badgeBarGap  = 3  // 信号条间距
	badgeBarH3   = 14 // 最高一格的高（三格 = 6/10/14，底对齐）
	badgePadX    = 10 // 盒内左右留白
	badgePadY    = 7  // 盒内上下留白
	badgeSp      = 18 // 文字字号
)

// usesPanelRow = 这个态走不走出厂胶囊行。态④ = false（走左下角小信号）。
func usesPanelRow(a *Alert) bool { return a != nil && a.State != AlertUnavailable }

// BadgeText = 小信号文字。语言口径与 Text 逐字一致：只有恰好 "zh" 走中文，其余一律英文。
func BadgeText(lang string) string {
	if lang == "zh" {
		return "无数据"
	}
	return "No data"
}

// badgeRect = 小信号盒（左下角固定边距）。退化入参返回零矩形（调用方必须自查 Dx/Dy）。
// 钳制（钉死单测）：盒不越出视口 ⇒ 小窗口/超大盒都不画到屏幕外。
func badgeRect(size image.Point, w, h int) image.Rectangle {
	if size.X <= 0 || size.Y <= 0 || w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	minX, maxX := badgeMarginX, badgeMarginX+w
	if maxX > size.X {
		maxX = size.X
	}
	minY, maxY := size.Y-badgeMarginY-h, size.Y-badgeMarginY
	if minY < 0 {
		minY = 0
		maxY = h
		if maxY > size.Y {
			maxY = size.Y
		}
	}
	if maxX <= minX || maxY <= minY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// drawCornerBadge 画左下角小信号。灰 {128,128,132} 恒定（与态④ 色块同色），
// 左格亮（A240）、右两格暗（A110）⇒ 读作"信号只有一格"= 取数异常。
func drawCornerBadge(gtx layout.Context, size image.Point, a *Alert, s *Screen) {
	lbl := s.label(BadgeText(a.Lang), unit.Sp(badgeSp), alertWhite, text.Middle)
	d := measureText(gtx, lbl)
	tw, th := d.Size.X, d.Size.Y
	if tw <= 0 || th <= 0 {
		return
	}
	barsW := 3*badgeBarW + 2*badgeBarGap
	bw := badgePadX + barsW + 8 + tw + badgePadX
	bh := max(th, badgeBarH3) + 2*badgePadY
	box := badgeRect(size, bw, bh)
	if box.Dx() <= 0 || box.Dy() <= 0 {
		return
	}
	rrectFill(gtx, box, 10, color.NRGBA{R: 12, G: 12, B: 16, A: 120})
	heights := [3]int{6, 10, badgeBarH3}
	x0 := box.Min.X + badgePadX
	yb := box.Min.Y + (box.Dy()-badgeBarH3)/2 + badgeBarH3 // 底对齐基线
	for i := 0; i < 3; i++ {
		al := uint8(110)
		if i == 0 {
			al = 240
		}
		x := x0 + i*(badgeBarW+badgeBarGap)
		rrectFill(gtx, image.Rect(x, yb-heights[i], x+badgeBarW, yb), 2,
			color.NRGBA{R: 128, G: 128, B: 132, A: al})
	}
	inPx(gtx, PxRect{X: x0 + barsW + 8, Y: box.Min.Y + (box.Dy()-th)/2, W: tw, H: th}, lbl)
}

package settings

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/geo"

	// R12：撤销栈的数据结构（证据 R12 / 旧 ui/settings_ui.go:136 history []Model）。
	"github.com/zyedidia/generic/stack"
)

// 设置面板。
//
// 面板项 = 上游 'weather' 块列出的全部项；键名逐字照抄
// core/settings/default_settings.py 'weather' 块 :1523-1540。
// （'input' 块 :27 'interaction_mode'）。
//
// 热键语义抄上游 rendering/quick/input_controller.py:205-232：
// Enter → custom_layout_save_requested（:222-226）、
// Esc → custom_layout_cancel_requested（:227-229）、
// Ctrl+Z → custom_layout_undo_requested（:207-214）、
// L → custom_layout_lock_requested（:215-221）。
//
// 为什么本文件不 import config：config 已 import ui（config.go 用
// ui.FontFamily，ui 再 import config 会成环。面板只持有 Model
// （字段与 config 的 JSON 键一一对应），读写与写盘由 cmd/scr 侧完成
// （复用 config.Config.Save）。

// Model 是设置面板的编辑模型。字段与 config.Config 的 JSON 键一一对应。
type Model struct {
	Location            string  // location
	FontSize            int     // font_size
	IconSize            int     // icon_size
	Margin              int     // margin
	Units               string  // units："c" 公制 / "i" 英制
	RefreshMinutes      int     // refresh_minutes
	ShowBackground      bool    // show_background
	BgOpacity           float64 // bg_opacity（天气块面板底，上游 0.3）
	ClockBgOpacity      float64 // clock_bg_opacity（时钟块面板底，默认 0.45；偏离上游，见 render.go PanelStyle.ClockBGOpacity）
	ShowConditionIcon   bool    // show_condition_icon
	ShowDetailsRow      bool    // show_details_row
	ShowForecast        bool    // show_forecast
	ShowFiveDayForecast bool    // show_five_day_forecast
	EffectsOn           bool    // effect.enabled 非空 = 开
	WallpaperDir        string  // wallpaper_dir（上游 sources.folders 语义）
	WallpaperIntervalS  int     // wallpaper_interval_s（上游 timing.interval 语义，秒）
	ClockYOffset        int     // clock_y_offset（TODO(未取证)：上游无同名标量键）
	WeatherYOffset      int     // weather_y_offset（同上）
	InteractionMode     bool    // interaction_mode

	// 降雨提醒（上游无预警功能 ⇒ 本组键是需求驱动的新增）
	AlertEnabled      bool // alert_enabled
	AlertThresholdPct int  // alert_threshold_pct（默认 30 = 官方"有机会出现降水"下限）
	AlertYOffset      int  // alert_y_offset（提醒条垂直偏移 px，默认 -60 贴紧天气文字）
	AlertPulse        bool // alert_pulse（缓慢呼吸；关掉则恒定）
	// 预警视野旋钮已在 b-alert-v1b 第 3 步从面板拆掉（视野 = 常量 2 h，
	// 窗口由官方四级配对决定）⇒ 面板模型里不再有视野字段。
	// AlertMinMM = 淋湿门槛（mm/15min，合法 0.02–0.5，默认 0.05）。
	AlertMinMM float64 // alert_min_mm

	// 位置：存坐标 + 搜索选点（定案）
	LocationLat     float64 // location_lat（0,0 = 未存 ⇒ 回落按名地理编码）
	LocationLon     float64 // location_lon
	LocationNameZh  string  // location_name_zh（搜索时存下的中文名）
	LocationNameEn  string  // location_name_en
	DisplayLanguage string  // display_language："en" 默认（当前样式不改）/ "zh"
}

// settingsKeyTag 是面板抢焦点用的 tag。
type settingsKeyTag struct{}

// Panel 是设置面板的控件集合。控件全部用 gioui material。
type Panel struct {
	m Model

	edLocation  widget.Editor
	edWallpaper widget.Editor
	edFontSize  widget.Editor
	edIconSize  widget.Editor
	edMargin    widget.Editor
	edRefresh   widget.Editor
	edInterval  widget.Editor
	edClockY    widget.Editor
	edWeatherY  widget.Editor
	edAlertTh   widget.Editor
	edAlertY    widget.Editor
	edAlertMin  widget.Editor
	edLocSearch widget.Editor

	cbShowBg          widget.Bool
	cbConditionIcon   widget.Bool
	cbDetailsRow      widget.Bool
	cbForecast        widget.Bool
	cbFiveDay         widget.Bool
	cbEffects         widget.Bool
	cbInteractionMode widget.Bool
	cbAlert           widget.Bool
	cbAlertPulse      widget.Bool
	btnLocSearch      widget.Clickable

	// 地名搜索候选：点"搜索"→ 后台请求 → 回填候选 → 点候选写入坐标
	locPlaces []geo.Place
	locClicks []widget.Clickable
	locStatus string
	// locMismatch 非空 = 需要在「已选：」行下方出红字提示（裁决 C）。
	locMismatch string

	slBgOpacity      widget.Float
	slClockBgOpacity widget.Float
	units            widget.Enum
	langSel          widget.Enum // display_language：en / zh

	btnSave   widget.Clickable
	btnCancel widget.Clickable
	btnUndo   widget.Clickable
	btnLock   widget.Clickable

	list widget.List
	// listRight 是横屏双栏的**右栏**列表（B5，spec 判据 2）。
	// 竖屏单栏只走 list，本字段不参与竖屏路径。
	listRight widget.List

	kt settingsKeyTag

	// history 是 Ctrl+Z 撤销栈（R12：github.com/zyedidia/generic/stack 平替旧切片
	// `history []Model`，证据 R12 / 旧 ui/settings_ui.go:136）。
	// 栈顶 = 最近一次改动前的快照；泛型零值可直接用（Push/Pop/Size 不依赖构造器）。
	history  stack.Stack[Model]
	locked   bool
	changed  bool
	status   string
	lastSave error
}

// NewPanel 用当前配置建面板。
func NewPanel(m Model) *Panel {
	p := &Panel{m: m}
	// 列表主轴 = 竖排（cmd/setshot 离屏量化定案）。
	//
	// widget.List 的零值 Axis 是 Horizontal：gioui v0.10.3 layout/layout.go:69-70
	// 把 Horizontal 排成 iota 的第一项（0），Vertical 是 1，
	// 所以 `list widget.List` 这个零值字段是一条**横排**列表。
	// 26 行沿窗口宽度方向排开、总长约 10600 px，视口只有窗口宽度：
	// 640×1100 首屏只有第 1 行（标题）落在视口内
	// （cmd/setshot 实测 bands=1/26 行、内容包围盒 x=[9,466]、
	// 滚动条是贴底 6 px 高、沿 X 走的横条）。
	// 本文件对 rows 的口径是"竖屏 1080×1920 上全部行放进可滚动的
	// material.List"⇒ 主轴取 Vertical，行沿窗口高度方向排。
	//
	// Position 显式置零：打开面板时列表停在内容起始处。
	p.list.Axis = layout.Vertical
	p.list.Position = layout.Position{}
	// 横屏右栏列表与左栏同一口径（主轴竖排、打开时停在内容起始处）：
	// 判据 2 要求两栏主轴都是 layout.Vertical。
	p.listRight.Axis = layout.Vertical
	p.listRight.Position = layout.Position{}
	p.edLocation.SingleLine = true
	p.edWallpaper.SingleLine = true
	for _, ed := range []*widget.Editor{&p.edFontSize, &p.edIconSize, &p.edMargin,
		&p.edRefresh, &p.edInterval, &p.edClockY, &p.edWeatherY,
		&p.edAlertTh, &p.edAlertY, &p.edAlertMin, &p.edLocSearch} {
		ed.SingleLine = true
		ed.InputHint = key.HintNumeric
	}
	p.setModel(m)
	p.status = "Enter 保存 · Esc 取消 · Ctrl+Z 撤销 · L 锁定"
	return p
}

// Model 返回面板当前值（编辑器文本已解析成数值）。
func (p *Panel) Model() Model { return p.m }

// Changed 报告上一帧是否有改动（改完即写盘）。
func (p *Panel) Changed() bool { return p.changed }

// Locked 报告锁定态（L）。
func (p *Panel) Locked() bool { return p.locked }

// SaveError 返回最近一次写盘错误（由调用方在保存后回填）。
func (p *Panel) SaveError() error { return p.lastSave }

// SetSaveError 记录写盘结果并显示在状态行。
func (p *Panel) SetSaveError(err error) {
	p.lastSave = err
	if err != nil {
		p.status = "写盘失败: " + err.Error()
		return
	}
	p.status = "已写盘 " + p.m.Location
}

func intOr(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func floatOr(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

// model 从控件读回一份模型；数值解析失败时保留旧值（不把面板写坏）。
func (p *Panel) model() Model {
	m := p.m
	m.Location = p.edLocation.Text()
	m.WallpaperDir = p.edWallpaper.Text()
	m.FontSize = intOr(p.edFontSize.Text(), m.FontSize)
	m.IconSize = intOr(p.edIconSize.Text(), m.IconSize)
	m.Margin = intOr(p.edMargin.Text(), m.Margin)
	m.RefreshMinutes = intOr(p.edRefresh.Text(), m.RefreshMinutes)
	m.WallpaperIntervalS = intOr(p.edInterval.Text(), m.WallpaperIntervalS)
	m.ClockYOffset = intOr(p.edClockY.Text(), m.ClockYOffset)
	m.WeatherYOffset = intOr(p.edWeatherY.Text(), m.WeatherYOffset)
	m.AlertThresholdPct = intOr(p.edAlertTh.Text(), m.AlertThresholdPct)
	m.AlertYOffset = intOr(p.edAlertY.Text(), m.AlertYOffset)
	if p.units.Value == "i" {
		m.Units = "i"
	} else {
		m.Units = "c"
	}
	m.ShowBackground = p.cbShowBg.Value
	// 滑条值经 float32 往返会把 0.3 变成 0.30000001192092896；量化到 0.01
	// 保证面板 → config → 面板 的往返稳定（也避免"无改动帧"被误判成改动）。
	m.BgOpacity = math.Round(float64(p.slBgOpacity.Value)*100) / 100
	m.ClockBgOpacity = math.Round(float64(p.slClockBgOpacity.Value)*100) / 100
	m.ShowConditionIcon = p.cbConditionIcon.Value
	m.ShowDetailsRow = p.cbDetailsRow.Value
	m.ShowForecast = p.cbForecast.Value
	m.ShowFiveDayForecast = p.cbFiveDay.Value
	m.EffectsOn = p.cbEffects.Value
	m.InteractionMode = p.cbInteractionMode.Value
	m.AlertEnabled = p.cbAlert.Value
	m.AlertPulse = p.cbAlertPulse.Value
	m.DisplayLanguage = p.langSel.Value
	// alert_min_mm：解析失败保留旧值（与其它数值行同口径，不把面板写坏）。
	m.AlertMinMM = floatOr(p.edAlertMin.Text(), m.AlertMinMM)
	return m
}

// setModel 把模型写回控件（初始化与 Ctrl+Z 撤销用）。
func (p *Panel) setModel(m Model) {
	p.m = m
	p.edLocation.SetText(m.Location)
	p.edWallpaper.SetText(m.WallpaperDir)
	p.edFontSize.SetText(strconv.Itoa(m.FontSize))
	p.edIconSize.SetText(strconv.Itoa(m.IconSize))
	p.edMargin.SetText(strconv.Itoa(m.Margin))
	p.edRefresh.SetText(strconv.Itoa(m.RefreshMinutes))
	p.edInterval.SetText(strconv.Itoa(m.WallpaperIntervalS))
	p.edClockY.SetText(strconv.Itoa(m.ClockYOffset))
	p.edWeatherY.SetText(strconv.Itoa(m.WeatherYOffset))
	p.edAlertTh.SetText(strconv.Itoa(m.AlertThresholdPct))
	p.edAlertY.SetText(strconv.Itoa(m.AlertYOffset))
	p.edAlertMin.SetText(strconv.FormatFloat(m.AlertMinMM, 'f', -1, 64))
	p.units.Value = m.Units
	p.cbShowBg.Value = m.ShowBackground
	p.slBgOpacity.Value = float32(m.BgOpacity)
	p.slClockBgOpacity.Value = float32(m.ClockBgOpacity)
	p.cbConditionIcon.Value = m.ShowConditionIcon
	p.cbDetailsRow.Value = m.ShowDetailsRow
	p.cbForecast.Value = m.ShowForecast
	p.cbFiveDay.Value = m.ShowFiveDayForecast
	p.cbEffects.Value = m.EffectsOn
	p.cbInteractionMode.Value = m.InteractionMode
	p.cbAlert.Value = m.AlertEnabled
	p.cbAlertPulse.Value = m.AlertPulse
	p.langSel.Value = m.DisplayLanguage
	if p.langSel.Value != "zh" {
		p.langSel.Value = "en"
	}
}

// Undo 弹撤销栈（上游 Ctrl+Z → custom_layout_undo_requested，input_controller.py:207-214）。
//
// R12 迁移（证据 R12 / 旧 ui/settings_ui.go:295-306）：弹栈口径逐字不变
// —— 空栈 ⇒ 状态「无可撤销」且返回 false；非空 ⇒ 弹栈顶、setModel、状态「已撤销」、返回 true。
// generic/stack v1.2.1 的 Pop() 只返回值、空栈返回零值（无 ok 标志），
// 所以"空栈"判据继续走 Size()（旧代码的 len(p.history) == 0）。
func (p *Panel) Undo() bool {
	if p.history.Size() == 0 {
		p.status = "无可撤销"
		return false
	}
	prev := p.history.Pop()
	p.setModel(prev)
	p.status = "已撤销"
	return true
}

// ToggleLock 切换锁定态（上游 L → custom_layout_lock_requested，input_controller.py:215-221）。
// 锁定 = 控件全部禁用（用 gtx.Disabled()，material 控件会禁用并置灰）。
func (p *Panel) ToggleLock() {
	p.locked = !p.locked
	if p.locked {
		p.status = "已锁定（L 解锁）"
	} else {
		p.status = "已解锁"
	}
}

// HandleKeys 处理面板热键，返回 "save"/"cancel"/"undo"/"lock"/""。
// 必须在 Layout 之前调用（先注册过滤器，本帧生效）。
//
// Esc 用无 Focus 的过滤器：编辑器有焦点时也能取消（上游 Esc 语义）。
// Enter 带 Focus：焦点在编辑器里时交给编辑器自身（数字输入回车 = 提交该字段）。
func (p *Panel) HandleKeys(gtx layout.Context) string {
	event.Op(gtx.Ops, &p.kt)
	gtx.Execute(key.FocusCmd{Tag: &p.kt})

	drain := func(f key.Filter) bool {
		for {
			ev, ok := gtx.Event(f)
			if !ok {
				return false
			}
			if ke, isK := ev.(key.Event); isK && ke.State == key.Press {
				return true
			}
		}
	}
	// 顺序即优先级：先 Esc/Ctrl+Z/L，再 Enter（带 Focus）。
	if drain(key.Filter{Name: key.NameEscape}) {
		return "cancel"
	}
	if drain(key.Filter{Name: "Z", Required: key.ModCtrl}) {
		return "undo"
	}
	if drain(key.Filter{Name: "L"}) {
		return "lock"
	}
	if drain(key.Filter{Focus: &p.kt, Name: key.NameReturn}) {
		return "save"
	}
	return ""
}

// panelLandMinWpx 横屏双栏的**最小宽度门槛**（px）。
//
// spec/b5-settings-landscape.md 判据口径：「双栏判定 = gtx 约束宽>高 且 宽≥1200」。
// 为什么要有宽度下限：双栏要把 17 行的右栏放进半宽 ≈ 960 px 的列里，
// 1200 px 以下（例：810×1389、1080×1920 这类竖屏比例）拆栏会把行挤成折行，
// 正是 b2 取证要挡的"逐字竖排"症状 ⇒ 窄窗口一律走竖屏单列。
const panelLandMinWpx = 1200

// IsLandscape 报告"宽 w × 高 h 的这块位置按**设置页双栏**排"= 宽>高 且 宽 ≥ 1200 px。
//
// 与 ui/layout.go:67 的 IsLandscape（屏保版式用，判据 = 纯 w>h）是两回事：
// 本函数多一道宽度下限，因为双栏的每栏只有半宽。屏保侧继续用 IsLandscape，
// 本函数只服务设置页（硬纪律「不改 ui/layout.go」⇒ 不合并两者）。
//
// 唯一口径（spec 判据 5「runSettings 允许引入 IsLandscape(宽,高) 判定」+
// 判据口径「双栏判定 = gtx 约束宽>高 且 宽≥1200」）：
//   - 面板侧在 panel.Layout 里用它判本帧 gtx 约束（不查系统旋转 API）；
//   - 窗口侧 cmd/scr 用它判主显示器工作区宽高（foreground_windows.go 的
//     SPI_GETWORKAREA 同款查询，导出为 workAreaIsLandscape）。
func IsLandscape(w, h int) bool {
	return w > h && w >= panelLandMinWpx
}

// Layout 画面板并同步控件状态。返回 true 表示本帧有改动（调用方写盘）。
func (p *Panel) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	before := p.m

	// 锁定态：控件全部禁用（gtx.Disabled() 不投递事件），
	// 键盘热键仍由 HandleKeys 在原始 gtx 上处理。
	wgtx := gtx
	if p.locked {
		wgtx = gtx.Disabled()
	}

	// 面板底色：gioui 的 Windows 后端每帧把窗口清成不透明白
	// （gioui.org v0.10.3 app/window.go:216），面板自身不画底 ⇒ 深色模式下
	// 浅字会落在白底上（等于没跟随）。这里按 palette.Bg 铺一层，
	// 配色才真正落到画面上。约束无界时跳过（不画越界矩形）。
	if max := gtx.Constraints.Max; max.X > 0 && max.Y > 0 {
		paint.FillShape(gtx.Ops, th.Palette.Bg, clip.Rect{Max: max}.Op())
	}
	// 朝向判定 = **本帧 gtx 的约束宽高**（spec 判据 5 与开放项处置：
	// 「横屏判定=传入 gtx 宽高（不查系统旋转）」）⇒ 面板被放进多大的框
	// 就长成多大，cmd/setshot 的离屏量化与真实窗口走同一条判据。
	land := IsLandscape(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
	var dims layout.Dimensions
	if !land {
		// 竖屏：现行单列，逐字不变（判据 4「竖屏零迁移」）。
		dims = material.List(th, &p.list).LayoutWidgets(wgtx, p.rows(th)...)
	} else {
		// 横屏：长方形双栏（判据 2）——外层 Flex{Horizontal} + 两个
		// material.List（各占一半宽度，主轴都是 layout.Vertical）。
		// 两栏的行取自与单栏同一份 rowSpecs ⇒ 行数守恒（判据 1）。
		//
		// 栏宽由 Flex 分配，所以每栏的 List 必须用**子 gtx**（Flex 交给它的
		// 960 px 约束）来排：把父 gtx 直接递进去，左栏会按 1920 px 排掉整条
		// 主轴，右栏剩下的空间被夹成 0 ⇒ 右栏整列不落地（实测：出图右半全空、
		// 只剩右栏滚动条）。锁定态同样在子 gtx 上取 gtx.Disabled()。
		left, right := p.rowsSplit(th)
		col := func(list *widget.List, rows []layout.Widget) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				cgtx := gtx
				if p.locked {
					cgtx = gtx.Disabled()
				}
				return material.List(th, list).LayoutWidgets(cgtx, rows...)
			}
		}
		dims = layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Flexed(1, col(&p.list, left)),
			layout.Flexed(1, col(&p.listRight, right)),
		)
	}

	// 按钮点击（点击后由调用方执行动作）
	p.changed = false
	p.m = p.model()
	if p.m != before {
		p.changed = true
		// R12 压栈（证据 R12 / 旧 ui/settings_ui.go:434-436：
		// `append(p.history, before)` 之后 `if len(p.history) > 64 { p.history = p.history[1:] }`
		// ⇒ 上限 64、溢出**丢最旧**，栈顶始终是"最近一次改动前的快照"）。
		// 溢出方向逐字保持：generic/stack v1.2.1 没有"删栈底"API（entries 是私有字段），
		// 这里把整栈弹出（顺序 = 顶→底，buf[n-1] 即最旧）后跳过最旧那份、
		// 按原序压回 ⇒ 得到的栈与旧的 history[1:] 逐项逐字相同。
		p.history.Push(before)
		if n := p.history.Size(); n > 64 {
			buf := make([]Model, n)
			for i := 0; i < n; i++ {
				buf[i] = p.history.Pop()
			}
			for i := n - 2; i >= 0; i-- {
				p.history.Push(buf[i])
			}
		}
	}
	return dims
}

// SaveClicked / CancelClicked / UndoClicked / LockClicked 报告按钮点击。
func (p *Panel) SaveClicked(gtx layout.Context) bool   { return p.btnSave.Clicked(gtx) }
func (p *Panel) CancelClicked(gtx layout.Context) bool { return p.btnCancel.Clicked(gtx) }
func (p *Panel) UndoClicked(gtx layout.Context) bool   { return p.btnUndo.Clicked(gtx) }
func (p *Panel) LockClicked(gtx layout.Context) bool   { return p.btnLock.Clicked(gtx) }

// rowSpecs 面板行的**单一事实源**（索引 = 行号，逐行连续）。
//
// 竖屏单栏走 rows()（= rowSpecs + 候选插入）；横屏双栏走 rowsSplit()
// （= rowSpecs 按切分表分栏 + 候选插左栏）。两条路取的是同一批行对象
// ⇒ 判据 1 的「双栏总行覆盖 = 单栏全行集」由构造保证，不靠人工数。
//
// 索引表（B5 切分表的口径，行号 = 本切片的下标）：
//
//	0 标题  1 状态行  2 location 地名  3 搜索行  4 搜索状态行  5 红字行
//	6 display_language 语言  7 wallpaper_dir  8 font_size/icon_size
//	9 margin/refresh_minutes（更新）  10 wallpaper_interval_s（更新）
//	11 clock_y/weather_y  12 alert_threshold/alert_y  13 alert_min_mm
//	14 units 单位  15 bg_opacity  16 clock_bg_opacity
//	17..25 九个 show_*/动效/interaction_mode/alert 开关  26 按钮行
//
// 注：注释里历史写过"26 行"（b-alert-v1b 拆掉视野小时组之前），
// spec 判据 1 明确「行数实测钉死，注释『26/27』失真以实测为准」⇒ 实测 27。
func (p *Panel) rowSpecs(th *material.Theme) []layout.Widget {
	rows := []layout.Widget{
		p.titleRow(th, "WeatherScr 设置"),
		p.plainRow(th, p.status),
		p.fieldRow(th, "location 地名", &p.edLocation),
		// 地名搜索选点：中文地名上游搜不到
		// language=en，geocode_completer.py:145 ⇒ 中文走 Nominatim、拉丁走 Open-Meteo。
		// 用按钮触发：gioui 的 Editor.Layout 内部把事件抽干，面板里拿不到 SubmitEvent。
		p.locSearchRow(th),
		p.locStatusRow(th),
		p.locMismatchRow(th),
		p.langRow(th),
		p.fieldRow(th, "wallpaper_dir 壁纸目录（sources.folders）", &p.edWallpaper),
		p.row2(th,
			fieldSpec{label: "font_size", ed: &p.edFontSize},
			fieldSpec{label: "icon_size", ed: &p.edIconSize}),
		p.row2(th,
			fieldSpec{label: "margin", ed: &p.edMargin},
			// 单位写明 + 下限写明：面板是自由文本输入，填 0/负数会被夹到 1 分钟
			// （refresher 里的下限，防止把 API 打成轮询）。默认 1 分钟。
			fieldSpec{label: "refresh_minutes 天气刷新(分钟，最小1)", ed: &p.edRefresh}),
		p.row2(th,
			fieldSpec{label: "wallpaper_interval_s 间隔(秒)", ed: &p.edInterval},
			fieldSpec{label: "timing.interval 语义", ed: nil}),
		p.row2(th,
			fieldSpec{label: "clock_y_offset 时钟 y 偏移(px)", ed: &p.edClockY},
			fieldSpec{label: "weather_y_offset 天气 y 偏移(px)", ed: &p.edWeatherY}),
		// 降雨提醒：触发阈值只进措辞（b-alert-v1 第 9 步：概率不决定亮灭）；
		// 偏移 = 提醒条上下位置（与上面同族可调）
		p.row2(th,
			fieldSpec{label: "alert_threshold_pct 措辞阈值(%，默认30)", ed: &p.edAlertTh},
			fieldSpec{label: "alert_y_offset 提醒 y 偏移(px，默认-60)", ed: &p.edAlertY}),
		// 淋湿门槛（mm/15min，默认 0.05，合法 0.02–0.5）
		p.row2(th,
			fieldSpec{label: "alert_min_mm 最小雨量(mm/15min，默认0.05)", ed: &p.edAlertMin},
			fieldSpec{label: "越界回落 0.05", ed: nil}),
		// 「视野小时」单选组已在 b-alert-v1b 第 3 步整组拆掉（视野 = 常量 2 h，
		// 窗口由官方量级配对决定）⇒ 面板少一行，淋湿门槛那行保留。
		p.unitsRow(th),
		p.sliderRow(th, "bg_opacity 天气面板底不透明度（上游 0.3）", &p.slBgOpacity),
		p.sliderRow(th, "clock_bg_opacity 时钟面板底不透明度（偏离上游，默认 0.45）", &p.slClockBgOpacity),
		p.checkRow(th, "show_background 面板底", &p.cbShowBg),
		p.checkRow(th, "show_condition_icon 天气图标", &p.cbConditionIcon),
		p.checkRow(th, "show_details_row 明细行", &p.cbDetailsRow),
		p.checkRow(th, "show_forecast 预报行", &p.cbForecast),
		p.checkRow(th, "show_five_day_forecast 五日预报", &p.cbFiveDay),
		p.checkRow(th, "背景动效（effect.enabled）", &p.cbEffects),
		p.checkRow(th, "interaction_mode（false=一动就退出）", &p.cbInteractionMode),
		p.checkRow(th, "alert_enabled 降雨提醒开关", &p.cbAlert),
		p.checkRow(th, "alert_pulse 提醒缓慢呼吸", &p.cbAlertPulse),
		p.buttonRow(th),
	}
	return rows
}

// rowsSplit 横屏双栏的行切分（spec/b5-settings-landscape.md 判据 2）。
//
// 左栏 = 地区与预报组：标题/状态/地名/搜索行/搜索状态行/红字行/显示语言
//   - 单位（units）+ 更新组（refresh_minutes、wallpaper_interval_s），
//     地名搜索候选插在左栏的「地名组」之后（与竖屏同一插入点：
//     搜索状态行之后、红字行之前）。
//
// 右栏 = 显示开关组 + 按钮行：其余全部行（壁纸目录、font/icon/margin 所在行、
//
//	偏移与提醒行、两条不透明度滑条、九个 checkRow、按钮行）。
//
// 行去向由 panelLandLeftIdx 点名，其余行**全部**落进右栏 ⇒
// 左 + 右 = 单栏全行集（判据 1 的行数守恒）。
func (p *Panel) rowsSplit(th *material.Theme) (left, right []layout.Widget) {
	rows := p.rowSpecs(th)
	taken := make([]bool, len(rows))
	for _, i := range panelLandLeftIdx {
		if i < 0 || i >= len(rows) {
			continue // 行集变化时越界下标跳过（守恒判据由测试钉死，不在运行时发明行为）
		}
		left = append(left, rows[i])
		taken[i] = true
	}
	if cands := p.locCandidateRows(th); len(cands) > 0 && len(left) >= panelLandCandAt {
		out := make([]layout.Widget, 0, len(left)+len(cands))
		out = append(out, left[:panelLandCandAt]...)
		out = append(out, cands...)
		out = append(out, left[panelLandCandAt:]...)
		left = out
	}
	for i := range rows {
		if !taken[i] {
			right = append(right, rows[i])
		}
	}
	return left, right
}

// 横屏切分表（spec 点位「rows() 切分表」）。行号见 rowSpecs 的索引表注释。
//
// 左栏 10 行：地区与预报组（含 units 与两条更新间隔行）；
// 右栏 17 行：显示开关组（9 个 checkRow + 2 条滑条 + 偏移/提醒行 + 壁纸目录
// + font/icon 行）+ 按钮行。
var panelLandLeftIdx = []int{0, 1, 2, 3, 4, 5, 6, 9, 10, 14}

// panelLandCandAt = 左栏里候选的插入位置：地名组 5 行（标题/状态/location/
// 搜索行/搜索状态行）之后，与竖屏 rows() 的 `rows[:5]` 插入点同一口径。
const panelLandCandAt = 5

// rows 面板行（竖屏单列）。竖屏 1080×1920 上全部行放进可滚动的 material.List。
func (p *Panel) rows(th *material.Theme) []layout.Widget {
	rows := p.rowSpecs(th)
	// 候选地名插在搜索行之后（Go 的 `f()...` 只能用在可变参数调用里，
	// 不能出现在复合字面量中 ⇒ 首版写 `p.locCandidateRows(th)...` 直接编译不过。
	if cands := p.locCandidateRows(th); len(cands) > 0 && len(rows) >= 5 {
		out := make([]layout.Widget, 0, len(rows)+len(cands))
		out = append(out, rows[:5]...)
		out = append(out, cands...)
		out = append(out, rows[5:]...)
		rows = out
	}
	return rows
}

func (p *Panel) titleRow(th *material.Theme, txt string) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		l := material.H5(th, txt)
		l.TextSize = fs.title // 标题行比正文大一号（spec §1.2）
		l.Color = th.Palette.Fg
		return layout.UniformInset(fs.titleInset).Layout(gtx, l.Layout)
	}
}

func (p *Panel) plainRow(th *material.Theme, txt string) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(th, txt)
		l.TextSize = fs.text
		l.Color = th.Palette.ContrastFg
		return layout.UniformInset(fs.rowInset).Layout(gtx, l.Layout)
	}
}

// panelCaptionColor 标签/说明文字的取色（B5.1 返工的出字修复点）。
//
// 根因（本批次 headless 真渲染探针，见 out/notes-b5-labels.md）：浅色模式下
// Palette.ContrastFg == Palette.Bg == 纯白（ui/theme.go:147-151 paletteForMode
// 浅色支），而面板每帧用 Bg 铺底（本文件 Layout 里的 paint.FillShape）
// ⇒ 取 ContrastFg 的文字画在白底上：排版尺寸正常（fieldRow 的标签 span 实测
// 258x18，见探针），像素却全白 = "标签消失"。探针 BAND 行：plainRow 标签带、
// fieldRow 标签行、row2 两格标签行的暗像素数全 = 0。
//
// 处置：横屏档标签取 Palette.Fg（浅色=黑、深色=近白；实拍里取 Fg 的
// checkRow / unitsRow / sliderRow 标签本来就出字）。
// 竖屏档逐字保持 ContrastFg —— 判据 4「竖屏零迁移」，竖屏像素一字不动。
// 选档口径与 panelFontForTheme 同一条：正文档 ≥ panelTextSpLand(19sp) = 横屏档。
func panelCaptionColor(th *material.Theme, fs panelFont) color.NRGBA {
	if fs.text >= panelTextSpLand {
		return th.Palette.Fg
	}
	return th.Palette.ContrastFg
}

func (p *Panel) fieldRow(th *material.Theme, label string, ed *widget.Editor) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, label)
					l.TextSize = fs.caption
					l.Color = panelCaptionColor(th, fs)
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					e := material.Editor(th, ed, "")
					// 面板局部字号：这里原先取主题的 th.TextSize，
					// 而它继承自屏保的 WeatherFontSize=40（spec §0 的根因链）
					// ⇒ 换成 fs.text。
					e.TextSize = fs.text
					e.Font.Weight = font.Normal
					return e.Layout(gtx)
				}),
			)
		})
	}
}

type fieldSpec struct {
	label string
	ed    *widget.Editor
}

// row2 两列并排。gioui v0.10.3 没有 layout.Row → 用 layout.Flex{Axis: Horizontal}
// （需求 1：已实测）。
//
// B5.1 返工：横屏档两格走 landFieldUnit（竖排单元：标签在上、编辑器在下），
// 竖屏档分支逐字不动（判据 4 零迁移）。选档 = 字号档 ≥ panelTextSpLand。
func (p *Panel) row2(th *material.Theme, a, b fieldSpec) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	land := fs.text >= panelTextSpLand
	return func(gtx layout.Context) layout.Dimensions {
		cells := []layout.FlexChild{}
		for _, sp := range []fieldSpec{a, b} {
			sp := sp
			cells = append(cells, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if land {
					return p.landFieldUnit(th, fs, sp)(gtx)
				}
				if sp.ed == nil {
					l := material.Caption(th, sp.label)
					l.TextSize = fs.caption
					l.Color = th.Palette.ContrastFg
					return layout.UniformInset(fs.rowInset).Layout(gtx, l.Layout)
				}
				return p.fieldRow(th, sp.label, sp.ed)(gtx)
			}))
		}
		return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{
				Axis:      layout.Horizontal,
				Alignment: layout.Middle,
				Spacing:   layout.SpaceBetween,
			}.Layout(gtx, cells...)
		})
	}
}

// landFieldUnit 横屏档 row2 的**竖排单元**：标签行在上、编辑器在下
// （sp.ed == nil 时只有标签行，语义与竖屏 row2 的 nil 格同款）。
//
// 排版与竖屏 fieldRow 同构（Flex{Vertical} + 同一套字号/行距），差别两处：
//   - 标签取色走 panelCaptionColor（横屏档 = Palette.Fg ⇒ 出字）；
//   - 只服务横屏档：row2 的 land 分支才进这里，竖屏路径一字不经过本函数。
func (p *Panel) landFieldUnit(th *material.Theme, fs panelFont, sp fieldSpec) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, sp.label)
					l.TextSize = fs.caption
					l.Color = panelCaptionColor(th, fs)
					return l.Layout(gtx)
				}),
			}
			if sp.ed != nil {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					e := material.Editor(th, sp.ed, "")
					e.TextSize = fs.text
					e.Font.Weight = font.Normal
					return e.Layout(gtx)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// unitsRow 公制/英制单选（units 键）。
func (p *Panel) unitsRow(th *material.Theme) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, "units 单位")
					l.TextSize = fs.text
					l.Color = th.Palette.Fg
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.RadioButton(th, &p.units, "c", "公制 (c)").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.RadioButton(th, &p.units, "i", "英制 (i)").Layout(gtx)
				}),
			)
		})
	}
}

// sliderRow bg_opacity 滑条。widget.Float 的 Value 在 [0;1]（v0.10.3 实测），
// 与上游 bg_opacity 同口径，直接对接。
func (p *Panel) sliderRow(th *material.Theme, label string, f *widget.Float) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, label)
					l.TextSize = fs.text
					l.Color = th.Palette.Fg
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Slider(th, f).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, formatNum(float64(f.Value)))
					l.TextSize = fs.text
					l.Color = th.Palette.Fg
					return l.Layout(gtx)
				}),
			)
		})
	}
}

func (p *Panel) checkRow(th *material.Theme, label string, b *widget.Bool) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(th, b, label).Layout(gtx)
		})
	}
}

// buttonRow 保存 / 取消 / 撤销 / 锁定（热键的鼠标入口，语义同热键）。
func (p *Panel) buttonRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Button(th, &p.btnSave, "保存 Enter").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Button(th, &p.btnCancel, "取消 Esc").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Button(th, &p.btnUndo, "撤销 Ctrl+Z").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					txt := "锁定 L"
					if p.locked {
						txt = "解锁 L"
					}
					return material.Button(th, &p.btnLock, txt).Layout(gtx)
				}),
			)
		})
	}
}

// ── 地名搜索选点（定案"存坐标 + 搜索选点"）──

// locMismatchText 裁决 C 的红字文案（spec/b-alert-v2.md 第 5 步逐字：
// 「行政区与坐标不一致，已按你的选择保留」）。
const locMismatchText = "行政区与坐标不一致，已按你的选择保留"

// locMismatchColor 裁决 C 的红字颜色（红 = 提示级，不与预警四级共用）。
var locMismatchColor = color.NRGBA{R: 226, G: 61, B: 61, A: 255}

// locSearchRow 搜索输入框 "搜索"按钮。
//
// 为什么是按钮不是回车：gioui v0.10.3 的 widget.Editor.Layout 内部
// 用 `for { _, ok := e.Update(gtx); if !ok {break} }` 把事件抽干
// （widget/editor.go:695-701），面板拿不到 SubmitEvent ⇒ 回车不可用。
// 按钮走 widget.Clickable，与面板里已验证的保存/取消按钮同一套机制。
func (p *Panel) locSearchRow(th *material.Theme) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(th, &p.edLocSearch, "地名（中文：绵阳 / 拉丁：Mianyang）")
					ed.TextSize = fs.text
					return ed.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Button(th, &p.btnLocSearch, "搜索").Layout(gtx)
				}),
			)
		})
	}
}

// locStatusRow 搜索状态行（搜索中 / 无候选 / 已选）。
func (p *Panel) locStatusRow(th *material.Theme) layout.Widget {
	return p.plainRow(th, p.locStatus)
}

// locMismatchRow 裁决 C 的红字行（无提示 ⇒ 零尺寸，不占行）。
func (p *Panel) locMismatchRow(th *material.Theme) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		if p.locMismatch == "" {
			return layout.Dimensions{}
		}
		l := material.Body2(th, p.locMismatch)
		l.TextSize = fs.caption
		l.Color = locMismatchColor
		return layout.UniformInset(fs.rowInset).Layout(gtx, l.Layout)
	}
}

// locCandidateRows 候选地名行（可点）。
func (p *Panel) locCandidateRows(th *material.Theme) []layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	for len(p.locClicks) < len(p.locPlaces) {
		p.locClicks = append(p.locClicks, widget.Clickable{})
	}
	out := make([]layout.Widget, 0, len(p.locPlaces))
	for i := range p.locPlaces {
		i := i
		out = append(out, func(gtx layout.Context) layout.Dimensions {
			// 注意：这里不能把局部变量命名为 p —— 会遮蔽面板接收者 p，
			// 下一行的 p.locPlaces 就变成 widget.Clickable 的字段了（实测编译错）。
			ck := &p.locClicks[i]
			place := p.locPlaces[i]
			return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								// 裁决 A（§13.4）：完整路径**允许换行** = gioui Label 在宽度
								// 受限时自己折行 ⇒ 不截断、不加省略号，控件数不变。
								l := material.Body2(th, place.PathOrLabel())
								l.TextSize = fs.text
								l.Color = th.Palette.Fg
								return l.Layout(gtx)
							}),
							// 裁决 C（§13.4）：搜索侧 admin 与坐标反查侧不一致 ⇒ 红字明说，
							// **保留用户原选择**，不阻断保存、不静默改点。
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if !place.Mismatch {
									return layout.Dimensions{}
								}
								l := material.Body2(th, locMismatchText)
								l.TextSize = fs.caption
								l.Color = locMismatchColor
								return l.Layout(gtx)
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Caption(th, fmt.Sprintf("%.3f,%.3f", place.Lat, place.Lon))
						l.TextSize = fs.caption
						l.Color = th.Palette.ContrastFg
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Button(th, ck, "选它").Layout(gtx)
					}),
				)
			})
		})
	}
	return out
}

// radioOpt 是竖排单选组的一项（key = widget.Enum 的值，label = 显示文本）。
type radioOpt struct {
	key   string
	label string
}

// radioGroupRow 单选组排版：标签独占一行，每个 radio 独占一行（spec §1.3）。
//
// 为什么竖排：横排时 radio 标签在 layout.Flex 的 Rigid 链里被挤到很窄的可用
// 宽度，gioui 的 widget.Label 按可用宽度换行 ⇒ 中文标签逐字竖排
// （"英/文/(/e/n/)"），行高炸开（spec §0 的取证）。竖排后每个 radio 拿到
// 整行宽度；640 px 下不截断由 ui/panel_radio_b2_test.go 量化断言。
func (p *Panel) radioGroupRow(th *material.Theme, label string, group *widget.Enum, opts []radioOpt) layout.Widget {
	fs := panelFontForTheme(th) // 字号档按主题选（竖屏=现档逐字不变，横屏=19/21/13）
	return func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, label)
				l.TextSize = fs.text
				l.Color = th.Palette.Fg
				return l.Layout(gtx)
			}),
		}
		for _, o := range opts {
			o := o
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: panelRadioIndent, Top: panelRadioGapDp}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return material.RadioButton(th, group, o.key, o.label).Layout(gtx)
					})
			}))
		}
		return layout.UniformInset(fs.rowInset).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// langRow 显示语言单选（display_language，默认 en = 当前样式不改）。
// 竖排：标签一行，"英文 (en)" 与 "中文 (zh)" 各占一行。
func (p *Panel) langRow(th *material.Theme) layout.Widget {
	return p.radioGroupRow(th, "display_language 地名显示", &p.langSel, []radioOpt{
		{key: "en", label: "英文 (en)"},
		{key: "zh", label: "中文 (zh)"},
	})
}

// LocSearchClicked 报告是否点了"搜索"（调用方据此发起后台搜索）。
func (p *Panel) LocSearchClicked(gtx layout.Context) bool { return p.btnLocSearch.Clicked(gtx) }

// SearchQuery 返回搜索框当前文本。
func (p *Panel) SearchQuery() string { return p.edLocSearch.Text() }

// SetSearching 把状态置为"搜索中"（发起后台请求时调用）。
func (p *Panel) SetSearching(q string) {
	p.locStatus = "搜索中：" + q + " …"
	p.locPlaces, p.locClicks = nil, nil
	p.locMismatch = ""
}

// SetLocationResults 回填搜索结果（成功给候选，失败给错误文案）。
func (p *Panel) SetLocationResults(places []geo.Place, status string) {
	p.locPlaces = places
	p.locClicks = make([]widget.Clickable, len(places))
	p.locStatus = status
	p.locMismatch = ""
}

// PickLocation 报告被点选的候选（返回索引）。
func (p *Panel) PickLocation(gtx layout.Context) (int, bool) {
	for i := range p.locClicks {
		if p.locClicks[i].Clicked(gtx) {
			return i, true
		}
	}
	return -1, false
}

// Candidate 返回第 i 个候选（越界安全）。
func (p *Panel) Candidate(i int) (geo.Place, bool) {
	if i < 0 || i >= len(p.locPlaces) {
		return geo.Place{}, false
	}
	return p.locPlaces[i], true
}

// withCountry 把国家名并进显示地名（实机反馈"地名的国籍没了"，2026-10-22）。
//
// 口径：
//   - 任一侧为空 ⇒ 原样返回（不凭空造国籍）；
//   - 名字里已含国家名 ⇒ 原样返回（不重复，`Hangzhou, China` 不会变成 `Hangzhou, China, China`）；
//   - 名字含中日韩文字 ⇒ 用「·」连接（`自流井区·中国`），拉丁文用「, 」（`Ziliujing, China`）。
//
// 只作用于**设置面板选点时写下的显示名**；屏保画面按裁决 B 不显示完整路径，
// 完整路径（含州/省/市/区）只在设置面板里显示。
func withCountry(name, country string) string {
	if name == "" || country == "" {
		return name
	}
	if strings.Contains(name, country) {
		return name
	}
	sep := ", "
	for _, r := range name {
		if r >= 0x2E80 && r <= 0x9FFF {
			sep = "·"
			break
		}
	}
	return name + sep + country
}

// SetPickedLocation 把选中的候选写进模型（坐标 + 中英文显示名）。
func (p *Panel) SetPickedLocation(pl geo.Place) {
	m := p.m
	m.LocationLat, m.LocationLon = pl.Lat, pl.Lon
	m.LocationNameEn = withCountry(pl.Name, pl.Country)
	if pl.Source == "nominatim" {
		m.LocationNameZh = withCountry(pl.Name, pl.Country)
	}
	if m.LocationNameZh == "" {
		m.LocationNameZh = withCountry(pl.Name, pl.Country)
	}
	// 显示名跟随当前语言选择
	if m.DisplayLanguage == "zh" {
		m.Location = m.LocationNameZh
	} else {
		m.Location = m.LocationNameEn
	}
	p.m = m
	p.setModel(p.m)
	// §13.4 裁决 D 定案后的文案：`已选：` + **完整路径** + ` (%.4f,%.4f)` + 来源标记
	// （完整路径在同一个 label 控件里自己折行 ⇒ 控件数不变、面板结构不变）。
	p.locStatus = "已选：" + pl.PathOrLabel() + fmt.Sprintf(" (%.4f,%.4f)", pl.Lat, pl.Lon) + " · " + pl.Source
	p.locMismatch = ""
	if pl.Mismatch {
		p.locMismatch = locMismatchText
	}
	p.locPlaces, p.locClicks = nil, nil
}

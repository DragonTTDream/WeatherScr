package ui

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
// ui.FontFamily，ui 再 import config 会成环。面板只持有 PanelModel
// （字段与 config 的 JSON 键一一对应），读写与写盘由 cmd/scr 侧完成
// （复用 config.Config.Save）。

// PanelModel 是设置面板的编辑模型。字段与 config.Config 的 JSON 键一一对应。
type PanelModel struct {
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

// SettingsPanel 是设置面板的控件集合。控件全部用 gioui material。
type SettingsPanel struct {
	m PanelModel

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

	kt settingsKeyTag

	history  []PanelModel // Ctrl+Z 撤销栈
	locked   bool
	changed  bool
	status   string
	lastSave error
}

// NewSettingsPanel 用当前配置建面板。
func NewSettingsPanel(m PanelModel) *SettingsPanel {
	p := &SettingsPanel{m: m}
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
func (p *SettingsPanel) Model() PanelModel { return p.m }

// Changed 报告上一帧是否有改动（改完即写盘）。
func (p *SettingsPanel) Changed() bool { return p.changed }

// Locked 报告锁定态（L）。
func (p *SettingsPanel) Locked() bool { return p.locked }

// SaveError 返回最近一次写盘错误（由调用方在保存后回填）。
func (p *SettingsPanel) SaveError() error { return p.lastSave }

// SetSaveError 记录写盘结果并显示在状态行。
func (p *SettingsPanel) SetSaveError(err error) {
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
func (p *SettingsPanel) model() PanelModel {
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
func (p *SettingsPanel) setModel(m PanelModel) {
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
func (p *SettingsPanel) Undo() bool {
	if len(p.history) == 0 {
		p.status = "无可撤销"
		return false
	}
	prev := p.history[len(p.history)-1]
	p.history = p.history[:len(p.history)-1]
	p.setModel(prev)
	p.status = "已撤销"
	return true
}

// ToggleLock 切换锁定态（上游 L → custom_layout_lock_requested，input_controller.py:215-221）。
// 锁定 = 控件全部禁用（用 gtx.Disabled()，material 控件会禁用并置灰）。
func (p *SettingsPanel) ToggleLock() {
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
func (p *SettingsPanel) HandleKeys(gtx layout.Context) string {
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

// Layout 画面板并同步控件状态。返回 true 表示本帧有改动（调用方写盘）。
func (p *SettingsPanel) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	before := p.m

	// 锁定态：控件全部禁用（gtx.Disabled() 不投递事件），
	// 键盘热键仍由 HandleKeys 在原始 gtx 上处理。
	wgtx := gtx
	if p.locked {
		wgtx = gtx.Disabled()
	}

	rows := p.rows(th)
	// 面板底色：gioui 的 Windows 后端每帧把窗口清成不透明白
	// （gioui.org v0.10.3 app/window.go:216），面板自身不画底 ⇒ 深色模式下
	// 浅字会落在白底上（等于没跟随）。这里按 palette.Bg 铺一层，
	// 配色才真正落到画面上。约束无界时跳过（不画越界矩形）。
	if max := gtx.Constraints.Max; max.X > 0 && max.Y > 0 {
		paint.FillShape(gtx.Ops, th.Palette.Bg, clip.Rect{Max: max}.Op())
	}
	dims := material.List(th, &p.list).LayoutWidgets(wgtx, rows...)

	// 按钮点击（点击后由调用方执行动作）
	p.changed = false
	p.m = p.model()
	if p.m != before {
		p.changed = true
		p.history = append(p.history, before)
		if len(p.history) > 64 {
			p.history = p.history[1:]
		}
	}
	return dims
}

// SaveClicked / CancelClicked / UndoClicked / LockClicked 报告按钮点击。
func (p *SettingsPanel) SaveClicked(gtx layout.Context) bool   { return p.btnSave.Clicked(gtx) }
func (p *SettingsPanel) CancelClicked(gtx layout.Context) bool { return p.btnCancel.Clicked(gtx) }
func (p *SettingsPanel) UndoClicked(gtx layout.Context) bool   { return p.btnUndo.Clicked(gtx) }
func (p *SettingsPanel) LockClicked(gtx layout.Context) bool   { return p.btnLock.Clicked(gtx) }

// rows 面板行。竖屏 1080×1920 上全部行放进可滚动的 material.List。
func (p *SettingsPanel) rows(th *material.Theme) []layout.Widget {
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

func (p *SettingsPanel) titleRow(th *material.Theme, txt string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.H5(th, txt)
		l.TextSize = panelTitleSp // 标题行比正文大一号（spec §1.2）
		l.Color = th.Palette.Fg
		return layout.UniformInset(panelTitleInsetDp).Layout(gtx, l.Layout)
	}
}

func (p *SettingsPanel) plainRow(th *material.Theme, txt string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(th, txt)
		l.TextSize = panelTextSp
		l.Color = th.Palette.ContrastFg
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, l.Layout)
	}
}

func (p *SettingsPanel) fieldRow(th *material.Theme, label string, ed *widget.Editor) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, label)
					l.TextSize = panelCaptionSp
					l.Color = th.Palette.ContrastFg
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					e := material.Editor(th, ed, "")
					// 面板局部字号：这里原先取主题的 th.TextSize，
					// 而它继承自屏保的 WeatherFontSize=40（spec §0 的根因链）
					// ⇒ 换成 panelTextSp。
					e.TextSize = panelTextSp
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
func (p *SettingsPanel) row2(th *material.Theme, a, b fieldSpec) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		cells := []layout.FlexChild{}
		for _, sp := range []fieldSpec{a, b} {
			sp := sp
			cells = append(cells, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if sp.ed == nil {
					l := material.Caption(th, sp.label)
					l.TextSize = panelCaptionSp
					l.Color = th.Palette.ContrastFg
					return layout.UniformInset(panelRowInsetDp).Layout(gtx, l.Layout)
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

// unitsRow 公制/英制单选（units 键）。
func (p *SettingsPanel) unitsRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, "units 单位")
					l.TextSize = panelTextSp
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
func (p *SettingsPanel) sliderRow(th *material.Theme, label string, f *widget.Float) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, label)
					l.TextSize = panelTextSp
					l.Color = th.Palette.Fg
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Slider(th, f).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, formatNum(float64(f.Value)))
					l.TextSize = panelTextSp
					l.Color = th.Palette.Fg
					return l.Layout(gtx)
				}),
			)
		})
	}
}

func (p *SettingsPanel) checkRow(th *material.Theme, label string, b *widget.Bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(th, b, label).Layout(gtx)
		})
	}
}

// buttonRow 保存 / 取消 / 撤销 / 锁定（热键的鼠标入口，语义同热键）。
func (p *SettingsPanel) buttonRow(th *material.Theme) layout.Widget {
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
func (p *SettingsPanel) locSearchRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(th, &p.edLocSearch, "地名（中文：绵阳 / 拉丁：Mianyang）")
					ed.TextSize = panelTextSp
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
func (p *SettingsPanel) locStatusRow(th *material.Theme) layout.Widget {
	return p.plainRow(th, p.locStatus)
}

// locMismatchRow 裁决 C 的红字行（无提示 ⇒ 零尺寸，不占行）。
func (p *SettingsPanel) locMismatchRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if p.locMismatch == "" {
			return layout.Dimensions{}
		}
		l := material.Body2(th, p.locMismatch)
		l.TextSize = panelCaptionSp
		l.Color = locMismatchColor
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, l.Layout)
	}
}

// locCandidateRows 候选地名行（可点）。
func (p *SettingsPanel) locCandidateRows(th *material.Theme) []layout.Widget {
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
			return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								// 裁决 A（§13.4）：完整路径**允许换行** = gioui Label 在宽度
								// 受限时自己折行 ⇒ 不截断、不加省略号，控件数不变。
								l := material.Body2(th, place.PathOrLabel())
								l.TextSize = panelTextSp
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
								l.TextSize = panelCaptionSp
								l.Color = locMismatchColor
								return l.Layout(gtx)
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Caption(th, fmt.Sprintf("%.3f,%.3f", place.Lat, place.Lon))
						l.TextSize = panelCaptionSp
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
func (p *SettingsPanel) radioGroupRow(th *material.Theme, label string, group *widget.Enum, opts []radioOpt) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, label)
				l.TextSize = panelTextSp
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
		return layout.UniformInset(panelRowInsetDp).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// langRow 显示语言单选（display_language，默认 en = 当前样式不改）。
// 竖排：标签一行，"英文 (en)" 与 "中文 (zh)" 各占一行。
func (p *SettingsPanel) langRow(th *material.Theme) layout.Widget {
	return p.radioGroupRow(th, "display_language 地名显示", &p.langSel, []radioOpt{
		{key: "en", label: "英文 (en)"},
		{key: "zh", label: "中文 (zh)"},
	})
}

// LocSearchClicked 报告是否点了"搜索"（调用方据此发起后台搜索）。
func (p *SettingsPanel) LocSearchClicked(gtx layout.Context) bool { return p.btnLocSearch.Clicked(gtx) }

// SearchQuery 返回搜索框当前文本。
func (p *SettingsPanel) SearchQuery() string { return p.edLocSearch.Text() }

// SetSearching 把状态置为"搜索中"（发起后台请求时调用）。
func (p *SettingsPanel) SetSearching(q string) {
	p.locStatus = "搜索中：" + q + " …"
	p.locPlaces, p.locClicks = nil, nil
	p.locMismatch = ""
}

// SetLocationResults 回填搜索结果（成功给候选，失败给错误文案）。
func (p *SettingsPanel) SetLocationResults(places []geo.Place, status string) {
	p.locPlaces = places
	p.locClicks = make([]widget.Clickable, len(places))
	p.locStatus = status
	p.locMismatch = ""
}

// PickLocation 报告被点选的候选（返回索引）。
func (p *SettingsPanel) PickLocation(gtx layout.Context) (int, bool) {
	for i := range p.locClicks {
		if p.locClicks[i].Clicked(gtx) {
			return i, true
		}
	}
	return -1, false
}

// Candidate 返回第 i 个候选（越界安全）。
func (p *SettingsPanel) Candidate(i int) (geo.Place, bool) {
	if i < 0 || i >= len(p.locPlaces) {
		return geo.Place{}, false
	}
	return p.locPlaces[i], true
}

// SetPickedLocation 把选中的候选写进模型（坐标 + 中英文显示名）。
func (p *SettingsPanel) SetPickedLocation(pl geo.Place) {
	m := p.m
	m.LocationLat, m.LocationLon = pl.Lat, pl.Lon
	m.LocationNameEn = pl.Name
	if pl.Source == "nominatim" {
		m.LocationNameZh = pl.Name
	}
	if m.LocationNameZh == "" {
		m.LocationNameZh = pl.Name
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

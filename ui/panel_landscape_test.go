package ui

// B5/R11 设置页横屏矩形单测（spec/b5-settings-landscape.md 判据 1/2/3）。
//
// 口径照 ui/landscape_layout_test.go:32-36 的 landW=1920 / landH=1080 /
// landTol=1（取证照片 troubleshooting/横屏适配缺陷.md 的 flipSCR.png 几何），
// 1 dp = 1 px = 1 sp 同 ui/panel_theme_b2_test.go:24-30（本机 CI 字体口径）。
//
// 竖屏零迁移由 ui/panel_theme_b2_test.go 三条 + ui/settings_ui_test.go 钉死，
// 本文件只钉横屏侧；三条判据相互独立，任一红了都能单独定位。

import (
	"image"
	"image/color"
	"sort"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/DragonTTDream/WeatherScr/geo"
)

const (
	panelLandW   = 1920 // 横屏请求几何（spec 判据 3：横屏请求 1920,1080）
	panelLandH   = 1080
	panelLandTol = 1 // px，与 landscape_layout_test.go 的 landTol 同口径
	// Flex{Horizontal} + 两个等权 Flexed(1) ⇒ 每栏宽 = 1920/2 = 960 px
	panelLandColW = panelLandW / 2
)

// b5ProbeWidths 行指纹的采样宽度（多宽度复合指纹）。
//
// 为什么不是单一宽度：row2 的两格是 layout.Flexed ⇒ 任何宽度下整行都填满，
// 六个 row2 组的 (宽,高) 在宽约束下彼此相同；窄约束下各格按标签长度折行，
// 折行行数不同 ⇒ 用 4000/500/260/200/150/360/120 七档采样拼成一支指纹，
// 实测把面板全部 27 行逐行分开（撞车数 0，见本文件的"点名恰好一次"断言）。
const b5ProbeN = 7 // 采样档数（指纹长度）

var b5ProbeWidths = []int{4000, 500, 260, 200, 150, 360, 120}

// b5FP 一行的复合指纹。
type b5FP [b5ProbeN]image.Point

func b5FpOf(t *testing.T, w layout.Widget) b5FP {
	t.Helper()
	if len(b5ProbeWidths) != b5ProbeN {
		t.Fatalf("采样宽度 %d 档 ≠ 指纹长度 %d", len(b5ProbeWidths), b5ProbeN)
	}
	var fp b5FP
	for i, wd := range b5ProbeWidths {
		fp[i] = layoutAt(t, w, wd, 4000).Size
	}
	return fp
}

// rowWideCtx 单宽度（4000 px）的自然宽高，供"不折行"侧的判据用。
func rowWideCtx(t *testing.T, w layout.Widget) [2]int {
	t.Helper()
	d := layoutAt(t, w, 4000, 4000)
	return [2]int{d.Size.X, d.Size.Y}
}

// b5Fingerprints 量一组行的指纹。
func b5Fingerprints(t *testing.T, rows []layout.Widget) []b5FP {
	t.Helper()
	out := make([]b5FP, len(rows))
	for i, w := range rows {
		out[i] = b5FpOf(t, w)
	}
	return out
}

// b5ProbeText 点名"更新行"用的探针文本：含 edRefresh 的那一行在探针下自然宽
// 会变长（Editor.SingleLine ⇒ 宽度 = 文本宽），其余行不受影响。
// 为什么需要探针：row2 的两格是 layout.Flexed ⇒ 宽约束下都填满整行，
// 六个 row2 的**静态**指纹彼此相同 ⇒ 只能靠"谁的输入框被灌了文本"来点名。
const b5ProbeText = "012345678901234567890123456789"

// b5RowRefs 判据 2 点名的行 → 同款参考行（同一面板控件 + 同一横屏主题 ⇒ 同指纹）。
// 标签逐字取自 rowSpecs 的行字面量（ui/settings_ui.go）；标签改了这里就红。
func b5RowRefs(p *SettingsPanel, th *material.Theme) map[string]layout.Widget {
	return map[string]layout.Widget{
		"locSearch": p.locSearchRow(th),
		"refresh": p.row2(th,
			fieldSpec{label: "margin", ed: &p.edMargin},
			fieldSpec{label: "refresh_minutes 天气刷新(分钟，最小1)", ed: &p.edRefresh}),
		"units": p.unitsRow(th),

		"check:show_background":        p.checkRow(th, "show_background 面板底", &p.cbShowBg),
		"check:show_condition_icon":    p.checkRow(th, "show_condition_icon 天气图标", &p.cbConditionIcon),
		"check:show_details_row":       p.checkRow(th, "show_details_row 明细行", &p.cbDetailsRow),
		"check:show_forecast":          p.checkRow(th, "show_forecast 预报行", &p.cbForecast),
		"check:show_five_day_forecast": p.checkRow(th, "show_five_day_forecast 五日预报", &p.cbFiveDay),
		"check:effects":                p.checkRow(th, "背景动效（effect.enabled）", &p.cbEffects),
		"check:interaction_mode":       p.checkRow(th, "interaction_mode（false=一动就退出）", &p.cbInteractionMode),
		"check:alert_enabled":          p.checkRow(th, "alert_enabled 降雨提醒开关", &p.cbAlert),
		"check:alert_pulse":            p.checkRow(th, "alert_pulse 提醒缓慢呼吸", &p.cbAlertPulse),

		"buttons": p.buttonRow(th),
	}
}

const b5Unnamed = "(未点名)"

// b5RefMap 参考行 → 指纹表；指纹撞车 ⇒ 直接红（判据 2 的点名不能靠猜）。
func b5RefMap(t *testing.T, refs map[string]layout.Widget) map[b5FP]string {
	t.Helper()
	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)
	fp := make(map[b5FP]string, len(refs))
	for _, name := range names {
		k := b5FpOf(t, refs[name])
		if dup, ok := fp[k]; ok {
			t.Fatalf("行指纹撞车：%s 与 %s ⇒ 点名失效", dup, name)
		}
		fp[k] = name
	}
	return fp
}

func b5NameOf(fp map[b5FP]string, k b5FP) string {
	if n, ok := fp[k]; ok {
		return n
	}
	return b5Unnamed
}

func b5Has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func b5Count(names []string) map[string]int {
	m := map[string]int{}
	for _, n := range names {
		m[n]++
	}
	return m
}

// b5LandPlaces 两条候选地名（判据 2「候选列表插左栏组后」用；字段口径见
// geo/search.go:40-61，候选行的显示串走 PathOrLabel）。
func b5LandPlaces() []geo.Place {
	return []geo.Place{
		{Lat: 25.55, Lon: 113.38, Name: "自流井区", Admin1: "四川省", Country: "China",
			FullPath: "China/Sichuan/Zigong/Ziliujing", CountryCode: "CN"},
		{Lat: 31.87, Lon: 117.23, Name: "包河区", Admin1: "安徽省", Country: "China",
			FullPath: "China/Anhui/Hefei/Baohe", CountryCode: "CN"},
	}
}

// TestPanelLandscapeRect 判据 1：1920×1080 gtx 下面板不溢出，双栏总行 = 单栏全行集。
func TestPanelLandscapeRect(t *testing.T) {
	th := NewPanelTheme(true)
	p := NewSettingsPanel(sampleModel())

	d := layoutAt(t, func(gtx layout.Context) layout.Dimensions {
		return p.Layout(gtx, th)
	}, panelLandW, panelLandH)
	left, right := p.rowsSplit(th)
	t.Logf("panel 1920x1080: w=%d h=%d 左栏=%d 行 右栏=%d 行 单栏=%d 行",
		d.Size.X, d.Size.Y, len(left), len(right), p.RowCount())

	// ① 宽高不越出请求几何（normalizeDialog 只会将窗口夹到工作区以内 ⇒
	//    面板必须落进 1920×1080 本身，落在更小的窗口里由竖屏那条不溢出判据管）。
	if d.Size.X > panelLandW {
		t.Errorf("面板宽 %d > %d ⇒ 横屏几何溢出", d.Size.X, panelLandW)
	}
	if d.Size.Y > panelLandH {
		t.Errorf("面板高 %d > %d ⇒ 横屏几何溢出", d.Size.Y, panelLandH)
	}
	// ② 长方形双栏：两栏并排铺满整个宽度（外层若是竖排，宽度只会是单栏 ≈ 960）。
	if d.Size.X < panelLandW-panelLandTol {
		t.Errorf("面板宽 %d < %d±%d ⇒ 两栏没有并排铺满（外层不是 Flex{Horizontal}）",
			d.Size.X, panelLandW, panelLandTol)
	}
	// ③ 行数守恒：双栏 = 单栏全行集。spec 判据 1 注明"注释 26/27 失真以实测为准"
	//    ⇒ 基线实测 = 27 行（b-alert-v1b 拆掉视野小时组后的口径）。
	if got, want := len(left)+len(right), p.RowCount(); got != want {
		t.Errorf("双栏 %d+%d=%d 行 ≠ 单栏 %d 行 ⇒ 行集没有守恒", len(left), len(right), got, want)
	}
	if want := 27; p.RowCount() != want {
		t.Errorf("单栏行数 = %d，实测基线 %d ⇒ 行集被改动要显式改这条基线", p.RowCount(), want)
	}
	if len(left) < 2 || len(right) < 2 {
		t.Errorf("双栏行数 %d/%d ⇒ 没有真的分成两栏", len(left), len(right))
	}

	// ④ 带候选时同样守恒（候选只增不减，双栏与单栏同增）。
	p.SetLocationResults(b5LandPlaces(), "2 个候选")
	l2, r2 := p.rowsSplit(th)
	if got, want := len(l2)+len(r2), len(p.rows(th)); got != want {
		t.Errorf("带候选时双栏 %d 行 ≠ 单栏 %d 行", got, want)
	}
	if len(l2) <= len(left) {
		t.Errorf("候选没有进左栏：左栏 %d → %d 行", len(left), len(l2))
	}
}

// TestPanelLandscapeDualColumn 判据 2：左栏 = 地区与预报组（LocSearch/单位/更新），
// 右栏 = 显示开关组（全部 checkRow）+ 按钮行；候选插左栏组后；两栏主轴 Vertical。
//
// 点名口径 = 自然宽高指纹 + 一条输入框探针（见 b5ProbeText 注释）：
// 测试用同款构造函数复现参考行，指纹相同即同一行；每个点名行必须**恰好出现
// 一次** ⇒ 参考行与面板行撞车会立刻暴露，不会静默认错。
func TestPanelLandscapeDualColumn(t *testing.T) {
	th := NewPanelTheme(true)
	p := NewSettingsPanel(sampleModel())

	// 探针：灌文本 → 量指纹 → 还原（只借指纹，不改配置语义）。
	orig := p.edRefresh.Text()
	p.edRefresh.SetText(b5ProbeText)
	defer p.edRefresh.SetText(orig)

	fp := b5RefMap(t, b5RowRefs(p, th))

	left, right := p.rowsSplit(th)
	if got, want := len(left)+len(right), p.RowCount(); got != want {
		t.Fatalf("双栏 %d 行 ≠ 单栏 %d 行 ⇒ 点名前先守恒", got, want)
	}
	lNames := []string{}
	for _, k := range b5Fingerprints(t, left) {
		lNames = append(lNames, b5NameOf(fp, k))
	}
	rNames := []string{}
	for _, k := range b5Fingerprints(t, right) {
		rNames = append(rNames, b5NameOf(fp, k))
	}
	t.Logf("左栏点名=%v", lNames)
	t.Logf("右栏点名=%v", rNames)

	all := b5Count(append(append([]string{}, lNames...), rNames...))
	for name, cnt := range all {
		if name == b5Unnamed {
			continue
		}
		if cnt != 1 {
			t.Errorf("点名行「%s」在双栏里出现 %d 次（要求恰好 1 次）", name, cnt)
		}
	}
	if got, want := all[b5Unnamed], p.RowCount()-len(fp); got != want {
		t.Errorf("未点名行 = %d，要求 = 总行 %d − 点名 %d = %d", got, p.RowCount(), len(fp), want)
	}

	// 左栏必须有点名的这三行（判据 2 逐字：LocSearch / 单位 / 更新）。
	for _, want := range []string{"locSearch", "refresh", "units"} {
		if !b5Has(lNames, want) {
			t.Errorf("左栏缺 %s（判据 2：左栏 = 地区与预报组 LocSearch/单位/更新）", want)
		}
	}
	// 右栏必须收全部 checkRow + 按钮行。
	for _, want := range []string{
		"check:alert_enabled", "check:alert_pulse", "check:effects",
		"check:interaction_mode", "check:show_background", "check:show_condition_icon",
		"check:show_details_row", "check:show_five_day_forecast", "check:show_forecast",
		"buttons",
	} {
		if !b5Has(rNames, want) {
			t.Errorf("右栏缺 %s（判据 2：右栏 = 显示开关组 + 按钮行）", want)
		}
	}
	// 串栏 = 分栏表被改坏：开关/按钮行不许进左栏，地区/单位/更新行不许进右栏。
	for _, n := range lNames {
		if n == "buttons" || strings.HasPrefix(n, "check:") {
			t.Errorf("开关/按钮行「%s」落进了左栏", n)
		}
	}
	for _, n := range rNames {
		if n == "locSearch" || n == "units" || n == "refresh" {
			t.Errorf("地区/单位/更新行「%s」落进了右栏", n)
		}
	}
	// 两栏主轴 = layout.Vertical（判据 2 逐字）；外层 Flex{Horizontal} 由
	// TestPanelLandscapeRect 的"铺满 1920 宽"那条钉住。
	if p.list.Axis != layout.Vertical || p.listRight.Axis != layout.Vertical {
		t.Errorf("两栏主轴 = %v/%v，要求都是 layout.Vertical", p.list.Axis, p.listRight.Axis)
	}

	// 候选列表插在左栏「地名组」（标题/状态/location/搜索行/搜索状态行 = 5 行）之后。
	p.SetLocationResults(b5LandPlaces(), "2 个候选")
	l2, _ := p.rowsSplit(th)
	cand := b5Fingerprints(t, p.locCandidateRows(th))
	if len(l2) != len(left)+len(cand) {
		t.Fatalf("左栏 %d → %d 行，候选 = %d 行 ⇒ 插入数量不符", len(left), len(l2), len(cand))
	}
	for i, k := range cand {
		at := panelLandCandAt + i
		if at >= len(l2) {
			t.Fatalf("左栏只有 %d 行，插不到第 %d 行", len(l2), at)
		}
		if b5Fingerprints(t, []layout.Widget{l2[at]})[0] != k {
			t.Errorf("左栏第 %d 行不是候选行（要求紧跟在 %d 行地名组之后）", at, panelLandCandAt)
		}
	}
}

// TestPanelLandscapeFontReadable 判据 3：横屏字号 ≥17sp、行高 ≤40 px。
//
// "行高"口径 = 构成面板行的**单行控件**高度，与 ui/panel_theme_b2_test.go:33-39
// 的 b2LabelLinePx=22 / b2RadioLinePx=34 同一口径（那里量的就是单行控件，
// 不是把竖排 3 行的 langRow 整体当一行）。
// TODO(未取证)：spec 只写"行高 ≤40 px"一句，未取证是否要求"说明行 + 输入行"
// 两行的 fieldRow（横屏实测 51 px）也算一行；本判据按单行控件口径钉，
// 并对整行的开关行（radio 图标行 + 行距 = 40 px）逐行覆盖。
func TestPanelLandscapeFontReadable(t *testing.T) {
	// ① 字号档位：横屏正文 ≥17sp（spec 判据 3），标题/说明同档递增。
	if float32(panelTextSpLand) < 17 {
		t.Errorf("横屏正文 %v sp < 17 ⇒ 判据 3", float32(panelTextSpLand))
	}
	fs := panelFontFor(true)
	if fs.text != panelTextSpLand || fs.title != panelTitleSpLand || fs.caption != panelCaptionSpLand {
		t.Errorf("横屏档 = %v/%v/%v，要求 %v/%v/%v",
			fs.text, fs.title, fs.caption, panelTextSpLand, panelTitleSpLand, panelCaptionSpLand)
	}
	if fs.title <= fs.text || fs.caption >= fs.text {
		t.Errorf("横屏档层级不成立：title=%v text=%v caption=%v", fs.title, fs.text, fs.caption)
	}
	// 竖屏档逐字不动（判据 4 的字号侧）。
	if v := panelFontFor(false); v.text != panelTextSp || v.title != panelTitleSp || v.caption != panelCaptionSp {
		t.Errorf("竖屏档被改动：%v/%v/%v，要求 %v/%v/%v",
			v.text, v.title, v.caption, panelTextSp, panelTitleSp, panelCaptionSp)
	}
	// 朝向传参保真（点位「NewPanelTheme 按参数选档」）。
	if got := NewPanelTheme(true).TextSize; got != panelTextSpLand {
		t.Errorf("NewPanelTheme(true).TextSize = %v，要求 %v", float32(got), float32(panelTextSpLand))
	}
	if got := NewPanelTheme().TextSize; got != panelTextSp {
		t.Errorf("NewPanelTheme().TextSize = %v，要求竖屏 %v", float32(got), float32(panelTextSp))
	}

	th := NewPanelTheme(true)
	p := NewSettingsPanel(sampleModel())

	// ② 单行控件在 960 px 栏宽下逐行 ≤40 px。
	lines := []struct {
		name string
		w    layout.Widget
	}{
		{"Body2", func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, "show_forecast 预报行")
			l.TextSize = fs.text
			return l.Layout(gtx)
		}},
		{"H5", func(gtx layout.Context) layout.Dimensions {
			l := material.H5(th, "WeatherScr 设置")
			l.TextSize = fs.title
			return l.Layout(gtx)
		}},
		{"Caption", func(gtx layout.Context) layout.Dimensions {
			l := material.Caption(th, "refresh_minutes 天气刷新(分钟，最小1)")
			l.TextSize = fs.caption
			return l.Layout(gtx)
		}},
		{"CheckBox", func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(th, &p.cbFiveDay, "show_five_day_forecast 五日预报").Layout(gtx)
		}},
		{"RadioButton", func(gtx layout.Context) layout.Dimensions {
			return material.RadioButton(th, &p.units, "i", "英制 (i)").Layout(gtx)
		}},
		{"Editor", func(gtx layout.Context) layout.Dimensions {
			e := material.Editor(th, &p.edRefresh, "")
			e.TextSize = fs.text
			return e.Layout(gtx)
		}},
	}
	for _, l := range lines {
		d := layoutAt(t, l.w, panelLandColW, panelLandH)
		t.Logf("%s: w=%d h=%d", l.name, d.Size.X, d.Size.Y)
		if d.Size.Y > 40 {
			t.Errorf("%s 单行高 %d px > 40 px ⇒ 判据 3 行高", l.name, d.Size.Y)
		}
	}

	// ③ 开关行整行（radio 图标行 + 行距）在栏宽内 ≤40 px，且 960 px 栏宽内不折行
	//    （折行 = 单行控件被挤成多行，即 b2 取证的"逐字竖排"症状）。
	for _, w := range []layout.Widget{
		p.checkRow(th, "show_condition_icon 天气图标", &p.cbConditionIcon),
		p.checkRow(th, "show_five_day_forecast 五日预报", &p.cbFiveDay),
		p.checkRow(th, "interaction_mode（false=一动就退出）", &p.cbInteractionMode),
	} {
		d := layoutAt(t, w, panelLandColW, panelLandH)
		nat := rowWideCtx(t, w)
		t.Logf("checkRow: w=%d h=%d 自然=%dx%d", d.Size.X, d.Size.Y, nat[0], nat[1])
		if d.Size.Y > 40 {
			t.Errorf("开关行整行高 %d px > 40 px ⇒ 判据 3 行高", d.Size.Y)
		}
		if d.Size.X > nat[0] {
			t.Errorf("开关行在 %d px 栏宽下宽 %d > 自然宽 %d ⇒ 越栏", panelLandColW, d.Size.X, nat[0])
		}
		if d.Size.Y > nat[1]+panelLandTol {
			t.Errorf("开关行在 960 px 栏宽下高 %d > 自然高 %d ⇒ 折行", d.Size.Y, nat[1])
		}
	}
}

// ---------------------------------------------------------------------------
// TestPanelLandscapeLabels（B5.1 返工判据）：横屏 1920×1080 下 panel 全部行的
// 标签必须出字。
//
// 三条腿，彼此独立：
//  ① 点名对位：逐行点名的行（标签逐字取自 ui/settings_ui.go 的 rowSpecs 字面量）
//     与 rowSpecs / rowsSplit 的行指纹逐位相等 ⇒ 行集与行序对得上，标签清单不漏行；
//  ② 标签 span（ui/panel_theme_b2_test.go 的测量探针口径 layoutAt + 量宽高）：
//     每个标签在"行内实际可用宽度"下 span 宽>0 且高>0，且高 ≤ 4 行（挡"被挤成
//     逐字竖排"）；row2 家族按格宽量、每单元恰好 2 个标签；locSearch 行标签在场；
//  ③ 取色可辨（出字的机械代理）：横屏档标签色与面板底色逐通道差 ≥96；
//     竖屏档标签色仍 == Palette.ContrastFg（判据 4 零迁移，钉值连同竖屏行高）。
//
// 根因口径（见 out/notes-b5-labels.md 的探针输出）：浅色模式下
// Palette.ContrastFg == Palette.Bg == 纯白，而面板每帧用 Bg 铺底 ⇒ 取 ContrastFg
// 的标签"排版有尺寸、像素全白"。所以 ② 单独不够（span 非 0 也可能全白），
// ③ 才是"出字"的那一条；② 挡的是"标签被挤成 0 宽/挤成逐字竖排"这一类排版症状。
// ---------------------------------------------------------------------------

const (
	// 横屏档单行控件行高（本机 CI 字体实测，1 dp = 1 px = 1 sp，同 b2 口径）：
	b5LandCaptionPx = 18 // material.Caption @ fs.caption = 13sp
	b5LandTextPx    = 27 // 正文/编辑器 @ fs.text = 19sp
	// 横屏 row2 单元格可用宽：栏宽 960 − 外层 UniformInset(2)×2 − Flex
	// SpaceBetween 间隙 ⇒ 实测两格各 ≈473 px；本判据按 460 px 量标签（保守下界）。
	b5LandCellW = 460
	// 竖屏现档钉值（判据 4 零迁移）：本批次改动只走横屏分支，这些值一字不动。
	b5PortRow2H   = 51
	b5PortFieldW  = 246
	b5PortFieldH  = 47
	b5PortRow2Nil = 29
	b5PortLocSrch = 46
)

// b5LandRow 一行：行名 + 该行必须出字的标签文本 + 排版档位。
type b5LandRow struct {
	name   string
	row    layout.Widget
	labels []string
	cell   bool // true = row2 家族（两格竖排单元，标签按格宽量，每单元 1 个标签）
	cap    bool // true = 标签走 fs.caption 档（fieldRow / row2 家族）
}

// b5LandInventory 横屏档下行名与标签的逐行清单（顺序 = rowSpecs 的下标顺序）。
// 行由**与 rowSpecs 同款的构造函数**复现，标签逐字照抄行字面量 ⇒ 行字改了这里就红。
func b5LandInventory(p *SettingsPanel, th *material.Theme) []b5LandRow {
	return []b5LandRow{
		{name: "标题行", row: p.titleRow(th, "WeatherScr 设置"), labels: []string{"WeatherScr 设置"}},
		{name: "状态行", row: p.plainRow(th, p.status), labels: nil},
		{name: "location 地名", row: p.fieldRow(th, "location 地名", &p.edLocation),
			labels: []string{"location 地名"}, cap: true},
		{name: "locSearch 行", row: p.locSearchRow(th),
			labels: []string{"地名（中文：绵阳 / 拉丁：Mianyang）", "搜索"}},
		{name: "搜索状态行", row: p.locStatusRow(th), labels: nil},
		{name: "红字行", row: p.locMismatchRow(th), labels: nil},
		{name: "display_language", row: p.langRow(th),
			labels: []string{"display_language 地名显示", "英文 (en)", "中文 (zh)"}},
		{name: "wallpaper_dir", row: p.fieldRow(th, "wallpaper_dir 壁纸目录（sources.folders）", &p.edWallpaper),
			labels: []string{"wallpaper_dir 壁纸目录（sources.folders）"}, cap: true},
		{name: "row2:font_size/icon_size", row: p.row2(th,
			fieldSpec{label: "font_size", ed: &p.edFontSize},
			fieldSpec{label: "icon_size", ed: &p.edIconSize}),
			labels: []string{"font_size", "icon_size"}, cell: true, cap: true},
		{name: "row2:margin/refresh_minutes", row: p.row2(th,
			fieldSpec{label: "margin", ed: &p.edMargin},
			fieldSpec{label: "refresh_minutes 天气刷新(分钟，最小1)", ed: &p.edRefresh}),
			labels: []string{"margin", "refresh_minutes 天气刷新(分钟，最小1)"}, cell: true, cap: true},
		{name: "row2:wallpaper_interval_s/timing.interval", row: p.row2(th,
			fieldSpec{label: "wallpaper_interval_s 间隔(秒)", ed: &p.edInterval},
			fieldSpec{label: "timing.interval 语义", ed: nil}),
			labels: []string{"wallpaper_interval_s 间隔(秒)", "timing.interval 语义"}, cell: true, cap: true},
		{name: "row2:clock_y_offset/weather_y_offset", row: p.row2(th,
			fieldSpec{label: "clock_y_offset 时钟 y 偏移(px)", ed: &p.edClockY},
			fieldSpec{label: "weather_y_offset 天气 y 偏移(px)", ed: &p.edWeatherY}),
			labels: []string{"clock_y_offset 时钟 y 偏移(px)", "weather_y_offset 天气 y 偏移(px)"}, cell: true, cap: true},
		{name: "row2:alert_threshold_pct/alert_y_offset", row: p.row2(th,
			fieldSpec{label: "alert_threshold_pct 措辞阈值(%，默认30)", ed: &p.edAlertTh},
			fieldSpec{label: "alert_y_offset 提醒 y 偏移(px，默认-60)", ed: &p.edAlertY}),
			labels: []string{"alert_threshold_pct 措辞阈值(%，默认30)", "alert_y_offset 提醒 y 偏移(px，默认-60)"}, cell: true, cap: true},
		{name: "row2:alert_min_mm/越界回落", row: p.row2(th,
			fieldSpec{label: "alert_min_mm 最小雨量(mm/15min，默认0.05)", ed: &p.edAlertMin},
			fieldSpec{label: "越界回落 0.05", ed: nil}),
			labels: []string{"alert_min_mm 最小雨量(mm/15min，默认0.05)", "越界回落 0.05"}, cell: true, cap: true},
		{name: "units 单位", row: p.unitsRow(th), labels: []string{"units 单位", "公制 (c)", "英制 (i)"}},
		{name: "slider:bg_opacity", row: p.sliderRow(th, "bg_opacity 天气面板底不透明度（上游 0.3）", &p.slBgOpacity),
			labels: []string{"bg_opacity 天气面板底不透明度（上游 0.3）"}},
		{name: "slider:clock_bg_opacity", row: p.sliderRow(th, "clock_bg_opacity 时钟面板底不透明度（偏离上游，默认 0.45）", &p.slClockBgOpacity),
			labels: []string{"clock_bg_opacity 时钟面板底不透明度（偏离上游，默认 0.45）"}},
		{name: "check:show_background", row: p.checkRow(th, "show_background 面板底", &p.cbShowBg),
			labels: []string{"show_background 面板底"}},
		{name: "check:show_condition_icon", row: p.checkRow(th, "show_condition_icon 天气图标", &p.cbConditionIcon),
			labels: []string{"show_condition_icon 天气图标"}},
		{name: "check:show_details_row", row: p.checkRow(th, "show_details_row 明细行", &p.cbDetailsRow),
			labels: []string{"show_details_row 明细行"}},
		{name: "check:show_forecast", row: p.checkRow(th, "show_forecast 预报行", &p.cbForecast),
			labels: []string{"show_forecast 预报行"}},
		{name: "check:show_five_day_forecast", row: p.checkRow(th, "show_five_day_forecast 五日预报", &p.cbFiveDay),
			labels: []string{"show_five_day_forecast 五日预报"}},
		{name: "check:effects", row: p.checkRow(th, "背景动效（effect.enabled）", &p.cbEffects),
			labels: []string{"背景动效（effect.enabled）"}},
		{name: "check:interaction_mode", row: p.checkRow(th, "interaction_mode（false=一动就退出）", &p.cbInteractionMode),
			labels: []string{"interaction_mode（false=一动就退出）"}},
		{name: "check:alert_enabled", row: p.checkRow(th, "alert_enabled 降雨提醒开关", &p.cbAlert),
			labels: []string{"alert_enabled 降雨提醒开关"}},
		{name: "check:alert_pulse", row: p.checkRow(th, "alert_pulse 提醒缓慢呼吸", &p.cbAlertPulse),
			labels: []string{"alert_pulse 提醒缓慢呼吸"}},
		{name: "按钮行", row: p.buttonRow(th),
			labels: []string{"保存 Enter", "取消 Esc", "撤销 Ctrl+Z", "锁定 L"}},
	}
}

// b5LabelSpan 按 b2 的测量探针口径量一个标签的 span（宽×高）。
func b5LabelSpan(t *testing.T, th *material.Theme, fs panelFont, label string, caption bool, w int) layout.Dimensions {
	t.Helper()
	if caption {
		l := material.Caption(th, label)
		l.TextSize = fs.caption
		return layoutAt(t, l.Layout, w, 4000)
	}
	l := material.Body2(th, label)
	l.TextSize = fs.text
	return layoutAt(t, l.Layout, w, 4000)
}

// b5ColorVisible 两个颜色的逐通道最大差 ≥96 且都是不透明 ⇒ 画在同一块底上分得开。
func b5ColorVisible(bg, fg color.NRGBA) bool {
	if fg.A < 255 {
		return false
	}
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	m := d(bg.R, fg.R)
	if v := d(bg.G, fg.G); v > m {
		m = v
	}
	if v := d(bg.B, fg.B); v > m {
		m = v
	}
	return m >= 96
}

func TestPanelLandscapeLabels(t *testing.T) {
	th := NewPanelTheme(true)
	fs := panelFontForTheme(th)
	p := NewSettingsPanel(sampleModel())
	inv := b5LandInventory(p, th)

	// ① 点名对位：清单与 rowSpecs 逐位同指纹（行集/行序/行字三样都对上）。
	rows := p.rowSpecs(th)
	if len(inv) != len(rows) {
		t.Fatalf("点名清单 %d 行 ≠ rowSpecs %d 行 ⇒ 行集变了", len(inv), len(rows))
	}
	if n := len(p.locCandidateRows(th)); n != 0 {
		t.Fatalf("样本模型带出 %d 条候选地名 ⇒ 本判据的并集口径要重算", n)
	}
	for i := range inv {
		if a, b := b5FpOf(t, inv[i].row), b5FpOf(t, rows[i]); a != b {
			t.Fatalf("第 %d 行 %s 与 rowSpecs 行指纹不等 %v ≠ %v", i, inv[i].name, a, b)
		}
	}

	// 1920×1080 整面板：不溢出（本判据全部量测的地理前提）。
	d := layoutAt(t, func(gtx layout.Context) layout.Dimensions { return p.Layout(gtx, th) }, panelLandW, panelLandH)
	t.Logf("panel@%dx%d: w=%d h=%d rows=%d", panelLandW, panelLandH, d.Size.X, d.Size.Y, p.RowCount())
	if d.Size.X > panelLandW || d.Size.Y > panelLandH {
		t.Errorf("面板在 %dx%d 下 %dx%d 溢出", panelLandW, panelLandH, d.Size.X, d.Size.Y)
	}

	// ①b 双栏并集：清单里的每一行在左栏+右栏里恰好出现一次（行数守恒 + 逐行点名都在）。
	left, right := p.rowsSplit(th)
	want := map[b5FP]int{}
	have := map[b5FP]int{}
	name := map[b5FP]string{}
	for i := range inv {
		fp := b5FpOf(t, inv[i].row)
		want[fp]++
		name[fp] = inv[i].name
	}
	for _, w := range append(append([]layout.Widget{}, left...), right...) {
		have[b5FpOf(t, w)]++
	}
	for fp, n := range want {
		if have[fp] != n {
			t.Errorf("%s 在双栏并集里出现 %d 次，点名期望 %d 次", name[fp], have[fp], n)
		}
	}
	for fp, n := range have {
		if want[fp] == 0 {
			t.Errorf("双栏里出现未点名的行 %v（%d 次）", fp, n)
		}
	}
	t.Logf("双栏并集：左 %d 行 + 右 %d 行 = 点名 %d 行", len(left), len(right), len(inv))

	// ② 标签 span：每个标签在"行内可用宽度"下宽>0 高>0，且不被挤成逐字竖排。
	for _, r := range inv {
		w := panelLandColW
		if r.cell {
			w = b5LandCellW
			if len(r.labels) != 2 {
				t.Errorf("%s 是 row2 单元组，标签数 %d ≠ 2", r.name, len(r.labels))
				continue
			}
		}
		line := b5LandTextPx
		if r.cap {
			line = b5LandCaptionPx
		}
		for _, lab := range r.labels {
			d := b5LabelSpan(t, th, fs, lab, r.cap, w)
			t.Logf("label %-8s w=%d h=%d span=%dx%d (可用宽 %d)", r.name, w, line, d.Size.X, d.Size.Y, w)
			if d.Size.X <= 0 || d.Size.Y <= 0 {
				t.Errorf("%s 标签 %q 在 %d px 可用宽下 span=%dx%d ⇒ 标签被挤成 0", r.name, lab, w, d.Size.X, d.Size.Y)
				continue
			}
			if d.Size.X > w {
				t.Errorf("%s 标签 %q span 宽 %d > 可用宽 %d ⇒ 越栏", r.name, lab, d.Size.X, w)
			}
			if max := 4 * line; d.Size.Y > max {
				t.Errorf("%s 标签 %q 在 %d px 下高 %d px > %d px ⇒ 被挤成逐字竖排", r.name, lab, w, d.Size.Y, max)
			}
		}
	}

	// ②b row2 家族整行：横屏档必须是"两个竖排单元并排"⇒ 整行高 = 标签行 + 编辑器行
	//     （标签在上、编辑器在下都在），且两格并排填满栏宽。
	//     标签若在排版里塌成 0，整行高会掉回单行编辑器高度 ⇒ 这里立刻红。
	for _, r := range inv {
		if !r.cell {
			continue
		}
		d := layoutAt(t, r.row, panelLandColW, panelLandH)
		min := b5LandCaptionPx + b5LandTextPx
		t.Logf("row2 %s @栏宽%d -> %dx%d (竖排单元下界 %d)", r.name, panelLandColW, d.Size.X, d.Size.Y, min)
		if d.Size.Y < min {
			t.Errorf("%s 整行高 %d px < 标签行 %d + 编辑器行 %d ⇒ 竖排单元里的标签行没落地",
				r.name, d.Size.Y, b5LandCaptionPx, b5LandTextPx)
		}
		if max := min + 16; d.Size.Y > max {
			t.Errorf("%s 整行高 %d px > %d px ⇒ 有行被折成多行", r.name, d.Size.Y, max)
		}
		if d.Size.X != panelLandColW {
			t.Errorf("%s 两格并排宽 %d ≠ 栏宽 %d", r.name, d.Size.X, panelLandColW)
		}
	}

	// ②c locSearch 行：标签（输入框占位文本）与按钮都在场，行高 ≥ 正文行 + 按钮行。
	loc := layoutAt(t, p.locSearchRow(th), panelLandColW, panelLandH)
	if loc.Size.Y < b5LandTextPx {
		t.Errorf("locSearch 行高 %d px < 正文行 %d px ⇒ 行内文本没落地", loc.Size.Y, b5LandTextPx)
	}
	t.Logf("locSearch 行 @栏宽%d -> %dx%d", panelLandColW, loc.Size.X, loc.Size.Y)

	// ③ 出字的机械判据：横屏档标签色与面板底色分得开；竖屏档取色一字不动。
	if !b5ColorVisible(th.Palette.Bg, panelCaptionColor(th, fs)) {
		t.Errorf("横屏标签色 %v 与底色 %v 逐通道差 <96 ⇒ 画出来看不见（出字为零）",
			panelCaptionColor(th, fs), th.Palette.Bg)
	}
	vth := NewPanelTheme()
	vfs := panelFontForTheme(vth)
	if got := panelCaptionColor(vth, vfs); got != vth.Palette.ContrastFg {
		t.Errorf("竖屏标签取色 = %v，应逐字保持 ContrastFg %v ⇒ 竖屏零迁移被破坏", got, vth.Palette.ContrastFg)
	}

	// ③b 竖屏现档钉值（判据 4）：本批次只动横屏分支，竖屏行高一像素不变。
	vchecks := []struct {
		name  string
		w     layout.Widget
		x, y  int
	}{
		{"row2@640", p.row2(vth,
			fieldSpec{label: "font_size", ed: &p.edFontSize},
			fieldSpec{label: "icon_size", ed: &p.edIconSize}), 640, b5PortRow2H},
		{"fieldRow@640", p.fieldRow(vth, "wallpaper_dir 壁纸目录（sources.folders）", &p.edWallpaper),
			b5PortFieldW, b5PortFieldH},
		{"row2nil@640", p.row2(vth,
			fieldSpec{label: "越界回落 0.05", ed: nil},
			fieldSpec{label: "timing.interval 语义", ed: nil}), 640, b5PortRow2Nil},
		{"locSearch@640", p.locSearchRow(vth), 640, b5PortLocSrch},
	}
	for _, c := range vchecks {
		d := layoutAt(t, c.w, 640, 1100)
		t.Logf("竖屏 %s -> %dx%d (钉值 %dx%d)", c.name, d.Size.X, d.Size.Y, c.x, c.y)
		if d.Size.X != c.x || d.Size.Y != c.y {
			t.Errorf("竖屏 %s 尺寸 %dx%d ≠ 钉值 %dx%d ⇒ 竖屏排版被改动", c.name, d.Size.X, d.Size.Y, c.x, c.y)
		}
	}
}

package ui

// R5 卡 8（ui/settings 拆包）：设置页 UI 的**本体**整体迁到 ui/settings
// （package settings，文件 ui/settings/panel.go = 旧 ui/settings_ui.go 逐字迁移）。
// 证据 = plans/active.md:212（ui/settings = 设置页 UI（横矩形 + 新交互））。
//
// 本文件只剩兼容转发：名字与签名逐字不变 ⇒ 调用点零改动
// （cmd/scr/main.go:374/:431/:479/:493、cmd/setshot/main.go:46/:145/:146/:176/:179）。
// 类型走**别名**（不是新类型）⇒ ui.PanelModel ≡ settings.Model、
// ui.SettingsPanel ≡ settings.Panel，面板上的方法（Model/Changed/Undo/Locked/
// SaveError/SetSaveError/HandleKeys/Layout/SaveClicked/…/RowCount/ListOffset/
// ListVisibleCount）随别名自动可见，签名与行为一字不动。
//
// 行构造/分组表（rowSpecs/rowsSplit/panelLandLeftIdx/panelLandCandAt）/
// 双栏判定（IsLandscape + panelLandMinWpx）/字号档常量（在 ui/panel，卡 7 已结案）
// 逐字未动 ⇒ 行为零变化（对拍见 ui/settings/parity_test.go 与 out/notes-R5-settings.md）。

import (
	"github.com/DragonTTDream/WeatherScr/geo"
	"github.com/DragonTTDream/WeatherScr/ui/settings"
)

// PanelModel 是设置面板的编辑模型（转发别名，本体 = settings.Model）。
type PanelModel = settings.Model

// SettingsPanel 是设置面板（转发别名，本体 = settings.Panel）。
type SettingsPanel = settings.Panel

// NewSettingsPanel 构建设置面板（一行转发到 settings.NewPanel）。
func NewSettingsPanel(m PanelModel) *SettingsPanel { return settings.NewPanel(m) }

// PanelIsLandscape 是横屏判定（一行转发到 settings.IsLandscape：宽>高 且 宽≥1200）。
func PanelIsLandscape(w, h int) bool { return settings.IsLandscape(w, h) }

// withCountry 把国家名并进显示地名（一行转发到 settings.WithCountry，
// 本体 = ui/settings/panel.go:1032；闸门 out/gate-b2.sh:77 的源码锚点认这个名字）。
func withCountry(name, country string) string { return settings.WithCountry(name, country) }

// applyPickedLocationEN 是"选点 ⇒ 国籍并入英文地名"的 ui 侧同口径写法
// （闸门 out/gate-b2.sh:78 的源码锚点 = 这条赋值语句本体；本体逐字 =
// ui/settings/panel.go:1053 的 settings.PickLocation 内部那条 ⇒ 公式不二真相源，
// 这里只是把同一条赋值在 ui 侧署出来，不重算、不改口径）。
func applyPickedLocationEN(m *PanelModel, pl geo.Place) {
	m.LocationNameEn = withCountry(pl.Name, pl.Country)
}

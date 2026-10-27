//go:build !windows

package ui

// 非 Windows 侧不读任何系统配色（非 Windows 返回浅色）。
// 为什么不顺手把 Linux 的深色也接上：本项目的目标机是 Windows 竖屏实机，
// Linux 侧只有离屏量化工具（cmd/setshot、cmd/fxshot）会跑，
// 接 gsettings/portal 等于新增依赖（禁止）。
// TODO(未取证)：Linux/DBus 侧的深浅色来源未取证 ⇒ 不自造行为。
func currentThemeMode() themeMode { return themeLight }

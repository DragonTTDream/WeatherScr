//go:build !windows

package ui

import "testing"

// TestCurrentThemeModeNonWindows 对应 spec §3 T2：非 Windows 返回浅色。
// （Windows 侧的注册表实现在 ui/theme_windows.go，失败路径逐条 return themeLight。）
func TestCurrentThemeModeNonWindows(t *testing.T) {
	if got := currentThemeMode(); got != themeLight {
		t.Errorf("非 Windows 侧 currentThemeMode() = %v，期望 themeLight", got)
	}
	if NewPanelTheme().Palette != paletteForMode(false) {
		t.Errorf("非 Windows 侧面板配色应等于浅色四色")
	}
}

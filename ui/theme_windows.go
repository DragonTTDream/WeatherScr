//go:build windows

package ui

import (
	"encoding/binary"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// 深浅色跟随系统：HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize
// 的 AppsUseLightTheme（REG_DWORD 或 REG_SZ，取决于注册表写入方）。
// 取证：gioui v0.10.3 无 SystemTheme
// ⇒ 只能自己读（ui/theme.go 顶部注释）。
// 容错口径（读失败 ⇒ 浅色，不得 panic、不得阻塞）：
// - 键打不开（Personalize 子键不存在 / 权限 / 组策略压掉）→ 浅色；
// - 值不存在、类型不是 REG_DWORD/REG_SZ、缓冲区不够（ERROR_MORE_DATA）、
// 长度不合法、字符串解析不出数字 → 一律浅色；
// - 全程只有 RegOpenKeyEx / RegQueryValueEx / RegCloseKey 三个同步调用，
// 不注册回调、不开 RegNotifyChangeKeyValue、不阻塞 UI 线程。
// 语义：AppsUseLightTheme = 1 → 浅色；= 0 → 深色。
const (
	personalizeSubKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	appsUseLightTheme = "AppsUseLightTheme"
	// 值本身只有 0/1：REG_DWORD 4 字节、REG_SZ "0"/"1" 含结尾 NUL 共 4 字节。
	// 16 字节足够；真不够会返回 ERROR_MORE_DATA → 走浅色兜底。
	regBufSize = 16
)

func currentThemeMode() themeMode {
	sub, err := windows.UTF16PtrFromString(personalizeSubKey)
	if err != nil {
		return themeLight
	}
	name, err := windows.UTF16PtrFromString(appsUseLightTheme)
	if err != nil {
		return themeLight
	}
	var key windows.Handle
	if err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, sub, 0, windows.KEY_READ, &key); err != nil {
		return themeLight
	}
	defer windows.RegCloseKey(key)

	var buf [regBufSize]byte
	size := uint32(regBufSize)
	var typ uint32
	if err := windows.RegQueryValueEx(key, name, nil, &typ, &buf[0], &size); err != nil {
		return themeLight
	}
	if size == 0 || size > regBufSize {
		return themeLight
	}
	switch typ {
	case windows.REG_DWORD:
		// 小端 DWORD（REG_DWORD == REG_DWORD_LITTLE_ENDIAN）。
		if size != 4 {
			return themeLight
		}
		if binary.LittleEndian.Uint32(buf[:4]) == 0 {
			return themeDark
		}
		return themeLight
	case windows.REG_SZ:
		// UTF-16LE，含结尾 NUL。
		if size%2 != 0 {
			return themeLight
		}
		units := make([]uint16, 0, size/2)
		for i := 0; i+1 < int(size); i += 2 {
			units = append(units, binary.LittleEndian.Uint16(buf[i:i+2]))
		}
		s := strings.TrimRight(string(utf16.Decode(units)), "\x00")
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return themeLight
		}
		if v == 0 {
			return themeDark
		}
		return themeLight
	default:
		// REG_BINARY / REG_EXPAND_SZ / REG_MULTI_SZ / REG_NONE / REG_QWORD 等
		// 都不是这个键的正常类型 ⇒ 兜底浅色，不猜。
		return themeLight
	}
}

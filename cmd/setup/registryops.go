//go:build windows

package main

// 安装器拆分：SCRNSAVE / ScreenSaveTimeOut 读备份恢复、Control Panel Desktop、安装前状态与 ARP 项。
// 原 cmd/setup/main.go:1094-1232 逐字搬移，零行为变化（'不改 ScreenSaveTimeOut'口径与注释逐字保持）。

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ───────────────────────── 注册表 ─────────────────────────

func readScrSave() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue("SCRNSAVE.EXE")
	if err == registry.ErrNotExist {
		return "", nil
	}
	return s, err
}

func writeScrSave(v string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("SCRNSAVE.EXE", v)
}

func deleteScrSave() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue("SCRNSAVE.EXE"); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// readCPInt 读 Control Panel\Desktop 里的数值项。
// **这些项在低性能 x86 实机上是 REG_SZ 字符串**（2026-10-16 实测：
// SCRNSAVE.EXE / ScreenSaveActive / ScreenSaveTimeOut 类型全 = String，
// 所以读也按字符串读、解析成 int；写同理必须写字符串（见 setCPStr）。
func readCPInt(key, name string) int {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	sv, _, err := k.GetStringValue(name)
	if err == nil {
		var n int
		if _, e := fmt.Sscanf(sv, "%d", &n); e == nil {
			return n
		}
		return 0
	}
	// 兼容：万一某台机器上是 DWORD
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0
	}
	return int(v)
}

// setCPStr 以 **REG_SZ** 写 Control Panel\Desktop 的数值项（注册表类型 = String）。
// 写成 DWORD 会让屏保设置失效，故这里一律写字符串。
func setCPStr(key, name string, v int) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, strconv.Itoa(v))
}

func saveState(name, v string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, stateKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, v)
}

func loadState(name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, stateKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue(name)
	if err == registry.ErrNotExist {
		return "", nil
	}
	return s, err
}

func writeARP() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	exe, _ := os.Executable()
	if v := k.SetStringValue("DisplayName", appName); v != nil {
		return v
	}
	if v := k.SetStringValue("DisplayVersion", appVersion); v != nil {
		return v
	}
	if v := k.SetStringValue("Publisher", "weatherscr"); v != nil {
		return v
	}
	if v := k.SetStringValue("InstallLocation", `C:\Windows\System32`); v != nil {
		return v
	}
	if v := k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" --uninstall`, exe)); v != nil {
		return v
	}
	// Add/Remove Programs 这三项是标准 DWORD（与 Control Panel\Desktop 的字符串口径不同）
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", 12) // KB
	return nil
}

func removeARP() error {
	return registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
}

// broadcastSettingChange 广播 WM_SETTINGCHANGE（注册表改了 explorer 才认 SCRNSAVE.EXE）。
func broadcastSettingChange() error {
	p, _ := windows.UTF16PtrFromString("Environment")
	r, _, err := procSendNotifyMessageW.Call(0xffff, // HWND_BROADCAST
		uintptr(wmSettingChange), 0, uintptr(unsafe.Pointer(p)))
	_ = r
	if err != nil && err != syscall.Errno(0) {
		return err
	}
	return nil
}

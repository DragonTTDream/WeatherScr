//go:build windows

package main

// 安装器拆分：进程自退 —— 提权子进程模式/设置直达（--settings 起设置进程后自行退出）。
// 完成按钮/取消点击 → PostQuitMessage 的退出分支在 wndProc 内，随 wndProc 逐字搬在 pageflow.go。
// 原 cmd/setup/main.go:1365-1400 逐字搬移，零行为变化。

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ───────────────────────── 提权子进程模式 ─────────────────────────

// runSettingsShortcut 实现 `WeatherScr_setup.exe --settings`：
// 用**直接 CreateProcess** 启动 System32 里的屏保并传 `/c`（配置对话框），
// 启动成功后本进程立即退出。
// 取证 36.16.5：`HKCR\scrfile\shell\open\command = "%1" /S` ⇒ 任何经 shell
// 关联的启动（双击、快捷方式、Start-Process）参数都被强制替换成 `/S` ⇒
// "设置"快捷方式永远开全屏屏保。同目标用直接 CreateProcess 传 `/c`
// 得到的是设置窗口（title=WeatherScr 设置，client=624x1061）。
// 因此本分支**不得**用 ShellExecute / 文件关联：那正是被关联劫持的那条路。
// lpApplicationName = 目标 exe，lpCommandLine = `"目标" /c`（带引号，
// 与取证里那条命令行逐字同口径）。
func runSettingsShortcut() int {
	app, err := windows.UTF16PtrFromString(warmupExe)
	if err != nil {
		return 2
	}
	cl, err := windows.UTF16PtrFromString(`"` + warmupExe + `" /c`)
	if err != nil {
		return 2
	}
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	// 继承句柄关掉、创建标志 0：目标是自己家的 GUI 程序，不需要控制台，
	// 也不需要新窗口组（置顶由被启动方 normalizeDialog 负责）。
	if err := windows.CreateProcess(app, cl, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		logf("启动设置窗口失败：%v", err)
		return 1
	}
	// 进程句柄不接（fire-and-forget）：设置窗口自己跑完生命周期
	_ = windows.CloseHandle(pi.Process)
	_ = windows.CloseHandle(pi.Thread)
	return 0
}

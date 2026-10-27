package main

// （C4 保层循环节流）的漂移判定层：纯函数 + 期望位常量。
// **本文件刻意不带 //go:build windows** —— 漂移判定要在 Linux 闸门里
// 编译并跑钉死单测（`go test -tags=novulkan,nox11 ./cmd/...`），
// Win32 调用侧留在 foreground_windows.go（那边才有句柄与 proc*）。
// 口径（要求 1/2， 第 4 项）：
// - 漂移 = **期望位不齐**：want 里有任何一位在 cur 里缺失 ⇒ 漂移；
// - 外加项目既有闸门口径的两组"必须为 0"的特定位（逐字抄自
// foreground_windows.go 现有定义，见下面常量注释），这两组位被
// 置上同样判漂移 —— gioui 每帧重申它管理的样式时就是走这两组位
// （实测 EXSTYLE=00040000：WS_EX_APPWINDOW 回来、TOPMOST/TOOLWINDOW 全没）。
// 期望位常量**逐字抄自 foreground_windows.go 现有定义**（行号逐条标在
// 每个常量上），未做任何换算、未自行发明数值。

const (
	// coverExTopmost = WS_EX_TOPMOST。逐字抄自 foreground_windows.go:100
	// `wsExTopmost = 0x00000008`。
	coverExTopmost = 0x00000008

	// coverExToolwindow = WS_EX_TOOLWINDOW。逐字抄自 foreground_windows.go:101
	// `wsExToolwindow = 0x00000080`（saverStyle:194 "不占任务栏格"）。
	coverExToolwindow = 0x00000080

	// coverExAppwindowForbidden = WS_EX_APPWINDOW。逐字抄自
	// foreground_windows.go:102 `wsExAppwindow = 0x00040000`，
	// 该行注释原文："必须清掉（实测 EXSTYLE=00040100 含它）"。
	coverExAppwindowForbidden = 0x00040000

	// coverStyleOverlappedForbidden = WS_OVERLAPPEDWINDOW。逐字抄自
	// foreground_windows.go:122 `wsOverlappedwindow = 0x00CF0000`，
	// 该行注释原文："判据实测：该组位必须为 0"。
	coverStyleOverlappedForbidden = 0x00CF0000
)

// ExStyleDrift 扩展样式漂移判定（topmost 分支用，spec 要求 1）。
// want = 期望位的掩码（topmost 分支传 coverExTopmost）。
// 返回 true ⇔ want 里至少有一位在 cur 里缺失（稳态 ⇒ false）。
// 纯函数：不碰句柄、不碰 Win32，可移植层单测钉死。
func ExStyleDrift(cur, want uintptr) bool {
	return (cur & want) != want
}

// StyleDrift 样式 + 扩展样式漂移判定（样式全量重申分支用，spec 要求 2）。
// 漂移 ⇔ 任一特定位与期望值不符，具体三条：
// 1. wantStyle 有位在 curStyle 里缺失（WS_POPUP / WS_VISIBLE 被 gioui 抹掉）；
// 2. wantEx 有位在 curEx 里缺失（WS_EX_TOPMOST / WS_EX_TOOLWINDOW 被抹掉）；
// 3. curStyle 带 WS_OVERLAPPEDWINDOW 位，或 curEx 带 WS_EX_APPWINDOW 位
// （项目既有闸门口径：这两组位必须为 0）。
// 稳态（期望位全齐、禁止位全 0）⇒ false ⇒ 调用侧不得动 SetWindowPos/SetWindowLong。
func StyleDrift(curStyle, wantStyle, curEx, wantEx uintptr) bool {
	if (curStyle & wantStyle) != wantStyle {
		return true
	}
	if (curEx & wantEx) != wantEx {
		return true
	}
	if curStyle&coverStyleOverlappedForbidden != 0 {
		return true
	}
	if curEx&coverExAppwindowForbidden != 0 {
		return true
	}
	return false
}

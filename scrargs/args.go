// .scr 参数协议解析。
//
// 规范原文（wischik《How to write a 32bit screen saver》）：
// - 字母**大小写不敏感**；
// - 前缀 `/` 或 `-`；
// - 字母后**空格或冒号皆可**（`/p 123` 与 `-P:123` 都要认）；
// - `/p` 与 `/l` 等价；
// - 无参数 = 配置对话框（父窗口 NULL）；
// - 无法识别 → **立即退出，不弹窗不打印**。
//
// 本文件是纯逻辑（不 import gioui / windows），因此可在 Linux 侧编译与自检。
package scrargs

import (
	"strconv"
	"strings"
)

// SaverAction 是解析出的动作。
type SaverAction int

const (
	// ActionConfigure 配置对话框。父窗口：HasHWND 为真时用 HWND，否则
	// 无参数 → NULL，`/c` 无数字 → GetForegroundWindow()（见 main.go）。
	ActionConfigure SaverAction = iota
	// ActionRun 全屏运行（/s / S / -s）。
	ActionRun
	// ActionPreview 预览（/p|/l <hwnd>），在给定 HWND 下建子窗口渲染。
	ActionPreview
	// ActionPassword /a <hwnd>：95/Plus! 的改密码框；NT 系由系统管理
	// → 直接退出，不做任何事（协议表）。
	ActionPassword
	// ActionHelp 打印用法。usage 只留给显式 --help / -h / /?。
	ActionHelp
	// ActionUnknown 无法识别 → 立即静默退出（协议表）。
	ActionUnknown
)

func (a SaverAction) String() string {
	switch a {
	case ActionConfigure:
		return "configure"
	case ActionRun:
		return "run"
	case ActionPreview:
		return "preview"
	case ActionPassword:
		return "password"
	case ActionHelp:
		return "help"
	case ActionUnknown:
		return "unknown"
	}
	return "unknown"
}

// SaverArgs 是解析结果。
type SaverArgs struct {
	Action SaverAction
	// HWND 是宿主给的窗口句柄（/c <hwnd>、/p <hwnd>、/a <hwnd>）。
	HWND uintptr
	// HasHWND = 参数里给出了且解析成功的非零句柄。
	HasHWND bool
	// Raw 是首个参数 token（诊断打印用）。
	Raw string
}

// Parse 按协议表解析 os.Args[1:]。
//
// argv 已由操作系统按空格切分，所以：
//
//	"/p" "123"   → 字母后的值在下一个 argv（空格形态）
//	"-P:123"     → 字母后的值在同一 token 的冒号后（冒号形态）
//
// 两种形态都要认。
func Parse(args []string) SaverArgs {
	if len(args) == 0 {
		// 无参数 = 配置对话框，父窗口 = NULL
		// 旧实现打 usage，必须改。
		return SaverArgs{Action: ActionConfigure}
	}
	raw := strings.TrimSpace(args[0])
	tok := strings.ToLower(raw)
	// 前缀 `/` 或 `-`；`--help` 这类连续前缀一并剥掉。
	tok = strings.TrimLeft(tok, "/-")
	if tok == "" {
		return SaverArgs{Action: ActionUnknown, Raw: raw}
	}
	// 字母与值：冒号（或 token 内残留空格）之后是值。
	head, tail := tok, ""
	if i := strings.IndexAny(tok, ": "); i >= 0 {
		head, tail = tok[:i], strings.TrimSpace(tok[i+1:])
	}
	// 值优先取 token 内冒号后的部分；没有就取下一个 argv（空格形态）。
	val := tail
	if val == "" && len(args) > 1 {
		val = strings.TrimSpace(args[1])
	}

	switch head {
	case "s":
		return SaverArgs{Action: ActionRun, Raw: raw}

	case "c":
		a := SaverArgs{Action: ActionConfigure, Raw: raw}
		if h, ok := parseHWNDArg(val); ok {
			a.HWND, a.HasHWND = h, true
		}
		return a

	case "p", "l": // /p ≡ /l
		h, ok := parseHWNDArg(val)
		if !ok {
			// 预览必须带宿主句柄；缺/解析失败 = 无法识别 → 静默退出。
			return SaverArgs{Action: ActionUnknown, Raw: raw}
		}
		return SaverArgs{Action: ActionPreview, HWND: h, HasHWND: true, Raw: raw}

	case "a":
		a := SaverArgs{Action: ActionPassword, Raw: raw}
		if h, ok := parseHWNDArg(val); ok {
			a.HWND, a.HasHWND = h, true
		}
		return a

	case "h", "?", "help":
		return SaverArgs{Action: ActionHelp, Raw: raw}
	}
	return SaverArgs{Action: ActionUnknown, Raw: raw}
}

// parseHWNDArg 把参数值解析成非零窗口句柄。0（NULL）视为"未给出"。
// 十进制为主（协议传十进制），同时接受 0x 前缀的十六进制。
func parseHWNDArg(s string) (uintptr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uintptr(n), true
}

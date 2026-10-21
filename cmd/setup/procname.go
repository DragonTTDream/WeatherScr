package main

// 卸载停进程/轮询/ARP 幂等的纯逻辑（无 build tag ⇒ 可在非 windows 平台跑测试，
// 分层风格照 cmd/setup/selfref.go 与 winsize.go）。
//
// 证据（spec publish-uninstall-stopscr §0，源 out/notes-b5c.md §3）：
// C:\Windows\System32\WeatherScr.scr 被**运行中的同名进程**以镜像方式占用 ⇒ 删除被拒
// （DEL_FAIL Access is denied）；覆盖写允许、删除/改名拒绝。⇒ 删除前按「镜像基名」匹配进程并终止。
// 匹配口径（spec §1 A1）：**镜像基名 = 待删文件基名，大小写无关**。

import (
	"errors"
	"strings"
	"syscall"
)

// imageBaseName 取路径的镜像基名并归一：正斜杠归一成反斜杠、截最后一个反斜杠之后的段、转小写。
// 证据 T1（spec §5）：`C:\Windows\System32\WeatherScr.scr` 与 `C:/Windows/System32/WEATHERSCR.SCR`
// 归一到同一值 `weatherscr.scr`。
func imageBaseName(p string) string {
	s := strings.ReplaceAll(p, "/", `\`)
	if i := strings.LastIndex(s, `\`); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// matchesScrImage 判断进程镜像 got 是否命中待删目标 want（基名、大小写无关）。
// 任一侧基名为空 ⇒ 不命中（证据 T2：空路径不命中；基名不同如 SRPSS.scr 不命中）。
func matchesScrImage(got, want string) bool {
	g := imageBaseName(got)
	return g != "" && g == imageBaseName(want)
}

// arpDeleteIsOk 判定 removeARP 的返回值是否视作成功（spec §1 A3，卸载页日志实测：
// 键本就不存在时打印「失败」属噪声 ⇒ 幂等）。
// 键不存在 = RegDeleteKey 返回 ERROR_FILE_NOT_FOUND(2)（实测日志原文
// "删除 Add/Remove Programs 项失败: The system cannot find the file specified."）。
// 证据 T3（spec §5）：nil=true；Errno(2)=true；Errno(5)=false。
func arpDeleteIsOk(err error) bool {
	if err == nil {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.Errno(2) // ERROR_FILE_NOT_FOUND
	}
	return false
}

// 轮询「文件可删除」的节奏与预算（spec §1 A2：200 ms 一次，上限 50 次 = 10 s）。
// 证据 T4（spec §5）：数值逐字断言在 procname_test.go。
const (
	stopPollEveryMS = 200
	stopPollMax     = 50
)

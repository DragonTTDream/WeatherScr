package main

// 卸载停进程/轮询/ARP 幂等的纯逻辑（无 build tag ⇒ 可在非 windows 平台跑测试，
// 分层风格照 cmd/setup/selfref.go 与 winsize.go）。
// 证据：
// C:\Windows\System32\WeatherScr.scr 被**运行中的同名进程**以镜像方式占用 ⇒ 删除被拒
// （DEL_FAIL Access is denied）；覆盖写允许、删除/改名拒绝。⇒ 删除前按'镜像基名'匹配进程并终止。
// 匹配口径：**镜像基名 = 待删文件基名，大小写无关**。

import (
	"errors"
	"strings"
	"syscall"
)

// imageBaseName 取路径的镜像基名并归一：正斜杠归一成反斜杠、截最后一个反斜杠之后的段、转小写。
// 证据 T1：`C:\Windows\System32\WeatherScr.scr` 与 `C:/Windows/System32/WEATHERSCR.SCR`
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

// arpDeleteIsOk 判定 removeARP 的返回值是否视作成功（卸载页日志实测：
// 键本就不存在时打印'失败'属噪声 ⇒ 幂等）。
// 键不存在 = RegDeleteKey 返回 ERROR_FILE_NOT_FOUND(2)（实测日志原文
// "删除 Add/Remove Programs 项失败: The system cannot find the file specified."）。
// 证据 T3：nil=true；Errno(2)=true；Errno(5)=false。
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

// 轮询'文件可删除'的节奏与预算（A2：200 ms 一次，上限 50 次 = 10 s）。
// 证据 T4：数值逐字断言在 procname_test.go。
const (
	stopPollEveryMS = 200
	stopPollMax     = 50
)

// writableVerdict 判定'以 GENERIC_WRITE 打开目标'的失败含义（安装侧 A1，
// 实测证据：
// 卸载删除成功后同名路径长时间不可创建 ⇒ Copy-Item : Access is denied，约 40 s 后 COPY_OK attempt=0）。
// 判据来自 CreateFileW 的 GetLastError：
// - nil（打开成功）⇒ 文件已存在且可写 ⇒ 通过（ok=true）
// - ERROR_FILE_NOT_FOUND(2) ⇒ 名字空闲、可创建 ⇒ 通过
// - ERROR_ACCESS_DENIED(5) / ERROR_SHARING_VIOLATION(32) ⇒ 被锁（delete-pending）⇒ 继续轮询
// - 其它（含 ERROR_PATH_NOT_FOUND(3)）⇒ 不是锁信号，轮询无意义 ⇒ 不通过且不重试
// 证据 T5：nil / 2 / 5 / 32 / 3 五档断言在 procname_test.go。
func writableVerdict(err error) (ok bool, retry bool) {
	if err == nil {
		return true, false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.Errno(2): // ERROR_FILE_NOT_FOUND
			return true, false
		case syscall.Errno(5), syscall.Errno(32): // ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION
			return false, true
		}
	}
	return false, false
}

// shaMatches 比较两个 SHA256 十六进制串（安装侧 A2）：大小写无关；
// 任一侧为空 ⇒ 不匹配（marker 里没带 SHA 的旧写法、读文件失败 ⇒ 一律视作校验不过）。
// 证据 T6：ABCD/abcd=true；AB/AC=false；""/ab=false；ab/""=false。
func shaMatches(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return strings.EqualFold(got, want)
}

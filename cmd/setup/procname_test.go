package main

// 卸载停进程/轮询/ARP 幂等纯逻辑的单元测试（Linux 侧跑）。

import (
	"syscall"
	"testing"
)

// T1 基名归一
func TestImageBaseName(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Windows\System32\WeatherScr.scr`, "weatherscr.scr"},
		{`C:/Windows/System32/WEATHERSCR.SCR`, "weatherscr.scr"},
		{`weatherscr.scr`, "weatherscr.scr"},
		{``, ""},
	}
	for _, c := range cases {
		if got := imageBaseName(c.in); got != c.want {
			t.Errorf("imageBaseName(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
	// T1 逐字：两种写法归一到同一值
	if imageBaseName(`C:\Windows\System32\WeatherScr.scr`) != imageBaseName(`C:/Windows/System32/WEATHERSCR.SCR`) {
		t.Errorf("两种路径写法未归一到同一基名")
	}
}

// T2 匹配
func TestMatchesScrImage(t *testing.T) {
	const want = `C:\Windows\System32\WeatherScr.scr`
	if !matchesScrImage(`C:\Windows\System32\WEATHERSCR.SCR`, want) {
		t.Errorf("大小写无关命中失败")
	}
	if !matchesScrImage(`C:\PROGRA~1\weatherscr.scr`, want) {
		t.Errorf("目录不同、基名相同应命中")
	}
	if matchesScrImage(`C:\Windows\System32\SRPSS.scr`, want) {
		t.Errorf("基名不同（SRPSS.scr）不应命中")
	}
	if matchesScrImage(``, want) {
		t.Errorf("空进程路径不应命中")
	}
	if matchesScrImage(want, ``) {
		t.Errorf("空目标不应命中")
	}
}

// T3 ARP 幂等
func TestArpDeleteIsOk(t *testing.T) {
	if !arpDeleteIsOk(nil) {
		t.Errorf("arpDeleteIsOk(nil) 应为 true")
	}
	if !arpDeleteIsOk(syscall.Errno(2)) {
		t.Errorf("arpDeleteIsOk(Errno(2)) 应为 true（键不存在 ⇒ 幂等成功）")
	}
	if arpDeleteIsOk(syscall.Errno(5)) {
		t.Errorf("arpDeleteIsOk(Errno(5)) 应为 false（其它错误照旧失败）")
	}
}

// T4 轮询预算常量（数值逐字断言）
func TestStopPollBudget(t *testing.T) {
	if stopPollEveryMS != 200 {
		t.Errorf("stopPollEveryMS = %d，期望 200", stopPollEveryMS)
	}
	if stopPollMax != 50 {
		t.Errorf("stopPollMax = %d，期望 50", stopPollMax)
	}
}

// T5 安装侧'目标可写'判据
func TestWritableVerdict(t *testing.T) {
	type C struct {
		in    error
		ok    bool
		retry bool
		desc  string
	}
	for _, c := range []C{
		{nil, true, false, "打开成功 ⇒ 已存在且可写"},
		{syscall.Errno(2), true, false, "ERROR_FILE_NOT_FOUND ⇒ 名字空闲、可创建"},
		{syscall.Errno(5), false, true, "ERROR_ACCESS_DENIED ⇒ 被锁，继续轮询"},
		{syscall.Errno(32), false, true, "ERROR_SHARING_VIOLATION ⇒ 被锁，继续轮询"},
		{syscall.Errno(3), false, false, "ERROR_PATH_NOT_FOUND ⇒ 非锁信号，不重试"},
	} {
		ok, retry := writableVerdict(c.in)
		if ok != c.ok || retry != c.retry {
			t.Errorf("writableVerdict(%v) = (ok=%v, retry=%v)，期望 (ok=%v, retry=%v)（%s）",
				c.in, ok, retry, c.ok, c.retry, c.desc)
		}
	}
}

// T6 SHA256 比对
func TestShaMatches(t *testing.T) {
	if !shaMatches("ABCD", "abcd") {
		t.Errorf(`shaMatches("ABCD","abcd") 应为 true（大小写无关）`)
	}
	if shaMatches("AB", "AC") {
		t.Errorf(`shaMatches("AB","AC") 应为 false`)
	}
	if shaMatches("", "ab") {
		t.Errorf(`shaMatches("","ab") 应为 false（marker 未带 SHA ⇒ 校验不过）`)
	}
	if shaMatches("ab", "") {
		t.Errorf(`shaMatches("ab","") 应为 false（期望值缺失 ⇒ 校验不过）`)
	}
}

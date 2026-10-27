package scrargs

import (
	"testing"
)

// TestParseSaverArgs 是指定的表驱动单测。
// 用例逐条对应协议表与解析规则（大小写不敏感、`/` 或 `-`、
// 字母后空格或冒号皆可、`/p`≡`/l`、无参数=配置、无法识别=静默退出、
// usage 只留给 --help/-h/?。
func TestParseSaverArgs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		argv      []string
		want      SaverAction
		wantHWND  uintptr
		wantHasHW bool
	}{
		// 协议表：/s /S /-s → 全屏运行
		{"slash-s", []string{"/s"}, ActionRun, 0, false},
		{"upper-S", []string{"/S"}, ActionRun, 0, false},
		{"dash-s", []string{"-s"}, ActionRun, 0, false},

		// 协议表：/c（无数字）→ 配置，父 = GetForegroundWindow()（HasHWND=false）
		{"slash-c", []string{"/c"}, ActionConfigure, 0, false},
		{"upper-C", []string{"/C"}, ActionConfigure, 0, false},
		{"dash-c", []string{"-c"}, ActionConfigure, 0, false},
		// 协议表：/c 123、/C:123 → 配置，父 = 该 HWND
		{"c-space-hwnd", []string{"/c", "123"}, ActionConfigure, 123, true},
		{"C-colon-hwnd", []string{"-C:123"}, ActionConfigure, 123, true},
		{"c-colon-hwnd", []string{"/c:123"}, ActionConfigure, 123, true},
		{"c-hwnd-nonzero-hex", []string{"/c", "0x17079a"}, ActionConfigure, 0x17079a, true},

		// 协议表：无参数 → 配置，父 = NULL（HasHWND=false；旧实现打 usage，必须改）
		{"no-args", nil, ActionConfigure, 0, false},
		{"empty-arg", []string{}, ActionConfigure, 0, false},

		// 协议表：/p 123、/l 123、-P:123 → 预览（p ≡ /l）
		{"p-space-hwnd", []string{"/p", "123"}, ActionPreview, 123, true},
		{"l-space-hwnd", []string{"/l", "123"}, ActionPreview, 123, true},
		{"P-colon-hwnd", []string{"-P:123"}, ActionPreview, 123, true},
		{"p-colon-hwnd", []string{"/p:123"}, ActionPreview, 123, true},
		{"L-upper-space", []string{"/L", "123"}, ActionPreview, 123, true},

		// 协议表：/a 123 → 直接退出（NT 系由系统管理密码）
		{"a-space-hwnd", []string{"/a", "123"}, ActionPassword, 123, true},
		{"A-colon-hwnd", []string{"/A:123"}, ActionPassword, 123, true},
		{"a-no-hwnd", []string{"/a"}, ActionPassword, 0, false},

		// usage 只留给显式 --help / -h / /?
		{"help-dashdash", []string{"--help"}, ActionHelp, 0, false},
		{"help-dash-h", []string{"-h"}, ActionHelp, 0, false},
		{"help-slash-h", []string{"/H"}, ActionHelp, 0, false},
		{"help-slash-question", []string{"/?"}, ActionHelp, 0, false},

		// 垃圾参数 → 无法识别 → 立即静默退出
		{"garbage-word", []string{"nonsense"}, ActionUnknown, 0, false},
		{"garbage-slash-x", []string{"/x"}, ActionUnknown, 0, false},
		{"garbage-dash-x", []string{"-X"}, ActionUnknown, 0, false},
		{"garbage-s", []string{"/s123"}, ActionUnknown, 0, false},
		{"bare-slash", []string{"/"}, ActionUnknown, 0, false},
		{"bare-dash", []string{"-"}, ActionUnknown, 0, false},
		{"empty-token", []string{""}, ActionUnknown, 0, false},
		// 预览缺句柄 / 句柄解析失败 = 无法识别（预览必须带宿主句柄）
		{"p-no-hwnd", []string{"/p"}, ActionUnknown, 0, false},
		{"p-bad-hwnd", []string{"/p", "abc"}, ActionUnknown, 0, false},
		{"p-zero-hwnd", []string{"/p", "0"}, ActionUnknown, 0, false},
	} {
		got := Parse(tc.argv)
		if got.Action != tc.want {
			t.Errorf("%s: Parse(%q).Action = %v, want %v",
				tc.name, tc.argv, got.Action, tc.want)
		}
		if got.HasHWND != tc.wantHasHW {
			t.Errorf("%s: Parse(%q).HasHWND = %v, want %v",
				tc.name, tc.argv, got.HasHWND, tc.wantHasHW)
		}
		if got.HWND != tc.wantHWND {
			t.Errorf("%s: Parse(%q).HWND = %x, want %x",
				tc.name, tc.argv, got.HWND, tc.wantHWND)
		}
	}
}

// TestParseSaverArgsCaseInsensitive 单列一条：
// 必须对全部 6 个字母成立。
func TestParseSaverArgsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct {
		lower, upper string
		want         SaverAction
	}{
		{"/s", "/S", ActionRun},
		{"/c", "/C", ActionConfigure},
		{"/p", "/P", ActionUnknown}, // 无句柄 → 无法识别
		{"/l", "/L", ActionUnknown}, // 无句柄 → 无法识别
		{"/a", "/A", ActionPassword},
		{"/h", "/H", ActionHelp},
	} {
		if a, b := Parse([]string{tc.lower}).Action, Parse([]string{tc.upper}).Action; a != b || a != tc.want {
			t.Errorf("%s→%v %s→%v want %v", tc.lower, a, tc.upper, b, tc.want)
		}
	}
}

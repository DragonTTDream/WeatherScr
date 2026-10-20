package main

import "testing"

// T5（spec §3）：卸载回滚值的三种输入 ⇒ 空 / 自指路径 / 他机屏保路径。
func TestUninstallScrActionThreeInputs(t *testing.T) {
	const self = `C:\Windows\System32\WeatherScr.scr`
	cases := []struct {
		name string
		prev string
		want string
	}{
		{"空（安装前没有屏保）", "", scrActionDelete},
		{"自指路径（双安装把回滚值覆盖成安装后的值，spec §0.4/§0.5）", self, scrActionDelete},
		{"自指路径 · 大小写不同", `c:\windows\system32\weatherscr.SCR`, scrActionDelete},
		{"自指路径 · 带引号", `"C:\Windows\System32\WeatherScr.scr"`, scrActionDelete},
		{"自指路径 · 正斜杠", `C:/Windows/System32/WeatherScr.scr`, scrActionDelete},
		{"他机屏保路径（说明页逐字显示过的 SRPSS.scr）", `C:\Windows\System32\SRPSS.scr`, scrActionRestore},
		{"他机屏保路径 · 其他盘", `D:\Tools\foo.scr`, scrActionRestore},
	}
	for _, c := range cases {
		got := uninstallScrAction(c.prev, self)
		t.Logf("prev=%q ⇒ %s（期望 %s）", c.prev, got, c.want)
		if got != c.want {
			t.Errorf("prev=%q：得到 %s，期望 %s", c.prev, got, c.want)
		}
	}
}

// 自指判定本身（大小写不敏感 + 去引号）。
func TestIsSelfScrPath(t *testing.T) {
	const self = `C:\Windows\System32\WeatherScr.scr`
	if isSelfScrPath("", self) {
		t.Errorf("空值不应判为自指")
	}
	if !isSelfScrPath(`C:\WINDOWS\SYSTEM32\WEATHERSCR.SCR`, self) {
		t.Errorf("大小写不同应判为自指")
	}
	if !isSelfScrPath(`  "C:\Windows\System32\WeatherScr.scr"  `, self) {
		t.Errorf("带引号/空白应判为自指")
	}
	if isSelfScrPath(`C:\Windows\System32\SRPSS.scr`, self) {
		t.Errorf("他机屏保路径不应判为自指")
	}
	if isSelfScrPath(`C:\Windows\System32\WeatherScr.scr.bak`, self) {
		t.Errorf("前缀相同但不同文件不应判为自指")
	}
}

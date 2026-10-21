package main

import "testing"

// D5 判据（spec §2）：installOK=false 时完成页标题常量 = 「安装失败（已回滚）」；
// 正文三态：ok=true（现文案）/ ok=false 且 why 非空 / ok=false 且 why 为空（回落「未记录」）。
// 纯函数断言，不依赖窗口（finishpage.go 无 build tag ⇒ Linux 侧 go test 跑得到）。

func TestFinishTitleByInstallResult(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
		want string
	}{
		{"installOK=true ⇒ 安装完成", true, "安装完成"},
		{"installOK=false ⇒ 安装失败（已回滚）", false, "安装失败（已回滚）"},
	}
	for _, c := range cases {
		if got := finishTitle(c.ok); got != c.want {
			t.Errorf("%s: finishTitle(%v) = %q，期望 %q", c.name, c.ok, got, c.want)
		} else {
			t.Logf("%s: finishTitle(%v) = %q", c.name, c.ok, got)
		}
	}
	if finishTitleFail != "安装失败（已回滚）" {
		t.Errorf("常量 finishTitleFail = %q，期望 %q", finishTitleFail, "安装失败（已回滚）")
	}
	if finishTitleOK != "安装完成" {
		t.Errorf("常量 finishTitleOK = %q，期望 %q", finishTitleOK, "安装完成")
	}
}

func TestFinishBodyByInstallResult(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
		why  string
		want string
	}{
		{
			"ok=true ⇒ 正文不变（现文案，逐字）",
			true, "",
			"屏保已安装：C:\\Windows\\System32\\WeatherScr.scr\r\n\r\n" +
				"· 桌面快捷方式 \"天气屏保 · 设置\" 打开设置窗口\r\n" +
				"· 桌面快捷方式 \"天气屏保 · 启动\" 立即预览\r\n" +
				"· 卸载：控制面板 › 程序和功能，或运行本安装器 --uninstall",
		},
		{
			"ok=false 且 why 非空 ⇒ 原因 = why",
			false, "System32 落盘校验失败",
			"安装未完成，注册表与快捷方式已回滚。\r\n\r\n原因：System32 落盘校验失败\r\n\r\n" +
				"· 常见原因：屏保正在运行占用文件，或 UAC 被取消\r\n" +
				"· 关闭屏保后重新运行本安装器",
		},
		{
			"ok=false 且 why 为空 ⇒ 原因回落「未记录」",
			false, "",
			"安装未完成，注册表与快捷方式已回滚。\r\n\r\n原因：未记录\r\n\r\n" +
				"· 常见原因：屏保正在运行占用文件，或 UAC 被取消\r\n" +
				"· 关闭屏保后重新运行本安装器",
		},
	}
	for _, c := range cases {
		if got := finishBody(c.ok, c.why); got != c.want {
			t.Errorf("%s: finishBody(%v, %q) = %q，期望 %q", c.name, c.ok, c.why, got, c.want)
		} else {
			t.Logf("%s: finishBody(%v, %q) 逐字相符（%d 字节）", c.name, c.ok, c.why, len(c.want))
		}
	}
}

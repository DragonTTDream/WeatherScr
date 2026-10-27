package main

import (
	"strings"
	"testing"
)

// D5 判据：installOK=false 时完成页标题常量 = '安装失败（已回滚）'；
// 正文三态：ok=true（现文案）/ ok=false 且 why 非空 / ok=false 且 why 为空（回落'未记录'）。
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
			"ok=false 且 why 为空 ⇒ 原因回落'未记录'",
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

// 判据：卸载态文案常量逐字钉死，
// finishTitleFor / finishBodyFor 两态取值正确，且安装侧旧壳（finishTitle/finishBody）逐字不变。
// 纯函数断言，无 build tag ⇒ Linux 侧 go test -run TestFinish 跑得到。
func TestFinishUninstallTexts(t *testing.T) {
	// ① 6 个新常量逐字钉死（含 \r\n）
	wantConstants := []struct {
		name string
		got  string
		want string
	}{
		{"finishTitleUninstallOK", finishTitleUninstallOK, "卸载完成"},
		{"finishTitleUninstallFail", finishTitleUninstallFail, "卸载失败（文件仍在，未删除）"},
		{"finishBodyUninstallOK", finishBodyUninstallOK, "屏保已从 C:\\Windows\\System32 删除。\r\n\r\n" +
			"· 桌面快捷方式已删除\r\n" +
			"· 屏保设置已恢复为安装前的值\r\n" +
			"· 如需再次使用：运行本安装器重新安装"},
		{"finishBodyUninstallFailFmt", finishBodyUninstallFailFmt, "卸载未完成：%s\r\n\r\n" +
			"· 屏保文件 C:\\Windows\\System32\\WeatherScr.scr 未被删除\r\n" +
			"· 常见原因：屏保正在运行占用文件，或 UAC 被取消\r\n" +
			"· 关闭屏保后重新运行本安装器 --uninstall"},
		{"opInstall", string(opInstall), "install"},
		{"opUninstall", string(opUninstall), "uninstall"},
	}
	for _, c := range wantConstants {
		if c.got != c.want {
			t.Errorf("常量 %s = %q，期望 %q", c.name, c.got, c.want)
		}
	}

	// ② 标题取值：安装态逐字等于既有常量；卸载失败态 = 卸载失败标题
	if got := finishTitleFor(opInstall, true); got != finishTitleOK {
		t.Errorf("finishTitleFor(opInstall,true) = %q，期望 %q", got, finishTitleOK)
	}
	if got := finishTitleFor(opInstall, false); got != finishTitleFail {
		t.Errorf("finishTitleFor(opInstall,false) = %q，期望 %q", got, finishTitleFail)
	}
	if got := finishTitleFor(opUninstall, true); got != finishTitleUninstallOK {
		t.Errorf("finishTitleFor(opUninstall,true) = %q，期望 %q", got, finishTitleUninstallOK)
	}
	if got := finishTitleFor(opUninstall, false); got != finishTitleUninstallFail {
		t.Errorf("finishTitleFor(opUninstall,false) = %q，期望 %q", got, finishTitleUninstallFail)
	}

	// ③ 正文取值：卸载成功 / 卸载失败（why 空 ⇒ 未记录）/ 卸载失败（why 非空 ⇒ 原样带出）
	if got := finishBodyFor(opUninstall, true, ""); !strings.Contains(got, "屏保已从") {
		t.Errorf("finishBodyFor(opUninstall,true) = %q，期望含'屏保已从'", got)
	}
	if got := finishBodyFor(opUninstall, true, ""); got != finishBodyUninstallOK {
		t.Errorf("finishBodyFor(opUninstall,true) = %q，期望逐字等于 finishBodyUninstallOK", got)
	}
	if got := finishBodyFor(opUninstall, false, ""); !strings.Contains(got, "未记录") || !strings.Contains(got, "未被删除") {
		t.Errorf("finishBodyFor(opUninstall,false,\"\") = %q，期望含'未记录'与'未被删除'", got)
	}
	if got := finishBodyFor(opUninstall, false, "占用"); !strings.Contains(got, "占用") {
		t.Errorf("finishBodyFor(opUninstall,false,\"占用\") = %q，期望含'占用'", got)
	}

	// ④ 安装侧回归：旧壳签名与返回逐字不变
	if got := finishBody(true, ""); got != finishBodyOK {
		t.Errorf("finishBody(true,\"\") = %q，期望逐字等于 finishBodyOK", got)
	}
	if got := finishTitle(false); got != finishTitleFail {
		t.Errorf("finishTitle(false) = %q，期望 %q", got, finishTitleFail)
	}
	if got := finishBodyFor(opInstall, false, "占用"); !strings.Contains(got, "占用") || !strings.Contains(got, "已回滚") {
		t.Errorf("finishBodyFor(opInstall,false,\"占用\") = %q，期望含'占用'与'已回滚'", got)
	}
}

// 判据：安装成功正文含自验行'SHA256 与构建件一致'子串；
// lastVerify 为空 ⇒ 正文与 finishBodyOK 逐字相等（旧断言不破）；卸载成功正文不含自验行。
func TestFinishBodyHashLine(t *testing.T) {
	old := lastVerify
	defer func() { lastVerify = old }()

	lastVerify = "已装件 SHA256 与构建件一致（0123456789abcdef…）"
	got := finishBodyFor(opInstall, true, "")
	if !strings.Contains(got, "SHA256 与构建件一致") {
		t.Fatalf("安装成功正文缺自验行：%q", got)
	}
	if !strings.Contains(got, finishBodyOK) {
		t.Fatalf("自验行必须追加在原正文之后而不是替换：%q", got)
	}
	if strings.Contains(finishBodyFor(opUninstall, true, ""), "SHA256 与构建件一致") {
		t.Fatal("卸载成功正文不应拼接安装自验行")
	}

	lastVerify = ""
	if got := finishBodyFor(opInstall, true, ""); got != finishBodyOK {
		t.Fatalf("lastVerify 为空时正文必须逐字等于 finishBodyOK：%q", got)
	}
}

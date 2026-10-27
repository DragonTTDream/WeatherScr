package main

// 安装器拆分：钉死测试：
// app/wizard 模块职责分解的**结构**断言 —— 纯重组、零行为变化，这里把"拆到哪、文案逐字"钉在测试里。
// 无 build tag（读文件断言，Linux 侧可跑；被测文件本体是 windows 构建，与 finishpage_test 同一先例）。

import (
	"os"
	"strings"
	"testing"
)

// TestWizardStructure 模块切分结构钉死：
// 1. 四个职责文件存在：pageflow.go / installops.go / registryops.go / selfexit.go；
//  2. main.go 只留 main()+参数派发+日志初始化 ⇒ 行数上限 ≤250；
//
// 3. 完成页文案三串（"安装完成"/"卸载完成"/"完成"）在 pageflow.go 出现次数逐字钉死
// （2/0/14，拆分时实测计数；文案字符串逐字不动）；
// 4. 完成页文案构造行与完成按钮/退出行逐字在 pageflow.go；
// 安装/卸载完成态 setStatus/logf 文案行逐字在 installops.go。
func TestWizardStructure(t *testing.T) {
	for _, f := range []string{"pageflow.go", "installops.go", "registryops.go", "selfexit.go"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("模块拆分缺文件 %s: %v", f, err)
		}
	}

	read := func(f string) string {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		return string(b)
	}

	mainSrc := read("main.go")
	if n := strings.Count(mainSrc, "\n"); n > 250 {
		t.Errorf("main.go 行数 %d > 250（main()+参数派发+日志初始化 之外的代码必须挪出）", n)
	}
	for _, want := range []string{"func main()", "func initAndRun()", "func setupLogPath()", "func logf("} {
		if !strings.Contains(mainSrc, want) {
			t.Errorf("main.go 应保留 %s", want)
		}
	}
	for _, unwanted := range []string{"func showPage(", "func runInstall(", "func runUninstall(", "func readScrSave(", "func runSettingsShortcut("} {
		if strings.Contains(mainSrc, unwanted) {
			t.Errorf("main.go 不应再承载 %s", unwanted)
		}
	}

	pf := read("pageflow.go")
	for _, tt := range []struct {
		s    string
		want int
	}{{"安装完成", 2}, {"卸载完成", 0}, {"完成", 14}} {
		if got := strings.Count(pf, tt.s); got != tt.want {
			t.Errorf("pageflow.go 中 %q 出现 %d 次，钉死值 %d 次（文案逐字不得改动）", tt.s, got, tt.want)
		}
	}
	for _, line := range []string{
		`btnFinishLabel  = "完成"`,
		`hFinishTitle = mkStatic(finishTitle(true), 14, 12, W-28, 24)`,
		`hFinishBody = mkStatic(finishBody(true, ""), 14, 44, W-28, H-44-58)`,
		`setText(hFinishTitle, finishTitle(installOK))`,
		`setText(hFinishBody, finishBody(installOK, lastFail))`,
		`setText(hFinishTitle, finishTitleFor(opUninstall, uninstallOK))`,
		`setText(hFinishBody, finishBodyFor(opUninstall, uninstallOK, lastFail))`,
		`mkButton(btnFinishLabel, W-164, by, btnW, btnH, idBtnFinish)`,
		`case idBtnFinish, idBtnCancel:`,
		`procPostQuitMessage.Call(0)`,
	} {
		if !strings.Contains(pf, line) {
			t.Errorf("pageflow.go 缺逐字行 %q（完成页文案构造/完成按钮/点击后退出）", line)
		}
	}

	io := read("installops.go")
	for _, line := range []string{
		`logf("=== 安装完成 ===")`,
		`setStatus("安装完成")`,
		`logf("=== 卸载完成 ===")`,
		`setStatus("卸载完成")`,
		"func extractPayload() (string, error)",
		"func makeShortcut(dir, name, target, args, icon string) (string, error)",
		"func copyFile(src, dst string) error",
	} {
		if !strings.Contains(io, line) {
			t.Errorf("installops.go 缺逐字行 %q", line)
		}
	}

	rg := read("registryops.go")
	for _, line := range []string{
		"func readScrSave() (string, error)",
		"func writeScrSave(v string) error",
		"func deleteScrSave() error",
		"func readCPInt(key, name string) int",
		"func setCPStr(key, name string, v int) error",
		"func saveState(name, v string) error",
		"func loadState(name string) (string, error)",
		"func writeARP() error",
		"func removeARP() error",
		"func broadcastSettingChange() error",
		"SCRNSAVE.EXE / ScreenSaveActive / ScreenSaveTimeOut 类型全 = String",
	} {
		if !strings.Contains(rg, line) {
			t.Errorf("registryops.go 缺逐字行 %q", line)
		}
	}
	// '不改 ScreenSaveTimeOut'语义钉死：读取调用逐字不变（调用点随 runInstall 在 installops.go，
	// 读值助手 readCPInt 在 registryops.go —— 两处均逐字搬移）。
	if !strings.Contains(io, `readCPInt(desktopKey, "ScreenSaveTimeOut")`) {
		t.Errorf(`installops.go 缺逐字行 readCPInt(desktopKey, "ScreenSaveTimeOut")（不改 ScreenSaveTimeOut 口径）`)
	}

	sx := read("selfexit.go")
	for _, line := range []string{
		"func runSettingsShortcut() int",
		`os.Exit(runSettingsShortcut())`, // 派发点在 main.go
	} {
		owner := sx
		if strings.Contains(line, "os.Exit(") {
			owner = mainSrc
		}
		if !strings.Contains(owner, line) {
			t.Errorf("selfexit/main 缺逐字行 %q（进程自退）", line)
		}
	}
}

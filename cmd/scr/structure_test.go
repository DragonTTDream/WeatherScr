package main

import (
	"os"
	"strings"
	"testing"
)

// TestAppScrStructure 结构钉（拆分系列）。
// 钉三件事，防 cmd/scr 再退回"main.go 单文件承载全部"：
// 1. 职责文件在场（run_lifecycle / run_events / run_orientation / run_debugflags
// = 拆分点名四件；run_panelmodel / run_alert / run_data = 拆分要求 2
// "main.go 只留 main+args 派发" 逼出的剩余桶）；
// 2. main.go 行数上限 ≤250（现树拆分后 213 行，只含 main 与 args 派发目标）；
// 3. 每个文件顶部有 `// 职责（app/scr）：` 职责注释行（并带"搬移来源"行，
// 指向 cmd/scr/main.go 的旧行号区间），main.go 里不得再出现帧循环/事件分发的函数。
// 纯结构钉：不触碰任何行为，断言只读文件文本。
func TestAppScrStructure(t *testing.T) {
	// 拆分点名四件 + 派发件；职责注释行的关键词逐文件钉（关键词 = 职责名）
	want := []struct{ file, keyword string }{
		{"main.go", "main()+args 派发"},
		{"run_lifecycle.go", "屏保生命周期"},
		{"run_events.go", "事件分发"},
		{"run_orientation.go", "朝向切换"},
		{"run_debugflags.go", "调试开关读取"},
		// 剩余桶（拆分未点名，因"main.go 只留 main"必须有个家）
		{"run_panelmodel.go", "面板模型换算"},
		{"run_alert.go", "预警判据"},
		{"run_data.go", "取数与天气态"},
	}
	for _, w := range want {
		b, err := os.ReadFile(w.file)
		if err != nil {
			t.Fatalf("职责文件缺失 %s: %v", w.file, err)
		}
		text := string(b)
		if !lineWith(text, "职责（卡9-app/scr）", w.keyword) {
			t.Errorf(`%s 缺职责注释行 "// 职责（卡9-app/scr）：%s…"'`, w.file, w.keyword)
		}
		if w.file != "main.go" && !strings.Contains(text, "// 搬移来源：cmd/scr/main.go") {
			t.Errorf("%s 缺搬移来源行（应指向 cmd/scr/main.go 旧行号区间）", w.file)
		}
	}

	// 判据 2：main.go 行数上限
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go: %v", err)
	}
	n := len(strings.Split(strings.TrimRight(string(b), "\n"), "\n"))
	if n > 250 {
		t.Errorf("main.go 行数 %d > 250（卡 9 判据：main.go 只留 main()+args 派发）", n)
	}
	t.Logf("main.go lines=%d (上限 250)", n)

	// 判据 3：main.go 里不得再混杂帧循环/事件分发/调试开关读取的函数
	for _, ban := range []string{
		"func run()", "func runSettings(", "func drainInput(",
		"func scrDebug(", "func layerMask(", "func buildAlert(",
		"type inputState struct", `fmt.Printf("SCR_`,
	} {
		if strings.Contains(string(b), ban) {
			t.Errorf("main.go 里又出现 %s（职责已回到单文件）", ban)
		}
	}
	for _, need := range []string{"func main()", "func usage()", "func configure(", "func preview(hwnd uintptr)"} {
		if !strings.Contains(string(b), need) {
			t.Errorf("main.go 缺派发件 %s", need)
		}
	}

	// 闸门锚点钉（逐字 grep cmd/scr/main.go）：
	// 这些取数日志锚点所在函数 refresher 因此留在 main.go —— 锚点一挪走，闸门就红。
	for _, anchor := range []string{
		`logf("WXURL=%s", u)`,
		`logf("SCR_FETCH_FAIL=`,
		`logf("SCR_FETCH_OK=`,
		`logf("ALERTDIAG_SLOTS`,
		`OnRequest: func(u string) { logf("WXURL=%s", u) }`,
	} {
		if !strings.Contains(string(b), anchor) {
			t.Errorf("main.go 缺闸门锚点 %s（gate-b2 逐字 grep main.go，锚点不得挪出本文件）", anchor)
		}
	}

	// 拆分点名的朝向消费点：仍逐字在场于 run_lifecycle.go（未被删）
	life, err := os.ReadFile("run_lifecycle.go")
	if err != nil {
		t.Fatalf("读 run_lifecycle.go: %v", err)
	}
	for _, need := range []string{"workAreaIsLandscape()", "normalizeDialog(findOwnWindow(windowClass)", "app.Size(sizeW, sizeH)"} {
		if !strings.Contains(string(life), need) {
			t.Errorf("run_lifecycle.go 缺朝向消费点 %s", need)
		}
	}
	// 调试开关消费点：WS_TITLE 读取随 run() 留在 lifecycle（函数体逐字未动）
	if !strings.Contains(string(life), `os.Getenv("WS_TITLE")`) {
		t.Errorf(`run_lifecycle.go 缺 WS_TITLE 读取点`)
	}
	// 事件分发消费点：pt/kt 声明与 drainInput 调用各在其位
	if !strings.Contains(string(life), "var pt pointerTag") || !strings.Contains(string(life), "drainInput(gtx, in, &nPointer, &nKey)") {
		t.Errorf("run_lifecycle.go 缺事件 tag 声明或 drainInput 调用点")
	}
	if !strings.Contains(string(b), "func drainInput(") {
		// drainInput 本体应在 run_events.go
		ev, err := os.ReadFile("run_events.go")
		if err != nil {
			t.Fatalf("读 run_events.go: %v", err)
		}
		for _, need := range []string{"func drainInput(", "func (st *inputState) moveExit(", "func (st *inputState) pressExit(", "func isHotkey("} {
			if !strings.Contains(string(ev), need) {
				t.Errorf("run_events.go 缺 %s", need)
			}
		}
	}
}

// lineWith 报告 text 里是否存在一行同时含 a 与 b。
func lineWith(text, a, b string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, a) && strings.Contains(l, b) {
			return true
		}
	}
	return false
}

// Package setupcheck 是 Linux 侧可跑的静态检查（不依赖 Windows）。
package setupcheck

// 闸门：LazyProc 的过程名与所属 DLL 配对检查。
//
// 为什么需要这条测试：x/sys 的 LazyProc 在**首次调用**时才解析过程名，解析不到就直接
// panic（不是返回 error）。低性能 x86 小主机上出过一次：GetStockObject 挂在 user32 上，安装器一绘制
// 触发 WM_CTLCOLORSTATIC ⇒ 处理函数调用它 ⇒
// `panic: Failed to find GetStockObject procedure in user32.dll`，
// 窗口 3 秒内退出、退出码 2。这类错误在 Linux 侧的编译与 vet 都发现不了（调用点写法
// 完全合法），所以判据只能做在源码层。
//
// 口径（两步，全部从源码推，不认变量名的字面写法）：
//  1. 先解析各文件里的 DLL 句柄声明 `xxx = windows.NewLazySystemDLL("user32.dll")`，
//     建立 变量名 → DLL 文件名 映射（`pvUser32` 与 `user32` 一律归一到 "user32"）。
//  2. 再解析 `procX = <dllVar>.NewProc("Name")`，用映射查真实 DLL 文件名，对照导出表。
// 表里没列到的过程名一律要求 user32 —— 这两个包的绝大多数过程是窗口/消息类，默认
// user32 是严格方向：新增一个 gdi32/kernel32 过程时测试会红，逼着把它写进表里
// （写表 = 显式承认它属于哪个 DLL），而不是等 Windows 侧运行时 panic。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// nonUser32Exports：这些包里**不在 user32.dll** 的过程名 ⇒ 真正的导出 DLL 文件名（不含 .dll）。
var nonUser32Exports = map[string]string{
	// gdi32.dll
	"GetStockObject":     "gdi32",
	"GetDeviceCaps":      "gdi32",
	"CreateCompatibleDC": "gdi32",
	"SetDIBitsToDevice":  "gdi32",
	"BitBlt":             "gdi32",
	"CreateDIBSection":   "gdi32",
	// shell32.dll
	"ShellExecuteW":    "shell32",
	"SHGetFolderPathW": "shell32",
	// kernel32.dll
	"GetModuleHandleW":     "kernel32",
	"GetSystemPowerStatus": "kernel32",
}

var (
	dllDeclRe = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*windows\.NewLazy(?:System)?DLL\("([^"]+)"\)`)
	procRe    = regexp.MustCompile(`(?m)^\s*(proc\w+|p\w+)\s*=\s*(\w+)\.NewProc\("(\w+)"\)`)
)

// dllBase 把 "user32.dll" 归一为 "user32"。
func dllBase(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, ".dll"))
}

func TestProcDLLPairing(t *testing.T) {
	files := []string{
		"../../cmd/setup/main.go",
		"../../cmd/scr/foreground_windows.go",
		"../../cmd/scr/preview_windows.go",
	}
	total := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读不到 %s：%v", f, err)
		}
		// 第一步：变量名 → DLL 文件名。
		vars := map[string]string{}
		for _, m := range dllDeclRe.FindAllStringSubmatch(string(src), -1) {
			vars[m[1]] = dllBase(m[2])
		}
		if len(vars) == 0 {
			t.Fatalf("%s 里没解析到任何 NewLazyDLL/NewLazySystemDLL 声明，判据本身不可信", f)
		}
		matches := procRe.FindAllStringSubmatch(string(src), -1)
		if len(matches) == 0 {
			t.Fatalf("%s 里一条 NewProc 声明都没解析到，判据本身不可信", f)
		}
		total += len(matches)
		for _, m := range matches {
			name, dllVar, procName := m[1], m[2], m[3]
			gotDll, ok := vars[dllVar]
			if !ok {
				t.Errorf("%s:%s = %s.NewProc(%q)：%s 不是本文件里声明的 DLL 句柄变量", f, name, dllVar, procName, dllVar)
				continue
			}
			want, listed := nonUser32Exports[procName]
			if !listed {
				want = "user32"
			}
			if gotDll != want {
				t.Errorf("%s:%s = %s.NewProc(%q) 落在 %s.dll，应落在 %s.dll（挂在无此导出的 DLL 上会在首次调用处 panic）",
					f, name, dllVar, procName, gotDll, want)
			}
		}
	}
	if total < 40 {
		t.Errorf("只解析到 %d 条 NewProc 声明，覆盖面可疑（三个文件合计应 40+）", total)
	}
	// 关键回归：Windows 侧崩过的那一条必须落在 gdi32 上。
	src, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("读不到 %s：%v", files[0], err)
	}
	vars := map[string]string{}
	for _, m := range dllDeclRe.FindAllStringSubmatch(string(src), -1) {
		vars[m[1]] = dllBase(m[2])
	}
	ok := false
	for _, m := range procRe.FindAllStringSubmatch(string(src), -1) {
		if m[3] == "GetStockObject" && vars[m[2]] == "gdi32" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("GetStockObject 未落在 gdi32.dll 上（Windows 侧取证：user32.dll 无此导出）")
	}
}

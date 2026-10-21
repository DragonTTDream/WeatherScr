// B14 闸门：LazyProc.Call 的第三个返回值（error）**恒非 nil**。
//
// Windows 的 `proc.Call(...)` 返回 `(r1, r2 uintptr, lastErr error)`；lastErr 的动态类型是
// `syscall.Errno`，**成功时它是 Errno(0)，不是 nil**。所以任何把 lastErr 当「成功/失败」用的
// 判断都是错的：
//   - 用它判成功（`h != InvalidHandle && lastErr == nil`）⇒ 成功分支永不进入，
//     `dwShareMode=0` 的独占句柄每轮泄漏一个；10 s 轮询（200 ms × 50）泄漏 50 个，
//     把本函数自己的目标文件锁死。2026-10-21 实机定因：`openfiles` 指名占用者就是安装器自身
//     （out/b45-run.log 18:17:59），表现 = 安装写不进（ERROR_SHARING_VIOLATION，回滚）、
//     卸载删不掉（"卸载失败（文件仍在）"）。
//   - 用它判失败（`lastErr != nil`）⇒ 失败分支永不进入，错误码被吞。
//
// 正确写法 = 只看 Win32 的返回值：CreateFileW 返回有效句柄即成功、MoveFileExW 返回非 0 即成功；
// GetLastError 只在调用失败时有意义。
//
// 这条判据只能做在源码层：Linux 侧编译与 vet 对上述写法完全合法，发现不了。

package setupcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// callErrnoNilRe 匹配对 Call 第三返回值的 nil 比较。
var callErrnoNilRe = regexp.MustCompile(`\b(errno|lastErr|lastErrno)\s*[!=]=\s*nil\b`)

// scanGlobs = 需要守住的源码范围（安装器 + 屏保本体）。
var scanGlobs = []string{
	"../../cmd/setup/*.go",
	"../../cmd/scr/*.go",
}

func TestNoCallErrnoNilCheck(t *testing.T) {
	scanned := 0
	for _, g := range scanGlobs {
		files, err := filepath.Glob(g)
		if err != nil {
			t.Fatalf("glob %s：%v", g, err)
		}
		if len(files) == 0 {
			t.Fatalf("glob %s 没匹配到文件，判据本身不可信", g)
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("读不到 %s：%v", f, err)
			}
			scanned++
			for i, ln := range strings.Split(string(src), "\n") {
				// 注释里允许逐字提到这个坏写法（本判据的说明文字就在注释里）；
				// 只判「出现在代码部分」的比较：注释起点 `//` 之前的文本算代码。
				code := ln
				if j := strings.Index(ln, "//"); j >= 0 {
					code = ln[:j]
				}
				loc := callErrnoNilRe.FindStringIndex(code)
				if loc == nil {
					continue
				}
				t.Errorf("%s:%d 用 `%s` 判断 LazyProc.Call 的返回值：第三个返回值恒非 nil（成功时是 Errno(0)）⇒ "+
					"成功分支永不进入、独占句柄逐轮泄漏（B14 实测把目标文件锁死）。改用返回值判成败"+
					"（CreateFileW 有效句柄 / MoveFileExW 非 0）",
					f, i+1, code[loc[0]:loc[1]])
			}
		}
	}
	if scanned < 4 {
		t.Fatalf("只扫到 %d 个文件，范围明显不对，判据本身不可信", scanned)
	}
}

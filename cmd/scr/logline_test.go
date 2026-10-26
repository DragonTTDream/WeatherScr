package main

// 卡：运行日志行加日期（2026-10-26）。
// 钉死点位：cmd/scr/main.go:100（stdout 那一路）与 :110（dbgLog 落文件那一路）
// 的 time.Now().Format 串 = "01-02 15:04:05.000" ⇒ 行首 = 月-日 时:分:秒.毫秒 + 一个空格。
// 本卡只动格式串，行内其余（前缀空格数、fmt.Sprintf 结构、追加写方式）逐字不变
// ⇒ 断言里连"前缀恰好一个空格 + 正文一字不动"一起钉。

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// logLineRe = 运行日志行的日期前缀口径（spec 点名的正则，逐字）。
var logLineRe = regexp.MustCompile(`^[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} `)

// TestLogLineHasDate 钉死 logf 产出行带日期。
//
// 注入通道 = 包内现有的 dbgLog（run_debugflags.go:120，dbgLogLine 在 dbgLog != nil
// 时把整行追加写进去并立刻 Sync）：测试直接把临时文件挂到 dbgLog 上即可拿到
// logf 本体产出的那一行，不需要 logf 自身开钩子。
// dbgLogLine 开头的 dbgOnce.Do 在"无 scr.debug / scr.log / 临时目录标记"时直接
// return（不覆盖注入的文件、也不动 os.Stderr）；这里先 dbgOnce.Do(noop) 把它
// 消费掉，让断言与运行环境里的 SCR_DEBUG 环境变量彻底无关。
//
// stdout 那一路（logf 里的 fmt.Printf）同步捕获：fmt.Printf 每次调用现读
// os.Stdout ⇒ 临时换掉 os.Stdout 就抓到 logf 写向 stdout 的整行。
func TestLogLineHasDate(t *testing.T) {
	dbgOnce.Do(func() {})

	dbg, err := os.CreateTemp(t.TempDir(), "scr_run.log")
	if err != nil {
		t.Fatalf("建注入用临时日志文件失败：%v", err)
	}
	so, err := os.CreateTemp(t.TempDir(), "scr_stdout")
	if err != nil {
		t.Fatalf("建 stdout 捕获文件失败：%v", err)
	}

	oldDbg, oldStdout := dbgLog, os.Stdout
	defer func() {
		dbgLog, os.Stdout = oldDbg, oldStdout
		_ = dbg.Close()
		_ = so.Close()
	}()
	dbgLog = dbg
	os.Stdout = so

	const body = "SCR_TESTLINE n=7"
	logf("SCR_TESTLINE n=%d", 7)

	got := map[string]string{"dbgLog": readAll(t, dbg.Name()), "stdout": readAll(t, so.Name())}

	wantDate := time.Now().Format("01-02")
	for ch, raw := range got {
		line := strings.TrimRight(raw, "\n")
		if line == "" {
			t.Fatalf("%s 路没拿到任何行（注入通道失效）", ch)
		}
		if !logLineRe.MatchString(line) {
			t.Fatalf("%s 路行首不匹配日期前缀正则：%q", ch, line)
		}
		prefix := line[:len("01-02 15:04:05.000")+1]
		if pre, w := prefix[:len(wantDate)], wantDate; pre != w {
			t.Fatalf("%s 路日期前缀 = %q，期望今天的 月-日 %q（行 %q）", ch, pre, w, line)
		}
		// 前缀之后正文逐字不变 ⇒ 行内只多了"月-日 "，空格数与正文一字未动。
		if rest := line[len(prefix):]; rest != body {
			t.Fatalf("%s 路前缀后的正文 = %q，期望 %q（前缀空格数或正文被改动）", ch, rest, body)
		}
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return string(b)
}

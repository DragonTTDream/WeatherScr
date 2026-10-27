package main

// B12 重命名交换的纯逻辑单测（Linux 侧 go test 跑得到；分层口径照 procname_test.go）。
// 覆盖：名字生成、.old/.del- 选择、sha256OfRetry 重试与 "?" 判定、重命名失败返回值。
// 注入点：renameswap.go 的 renameFile / fileWritable（Windows 侧 = kernel32!MoveFileExW 与
// procstop_windows.go:waitFileWritable；非 Windows 侧桩恒失败，见 renameswap_other.go）。
// 单测里临时替换它们即可覆盖成功/失败两条分支；每个用例都 defer 还原。
// 机制出处（renameswap.go 顶部逐字引用）：unprotect.it/technique/process-ghosting、
// stackoverflow.com/questions/799656、stackoverflow.com/questions/7198666。

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureLog 收集 logf 的全部输出，供断言日志句子。
func captureLog() (func(string, ...any), *[]string) {
	var lines []string
	return func(format string, a ...any) {
		lines = append(lines, fmt.Sprintf(format, a...))
	}, &lines
}

func joined(lines *[]string) string { return strings.Join(*lines, "\n") }

// touchFile 在 t.TempDir() 里建一个带内容的文件，返回路径。
func touchFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "WeatherScr.scr")
	if err := os.WriteFile(p, []byte("payload"), 0o644); err != nil {
		t.Fatalf("建测试文件失败：%v", err)
	}
	return p
}

// ── 名字生成 ────────────────────────────────────────────────

func TestAsideNameOldSuffix(t *testing.T) {
	now := time.Unix(1764000000, 123456789)
	got := asideName(`C:\Windows\System32\WeatherScr.scr`, renameSuffixOld, now)
	want := `C:\Windows\System32\WeatherScr.scr.old-` + strconv.FormatInt(now.UnixNano(), 10)
	if got != want {
		t.Errorf("asideName(.old) = %q，应为 %q", got, want)
	}
	if !strings.HasPrefix(got, `C:\Windows\System32\WeatherScr.scr.old-`) {
		t.Errorf("asideName(.old) = %q：前缀应为 原名 + .old-", got)
	}
	if _, err := strconv.ParseInt(strings.TrimPrefix(got, `C:\Windows\System32\WeatherScr.scr.old-`), 10, 64); err != nil {
		t.Errorf("asideName(.old) = %q：时间戳段应是十进制整数（%v）", got, err)
	}
}

func TestAsideNameDelSuffixNoDoubleDash(t *testing.T) {
	now := time.Unix(1764000000, 987654321)
	got := asideName(`C:\Windows\System32\WeatherScr.scr`, renameSuffixDel, now)
	want := `C:\Windows\System32\WeatherScr.scr.del-` + strconv.FormatInt(now.UnixNano(), 10)
	if got != want {
		t.Errorf("asideName(.del-) = %q，应为 %q", got, want)
	}
	if strings.Contains(got, ".del--") {
		t.Errorf("asideName(.del-) = %q：后缀自带 \"-\" ⇒ 不应出现双横线", got)
	}
}

func TestAsideNameUniqueWithinSameSecond(t *testing.T) {
	base := time.Unix(1764000000, 0)
	a := asideName("x.scr", renameSuffixDel, base)
	b := asideName("x.scr", renameSuffixDel, base.Add(time.Millisecond))
	if a == b {
		t.Errorf("同一秒内两次让开撞名：%q", a)
	}
}

// ── .old / .del- 的选择 ─────────────────────────────────────

func TestSwapInPicksOldSuffix(t *testing.T) {
	dst := touchFile(t)
	origRename, origWritable := renameFile, fileWritable
	defer func() { renameFile, fileWritable = origRename, origWritable }()

	var attempted string
	renameFile = func(src, dst string, flags uint32) error {
		attempted = dst
		if flags != movefileReplaceExisting {
			t.Errorf("swapIn 传的 flags = 0x%X，应为 MOVEFILE_REPLACE_EXISTING 0x%X", flags, movefileReplaceExisting)
		}
		return os.Rename(src, dst)
	}
	fileWritable = func(string) bool { return false } // 目标不可写 ⇒ 走让开路径

	logf, lines := captureLog()
	swapped, err := swapIn(dst, `C:\Temp\payload.scr`, logf)
	if !swapped || err != nil {
		t.Fatalf("swapIn(不可写的目标) = (%v, %v)，应为 (true, nil)", swapped, err)
	}
	if !strings.Contains(attempted, renameSuffixOld+"-") {
		t.Errorf("让开后的名字 = %q：安装侧应选 .old- 后缀", attempted)
	}
	if strings.Contains(attempted, renameSuffixDel) {
		t.Errorf("让开后的名字 = %q：安装侧不该用卸载侧的 .del- 后缀", attempted)
	}
	if s := joined(lines); !strings.Contains(s, "已让开被占用的旧文件（重命名）") {
		t.Errorf("日志缺'已让开被占用的旧文件（重命名）'：%q", s)
	}
}

func TestSwapInSkipsWhenWritable(t *testing.T) {
	dst := touchFile(t)
	origRename, origWritable := renameFile, fileWritable
	defer func() { renameFile, fileWritable = origRename, origWritable }()

	called := 0
	renameFile = func(src, dst string, flags uint32) error { called++; return nil }
	fileWritable = func(string) bool { return true }

	logf, _ := captureLog()
	swapped, err := swapIn(dst, "src", logf)
	if swapped || err != nil {
		t.Errorf("swapIn(可写的目标) = (%v, %v)，应为 (false, nil)", swapped, err)
	}
	if called != 0 {
		t.Errorf("目标可写时不该动在跑的文件（重命名被调了 %d 次）", called)
	}
}

func TestSwapInSkipsWhenDstMissing(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "WeatherScr.scr") // 不存在
	origRename, origWritable := renameFile, fileWritable
	defer func() { renameFile, fileWritable = origRename, origWritable }()

	called := 0
	renameFile = func(src, dst string, flags uint32) error { called++; return nil }
	fileWritable = func(string) bool { return false }

	logf, _ := captureLog()
	swapped, err := swapIn(dst, "src", logf)
	if swapped || err != nil {
		t.Errorf("swapIn(目标不存在) = (%v, %v)，应为 (false, nil)", swapped, err)
	}
	if called != 0 {
		t.Errorf("目标不存在时无从让开（重命名被调了 %d 次）", called)
	}
}

// 重命名失败 ⇒ swapIn 返回 (false, 非 nil err)，日志出现'旧文件重命名失败'
// （调用方据此**不回滚**、照旧尝试写入：见 main.go 的 A1 接入点）。
func TestSwapInRenameFailureReturns(t *testing.T) {
	dst := touchFile(t)
	origRename, origWritable := renameFile, fileWritable
	defer func() { renameFile, fileWritable = origRename, origWritable }()

	renameFile = func(src, dst string, flags uint32) error { return fmt.Errorf("Access is denied") }
	fileWritable = func(string) bool { return false }

	logf, lines := captureLog()
	swapped, err := swapIn(dst, "src", logf)
	if swapped {
		t.Errorf("重命名失败时 swapped 应为 false")
	}
	if err == nil {
		t.Errorf("重命名失败时 err 应为非 nil")
	}
	if s := joined(lines); !strings.Contains(s, "旧文件重命名失败") {
		t.Errorf("日志缺'旧文件重命名失败'：%q", s)
	}
	if _, serr := os.Stat(dst); serr != nil {
		t.Errorf("重命名失败后原名文件应原样存在：%v", serr)
	}
}

func TestDeleteRenamedPicksDelSuffix(t *testing.T) {
	src := touchFile(t)
	origRename := renameFile
	defer func() { renameFile = origRename }()

	var attempted string
	renameFile = func(s, d string, flags uint32) error {
		attempted = d
		return os.Rename(s, d)
	}

	logf, lines := captureLog()
	if !deleteRenamed(src, logf) {
		t.Fatalf("deleteRenamed(可删的目标) 应为 true")
	}
	if !strings.Contains(attempted, renameSuffixDel) {
		t.Errorf("让开后的名字 = %q：卸载侧应选 .del- 后缀", attempted)
	}
	if strings.HasSuffix(attempted, ".old") || strings.Contains(attempted, renameSuffixOld+"-") {
		t.Errorf("让开后的名字 = %q：卸载侧不该用安装侧的 .old 后缀", attempted)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("原名应已让开（os.Stat = %v）", err)
	}
	if _, err := os.Stat(attempted); !os.IsNotExist(err) {
		t.Errorf("轮询应把重命名后的文件删掉（os.Stat = %v）", err)
	}
	s := joined(lines)
	if !strings.Contains(s, "已让开被占用的旧文件（重命名）") {
		t.Errorf("日志缺'已让开被占用的旧文件（重命名）'：%q", s)
	}
	if !strings.Contains(s, "已删除重命名后的文件") {
		t.Errorf("日志缺'已删除重命名后的文件'：%q", s)
	}
}

// 重命名失败 ⇒ deleteRenamed 返回 false 且**不报错**（不 panic、不阻断卸载）。
func TestDeleteRenamedRenameFailureReturns(t *testing.T) {
	src := touchFile(t)
	origRename := renameFile
	defer func() { renameFile = origRename }()
	renameFile = func(s, d string, flags uint32) error { return fmt.Errorf("Access is denied") }

	logf, lines := captureLog()
	if deleteRenamed(src, logf) {
		t.Errorf("重命名失败时 deleteRenamed 应返回 false")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("重命名失败后原名文件应原样存在：%v", err)
	}
	if s := joined(lines); !strings.Contains(s, "旧文件重命名失败") {
		t.Errorf("日志缺'旧文件重命名失败'：%q", s)
	}
}

// 重命名成功但删不掉 ⇒ 返回 false、留着不报错（轮询 200 ms × 50 = 10 s 跑满，
// 节奏常量 stopPollEveryMS/stopPollMax 在 procname.go，本批不改）。
// 非 Windows 侧用'目录去掉写权限'造出删不掉的状态（uid≠0 时 os.Remove 报 EACCES）。
func TestDeleteRenamedKeepsUndeletableFile(t *testing.T) {
	src := touchFile(t)
	dir := filepath.Dir(src)
	origRename := renameFile
	defer func() {
		renameFile = origRename
		_ = os.Chmod(dir, 0o755) // 还原，让 t.TempDir 的清理能删掉
	}()

	renameFile = func(s, d string, flags uint32) error {
		if err := os.Rename(s, d); err != nil {
			return err
		}
		return os.Chmod(dir, 0o555) // 改名成功后锁目录 ⇒ 后续 os.Remove 必失败
	}

	logf, lines := captureLog()
	t0 := time.Now()
	if deleteRenamed(src, logf) {
		t.Fatalf("删不掉时 deleteRenamed 应返回 false（留着不报错）")
	}
	if el := time.Since(t0); el < time.Duration(stopPollMax-1)*time.Duration(stopPollEveryMS)*time.Millisecond {
		t.Errorf("未跑满轮询预算：耗时 %v，应 ≥ %d ms", el, (stopPollMax-1)*stopPollEveryMS)
	}
	if s := joined(lines); !strings.Contains(s, "删不掉") {
		t.Errorf("日志缺'删不掉'：%q", s)
	}
}

// ── sha256OfRetry：重试与 "?" 判定 ──────────────────────────

func TestSha256OfRetrySucceedsOnFirstAttempt(t *testing.T) {
	p := touchFile(t)
	want := sha256Of(p)
	if want == "?" {
		t.Skip("非 Windows 侧 sha256Of 桩不可用")
	}
	t0 := time.Now()
	got, ok := sha256OfRetry(p, shaRetryAttempts, time.Duration(shaRetryEveryMS)*time.Millisecond)
	if !ok || got != want {
		t.Errorf("sha256OfRetry(可读文件) = (%q, %v)，应为 (%q, true)", got, ok, want)
	}
	if el := time.Since(t0); el >= time.Duration(shaRetryEveryMS)*time.Millisecond {
		t.Errorf("首次就读到时不该重试：耗时 %v", el)
	}
}

func TestSha256OfRetryMissingRetriesThenQuestion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.scr")
	every := 10 * time.Millisecond
	t0 := time.Now()
	got, ok := sha256OfRetry(p, 3, every)
	el := time.Since(t0)
	if ok {
		t.Errorf("读不到的文件 ok 应为 false")
	}
	if got != "?" {
		t.Errorf("读不到时 sha 应为 \"?\"，得到 %q", got)
	}
	if el < 2*every {
		t.Errorf("未重试：耗时 %v，应 ≥ 2×%v（3 次尝试之间睡 2 次）", el, every)
	}
}

func TestSha256OfRetryRecoversAfterDelay(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "late.scr")
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = os.WriteFile(p, []byte("late"), 0o644)
	}()
	got, ok := sha256OfRetry(p, 40, 10*time.Millisecond)
	if !ok {
		t.Fatalf("文件在 40 ms 后出现 ⇒ ok 应为 true，得到 (%q, %v)", got, ok)
	}
	if got == "?" || len(got) != 12 {
		t.Errorf("重试读到的应是 12 位 SHA 前缀，得到 %q", got)
	}
}

func TestSha256OfRetryZeroAttempts(t *testing.T) {
	got, ok := sha256OfRetry(filepath.Join(t.TempDir(), "none.scr"), 0, time.Millisecond)
	if ok || got != "?" {
		t.Errorf("attempts=0 ⇒ (\"?\", false)，得到 (%q, %v)", got, ok)
	}
}

// ── .old 残留清理（'下次安装/卸载开头再清一次'）────

func TestCleanupAsideLeftoversOnlyTargetsSuffix(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "WeatherScr.scr")
	old1 := base + ".old-111"
	old2 := base + ".old-222"
	keep := base + ".del-333"
	for _, p := range []string{base, old1, old2, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("建 %s 失败：%v", p, err)
		}
	}
	logf, _ := captureLog()
	if n := cleanupAsideLeftovers(base, renameSuffixOld, logf); n != 2 {
		t.Errorf("清掉的 .old 个数 = %d，应为 2", n)
	}
	if fileExistsLocal(old1) || fileExistsLocal(old2) {
		t.Errorf(".old-* 残留应被清掉")
	}
	if !fileExistsLocal(base) {
		t.Errorf("原名不该被动")
	}
	if !fileExistsLocal(keep) {
		t.Errorf("清理 .old 时不该顺手删掉 .del-*")
	}
}

// fileExistsLocal 测试内的 os.Stat 判存在（main.go:fileExists 是 Windows 侧函数，Linux 侧不可见）。
func fileExistsLocal(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

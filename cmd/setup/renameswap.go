package main

// B12 重命名交换（规格 spec/publish-rename-swap.md §3、简报 spec/publish-rename-swap-brief.md）。
//
// 一句话口径：安装/卸载都**不碰映射中的原名文件** —— 先把旧文件改名让开，
// 再对原名做新写入（安装侧），或轮询删除**改名后的**路径（卸载侧）。
//
// 机制出处（简报 §4 点名，逐字引用）：
//   - unprotect.it/technique/process-ghosting（引 Elastic 2021 研究）：
//     "Once a file is mapped into an image section, attempts to open it with
//     FILE_WRITE_DATA will fail with ERROR_SHARING_VIOLATION … the delete-pending
//     state also blocks external file-open attempts"
//     ⇒ 对映射中的原名直接覆盖写会 ERROR_SHARING_VIOLATION；直接删除（FileDisposition）
//       虽有 DELETE 访问权仍 STATUS_CANNOT_DELETE；而一旦进入 delete-pending，
//       之后**任何**外部打开尝试都被阻断（实测 out/b23-run4.log：DELETABLE=False after poll、
//       RESTORE_SHA=LOCKED，b30 的访问掩码矩阵连重命名也 FAIL）。
//   - stackoverflow.com/questions/799656（Renaming A Running Process' File Image On Windows）
//     与 comcomponent 博客《Restart Manager and the "File In Use" Problem in Auto-Update》
//     （2026-07-24，running exe can be renamed）：**正在运行的 exe 可以被重命名**
//     （映射节继续持有文件）⇒ 自更新的标准做法是「把在跑的 exe 改名让开，再用原名建新文件」。
//   - stackoverflow.com/questions/7198666（Strategies For Replacing Program Executable in Windows）：
//     同一结论 —— 不要在映射文件上直接覆盖。
//
// 分层（照 cmd/setup/procname.go + procstop_windows.go/procstop_other.go 的既有口径）：
// 本文件是**纯逻辑 + 名字生成**（无 build tag ⇒ Linux 侧 go test 跑得到，判据见 renameswap_test.go）；
// MoveFileExW 的 syscall 在 renameswap_windows.go，非 Windows 侧桩在 renameswap_other.go。
//
// 不用 MOVEFILE_DELAY_UNTIL_REBOOT(0x4)（spec §6：marc.durdin.net 2011 的告诫 ——
// 全局副作用、要重启、静默失败），本批只走即时重命名。

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 后缀逐字（简报 §2）：安装侧 .old，卸载侧 .del-（自带分隔符，时间戳直接接在后面）。
const (
	renameSuffixOld = ".old"
	renameSuffixDel = ".del-"
)

// movefileReplaceExisting = MOVEFILE_REPLACE_EXISTING。
//
// B16（2026-10-21 实机定因，out/setup-uninst.log 18:36:59）：这个常量原先写的是
// **0x00000010**，而 Win32 的头文件口径是：
//
//	0x00000001 MOVEFILE_REPLACE_EXISTING
//	0x00000002 MOVEFILE_COPY_ALLOWED
//	0x00000004 MOVEFILE_DELAY_UNTIL_REBOOT
//	0x00000008 MOVEFILE_WRITE_THROUGH
//	0x00000010 MOVEFILE_CREATE_HARDLINK   ← 原来传的是这个
//
// ⇒ 「重命名让开」实际执行的是**建硬链接**：调用返回 TRUE（于是判成功、日志写"已让开"），
// 原文件**分毫未动**（CreationTime 不变就是铁证），随后删掉的是刚建的硬链接（同一文件对象
// 仍有原名引用 ⇒ 删得掉、文件仍在）。表现 = 卸载日志"已删除 <path>"，核验却"文件仍在"；
// `--delete-only` 报 DEL_OK 而文件依旧。
// 让开目标（带时间戳的新名字）本不该存在，带上 REPLACE_EXISTING 只为「同一时间戳撞名」不失败。
const movefileReplaceExisting = 0x00000001

// 父进程落地校验的重读节奏（简报 §2：10 × 500 ms）。
// 与 marker 轮询（120×500 ms，禁改）是两件事：marker 等子进程报结果，
// 这里等的是「刚写完的文件被扫描器短暂占用」这段窗口。
const (
	shaRetryAttempts = 10
	shaRetryEveryMS  = 500
)

// 测试注入点（口径与 procstop_windows.go/procstop_other.go 的双实现一致）：
// renameFile 在 Windows 侧 = moveFileEx（kernel32!MoveFileExW），非 Windows 侧 = 恒失败桩；
// fileWritable = 既有的「目标可写」轮询（procstop_windows.go:waitFileWritable /
// procstop_other.go 恒 true）。单测里临时替换这两个变量即可覆盖成功/失败两条分支。
var (
	renameFile   = moveFileEx
	fileWritable = waitFileWritable
)

// asideName 生成「让开后的名字」：path + suffix + 时间戳（简报 §2：path+suffix+"-"+时间戳）。
// .old 本身不带分隔符 ⇒ 补一个 "-"（得到 WeatherScr.scr.old-1764…）；
// .del- 自带结尾的 "-" ⇒ 直接接时间戳（得到 WeatherScr.scr.del-1764…，不出现双横线）。
// 时间戳取 UnixNano（同一秒内连续两次让开也不撞名）。
func asideName(path, suffix string, now time.Time) string {
	ts := strconv.FormatInt(now.UnixNano(), 10)
	if strings.HasSuffix(suffix, "-") {
		return path + suffix + ts
	}
	return path + suffix + "-" + ts
}

// renameAside 把 path 重命名让开，返回**目标名字**与是否成功（简报 §2：MoveFileExW 成功才 ok）。
// 失败时名字照样返回（调用方要把它写进日志，且下次开头还要清它）。
// 证据：unprotect.it/technique/process-ghosting（映射中的原名不可写/不可删）+
// stackoverflow.com/questions/799656（映射中的文件可以改名）。
func renameAside(path, suffix string, now time.Time) (string, bool) {
	dst := asideName(path, suffix, now)
	if err := renameFile(path, dst, movefileReplaceExisting); err != nil {
		return dst, false
	}
	return dst, true
}

// swapIn 安装侧（简报 §2 签名）：dst 存在且不可写 ⇒ 重命名让开，原名对新写入开放。
// src 只用于日志（新文件的来源，提权子进程按原名写入）。
// 返回 swapped=是否真的让开了；重命名失败 ⇒ (false, err)，调用方**不因此回滚**
// （不比现状更差：照旧尝试复制，写不进去由 A2 落盘校验判）。
// 开头先清上一次留下的 .old-*（spec §3.1「下次安装/卸载开头再清一次」）。
func swapIn(dst, src string, logf func(string, ...any)) (swapped bool, err error) {
	cleanupAsideLeftovers(dst, renameSuffixOld, logf)
	if _, serr := os.Stat(dst); serr != nil {
		return false, nil // 原名空闲 ⇒ 直接复制，不需要让开
	}
	if fileWritable(dst) {
		return false, nil // 原名可写 ⇒ 覆盖写即可，不动在跑的文件
	}
	name, ok := renameAside(dst, renameSuffixOld, time.Now())
	if !ok {
		logf("旧文件重命名失败：%s ⇒ %s（原名未让开，仍按原名尝试写入）", dst, name)
		return false, fmt.Errorf("旧文件重命名失败：%s ⇒ %s", dst, name)
	}
	logf("已让开被占用的旧文件（重命名）：%s ⇒ %s（原名对 %s 的新写入开放）", dst, name, src)
	return true, nil
}

// deleteRenamed 卸载侧（简报 §2 签名）：重命名成 .del-<时间戳> → 轮询 os.Remove **重命名后的**路径。
// 轮询节奏复用既有常量 stopPollEveryMS=200 ms × stopPollMax=50 = 10 s（procname.go，不改）。
// **绝不**对原名文件调 os.Remove/DeleteFile：那会把名字送进 delete-pending，
// 之后连重命名都 FAIL（证据 out/b23-run4.log 的 b30 访问掩码矩阵、RESTORE_SHA=LOCKED）。
// 删不掉 ⇒ 返回 false 并只记一行日志，**不报错**（原名已空 ⇒ 之后安装/复原可写）。
func deleteRenamed(path string, logf func(string, ...any)) bool {
	name, ok := renameAside(path, renameSuffixDel, time.Now())
	if !ok {
		logf("旧文件重命名失败：%s（原名未让开，删除交回提权侧或留待下次）", path)
		return false
	}
	logf("已让开被占用的旧文件（重命名）：%s ⇒ %s", path, name)
	for i := 0; i < stopPollMax; i++ {
		if rerr := os.Remove(name); rerr == nil || os.IsNotExist(rerr) {
			logf("已删除重命名后的文件 %s", name)
			return true
		}
		if i < stopPollMax-1 {
			time.Sleep(time.Duration(stopPollEveryMS) * time.Millisecond)
		}
	}
	logf("重命名后的文件删不掉，留着不报错：%s", name)
	return false
}

// removeBestEffort 尽力删（轮询 10 s，同 deleteRenamed 的节奏）；删不掉只记一行、返回 false。
// 用于 .old 的清理：spec §3.1「轮询 10 s；删不掉就留着（不报错，不阻塞安装）」。
func removeBestEffort(path string, logf func(string, ...any)) bool {
	for i := 0; i < stopPollMax; i++ {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			return true
		}
		if i < stopPollMax-1 {
			time.Sleep(time.Duration(stopPollEveryMS) * time.Millisecond)
		}
	}
	logf("旧文件删不掉，留着不报错：%s", path)
	return false
}

// cleanupAsideLeftovers 清掉 base 留下的 <base><suffix>* 残留，返回删掉的个数。
// 不用 filepath.Glob：Windows 路径里的反斜杠在 Glob 模式里是转义符（C:\Windows\… 会被吃掉），
// 改成读目录 + 前缀匹配。目录读不到 ⇒ 0（不报错）。
func cleanupAsideLeftovers(base, suffix string, logf func(string, ...any)) int {
	dir := filepath.Dir(base)
	prefix := filepath.Base(base) + suffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			if removeBestEffort(filepath.Join(dir, e.Name()), logf) {
				n++
			}
		}
	}
	return n
}

// sha256OfRetry 带重试的落地校验读取（简报 §2 签名：10 × 500 ms）。
// sha256Of 读失败返回 "?" ⇒ 视作读失败继续重试（"?" 既不得判成功、也不得立即判失败，
// spec §3.3「重试到超时才判」）。证据 out/b23-run4.log PH1：文件确实落成功
// （SYS32=PRESENT 1169194F = 期望新二进制的 SHA 前 8 位），父进程一次读失败 ⇒ 假阴性回滚。
// 返回 (sha, ok)：ok=false 时 sha 恒为 "?"。
func sha256OfRetry(p string, attempts int, every time.Duration) (string, bool) {
	sha := "?"
	for i := 0; i < attempts; i++ {
		sha = sha256Of(p)
		if sha != "?" {
			return sha, true
		}
		if i < attempts-1 && every > 0 {
			time.Sleep(every)
		}
	}
	return sha, false
}

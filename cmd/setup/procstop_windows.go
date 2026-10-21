//go:build windows

package main

// 卸载前停运行中的屏保进程 + 轮询「文件可删除」（Windows 侧实现）。
// 走 kernel32 直调（spec publish-uninstall-stopscr §3：禁 taskkill/wmic/PowerShell 子进程，
// 只用 x/sys）；句柄与 LazyProc 声明在 main.go 的 DLL 句柄块，配对由
// internal/setupcheck/procdll_test.go 闸门强制登记。
// 证据：out/notes-b5c.md §3（运行中镜像占用 ⇒ 删除被拒；终止后锁短暂残留 ⇒ 必须轮询）。

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys v0.39.0 未导出这三个常量，本地定义（值取自 Win32 头文件口径）。
const (
	accessDelete        = 0x00100000        // DELETE（访问掩码，非 FILE_* 属性）
	errFileNotFound     = syscall.Errno(2)  // ERROR_FILE_NOT_FOUND
	errPathNotFound     = syscall.Errno(3)  // ERROR_PATH_NOT_FOUND
	errSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
)

// stopRunningScrImages 枚举系统进程，终止所有「镜像基名 = 待删文件基名」（大小写无关）的
// 进程，返回命中并终止的个数（spec §1 A1；日志口径由调用方打印）。
// 跳过自身 PID（安装器自身基名与目标不同，此判定只防「安装器被改名成 .scr 跑」的自杀边界）。
// 快照句柄与 OpenProcess 句柄都经 CloseHandle 关闭。
func stopRunningScrImages(target string) int {
	want := imageBaseName(target)
	if want == "" {
		return 0
	}
	snap, _, _ := procCreateToolhelp32Snapshot.Call(uintptr(windows.TH32CS_SNAPPROCESS), 0)
	if snap == uintptr(windows.InvalidHandle) {
		return 0
	}
	defer procCloseHandle.Call(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	stopped := 0
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	for r == 1 { // Process32FirstW/NextW 的 TRUE
		if int(pe.ProcessID) != os.Getpid() &&
			matchesScrImage(syscall.UTF16ToString(pe.ExeFile[:]), want) {
			if terminateProcessById(pe.ProcessID) {
				stopped++
			}
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}
	return stopped
}

// terminateProcessById OpenProcess(PROCESS_TERMINATE) + TerminateProcess(exitCode=0) + CloseHandle。
// 打开失败（系统进程/已退出/无权限）⇒ 计为未命中，不阻断卸载流程。
func terminateProcessById(pid uint32) bool {
	h, _, _ := procOpenProcess.Call(uintptr(windows.PROCESS_TERMINATE), 0 /*FALSE*/, uintptr(pid))
	if h == uintptr(windows.InvalidHandle) {
		return false
	}
	defer procCloseHandle.Call(h)
	r, _, _ := procTerminateProcess.Call(h, 0)
	return r == 1
}

// waitFileDeletable 轮询「打开该文件请求删除权」判可删（spec §1 A2）：
// CreateFileW(DELETE, READ|WRITE|DELETE 共享, OPEN_EXISTING) 成功 ⇒ 可删；
// 共享冲突 ⇒ 锁未释放，继续等；文件已不存在 ⇒ 视作可删（与 --delete-only 分支
// os.IsNotExist ⇒ DEL_OK 的既有口径一致）；其它错误（含 ACCESS_DENIED：非提权侧对
// System32 的 ACL 拒绝，非锁信号）⇒ 按「未可删」继续等，预算耗尽返回 false，
// 调用方照旧走提权删除（行为不比现状更差）。
// 节奏 stopPollEveryMS=200 ms × stopPollMax=50 次 = 10 s。
func waitFileDeletable(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true // 路径非法 ⇒ 交给删除调用本身报错，与现状一致
	}
	for i := 0; i < stopPollMax; i++ {
		h, _, errno := procCreateFileW.Call(
			uintptr(unsafe.Pointer(p)),
			uintptr(accessDelete),
			uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE),
			0, // lpSecurityAttributes=NULL
			uintptr(windows.OPEN_EXISTING),
			0, // dwFlagsAndAttributes
			0, // hTemplateFile
		)
		// B14（定因证据：out/b45-run.log 18:17:59 = `openfiles` 指名占用者就是本安装器
		// `1024 Administrator 1524 b9_setup.exe C:\Windows\System32\WeatherScr.scr`）。
		// LazyProc.Call 的第三个返回值是 error（动态类型 syscall.Errno），**恒非 nil** ——
		// 成功时它是 Errno(0)，不是 nil。原先写 `h != InvalidHandle && errno == nil` ⇒ 成功
		// 分支永不进入 ⇒ 每轮泄漏一个 dwShareMode=0 的独占句柄，10 秒轮询泄漏 50 个，
		// 把本函数自己的目标文件锁死（= 历史上安装写不进、卸载删不掉的真因）。
		// 正确判据只看句柄：CreateFileW 成功必返回有效句柄（GetLastError 只在失败时有意义）。
		if h != uintptr(windows.InvalidHandle) {
			procCloseHandle.Call(h)
			return true
		}
		if errno == errFileNotFound || errno == errPathNotFound {
			return true
		}
		if i < stopPollMax-1 {
			time.Sleep(time.Duration(stopPollEveryMS) * time.Millisecond)
		}
	}
	return false
}

// waitFileWritable 轮询「目标可写」（安装侧 A1，spec publish-install-verify §1 A1；
// 与 waitFileDeletable 同风格、同节奏 stopPollEveryMS=200 ms × stopPollMax=50 = 10 s）。
// 判据 = CreateFileW(path, GENERIC_WRITE, 0, NULL, OPEN_EXISTING) 的返回值，
// 逐档口径由纯函数 writableVerdict 给出（procname.go，Linux 侧 T5 断言）：
//   - 成功 ⇒ 已存在且可写 ⇒ 通过（CloseHandle 后返回）
//   - ERROR_FILE_NOT_FOUND(2) ⇒ 名字空闲、可创建 ⇒ 通过
//   - ERROR_ACCESS_DENIED(5) / ERROR_SHARING_VIOLATION(32) ⇒ 被锁（delete-pending）⇒ 继续轮询
//   - 其它错误 ⇒ 不通过且不再轮询（轮询下去也不会变好）
//
// dwShareMode 传 0（独占打开）：delete-pending 状态下请求独占写权会被拒 ⇒ 正是锁信号。
// 证据 out/b18-run.log:100-105（删除成功后立刻 Copy-Item ⇒ Access is denied）、
// out/b19-run.log:2（约 40 s 后 COPY_OK attempt=0）。
func waitFileWritable(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true // 路径非法 ⇒ 交给复制调用本身报错，与现状一致
	}
	for i := 0; i < stopPollMax; i++ {
		h, _, errno := procCreateFileW.Call(
			uintptr(unsafe.Pointer(p)),
			uintptr(windows.GENERIC_WRITE),
			0, // dwShareMode=0 ⇒ 独占，锁未释放时直接失败
			0, // lpSecurityAttributes=NULL
			uintptr(windows.OPEN_EXISTING),
			0, // dwFlagsAndAttributes
			0, // hTemplateFile
		)
		// B14：同 waitFileDeletable —— 成功判据只看句柄，句柄必关（errno 恒非 nil，
		// 原先的 `errno == nil` 让独占句柄每轮泄漏一个，自己把目标锁死）。
		if h != uintptr(windows.InvalidHandle) {
			procCloseHandle.Call(h)
			return true
		}
		ok, retry := writableVerdict(errno)
		if ok {
			return true
		}
		if !retry {
			return false
		}
		if i < stopPollMax-1 {
			time.Sleep(time.Duration(stopPollEveryMS) * time.Millisecond)
		}
	}
	return false
}

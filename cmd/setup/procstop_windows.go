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
		if h != uintptr(windows.InvalidHandle) && errno == nil {
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

//go:build windows

package main

// MoveFileExW 的 syscall 侧实现（B12 重命名交换，设计口径）。
// 纯逻辑与名字生成在 renameswap.go（Linux 侧可测），非 Windows 侧桩在 renameswap_other.go。
// 机制出处（逐字引用）：
// unprotect.it/technique/process-ghosting —'Once a file is mapped into an image section,
// attempts to open it with FILE_WRITE_DATA will fail with ERROR_SHARING_VIOLATION …
// the delete-pending state also blocks external file-open attempts'
// stackoverflow.com/questions/799656（Renaming A Running Process' File Image On Windows）+
// comcomponent 博客 2026-07-24：running exe can be renamed ⇒ 改名让开、原名重建。
// flags 只用 MOVEFILE_REPLACE_EXISTING(0x00000010)；**不用** MOVEFILE_DELAY_UNTIL_REBOOT(0x4)
// （全局副作用、要重启、静默失败）。

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// moveFileEx 调 kernel32!MoveFileExW(src, dst, flags)。
// 句柄 procMoveFileExW 声明在 main.go 的 kernel32 句柄块；过程名↔DLL 配对由
// internal/setupcheck/procdll_test.go 的 nonUser32Exports 表强制登记（MoveFileExW ⇒ kernel32）。
// 返回 FALSE 时把 GetLastError 原样返回（LazyProc.Call 的第三个返回值就是它）。
func moveFileEx(src, dst string, flags uint32) error {
	srcP, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstP, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	r, _, errno := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(srcP)),
		uintptr(unsafe.Pointer(dstP)),
		uintptr(flags))
	if r != 0 {
		return nil
	}
	// Call 的第三个返回值恒非 nil（成功时是 syscall.Errno(0)）⇒ 只能按 Errno 判，
	// 不能用 `errno != nil` 判失败（那样失败分支永不进入、错误码被吞）。
	if e, ok := errno.(syscall.Errno); ok && e != 0 {
		return e
	}
	return errors.New("MoveFileExW 返回 FALSE 且无错误码")
}

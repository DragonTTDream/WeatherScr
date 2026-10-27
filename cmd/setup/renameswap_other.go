//go:build !windows

package main

// 非 Windows 侧桩：保证 go test ./cmd/setup 在 Linux 跑得过（分层口径照 procstop_other.go）。
// 真实实现见 renameswap_windows.go（kernel32!MoveFileExW）与 main.go 的 sha256Of 本体。

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

// moveFileEx 非 Windows 侧没有 MoveFileExW ⇒ 恒失败。
// renameswap.go 的 renameAside 因此在这侧恒返回 ok=false，
// 单测据此断言'重命名失败 ⇒ swapped=false / deleteRenamed=false 且不报错'的返回值口径
// （成功分支由单测临时替换 renameFile 注入点覆盖）。
func moveFileEx(src, dst string, flags uint32) error {
	return errors.New("moveFileEx: 非 Windows 侧无 MoveFileExW")
}

// sha256Of 非 Windows 侧的同口径实现：读失败 ⇒ "?"；成功 ⇒ 12 位小写十六进制前缀
// （与 main.go:sha256Of 本体逐字同口径，12 位前缀口径不改，见设计口径 禁改清单）。
// 只为 Linux 侧单测跑 sha256OfRetry 的重试与 "?" 判据；Windows 侧走 main.go 的本体。
func sha256Of(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return "?"
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<16)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

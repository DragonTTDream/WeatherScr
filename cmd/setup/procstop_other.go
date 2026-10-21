//go:build !windows

package main

// 非 Windows 侧空实现：保证 go test ./cmd/setup 在 Linux 跑得过（spec §3 分层口径）。
// 真实实现见 procstop_windows.go。

// stopRunningScrImages 非 Windows 侧无进程可停 ⇒ 恒 0 命中。
func stopRunningScrImages(target string) int { return 0 }

// waitFileDeletable 非 Windows 侧无镜像锁概念 ⇒ 恒可删。
func waitFileDeletable(path string) bool { return true }

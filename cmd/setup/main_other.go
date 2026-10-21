//go:build !windows

package main

// 非 Windows 平台的 main 桩。
//
// 真 main 在 main.go（`//go:build windows`）：安装向导是 Win32 向导，
// 非 Windows 上没有可安装的对象。本文件的存在理由 = 闸门 b1 第 1 步跑
// `go build ./...`（Linux 侧），而 cmd/setup 里其余文件（finishpage.go /
// iconres.go / procname.go / winsize.go …）**不带 build tag** ⇒
// 该包在 Linux 上是个没有 main 的 main 包，链接期报
// "function main is undeclared in the main package"。
// 与 procstop_other.go / renameswap_other.go 同一分层套路。
func main() {}

package main

import "sync/atomic"

// 单轮驱动 = 安装/卸载原子上锁。
// 根因（根因位置 installops.go:159-204）：向导校验回滚 × 重复触发的双轮时序撞车——
// 第二次 runInstall 把 PreviousScr 覆盖成安装后的值，并在'回滚写旧件 → 再装正件'
// 的窗口里会让人看到旧版在跑（现象：装的明明是新版，屏幕上跑的仍是旧版构筑）。
// pageflow 的 installing 旗标 + uninstalling 旗标是 UI/分发层防线，
// 本锁是数据层第二道保险：CAS 原子，同一时刻只许一轮安装或卸载在跑，跑完 release。
// 无 build tag ⇒ Linux 侧可测（分层同 finishpage.go）。
var installInFlight atomic.Bool

// lastVerify 完成页自验行：runInstall 落盘校验点写入，
// finishpage.go（无 build tag）读取拼接 ⇒ 声明必须同样无 tag，故放本文件而非 pageflow.go。
var lastVerify string

// acquireInstall 取锁：成功返回 true；已有一轮在跑返回 false（调用方立即 return，不产生副作用）。
func acquireInstall() bool { return installInFlight.CompareAndSwap(false, true) }

// releaseInstall 放锁：runInstall/runUninstall 出口 defer 调用。
func releaseInstall() { installInFlight.Store(false) }

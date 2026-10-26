package main

import "testing"

// R16-A 判据（spec r16-wizard §2.1）：重入必须被拒；release 后可再取。
// 纯原子断言，无窗口依赖（Linux 侧 go test 跑得到）。

func TestInstallGuardReentry(t *testing.T) {
	if !acquireInstall() {
		t.Fatal("首次 acquireInstall 必须成功")
	}
	if acquireInstall() {
		t.Fatal("锁被占用期间第二次 acquireInstall 必须失败")
	}
	releaseInstall()
	if !acquireInstall() {
		t.Fatal("releaseInstall 之后 acquireInstall 必须成功")
	}
	releaseInstall()
	if !acquireInstall() {
		t.Fatal("锁释放后必须可再次获取（安装器可重复使用）")
	}
	releaseInstall()
}

package main

import (
	"path/filepath"
	"testing"
)

// TestDbgLogPath 钉死带外日志落点口径（实机取证：
// exe 同目录要跨 ACL/完整性级别的墙，取证不能依赖安装位置）。
func TestDbgLogPath(t *testing.T) {
	want := filepath.Join(`C:\Temp`, "WeatherScr-scr_run.log")
	if got := dbgLogPath(`C:\Install`, `C:\Temp`, true); got != want {
		t.Fatalf("临时标记落点 = %q，期望 %q", got, want)
	}
	want = filepath.Join(`C:\Install`, "scr_run.log")
	if got := dbgLogPath(`C:\Install`, `C:\Temp`, false); got != want {
		t.Fatalf("exe 同目录落点 = %q，期望 %q", got, want)
	}
	if tempLogMarkerName != "WeatherScr-scr.log" {
		t.Fatalf("临时标记名 = %q，期望 %q", tempLogMarkerName, "WeatherScr-scr.log")
	}
}

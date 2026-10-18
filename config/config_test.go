package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestAlertWindowHoursKey 对应配置键与测试 3：
// - 新键 alert_window_hours，Defaults() = 3；
// - 写 6 读回 6；
// - 写 9 回落 3，**并写一行日志**（不静默）。
func TestAlertWindowHoursKey(t *testing.T) {
	if got := Defaults().AlertWindowHours; got != 3 {
		t.Errorf("Defaults().AlertWindowHours = %d，期望 3", got)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")

	// 写 6 读回 6
	if err := os.WriteFile(p, []byte(`{"location":"Hangzhou, China","alert_window_hours":6}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadFrom(p).AlertWindowHours; got != 6 {
		t.Errorf("写 6 读回 %d，期望 6", got)
	}
	// 写 3 读回 3
	if err := os.WriteFile(p, []byte(`{"alert_window_hours":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadFrom(p).AlertWindowHours; got != 3 {
		t.Errorf("写 3 读回 %d，期望 3", got)
	}
	// 键缺失 → 默认 3（旧配置兼容）
	if err := os.WriteFile(p, []byte(`{"location":"Hangzhou, China"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadFrom(p).AlertWindowHours; got != 3 {
		t.Errorf("缺键读回 %d，期望默认 3", got)
	}

	// 写 9 回落 3，且必须有一行日志（不要静默）
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	for _, bad := range []int{9, 0, 12, -3} {
		buf.Reset()
		if err := os.WriteFile(p, []byte(`{"alert_window_hours":`+strconv.Itoa(bad)+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
		c := LoadFrom(p)
		if c.AlertWindowHours != 3 {
			t.Errorf("写 %d 读回 %d，期望回落 3", bad, c.AlertWindowHours)
		}
		line := buf.String()
		if !strings.Contains(line, "alert_window_hours") || !strings.Contains(line, strconv.Itoa(bad)) {
			t.Errorf("写 %d 未写回落日志，实得 %q", bad, line)
		}
	}
}

// TestAlertWindowHoursLegal 钉合法集 = {3, 6}。
func TestAlertWindowHoursLegal(t *testing.T) {
	for _, v := range []int{3, 6} {
		if !AlertWindowHoursLegal(v) {
			t.Errorf("%d 应为合法值", v)
		}
		if got, fell := NormalizeAlertWindowHours(v); got != v || fell {
			t.Errorf("合法值 %d 被改动: got=%d fell=%v", v, got, fell)
		}
	}
	for _, v := range []int{0, 1, 2, 4, 5, 9, 12, -1} {
		if AlertWindowHoursLegal(v) {
			t.Errorf("%d 应为非法值", v)
		}
		if got, fell := NormalizeAlertWindowHours(v); got != 3 || !fell {
			t.Errorf("非法值 %d 应回落 3 并报告回落: got=%d fell=%v", v, got, fell)
		}
	}
}

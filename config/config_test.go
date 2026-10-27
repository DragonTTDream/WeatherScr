package config

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// + 的判据：
// 删 alert_window_hours；删视野小时旋钮（alert_horizon_hours 也拆掉，
// 视野 = 常量 2 h，窗口由官方四级配对决定）；保留 alert_min_mm
// （默认 0.05，范围 0.02–0.5，越界回落默认并写日志）；
// 旧键 alert_window_hours 读到时只写一行作废日志，不映射成任何视野值。

func writeCfg(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestNoAlertHorizonKnob 钉'视野旋钮已拆'：
//
//	Config 里没有视野字段 ⇒ 写回的 JSON 不含视野键；旧键 alert_horizon_hours 读到时
//	不改变任何行为（门槛/其它键照旧），也不写进 JSON。
func TestNoAlertHorizonKnob(t *testing.T) {
	dir := t.TempDir()
	out, err := json.MarshalIndent(Defaults(), "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "alert_horizon_hours") {
		t.Errorf("Defaults() 写回的 JSON 里仍有视野键 alert_horizon_hours：%s", out)
	}
	// 旧配置里带视野键：读进来不改变任何行为（门槛仍是默认 0.05）
	p := writeCfg(t, dir, `{"alert_horizon_hours":24,"alert_min_mm":0.2}`)
	c := LoadFrom(p)
	if got := c.AlertMinMM; got != 0.2 {
		t.Errorf("带视野键的配置里 alert_min_mm = %v，期望照旧 0.2", got)
	}
	out, err = json.MarshalIndent(c, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "alert_horizon_hours") {
		t.Errorf("视野键被写回了 JSON（旋钮应已拆）：%s", out)
	}
}

// TestAlertMinMMKey 钉默认 0.05、合法范围 0.02–0.5、越界回落 + 日志。
func TestAlertMinMMKey(t *testing.T) {
	if got := Defaults().AlertMinMM; got != 0.05 {
		t.Errorf("Defaults().AlertMinMM = %v，期望 0.05", got)
	}
	dir := t.TempDir()
	for _, v := range []float64{0.02, 0.05, 0.3, 0.5} {
		p := writeCfg(t, dir, `{"alert_min_mm":`+strconv.FormatFloat(v, 'f', -1, 64)+`}`)
		if got := LoadFrom(p).AlertMinMM; got != v {
			t.Errorf("写 %v 读回 %v，期望 %v", v, got, v)
		}
	}
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	for _, bad := range []float64{0.0, 0.01, 0.51, 5, -0.1} {
		buf.Reset()
		p := writeCfg(t, dir, `{"alert_min_mm":`+strconv.FormatFloat(bad, 'f', -1, 64)+`}`)
		c := LoadFrom(p)
		if c.AlertMinMM != 0.05 {
			t.Errorf("写 %v 读回 %v，期望回落 0.05", bad, c.AlertMinMM)
		}
		line := buf.String()
		if !strings.Contains(line, "alert_min_mm") {
			t.Errorf("写 %v 未写回落日志，实得 %q", bad, line)
		}
	}
}

// TestLegacyAlertWindowHoursLogsOnly 钉'旧键只写日志、不改变任何行为'
// （窗口由官方量级配对决定，旧键作废且不映射成任何视野值）。
func TestLegacyAlertWindowHoursLogsOnly(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	for _, v := range []int{3, 6, 12, 48} {
		buf.Reset()
		p := writeCfg(t, dir, `{"alert_window_hours":`+strconv.Itoa(v)+`,"alert_min_mm":0.2}`)
		c := LoadFrom(p)
		line := buf.String()
		// ① 写一行日志，且点名旧键与作废口径
		if !strings.Contains(line, "alert_window_hours") || !strings.Contains(line, strconv.Itoa(v)) {
			t.Errorf("旧键 %d 未写作废日志，实得 %q", v, line)
		}
		if !strings.Contains(line, "作废") || !strings.Contains(line, "官方") {
			t.Errorf("旧键 %d 的日志未写明'窗口由官方量级配对决定、本键作废'，实得 %q", v, line)
		}
		// ② 不改变任何行为：门槛照旧、视野键不进 Config
		if got := c.AlertMinMM; got != 0.2 {
			t.Errorf("旧键 %d 读到时改了行为：alert_min_mm = %v，期望照旧 0.2", v, got)
		}
		out, err := json.MarshalIndent(c, "", "    ")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "alert_window_hours") ||
			strings.Contains(string(out), "alert_horizon_hours") {
			t.Errorf("旧键 %d 被写回 JSON（应只写日志）：%s", v, out)
		}
	}
	// 值 0 = 旧配置里等于没设 ⇒ 不写作废日志（MigrateAlertWindowHours 的口径）
	buf.Reset()
	p0 := writeCfg(t, dir, `{"alert_window_hours":0}`)
	_ = LoadFrom(p0)
	if strings.Contains(buf.String(), "作废") {
		t.Errorf("旧键 0（等于没设）不应写作废日志，实得 %q", buf.String())
	}
}

// TestNoLegacyWindowHoursField 钉旧字段与视野旋钮都已从 Config 里删掉
// （写回的 JSON 不再有这两个键位，淋湿门槛保留）。
func TestNoLegacyWindowHoursField(t *testing.T) {
	dir := t.TempDir()
	p := writeCfg(t, dir, `{"alert_window_hours":6,"alert_horizon_hours":24}`)
	c := LoadFrom(p)
	out, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "alert_window_hours") {
		t.Errorf("Config 写回的 JSON 里仍有旧键 alert_window_hours：%s", out)
	}
	if strings.Contains(string(out), "alert_horizon_hours") {
		t.Errorf("Config 写回的 JSON 里仍有视野键 alert_horizon_hours（旋钮应已拆）：%s", out)
	}
	if !strings.Contains(string(out), "alert_min_mm") {
		t.Errorf("Config 写回的 JSON 里没有新键 alert_min_mm：%s", out)
	}
}

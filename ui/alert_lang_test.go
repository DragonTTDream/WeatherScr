package ui

import (
	"strings"
	"testing"
)

// TestAlertTextFollowsLanguage 对应需求 6 的语言口径
// （lang="en" 含 "Alert" 且不含"警""降雨"；lang="zh" 走中文；lang="" 走英文）
// + 正文内容（未来 N 小时滚动窗口）。
func TestAlertTextFollowsLanguage(t *testing.T) {
	// 橙警数据：窗口 88%、2.6 mm、窗口 3 小时（Tier()=3）
	a := &Alert{Pct: 88, MM: 2.6, WindowHours: 3, Style: "panelrow"}

	en := a.Line("en")
	if !strings.Contains(en, "Alert") {
		t.Errorf(`lang="en" 应含 "Alert"，得到 %q`, en)
	}
	for _, zh := range []string{"警", "降雨"} {
		if strings.Contains(en, zh) {
			t.Errorf(`lang="en" 不得含 %q，得到 %q`, zh, en)
		}
	}
	if want := "Orange Alert · Rain in next 3 h 88% · 2.6 mm expected"; en != want {
		t.Errorf(`lang="en" 全文 = %q，期望 %q`, en, want)
	}

	zh := a.Line("zh")
	if want := "橙警 · 未来 3 小时降雨 88% · 预计 2.6 mm"; zh != want {
		t.Errorf(`lang="zh" 全文 = %q，期望 %q`, zh, want)
	}

	none := a.Line("")
	if none != en {
		t.Errorf(`lang="" 应走英文：%q vs %q`, none, en)
	}

	// 无毫米数（第二个模板）
	b := &Alert{Pct: 88, WindowHours: 3}
	if got, want := b.Line("en"), "Orange Alert · Rain in next 3 h 88%"; got != want {
		t.Errorf("无毫米数 en = %q，期望 %q", got, want)
	}
	if got, want := b.Line("zh"), "橙警 · 未来 3 小时降雨 88%"; got != want {
		t.Errorf("无毫米数 zh = %q，期望 %q", got, want)
	}

	// 明日分支保留现状不动（最后一句）
	c := &Alert{Pct: 78, Tomorrow: true}
	if got, want := c.Line("en"), "Orange Alert · Rain tomorrow 78%"; got != want {
		t.Errorf("明日 en = %q，期望 %q", got, want)
	}
	if got, want := c.Line("zh"), "橙警 · 今日降雨 78%"; got != want {
		t.Errorf("明日 zh = %q，期望 %q（现状逐字，要求不动）", got, want)
	}

	// 档位名映射（需求 2）
	for _, tc := range []struct {
		pct            int
		zhTier, enTier string
	}{
		{30, "蓝", "Blue"},
		{55, "黄", "Yellow"},
		{75, "橙", "Orange"},
		{95, "红", "Red"},
	} {
		d := &Alert{Pct: tc.pct, WindowHours: 3}
		if got := d.TierName("zh"); got != tc.zhTier {
			t.Errorf("Pct=%d zh 档位名 = %q，期望 %q", tc.pct, got, tc.zhTier)
		}
		if got := d.TierName("en"); got != tc.enTier {
			t.Errorf("Pct=%d en 档位名 = %q，期望 %q", tc.pct, got, tc.enTier)
		}
		if got, want := d.TierLabel("en"), tc.enTier+" Alert"; got != want {
			t.Errorf("Pct=%d en 档位标签 = %q，期望 %q", tc.pct, got, want)
		}
		if got, want := d.TierLabel("zh"), tc.zhTier+"警"; got != want {
			t.Errorf("Pct=%d zh 档位标签 = %q，期望 %q", tc.pct, got, want)
		}
	}

	// tier 1–2 不带档位前缀（现状 drawFusion3/alertRow 的拼接口径）
	low := &Alert{Pct: 55, WindowHours: 3}
	if got, want := low.Line("en"), "Rain in next 3 h 55%"; got != want {
		t.Errorf("tier2 en = %q，期望 %q", got, want)
	}
	if got, want := low.Line("zh"), "未来 3 小时降雨 55%"; got != want {
		t.Errorf("tier2 zh = %q，期望 %q", got, want)
	}
}

// TestAlertTextWindowHours 对应测试 2：
// WindowHours=3 的 en 串含 "next 3 h"；WindowHours=6 的 zh 串含 "未来 6 小时"。
// 窗口小时数必须**逐字**出现在文案里。
func TestAlertTextWindowHours(t *testing.T) {
	a3 := &Alert{Pct: 62, MM: 0.3, WindowHours: 3}
	if got := a3.Text("en"); !strings.Contains(got, "next 3 h") {
		t.Errorf(`WindowHours=3 的 en 串应含 "next 3 h"，得到 %q`, got)
	}
	if got, want := a3.Text("en"), "Rain in next 3 h 62% · 0.3 mm expected"; got != want {
		t.Errorf("en 全文 = %q，期望 %q", got, want)
	}

	a6 := &Alert{Pct: 62, MM: 0.3, WindowHours: 6}
	if got := a6.Text("zh"); !strings.Contains(got, "未来 6 小时") {
		t.Errorf(`WindowHours=6 的 zh 串应含 "未来 6 小时"，得到 %q`, got)
	}
	if got, want := a6.Text("zh"), "未来 6 小时降雨 62% · 预计 0.3 mm"; got != want {
		t.Errorf("zh 全文 = %q，期望 %q", got, want)
	}
	if got, want := a6.Text("en"), "Rain in next 6 h 62% · 0.3 mm expected"; got != want {
		t.Errorf("en 全文 = %q，期望 %q", got, want)
	}

	// 零值/非法值按 3 出文案（config.alert_window_hours 的合法集 = {3,6}，默认 3）
	for _, h := range []int{0, 1, 9, 12} {
		z := &Alert{Pct: 40, WindowHours: h}
		if got := z.Text("zh"); !strings.Contains(got, "未来 3 小时") {
			t.Errorf("WindowHours=%d 应回落 3 小时文案，得到 %q", h, got)
		}
		if got := z.Text("en"); !strings.Contains(got, "next 3 h") {
			t.Errorf("WindowHours=%d 应回落 next 3 h 文案，得到 %q", h, got)
		}
	}
}

package main

// 官方信号层的进程侧单测（批次 b-alert-v2 第 9 步：非 CN 不发 NMC）。

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/ui"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// TestNMCRequestSkipsNonCN 钉死 spec/b-alert-v2.md 目标 1 逐字：
// **仅 country_code==CN 才发官方请求**，非中国区不发。
func TestNMCRequestSkipsNonCN(t *testing.T) {
	for _, cc := range []string{"cn", "CN", " Cn "} {
		if !nmcRequestAllowed(cc) {
			t.Fatalf("country_code=%q 应允许发官方请求", cc)
		}
	}
	for _, cc := range []string{"jp", "JP", "de", "US", "", "CNX"} {
		if nmcRequestAllowed(cc) {
			t.Fatalf("country_code=%q 不得发官方请求", cc)
		}
	}
}

// TestOfficialAlertPriorityAndExpiry 钉死态① 的取值与 12 h 窗口
// （§11 态① 触发条件：`issuetime` ≤12 h；12 逐字取既有常量 officialMaxWindowHours）。
func TestOfficialAlertPriorityAndExpiry(t *testing.T) {
	body := []byte(`{"msg":"success","code":0,"data":{"page":{"count":1,"list":[
		{"alertid":"530000_1","issuetime":"2026-10-21 22:44",
		 "title":"云南省红河哈尼族彝族自治州元阳县气象台发布暴雨黄色预警信号",
		 "pic":"https://image.nmc.cn/assets/img/alarm/p0002003.png"}
	]}}}`)
	page, err := weather.NMCDecodePage("云南省", body)
	if err != nil {
		t.Fatalf("解析失败 %v", err)
	}
	setOfficial(page, time.Now())
	defer clearOfficial()

	now := time.Date(2026, 10, 21, 23, 0, 0, 0, time.UTC)
	a := officialAlert(config.Config{}, now)
	if a == nil {
		t.Fatalf("官方信号命中应出态①")
	}
	if a.State != ui.AlertOfficial {
		t.Fatalf("State=%v 期望 AlertOfficial", a.State)
	}
	if a.OfficialLevel != 2 {
		t.Fatalf("官方等级 = %d 期望 2（黄）", a.OfficialLevel)
	}
	if got := a.Text("zh"); got != "暴雨 黄色预警 · 22:44 发布" {
		t.Fatalf("态① 中文文案 = %q 期望 `暴雨 黄色预警 · 22:44 发布`", got)
	}
	if got := a.Text("en"); got != "Rainstorm Yellow · issued 22:44" {
		t.Fatalf("态① 英文文案 = %q 期望 `Rainstorm Yellow · issued 22:44`", got)
	}
	// §11 态① 逐字：橙/红 1.2 s 呼吸，蓝/黄 恒定
	if a.PulseOn {
		t.Fatalf("黄色档应恒定不脉动")
	}
	// 超 12 h ⇒ 不显示（回退自算层）
	// issuetime 22:44 CST = 14:44 UTC；now = 23:00 UTC ⇒ age 8.27 h ≤ 12 h
	if b := officialAlert(config.Config{}, now.Add(13*time.Hour)); b != nil {
		t.Fatalf("issuetime 超 12 h 不得显示态①")
	}
}

package main

// 职责（app/scr）：预警判据：配置阈值取数、进程内存滞回态、官方信号预警构造
// 搬移来源：cmd/scr/main.go:416-428,1269-1281,1283-1309,1436-1474,1571-1630（逐字，含注释）
// 规格：—— app/scr = 屏保生命周期；本卡零行为变化。

import (
	"math"
	"sync"
	"time"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/ui"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// alertThreshold 取配置阈值，未设时回落到包默认（30%）。
func alertThreshold(c config.Config) int {
	if c.AlertThresholdPct >= 1 {
		return c.AlertThresholdPct
	}
	return ui.AlertThresholdPct
}

// alertMinMM 取淋湿门槛（mm/15min）：合法 0.02–0.5，越界回落默认 0.05。
func alertMinMM(c config.Config) float64 {
	v, _ := config.NormalizeAlertMinMM(c.AlertMinMM)
	return v
}

// ---- 预警判据的进程内存态（滞回"状态存进程内存，不持久化"）----
// 一个采样周期 = 一次取数（refresh_minutes）—— 规格'待定点'第 3 条本批取此口径，已记未决。
// refresher 每取到一次数据就推进一次滞回并缓存判据结果；渲染端 buildAlert 只读结果，
// 不逐帧推进滞回（逐帧推进会把"连续 2 个采样周期"变成"连续 2 帧"）。
type alertDiagState struct {
	mu   sync.Mutex
	hyst *weather.Hysteresis
	eval weather.AlertEval
	have bool
}

var alertDiag alertDiagState

// advanceAlertEval 取到数据后算一次判据（推进滞回一个采样周期）并记下结果。
func advanceAlertEval(cfg config.Config, s *weather.Sample, now time.Time) weather.AlertEval {
	local := now.UTC().Add(time.Duration(s.UTCOffsetSeconds) * time.Second)
	alertDiag.mu.Lock()
	defer alertDiag.mu.Unlock()
	if alertDiag.hyst == nil {
		alertDiag.hyst = &weather.Hysteresis{}
	}
	e := weather.Evaluate(s, local, alertMinMM(cfg), alertDiag.hyst)
	alertDiag.eval, alertDiag.have = e, true
	// 带外取证（实机）：提醒的判定过程此前在日志里**一行都没有**——
	// 实测 5928 行进程内日志里只有 SCR_WEATHER=class=rain，画面亮了、为什么亮、
	// 为什么不亮，命令行侧完全测不到（'带外可观测是硬需求'：只在 GUI 里
	// 取得到的信息 = 不存在）。每个采样周期（= 一次取数）落一行判据现场。
	// 注：这行只在标记文件开启的日志里存在，生产态零字节。
	logf("ALERTDIAG avail=%v on=%v tier=%d disaster=%v daily=%v prob=%d mmph=%.2f mm=%.1f hours=%d slots=%d min15=%d",
		e.Available, e.On, e.Tier, e.Disaster, e.Daily, e.Prob, e.MMPerH, e.MM, e.Hours,
		len(s.HourlyTime), s.Min15N)
	return e
}

// getAlertEval 返回上一次取数算出的判据（无 ⇒ false，调用方按无状态现算）。
func getAlertEval() (weather.AlertEval, bool) {
	alertDiag.mu.Lock()
	defer alertDiag.mu.Unlock()
	return alertDiag.eval, alertDiag.have
}

// officialAlert 由官方结果构造 态①（取不到 ⇒ nil ⇒ 回退自算层）。
// 触发条件（态①）：country_code==CN 且 NMC 返回有效信号（`issuetime` ≤12 h、
// 地名匹配到本机层级）。12 h 上限逐字取既有常量 officialMaxWindowHours（禁改清单内）。
// TODO(未取证)：'地名匹配到本机层级'本批只做到**省级**（省名由坐标反查的
// ISO3166-2 闭表给出 ⇒ 请求本身就是省级过滤），市/区层级的匹配表未取证。
func officialAlert(cfg config.Config, now time.Time) *ui.Alert {
	page, ok := getOfficial()
	if !ok {
		return nil
	}
	top, ok := weather.NMCTop(page)
	if !ok {
		return nil
	}
	age := now.UTC().Sub(top.Issued.UTC())
	if age > weather.OfficialMaxWindowHours*time.Hour || age < -time.Hour {
		return nil // 超 12 h 的旧信号不显示；时间明显在未来也不显示
	}
	pulseOn := true
	if cfg.AlertPulse != nil {
		pulseOn = *cfg.AlertPulse
	}
	ph := math.Mod(float64(now.UnixNano())/1e9/1.2, 1.0)
	return &ui.Alert{
		State: ui.AlertOfficial,
		Lang:  displayLang(cfg),
		Pulse: ph,
		// 态① 逐字：橙/红 1.2 s 呼吸，蓝/黄 恒定
		PulseOn:        pulseOn && (top.TitleLevel >= 3),
		YOff:           cfg.AlertYOffset,
		AsOf:           top.Issued,
		OfficialTypeZh: top.TypeZh,
		OfficialTypeEn: weather.NMCTypeEn(top.TypeZh),
		OfficialLevel:  top.TitleLevel,
		OfficialIssued: top.Issued,
	}
}

// buildAlert 按配置与天气样本构造降雨提醒（的三态）。
// 判据来自 weather.Evaluate（灾害层官方配对 + 日常层锚定 + 滞回 + 数据闸门）。
// 滞回状态由 refresher 每取一次数推进一次（见 advanceAlertEval）；这里只读结果，
// 取数还没跑过时（首帧 / 面板预览）按无状态现算一次（门槛直达，无滞回）。
// 优先级（本批无①官方信号）：灾害 > 日常 > 不可用。
// 版式：panelrow = 天气面板下沿内的居中胶囊（定案"贴紧-60"，绘制代码不动）。
// 脉动相位：1.2 秒一个周期，按墙钟时间取相位 ⇒ 与帧率无关（时间积分，口径）。
func buildAlert(cfg config.Config, s *weather.Sample, now time.Time) *ui.Alert {
	if cfg.AlertEnabled != nil && !*cfg.AlertEnabled {
		return nil
	}
	// 优先级 ① > ② > ③ > ④：
	// 中国区官方信号优先出态①；取不到 ⇒ 回退自算层（态②/③/④，判据与常量不动）。
	if oa := officialAlert(cfg, now); oa != nil {
		return oa
	}
	if s == nil {
		return nil
	}
	local := now.UTC().Add(time.Duration(s.UTCOffsetSeconds) * time.Second)
	e, ok := getAlertEval()
	if !ok {
		e = weather.Evaluate(s, local, alertMinMM(cfg), nil)
	}
	pulseOn := true
	if cfg.AlertPulse != nil {
		pulseOn = *cfg.AlertPulse
	}
	ph := math.Mod(float64(now.UnixNano())/1e9/1.2, 1.0)
	a := &ui.Alert{
		Lang:    displayLang(cfg),
		Pulse:   ph,
		PulseOn: pulseOn,
		YOff:    cfg.AlertYOffset,
		AsOf:    local,
	}
	switch {
	case !e.Available:
		// 态④：预警数据不可用 · 截至 HH:MM（灰，恒定）
		a.State = ui.AlertUnavailable
		a.PulseOn = false
	case e.Disaster:
		// 态②：自算灾害（官方配对判据命中，取满足的最高级）
		a.State = ui.AlertDisaster
		a.TierVal = e.Tier
		a.MM = e.MM
		a.DisasterHours = e.Hours
	case e.Daily && e.On:
		// 态③：带伞提醒（视野内第一个达门槛的 15 分钟格，蓝色恒定）
		a.State = ui.AlertDaily
		a.Pct = e.Prob
		a.MMPerH = e.MMPerH
		a.Start = e.Start
		a.PulseOn = false
	default:
		return nil
	}
	return a
}

package weather

import (
	"fmt"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// 云量口径（D1 云量接入）。
//
// 数据源实测（Open-Meteo，本机直连，2026-10-20）：
//
//	current=cloud_cover        = ACCEPT
//	hourly=cloud_cover         = ACCEPT
//	hourly=thunder_probability = REJECT（响应报 invalid String value，该字段不存在）
//
// ⇒ 云量可用，闪电概率字段不存在、不得使用。检索出处：
// "https://open-meteo.com/en/docs"（hourly / current 变量表）。
//
// 逐时口径：hourly 的 cloud_cover 是整点阶梯（每格一个值），直接取整点会在整点
// 交界处跳变。实测同一次请求前 12 小时的码↔云量序列：
// 1↔39, 1↔29, 1↔27, 1↔33, 2↔67, 3↔100, 2↔79, 1↔44, 2↔52, 2↔57, 1↔37, 1↔41
// ⇒ 云量是连续量、码是它的离散化。太阳可见度是逐帧画出来的量，整点跳变会读成
// "太阳突然变暗"，故取**线性插值**。
//
// current 的值只用于交叉校验与日志（见 Fetch）。

// cloudHourLayout = hourly.time 的格式（请求带 timezone=auto ⇒ 当地墙钟、无时区标记）。
const cloudHourLayout = "2006-01-02T15:00"

// clampCloudPct 云量钳位 [0,100]。
func clampCloudPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// InterpCloudPct 逐时云量在 now 这一时刻的线性插值。
//
// times 是整点墙钟（升序），cloud 与 times 平行。口径：
//
//	落在两整点之间        → c[i] + (c[i+1]-c[i])·f，f = 已过的时长/整点间隔
//	早于首格 / 晚于末格    → 取最近那一格（不外推到数组之外）
//	数组空 / 长度不齐 / 时间戳解不开 → (0, false)，调用方走回落
func InterpCloudPct(times []string, cloud []int, offsetSeconds int, now time.Time) (int, bool) {
	n := len(times)
	if len(cloud) < n {
		n = len(cloud)
	}
	if n == 0 {
		return 0, false
	}
	local := now.UTC().Add(time.Duration(offsetSeconds) * time.Second)
	type cell struct {
		t time.Time
		v int
	}
	cells := make([]cell, 0, n)
	for i := 0; i < n; i++ {
		t, err := time.Parse(cloudHourLayout, times[i])
		if err != nil {
			continue
		}
		cells = append(cells, cell{t: t, v: cloud[i]})
	}
	if len(cells) == 0 {
		return 0, false
	}
	if len(cells) == 1 {
		return clampCloudPct(cells[0].v), true
	}
	if local.Before(cells[0].t) {
		return clampCloudPct(cells[0].v), true
	}
	last := cells[len(cells)-1]
	if !local.Before(last.t) {
		return clampCloudPct(last.v), true
	}
	for i := 0; i < len(cells)-1; i++ {
		a, b := cells[i], cells[i+1]
		if local.Before(a.t) || !local.Before(b.t) {
			continue
		}
		span := b.t.Sub(a.t)
		if span <= 0 {
			return clampCloudPct(a.v), true
		}
		f := float64(local.Sub(a.t)) / float64(span)
		return clampCloudPct(int(float64(a.v) + (float64(b.v)-float64(a.v))*f + 0.5)), true
	}
	return 0, false
}

// cloudPctFromCode 响应里没有 cloud_cover 时的回落表（按码推）。
//
// 依据：实测码↔云量（码 1 ≈ 27–41%、码 2 ≈ 52–79%、码 3 = 100%）
// ⇒ 码 1→30、码 2→65、码 3→100；其余档取该档的典型云量：
// 雨/雷/雾→90、雪→95、尘→70（降水类由天气码决定，云量不覆盖它）。
//
// TODO(未取证)：回落表只给了 1/2/3 三个码。码 0 与落进 Clear 档的其余码
// （4 以上、未知码）表内没有条目 ⇒ 取码 1 那一档的 30，不另造数。
func cloudPctFromCode(code int) int {
	switch code {
	case 1:
		return 30
	case 2:
		return 65
	case 3:
		return 100
	}
	switch c, _ := effect.Classify(code); c {
	case effect.Rain, effect.Thunder, effect.Fog:
		return 90
	case effect.Snow:
		return 95
	case effect.Dust:
		return 70
	default:
		return 30
	}
}

// CloudPctLog 打一行云量取数日志（Fetch 的正常路径与回落路径共用）。
//
// src = hourly_interp / current / code_fallback；current = 同刻的 current 值，
// 只作交叉校验（两者差得多 = 上游两个口径不一致，日志里要看得见）。
func CloudPctLog(pct int, src string, code int, current string) {
	fmt.Printf("CLOUD_PCT=%d src=%s code=%d current=%s\n", pct, src, code, current)
}

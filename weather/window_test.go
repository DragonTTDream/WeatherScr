package weather

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// 未来 N 小时滚动窗口的 fixture（测试 1）。
//
// 实测序列逐字照抄需求 2（2026-10-16，坐标 30.2495,120.1583，
// 每 3 小时抽样：
//
//	00:00 16%/0.0 · 03:00 56%/0.5 · 06:00 62%/0.2 · 09:00 57%/0.1
//	12:00 79%/0.1 · 15:00 77%/0.1 · 18:00 28%/0.0 · 21:00 0%/0.0
//
// 中间小时（01,02,04,05,…）**实测未取证** ⇒ 按"步进取下一个实测点"补齐
// （01,02 ← 03:00 的 56%；07,08 ← 09:00 的 57%；22,23 无下一个实测点 ⇒ 用 21:00 的 0%）。
// 补齐只影响未实测的中间格，实测点逐字不动；补齐规则已记入工程记录。
//
// 次日（2026-10-17）逐小时值实测只取证到"forecast_days=2 → 48 条"这一条事实，
// 数值未取证 ⇒ 次日部分用**合成**序列 prob=4·(h+1)、mm=0.1·(h+1)（见 notes），
// 断言只按 fixture 自身算出的最大值/求和来钉，不引入外部数字。

var day1Prob = []float64{16, 56, 62, 57, 79, 77, 28, 0} // 00,03,06,09,12,15,18,21 实测
var day1MM = []float64{0.0, 0.5, 0.2, 0.1, 0.1, 0.1, 0.0, 0.0}

// fill24 把 3 小时抽样补齐成 24 格（步进取下一个实测点）。
func fill24(probs, mms []float64) ([]float64, []float64) {
	p := make([]float64, 24)
	m := make([]float64, 24)
	for h := 0; h < 24; h++ {
		k := h / 3
		if h%3 != 0 {
			k++ // 步进：下一个实测点
		}
		if k > 7 {
			k = 7 // 22/23 点无下一个实测点 ⇒ 用最后一个实测点（21:00）
		}
		p[h] = probs[k]
		m[h] = mms[k]
	}
	return p, m
}

// day1Sample 造 2026-10-16 的 24 条 fixture（当地墙钟，无时区标记）。
func day1Sample() *Sample {
	p, m := fill24(day1Prob, day1MM)
	s := &Sample{UTCOffsetSeconds: 28800}
	for h := 0; h < 24; h++ {
		s.HourlyTime = append(s.HourlyTime, "2026-10-16T"+hourStr(h)+":00")
		s.HourlyProb = append(s.HourlyProb, p[h])
		s.HourlyMM = append(s.HourlyMM, m[h])
	}
	return s
}

// day2Sample 追加次日 n 条（合成序列）。
func day2Sample(s *Sample, n int) *Sample {
	for h := 0; h < n; h++ {
		s.HourlyTime = append(s.HourlyTime, "2026-10-17T"+hourStr(h)+":00")
		s.HourlyProb = append(s.HourlyProb, 4*float64(h+1))
		s.HourlyMM = append(s.HourlyMM, 0.1*float64(h+1))
	}
	return s
}

func hourStr(h int) string { return fmt.Sprintf("%02d", h) }

func localAt(y, mo, d, hh, mi int) time.Time {
	// 条目串无时区 ⇒ 解析成 UTC 基的墙钟；nowLocal 同基即可直接比较。
	return time.Date(y, time.Month(mo), d, hh, mi, 0, 0, time.UTC)
}

// TestWindowProbSameDay 断言测试 1 的前两条：
// - now=20:45, hours=3 → 21,22,23 三格 ⇒ prob = 三格最大值、mm = 三格和；
// - now=07:00, hours=3 → 07,08,09 ⇒ prob = 57（实测给定的数字）。
func TestWindowProbSameDay(t *testing.T) {
	s := day1Sample()

	// 1 now=20:45 ⇒ 严格未来 = 21:00, 22:00, 23:00
	prob, mm, ok := WindowProb(s, localAt(2026, 10, 16, 20, 45), 3)
	if !ok {
		t.Fatalf("20:45 应有可用条目")
	}
	wantProb := math.Max(s.HourlyProb[21], math.Max(s.HourlyProb[22], s.HourlyProb[23]))
	wantMM := math.Round((s.HourlyMM[21]+s.HourlyMM[22]+s.HourlyMM[23])*10) / 10
	if float64(prob) != wantProb {
		t.Errorf("20:45/3h prob = %d，期望三格最大值 %v", prob, wantProb)
	}
	if mm != wantMM {
		t.Errorf("20:45/3h mm = %v，期望三格和 %v", mm, wantMM)
	}

	// 2 now=07:00 ⇒ 07,08,09 ⇒ prob = 57（测试 1 第 2 条）
	prob, mm, ok = WindowProb(s, localAt(2026, 10, 16, 7, 0), 3)
	if !ok {
		t.Fatalf("07:00 应有可用条目")
	}
	if prob != 57 {
		t.Errorf("07:00/3h prob = %d，期望 57", prob)
	}
	if want := 0.3; mm != want {
		t.Errorf("07:00/3h mm = %v，期望 0.3（0.1+0.1+0.1）", mm)
	}
}

// TestWindowProbMidnightCrossing 断言测试 1 第 3 条：
// now=23:00, hours=3 → **跨午夜**（48 条 fixture），prob 与 mm 正确。
// 口径 = slot >= nowLocal ⇒ 窗口是 23:00 + 次日 00:00,01:00（下标 23,24,25）；
// 原文写"取次日 00,01,02"，那是按 `slot > nowLocal` 的说法，
// 与同节测试 1 第 2 条给的硬数字（07:00 ⇒ 57）互斥，取硬数字口径，
// 见 window.go 的注释。**跨午夜这一判据不变**。
func TestWindowProbMidnightCrossing(t *testing.T) {
	s := day2Sample(day1Sample(), 24) // 48 条
	if len(s.HourlyTime) != 48 {
		t.Fatalf("fixture 应 48 条，实得 %d", len(s.HourlyTime))
	}
	prob, mm, ok := WindowProb(s, localAt(2026, 10, 16, 23, 0), 3)
	if !ok {
		t.Fatalf("23:00 跨午夜应有可用条目")
	}
	// 窗口 = 当日 23:00 + 次日 00:00,01:00 = 下标 23,24,25
	if s.HourlyTime[23] != "2026-10-16T23:00" || s.HourlyTime[25] != "2026-10-17T01:00" {
		t.Fatalf("fixture 跨午夜条目异常: %s … %s", s.HourlyTime[23], s.HourlyTime[25])
	}
	wantProb := math.Max(s.HourlyProb[23], math.Max(s.HourlyProb[24], s.HourlyProb[25]))
	wantMM := math.Round((s.HourlyMM[23]+s.HourlyMM[24]+s.HourlyMM[25])*10) / 10
	if float64(prob) != wantProb {
		t.Errorf("23:00/3h prob = %d，期望窗口三格最大值 %v", prob, wantProb)
	}
	if mm != wantMM {
		t.Errorf("23:00/3h mm = %v，期望窗口三格和 %v", mm, wantMM)
	}
	if prob == 0 {
		t.Errorf("跨午夜窗口 prob 不应为 0（23:00 之后的条目有值）")
	}
}

// TestWindowProbTruncates 断言测试 1 第 4 条：
// now=23:30, hours=6 且只剩 2 条 ⇒ 用全部 2 条，ok=true（末日截断，不报错）。
func TestWindowProbTruncates(t *testing.T) {
	s := day2Sample(day1Sample(), 2) // 26 条：23:30 之后只剩次日 00:00、01:00
	prob, mm, ok := WindowProb(s, localAt(2026, 10, 16, 23, 30), 6)
	if !ok {
		t.Fatalf("只剩 2 条时 ok 应为 true")
	}
	wantProb := math.Max(s.HourlyProb[24], s.HourlyProb[25])
	wantMM := math.Round((s.HourlyMM[24]+s.HourlyMM[25])*10) / 10
	if float64(prob) != wantProb {
		t.Errorf("23:30/6h prob = %d，期望 2 条最大值 %v", prob, wantProb)
	}
	if mm != wantMM {
		t.Errorf("23:30/6h mm = %v，期望 2 条和 %v", mm, wantMM)
	}
}

// TestWindowProbNoFuture 断言测试 1 第 5 条：全部条目已过去 ⇒ ok=false。
func TestWindowProbNoFuture(t *testing.T) {
	s := day1Sample() // 到 23:00 为止
	if prob, mm, ok := WindowProb(s, localAt(2026, 10, 16, 23, 30), 3); ok {
		t.Errorf("全部条目已过去应 ok=false，实得 prob=%d mm=%v", prob, mm)
	}
	// 空样本 / 非法小时数同样不显示
	if _, _, ok := WindowProb(&Sample{}, localAt(2026, 10, 16, 12, 0), 3); ok {
		t.Errorf("空 HourlyTime 应 ok=false")
	}
	if _, _, ok := WindowProb(s, localAt(2026, 10, 16, 12, 0), 0); ok {
		t.Errorf("hours=0 应 ok=false")
	}
	if _, _, ok := WindowProb(nil, localAt(2026, 10, 16, 12, 0), 3); ok {
		t.Errorf("nil 样本应 ok=false")
	}
}

// TestWindowProbCurrentHourBoundary 钉"当前小时"边界口径：
// - now = 12:00 整点 ⇒ 12:00 那一格（79%）**算**进窗口（该小时还没走完）；
// - now = 12:01 ⇒ 12:00 那一格**不算**，窗口 = 13,14,15 = 77,77,77。
func TestWindowProbCurrentHourBoundary(t *testing.T) {
	s := day1Sample()
	prob, _, ok := WindowProb(s, localAt(2026, 10, 16, 12, 0), 3)
	if !ok {
		t.Fatalf("12:00 应有可用条目")
	}
	if prob != 79 {
		t.Errorf("12:00 整点/3h prob = %d，期望 79（12:00 那格算进窗口）", prob)
	}
	prob, _, ok = WindowProb(s, localAt(2026, 10, 16, 12, 1), 3)
	if !ok {
		t.Fatalf("12:01 应有可用条目")
	}
	if prob != 77 {
		t.Errorf("12:01/3h prob = %d，期望 77（12:00 那格已不算）", prob)
	}
	// 20:45 同理：20:00 那格不算 ⇒ 窗口 21,22,23 = 0,0,0
	prob, _, ok = WindowProb(s, localAt(2026, 10, 16, 20, 45), 3)
	if !ok {
		t.Fatalf("20:45 应有可用条目")
	}
	if prob != 0 {
		t.Errorf("20:45/3h prob = %d，期望 0（21,22,23 三格实测都是 0%%）", prob)
	}
}

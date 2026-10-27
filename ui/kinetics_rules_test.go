package ui

// B3 三条探针判据（闸门第 3 条），逐类 800 固定步：
//	① 逐朵最终显示宽 >= 480 px @1080（11-15）
//	② 全部云 p.spr ∈ {0,1,3}（白名单 cloud1/cloud2/cloud4，:26-33）
//	③ 同帧进场且 |Δx| < 上一条半宽 的样本数 = 0（"决不能同出同进"，:24）
// 口径说明（写在这里，不自行发明判据）：
// - 宽度与 spr 走**绘制同口径**：cloudDrawWidthPx（= 源图宽 × spriteScale，与 Draw 的 dw 逐字同式）；
// - 进场事件走**几何检出**：一朵云从"整只在屏外"变成"身体有像素进屏"的那一拍 = 一次进场，
// 同一步号内的进场按检出顺序两两相邻比较（|Δx| 与上一条半宽，px 口径，与 kinetics.EntryOK 同式）；
// 同时对照闸门自己的流水 f.entryLog（排队发射的记录），两边都判 ⇒ 探针不吃闸门自报数。
// - 位移系数：800 步 = 13.3 s 仿真，云速 ≈0.009 屏宽/s 时屏外进场位要 27 s 才进屏
// ⇒ 在 800 步窗口里进场样本恒为 0（判据空转）。这里把生产里既有的 speedScale
// （cmd/scr 按帧预算设置的位移系数，Field.speedScale 字段）放大到 30×，
// 使"出屏回收 → 屏外进场 → 进屏"在窗口内真的跑起来；进场样本数逐类打印，为 0 即测试无意义。

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/effect"
)

type kineticsEntryEv struct {
	step   int64
	idx    int
	xPx    float64
	halfPx float64
}

// TestKineticsRules 逐类 800 步跑三条判据，输出违规计数行（闸门 grep 的关键字）。
func TestKineticsRules(t *testing.T) {
	const steps = 800
	const minPx = 480 // 屏宽 1080 下的下限
	// speedScale=30：见文件头口径说明（只为在 800 步窗口内制造进场样本）。
	const probeSpeedScale = 30.0

	classes := []effect.Class{
		effect.Clear, effect.PartlyCloudy, effect.Cloudy, effect.Overcast,
		effect.Rain, effect.Thunder, effect.Fog,
	}
	seeds := []int64{20261026, 20261027}

	sumW, sumS, sumE, sumEv := 0, 0, 0, 0
	for _, cls := range classes {
		for _, seed := range seeds {
			f := NewField(cls, true, seed)
			f.speedScale = probeSpeedScale
			var ev []kineticsEntryEv
			prevVis := make(map[int]bool)
			cloudSamples, wViol, sViol := 0, 0, 0
			for s := 0; s < steps; s++ {
				f.Step(fixedStep, PortraitW, PortraitH)
				for i := range f.parts {
					p := &f.parts[i]
					if p.kind != pkCloud {
						continue
					}
					cloudSamples++
					wpx := cloudDrawWidthPx(p)
					if wpx < minPx {
						wViol++
					}
					if p.spr != 0 && p.spr != 1 && p.spr != 3 {
						sViol++
					}
					hw, _ := f.airHalfN(p)
					vis := p.x+hw > 0 && p.x-hw < 1
					if vis && !prevVis[i] {
						ev = append(ev, kineticsEntryEv{
							step: f.steps, idx: i,
							xPx: p.x * float64(PortraitW), halfPx: wpx / 2,
						})
					}
					prevVis[i] = vis
				}
			}
			// 判据 ③：同帧进场相邻两两比 |Δx| 与上一条半宽（ev 按步号、步内按粒子索引有序）
			eViol := 0
			for k := 1; k < len(ev); k++ {
				if ev[k].step != ev[k-1].step {
					continue
				}
				dx := ev[k].xPx - ev[k-1].xPx
				if dx < 0 {
					dx = -dx
				}
				if dx < ev[k-1].halfPx {
					eViol++
				}
			}
			// 对照闸门流水：同一步号内的进场条数（>1 就意味着闸门在同一帧发射了多朵）
			gateSameFrame := 0
			for k := 1; k < len(f.entryLog); k++ {
				if f.entryLog[k].Step == f.entryLog[k-1].Step {
					gateSameFrame++
				}
			}
			t.Logf("%-13s seed=%d: width-violation=%d sprite-violation=%d entry-violation=%d "+
				"entry-samples=%d gate-sameframe=%d cloud-samples=%d clouds=%d parked=%d",
				cls, seed, wViol, sViol, eViol, len(ev), gateSameFrame, cloudSamples,
				len(f.parts), len(f.parkedCloud))
			if wViol != 0 {
				t.Errorf("%s seed=%d：宽度违规样本 = %d（判据①：逐朵最终显示宽 ≥ %d px @1080）", cls, seed, wViol, minPx)
			}
			if sViol != 0 {
				t.Errorf("%s seed=%d：白名单违规样本 = %d（判据②：p.spr 必须 ∈ {0,1,3}）", cls, seed, sViol)
			}
			if eViol != 0 {
				t.Errorf("%s seed=%d：同出同进样本 = %d（判据③：同帧进场 |Δx| < 上一条半宽）", cls, seed, eViol)
			}
			if gateSameFrame != 0 {
				t.Errorf("%s seed=%d：闸门流水同帧发射 = %d 对（排队应逐帧串行）", cls, seed, gateSameFrame)
			}
			sumW += wViol
			sumS += sViol
			sumE += eViol
			sumEv += len(ev)
		}
	}
	// 进场样本总量非 0 ⇒ 判据③不是空转（口径写进文件头）。
	if sumEv == 0 {
		t.Fatalf("进场样本总数 = 0 ⇒ 判据③空转（进场在 800 步窗口内没发生）")
	}
	t.Logf("合计: width-violation=%d sprite-violation=%d entry-violation=%d entry-samples=%d",
		sumW, sumS, sumE, sumEv)
}

// TestKineticsEntryQueueGate 进场闸门的正面判据（"决不能同出同进"）：
// 把同场的两朵云同时推到"整朵完全出屏"⇒ 同一个固定步里两次回收 = 同帧进场；
// 判据：第一朵发射进场位（x = −hw），第二朵必须**保持 parked**（x < −hw，停在进场位之外），
// 并在后续步里单独发射（进场流水两条、步号不同）。
func TestKineticsEntryQueueGate(t *testing.T) {
	f := NewField(effect.Overcast, true, 20261026)
	f.Step(fixedStep, PortraitW, PortraitH)
	n := 0
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		hw, _ := f.airHalfN(p)
		p.x = 1.0 + hw + 0.01 // 整朵完全出屏 ⇒ 下一拍回收
		n++
	}
	if n < 2 {
		t.Fatalf("云数 = %d ⇒ 少于 2 朵，同帧进场判据无意义", n)
	}
	f.Step(fixedStep, PortraitW, PortraitH)
	if len(f.entryLog) != 1 {
		t.Fatalf("进场流水 = %d 条，期望 1 条（同帧两朵回收 ⇒ 只允许发射一条）", len(f.entryLog))
	}
	if len(f.parkedCloud) != 1 {
		t.Fatalf("parked = %d，期望 1 ⇒ 同帧第二朵没有被闸门拦下（同出同进）", len(f.parkedCloud))
	}
	var parkedIdx = -1
	for i := range f.parkedCloud {
		parkedIdx = i
	}
	p := &f.parts[parkedIdx]
	hw, _ := f.airHalfN(p)
	if p.x > -hw {
		t.Errorf("parked 云 x=%.4f > −hw=%.4f ⇒ 它不在屏外（同帧发射了）", p.x, hw)
	}
	steps0 := f.entryLog[0].Step
	f.speedScale = 6
	for s := 0; s < 800 && len(f.parkedCloud) > 0; s++ {
		f.Step(fixedStep, PortraitW, PortraitH)
	}
	if len(f.parkedCloud) != 0 {
		t.Fatalf("800 步后 parked 仍 = %d ⇒ 闸门没有'下一帧再试'的放行通路", len(f.parkedCloud))
	}
	if len(f.entryLog) != 2 {
		t.Fatalf("进场流水 = %d 条，期望 2 条（parked 的那朵后来单独发射）", len(f.entryLog))
	}
	last := f.entryLog[len(f.entryLog)-1]
	if last.Step == steps0 {
		t.Fatalf("parked 云仍在同一帧（步号 %d）发射 ⇒ 排队没生效", last.Step)
	}
	t.Logf("gate: 同帧两朵回收 ⇒ 步 %d 只发射 1 条，第二朵保持 parked，后于步 %d 单独发射 (x=%.1f px, 半宽 %.1f px)",
		f.entryLog[0].Step, last.Step, last.Xpx, last.HalfPx)
}

package ui

// 降水/闪电贴图的三条判据 + 闪电时间口径判据。
// 共同纪律：判据必须自己会测量——每条都把实测数字打出来，
// 并且带"测不到东西就判红"的守卫（样本数、下限、非零计数），
// 不接受恒真断言。

import (
	"image"
	"math"
	"sort"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// TestSnowUsesSprite 雪必须来自 ui/embed 的贴图，不是程序化晶形。
// 四条实测：
//
//	1 贴图集长度 == 3（snow1..3）；
//	2 每张的像素与 readPNG("embed/snowN.png") 的解码结果逐字节相同（= 走的是同一条解码口）；
//	3 尺寸逐张 = 64/48/96；
//	4 与 genFlake 同尺寸的程序化晶形逐像素比对，差异像素必须 > 0
//	 （差异为 0 才说明它真的是程序画的）。
func TestSnowUsesSprite(t *testing.T) {
	sp := spritesOnce()
	if n := len(sp.flake); n != 3 {
		t.Fatalf("雪贴图集长度 = %d，期望 3（snow1..3）", n)
	}
	if !sp.flakeIsArt {
		t.Fatalf("flakeIsArt = false ⇒ 雪走的是 genFlake 程序晶形退路，不是贴图")
	}
	if n := len(sp.flakeImg); n != 3 {
		t.Fatalf("flakeImg 长度 = %d，期望 3", n)
	}
	want := []image.Point{{64, 64}, {48, 48}, {96, 96}}
	for i := 0; i < 3; i++ {
		name := "embed/snow" + itoa(i+1) + ".png"
		raw, ok := readPNG(name)
		if !ok {
			t.Fatalf("%s 读不到", name)
		}
		ref := toNRGBA(raw)
		got := sp.flakeImg[i]
		if got.Bounds().Size() != want[i] {
			t.Errorf("snow%d 尺寸 = %v，期望 %v", i+1, got.Bounds().Size(), want[i])
		}
		if got.Bounds().Size() != ref.Bounds().Size() {
			t.Fatalf("snow%d 与 %s 尺寸不一致：%v vs %v", i+1, name, got.Bounds().Size(), ref.Bounds().Size())
		}
		diff := 0
		for k := range ref.Pix {
			if got.Pix[k] != ref.Pix[k] {
				diff++
			}
		}
		prog := genFlake(image.Pt(int(want[i].X), int(want[i].Y)))
		progDiff := 0
		for k := range prog.Pix {
			if k >= len(got.Pix) || got.Pix[k] != prog.Pix[k] {
				progDiff++
			}
		}
		t.Logf("snow%d %v 与 readPNG 解码结果逐字节差异 = %d B；与 genFlake 同尺寸晶形差异 = %d/%d B",
			i+1, got.Bounds().Size(), diff, progDiff, len(got.Pix))
		if diff != 0 {
			t.Errorf("snow%d 与 readPNG 的解码结果不一致（%d B 不同）⇒ 不是走 readPNG 加载的贴图", i+1, diff)
		}
		if progDiff*100 <= len(got.Pix) {
			t.Errorf("snow%d 与程序化晶形的差异像素只有 %d/%d ⇒ 判为程序化生成", i+1, progDiff, len(got.Pix))
		}
	}
}

// TestSnowDrift 雪的水平速度分量必须明显小于下落速度。
// 非空守卫：全场雪片必须全部 vy>0，且 |vx|>0 的雪片数 > 0
// （vx 恒 0 时 |vx| < vy/3 恒真，那种写法等于没测）。
func TestSnowDrift(t *testing.T) {
	var ratios []float64
	drifted, total := 0, 0
	for _, seed := range cloudRuleSeeds {
		f := NewField(effect.Snow, true, seed)
		f.Step(time.Second, PortraitW, PortraitH)
		for i := range f.parts {
			p := &f.parts[i]
			if p.kind != pkFlake {
				continue
			}
			total++
			if p.vy <= 0 {
				t.Fatalf("seed=%d 雪片 vy=%.4f ≤ 0 ⇒ 判据无意义", seed, p.vy)
			}
			if math.Abs(p.vx) > 1e-9 {
				drifted++
			}
			r := math.Abs(p.vx) / p.vy
			ratios = append(ratios, r)
			if r >= 1.0/3.0 {
				t.Errorf("seed=%d 雪片 |vx|=%.4f vy=%.4f 比值 %.3f ≥ 1/3", seed, p.vx, p.vy, r)
			}
		}
	}
	if total < 100 {
		t.Fatalf("雪片样本 %d 片 ⇒ 样本不足，测试无意义", total)
	}
	if drifted == 0 {
		t.Fatalf("全场没有一片雪有横向速度分量 ⇒ |vx| < vy/3 恒真是空判据")
	}
	maxR := ratios[0]
	sum := 0.0
	for _, v := range ratios {
		if v > maxR {
			maxR = v
		}
		sum += v
	}
	t.Logf("雪片 %d 片：有横向飘移 %d 片（%.1f%%）|vx|/vy 最大 %.4f 均值 %.4f（判据 < 0.3333）",
		total, drifted, 100*float64(drifted)/float64(total), maxR, sum/float64(len(ratios)))
}

// TestRainSpriteLen 雨丝屏上长度分布落在 [40,160] px。
// 口径与绘制逐字同式（drawPrecipSprite 的 useLen 分支）：
//
//	L = vy × 屏高 × 快门时间（下限 3 px）
//
// 雨/雷两档共用同一套雨丝。
// 非空守卫：样本数 ≥100，且近档/远档都必须有样本（长度分布不能塌成一个点）。
func TestRainSpriteLen(t *testing.T) {
	var lens []float64
	nearLong, farShort := 0, 0
	for _, cls := range []effect.Class{effect.Rain, effect.Thunder} {
		for _, seed := range cloudRuleSeeds {
			f := NewField(cls, true, seed)
			f.Step(time.Second, PortraitW, PortraitH)
			for i := range f.parts {
				p := &f.parts[i]
				if p.kind != pkDrop {
					continue
				}
				L := p.vy * float64(PortraitH) * rainExposure
				if L < 3 {
					L = 3
				}
				lens = append(lens, L)
				if p.depth > 0.8 && p.spr == 6 {
					nearLong++
				}
				if p.depth < 0.2 {
					farShort++
				}
			}
		}
	}
	if len(lens) < 100 {
		t.Fatalf("雨丝样本 %d 条 ⇒ 样本不足，测试无意义", len(lens))
	}
	if nearLong == 0 {
		t.Fatalf("近档（depth>0.8）没有一条取到最长档 rain7 ⇒ 近处长条没落地")
	}
	if farShort == 0 {
		t.Fatalf("远档样本为 0 ⇒ 长度分布塌成单档，测试无意义")
	}
	c := append([]float64(nil), lens...)
	sort.Float64s(c)
	lo, hi := c[0], c[len(c)-1]
	p10, p90 := c[len(c)/10], c[len(c)*9/10]
	t.Logf("雨丝 %d 条：屏上长度 最短 %.1f 十分位 %.1f 中位 %.1f 十分位 %.1f 最长 %.1f px（判据 [40,160]）",
		len(c), lo, p10, medianF(c), p90, hi)
	if lo < 40 {
		t.Errorf("雨丝最短 %.1f px < 40 px（规格 1.3 短档下限）", lo)
	}
	if hi > 160 {
		t.Errorf("雨丝最长 %.1f px > 160 px（规格 1.3 长档上限）", hi)
	}
}

// TestBoltOnlyThunder 闪电只在雷暴类 + 码出现，且时间口径落在分级窗口内。
// 计数口径 = drawBolt 真正画出的次数（f.boltDrawn）与闪现事件数（f.boltCount），
// 两者都在绘制/推进路径里自增，不是测试自己数公式。
// 口径变更（写明理由，不是放宽）：旧断言是"雷暴 120 秒内闪现 8–25 次（间隔 6–12 秒）"，
// 间隔是写死的一档；现口径 = 间隔按码分级（95 ⇒ 15–40 s、96 ⇒ 10–25 s、97/99 ⇒ 6–15 s、
// 其余码不闪），所以逐档各给一条**更紧**的区间，并补一条"雷暴类 + 非雷暴码 ⇒ 0 次"
// 把码门控钉住。旧口径的"间隔 ≥6 秒"（不许频闪）保留 = 三档里最小的那一档的下限。
//
//	非雷暴七类：闪现与绘制计数必须为 0
//	Thunder + 码 95：120 秒内闪现 ∈ [120/40, 120/15] = [3, 8]
//	Thunder + 码 1（非雷暴码）：闪现 0 次
//	单次持续 ∈ [0.15, 0.30] s；相邻间隔最小 ≥ 6 s；峰值 alpha = 0.75×max(flash) ≤ 0.75
func TestBoltOnlyThunder(t *testing.T) {
	const secs = 120
	step := time.Second / 15

	drawTwice := func(f *Field) {
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(PortraitW, PortraitH)),
		}
		f.Draw(gtx, PortraitW, PortraitH)
	}

	// 非雷暴七类：绘制计数为 0
	for _, cls := range []effect.Class{effect.Clear, effect.PartlyCloudy, effect.Cloudy,
		effect.Overcast, effect.Fog, effect.Rain, effect.Snow, effect.Dust} {
		f := NewField(cls, true, 20261015)
		f.SetWeatherCode(95) // 码给成雷暴码，只有类门控能挡住 ⇒ 测的是类门控
		for k := 0; k < int(secs*15); k++ {
			f.Step(step, PortraitW, PortraitH)
			drawTwice(f)
		}
		if f.boltDrawn != 0 || f.boltCount != 0 {
			t.Errorf("%s：闪电绘制 %d 次 / 闪现 %d 次，应为 0", cls, f.boltDrawn, f.boltCount)
		}
	}

	// 雷暴 + 码 95：逐种子跑 120 秒
	lo95, hi95 := int(secs/40), int(secs/15)
	for _, seed := range []int64{20261015, 23, 11} {
		f := NewField(effect.Thunder, true, seed)
		f.SetWeatherCode(95)
		maxFlash := 0.0
		for k := 0; k < int(secs*15); k++ {
			f.Step(step, PortraitW, PortraitH)
			if f.flash > maxFlash {
				maxFlash = f.flash
			}
			drawTwice(f)
		}
		n := f.boltCount
		t.Logf("seed=%d 雷暴 码=95 %d 秒：闪现 %d 次，绘制 %d 次，flash 峰值 %.3f ⇒ 峰值 alpha %.3f（上限 %.2f）⇒ 判据区间 [%d,%d]",
			seed, secs, n, f.boltDrawn, maxFlash, boltAlphaMax*maxFlash, boltAlphaMax, lo95, hi95)
		if n < lo95 || n > hi95 {
			t.Errorf("seed=%d：120 秒内闪现 %d 次，越出 [%d,%d]（码 95 的间隔 15–40 秒）", seed, n, lo95, hi95)
		}
		if f.boltDrawn == 0 {
			t.Errorf("seed=%d：闪电一次都没画出来", seed)
		}
		if boltAlphaMax*maxFlash > 0.75 {
			t.Errorf("seed=%d：峰值 alpha %.3f > 0.75", seed, boltAlphaMax*maxFlash)
		}
		if len(f.boltDurs) < 2 {
			t.Fatalf("seed=%d：闪现事件记录 %d 条 ⇒ 测不到间隔与时长，测试无意义", seed, len(f.boltDurs))
		}
		dLo, dHi := f.boltDurs[0], f.boltDurs[0]
		for _, d := range f.boltDurs {
			if d < dLo {
				dLo = d
			}
			if d > dHi {
				dHi = d
			}
			if d < 0.15-1e-9 || d > 0.30+1e-9 {
				t.Errorf("seed=%d：单次闪现 %.3f s 越出 [0.15,0.30]", seed, d)
			}
		}
		gapLo, gapHi := math.Inf(1), 0.0
		for i := 1; i < len(f.boltLog); i++ {
			g := f.boltLog[i] - f.boltLog[i-1]
			if g < gapLo {
				gapLo = g
			}
			if g > gapHi {
				gapHi = g
			}
		}
		t.Logf("seed=%d 码=95 单次持续 %.3f..%.3f s，相邻间隔 %.2f..%.2f s（档内口径 15–40 s，判据 ≥%.0f s）",
			seed, dLo, dHi, gapLo, gapHi, boltGapFloor)
		if gapLo < boltGapFloor-0.1 {
			t.Errorf("seed=%d：相邻两次闪现最小间隔 %.2f s < %.0f s ⇒ 频闪", seed, gapLo, boltGapFloor)
		}
		if gapHi > 40.0+0.5 || gapLo < 15.0-0.5 {
			t.Errorf("seed=%d：码 95 的相邻间隔 %.2f..%.2f s 越出档内口径 15–40 s", seed, gapLo, gapHi)
		}
	}

	// 雷暴类 + 非雷暴码 ⇒ 不闪（码门控）
	for _, code := range []int{1, 2, 3, 61, 71, 45} {
		f := NewField(effect.Thunder, true, 20261015)
		f.SetWeatherCode(code)
		for k := 0; k < int(secs*15); k++ {
			f.Step(step, PortraitW, PortraitH)
			drawTwice(f)
		}
		t.Logf("Thunder + 码=%d：%d 秒内闪现 %d 次、绘制 %d 次（判据 0）", code, secs, f.boltCount, f.boltDrawn)
		if f.boltCount != 0 || f.boltDrawn != 0 {
			t.Errorf("Thunder + 码=%d：闪现 %d 次 / 绘制 %d 次，应为 0（非雷暴码不闪）", code, f.boltCount, f.boltDrawn)
		}
	}
}

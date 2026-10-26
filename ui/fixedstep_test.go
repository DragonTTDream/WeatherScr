package ui

// 批次 A2 / C1 + C3（渲染侧治卡顿）的钉死单测。
//
// 判据来源：spec/a2-cloud-stutter.md 的 C1（位移改固定步长）与 C3（启动期拆开）。
// 全部断言为精确值，无 t.Skip、无放宽。
//
// 与 spec 的已知不自洽处（记 TODO 于 out/notes-a2.md，逐字）：
// C1 的钉死单测写着"喂 dt 序列 61ms,112ms,451ms,1429ms ⇒ 累计步数 = {1,1,3,3}"，
// 而 C1 第 1 条把基准写死 fixedStep = time.Second/60 = 16.6666…ms。
// 61 ms ÷ 16.6667 ms = 3.66 ⇒ 61 ms 那一帧在 1/60 基准下**不可能只走 1 步**；
// {1,1,3,3} 只在 fixedStep ≈ 57–61 ms（≈1/17 s）时成立，与"写死 1/60 s"直接矛盾。
// 处理：以常量口径为准（1/60 s + 追赶上限 3 步），把步数钉成 {3,3,3,3}
// —— 追赶上限对四个 dt 全部生效（四个 dt 都 ≥ 3×fixedStep = 49.999998 ms）；
// "每步位移相等"这条 C1 本质判据逐字钉死（旧口径下四个 dt 的位移比 = 23.4）。

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

// TestFixedStepDisplacement 钉死 C1 第 2/3 条：每步位移只由 fixedStep 导出，
// 与喂进来的实测帧 dt 无关（治"一顿一顿"）；追赶上限 3 步生效。
func TestFixedStepDisplacement(t *testing.T) {
	const vx = 0.02
	dts := []time.Duration{
		61 * time.Millisecond,
		112 * time.Millisecond,
		451 * time.Millisecond,
		1429 * time.Millisecond,
	}
	wantStepDx := vx * fixedStep.Seconds() // 每步位移 = vx × fixedStep（不吃 dt）

	f := NewField(effect.Rain, true, 20261023)
	// 钉一个可预测的粒子：直线、不出屏（vx 0.02 × 12 步 ⇒ x 从 0.5 走到 0.504）
	p := &f.parts[0]
	*p = particle{kind: pkDrop, idx: p.idx, x: 0.5, y: 0.5, vx: vx, vy: 0, rotV: 0, alpha: 0.5, sc: 1}

	var stepsPer []int
	var dxs []float64
	prevX, prevSteps := p.x, f.steps
	for _, dt := range dts {
		f.Step(dt, PortraitW, PortraitH)
		n := int(f.steps - prevSteps)
		if n == 0 {
			t.Fatalf("dt=%s 一步没推进 ⇒ 固定步长没生效", dt)
		}
		if n > fixedStepCatchup {
			t.Fatalf("dt=%s 推进 %d 步 ⇒ 越过追赶上限 %d", dt, n, fixedStepCatchup)
		}
		stepsPer = append(stepsPer, n)
		dxs = append(dxs, p.x-prevX)
		prevX, prevSteps = p.x, f.steps
		if f.acc != 0 {
			t.Fatalf("dt=%s 后 acc = %s ⇒ 长帧多余累加未丢弃置 0（C1 第 2 条）", dt, f.acc)
		}
	}

	// 1 每步位移相等：四帧的位移 ÷ 步数 必须逐字相等，且 = vx×fixedStep
	for i := range dts {
		per := dxs[i] / float64(stepsPer[i])
		if math.Abs(per-wantStepDx) > 1e-12 {
			t.Fatalf("dt=%s：每步位移 %.10f ≠ vx×fixedStep %.10f", dts[i], per, wantStepDx)
		}
		if math.Abs(per-dxs[0]/float64(stepsPer[0])) > 1e-15 {
			t.Fatalf("每步位移不相等：dt=%s 得 %.10f，dt=%s 得 %.10f",
				dts[i], per, dts[0], dxs[0]/float64(stepsPer[0]))
		}
	}
	// 2 追赶上限生效：四个 dt 全部触到上限 3（61 ms = 3.66 步 ≥ 3）
	for i, dt := range dts {
		if stepsPer[i] != fixedStepCatchup {
			t.Errorf("dt=%s：步数 = %d，期望 %d（四个 dt 均 ≥ 3×fixedStep=%s）",
				dt, stepsPer[i], fixedStepCatchup, fixedStepCatchup*fixedStep)
		}
	}
	t.Logf("步数/位移：%v ⇒ 每步位移 %.8f（旧吃 dt 口径下四帧位移比 = %.3f）",
		stepsPer, wantStepDx, float64(dts[len(dts)-1])/float64(dts[0]))
	// 3 对照（旧口径的机制，解析值）：吃 dt 时逐帧位移 = vx·dt ⇒ 比值 = dt 之比 ≈ 23.43
	//   新口径的比值必须 = 1.0（同步数 ⇒ 同位移）⇒ "一顿一顿"的机制被封死。
	oldRatio := float64(dts[len(dts)-1]) / float64(dts[0])
	newRatio := dxs[len(dxs)-1] / dxs[0]
	if oldRatio < 20 {
		t.Fatalf("对照组失效：dt 之比 = %.3f，应显著大于 20", oldRatio)
	}
	if math.Abs(newRatio-1.0) > 1e-12 {
		t.Fatalf("新口径逐帧位移之比 = %.6f，期望 1.0（旧口径同一序列是 %.3f）", newRatio, oldRatio)
	}
	// 4 逐帧位移之比 = 步数之比（不吃 dt）
	if math.Abs(newRatio-float64(stepsPer[len(stepsPer)-1])/float64(stepsPer[0])) > 1e-12 {
		t.Fatalf("逐帧位移之比不等于步数之比 ⇒ 位移仍在吃墙钟 dt")
	}
}

// TestFixedStepNoWallClock 钉死 C1 第 3 条：云带横移只由累加步数驱动，
// 与墙钟无关 ⇒ 同步数 ⇒ 同 x。
//
// 两条驱动路径：A 用 4 个长帧（墙钟合计 2053 ms），B 用 12 个精确 fixedStep 帧
// （墙钟合计 200 ms）。墙钟差 10 倍、步数相同 ⇒ 动画自变量与云带落位逐字相同。
func TestFixedStepNoWallClock(t *testing.T) {
	const seed = 20261023
	fa := NewField(effect.Overcast, true, seed)
	fb := NewField(effect.Overcast, true, seed)
	if fa.speedScale != fb.speedScale {
		t.Fatalf("对照场速度系数不一致 ⇒ 测试无意义")
	}
	long := []time.Duration{61 * time.Millisecond, 112 * time.Millisecond, 451 * time.Millisecond, 1429 * time.Millisecond}
	wallA := time.Duration(0)
	for _, dt := range long {
		fa.Step(dt, PortraitW, PortraitH)
		wallA += dt
	}
	wallB := time.Duration(0)
	for k := 0; k < 12; k++ {
		fb.Step(fixedStep, PortraitW, PortraitH)
		wallB += fixedStep
	}
	if fa.steps != fb.steps {
		t.Fatalf("步数不同：A=%d B=%d ⇒ 对照失效", fa.steps, fb.steps)
	}
	if fa.steps != 12 {
		t.Fatalf("步数 = %d，期望 12（4 帧 × 追赶上限 3 步）", fa.steps)
	}
	if wallA == wallB {
		t.Fatalf("两条驱动路径墙钟相同 ⇒ 测试无意义")
	}
	if fa.t != fb.t {
		t.Fatalf("动画自变量随墙钟变了：A=%.10f B=%.10f ⇒ t 没有由步数导出", fa.t, fb.t)
	}
	n := fa.bandCount()
	if n != fb.bandCount() {
		t.Fatalf("云带片数不同：%d / %d", fa.bandCount(), fb.bandCount())
	}
	for i := 0; i < n; i++ {
		xa, oka := fa.bandPlace(i, fa.t, PortraitW, PortraitH)
		xb, okb := fb.bandPlace(i, fb.t, PortraitW, PortraitH)
		if !oka || !okb {
			t.Fatalf("带 %d 取不到落位（ok=%v/%v）⇒ 测试无意义", i, oka, okb)
		}
		if xa.x != xb.x || xa.y != xb.y {
			t.Fatalf("同步数、异墙钟下云带落位不同：带%d A=(%.10f,%.10f) B=(%.10f,%.10f) ⇒ 位移吃了墙钟",
				i, xa.x, xa.y, xb.x, xb.y)
		}
	}
	// 粒子位移同口径：同步数 ⇒ 逐粒子 x 相同
	for i := range fa.parts {
		if fa.parts[i].x != fb.parts[i].x || fa.parts[i].y != fb.parts[i].y {
			t.Fatalf("同步数、异墙钟下粒子 %d 位移不同：A=(%.10f,%.10f) B=(%.10f,%.10f)",
				i, fa.parts[i].x, fa.parts[i].y, fb.parts[i].x, fb.parts[i].y)
		}
	}
	t.Logf("墙钟 A=%s B=%s（差 %.2f 倍）⇒ 步数同为 %d、t=%.10f、云带 %d 条落位逐字相同",
		wallA, wallB, float64(wallA)/float64(wallB), fa.steps, fa.t, n)
}

// TestFixedStepSpeedScale 钉死"speedScale 乘在固定步长上、语义保持"：
// 位移 = vx × fixedStep × speedScale，且 speedScale 不吃实测 dt。
func TestFixedStepSpeedScale(t *testing.T) {
	for _, ss := range []float64{1.0, 0.5333333, 2.0} {
		f := NewField(effect.Rain, true, 20261024)
		f.SetSpeedScale(ss)
		p := &f.parts[0]
		*p = particle{kind: pkDrop, idx: p.idx, x: 0.5, y: 0.5, vx: 0.02, alpha: 0.5, sc: 1}
		x0 := p.x
		f.Step(fixedStep, PortraitW, PortraitH)
		got := p.x - x0
		want := 0.02 * fixedStep.Seconds() * ss
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("speedScale=%.7f：一步位移 %.12f ≠ vx×fixedStep×ss %.12f", ss, got, want)
		}
	}
	// speedScale = 0 / 未设置 = 不缩放（口径不动）
	f := NewField(effect.Rain, true, 20261025)
	p := &f.parts[0]
	*p = particle{kind: pkDrop, idx: p.idx, x: 0.5, y: 0.5, vx: 0.02, alpha: 0.5, sc: 1}
	f.Step(fixedStep, PortraitW, PortraitH)
	if d := p.x - 0.5; math.Abs(d-0.02*fixedStep.Seconds()) > 1e-12 {
		t.Fatalf("未设 speedScale 时应不缩放：位移 %.12f", d)
	}
}

// TestBootCap 钉死 C3 第 1 条的三档数值（frameIndex 从 0 起算）：
//
//	前 60 帧 ⇒ 80；61–120 帧 ⇒ min(total,160)；之后 ⇒ total
func TestBootCap(t *testing.T) {
	cases := []struct {
		frame, total, want int
	}{
		{0, 320, 80},
		{0, 220, 80},
		{1, 300, 80},
		{59, 320, 80},
		{60, 320, 160},
		{60, 220, 160},
		{119, 320, 160},
		{120, 320, 320},
		{120, 300, 300},
		{300, 320, 320},
		{0, 5, 5},  // 云量档：上限不高于总数
		{60, 5, 5}, // 同上
		{70, 100, 100},
		{70, 200, 160},
		{-3, 220, 80}, // 负数按第 1 帧口径
		{5, 0, 0},     // 空场
	}
	for _, c := range cases {
		if got := BootCap(c.frame, c.total); got != c.want {
			t.Errorf("BootCap(%d, %d) = %d，期望 %d", c.frame, c.total, got, c.want)
		}
	}
	if bootCapFirst != 80 || bootCapSecond != 160 {
		t.Fatalf("启动期三档常量被改：bootCapFirst=%d bootCapSecond=%d（口径 80 / 160）",
			bootCapFirst, bootCapSecond)
	}
}

// TestActiveCountConsumesBootCap 钉死 ActiveCount 消费 BootCap 的帧边界，
// 以及与 C2 削减取小的口径。
func TestActiveCountConsumesBootCap(t *testing.T) {
	f := NewField(effect.Rain, true, 20261023)
	n := f.TotalCount()
	if n != 220 {
		t.Fatalf("Rain 总数 = %d，期望 220（C5a）", n)
	}
	if got := f.ActiveCount(); got != n {
		t.Fatalf("建场未跑帧时活跃数 = %d，期望 %d（建场口径 = 全部）", got, n)
	}
	stepOne := func() { f.Step(time.Millisecond, PortraitW, PortraitH) }
	for k := 1; k <= 60; k++ {
		stepOne()
		if got := f.ActiveCount(); got != 80 {
			t.Fatalf("第 %d 帧 ActiveCount = %d，期望 80（前 60 帧）", k, got)
		}
	}
	for k := 61; k <= 120; k++ {
		stepOne()
		if got := f.ActiveCount(); got != 160 {
			t.Fatalf("第 %d 帧 ActiveCount = %d，期望 160（61–120 帧）", k, got)
		}
	}
	stepOne() // 第 121 帧 ⇒ 稳态
	if got := f.ActiveCount(); got != n {
		t.Fatalf("第 121 帧 ActiveCount = %d，期望 %d（启动期结束 = 不封顶）", got, n)
	}
	// 与 C2 的削减取小：削减到 100（< 启动期上限）⇒ 生效值 = 100
	f.adaptFps(20) // 单轮不恢复（滞回未武装）
	f.recoverStreak = recoverArmRounds
	f.adaptFps(20)
	f.active = 100
	if got := f.ActiveCount(); got != 100 {
		t.Fatalf("削减到 100 后 ActiveCount = %d，期望 100（取小）", got)
	}
}

// TestBakeScaleFirstFrameAfterSizeChange 钉死 C3 第 2 条：
// 尺寸变化后的首帧用比稳态粗一档的尺度，第二帧起回到稳态尺度，稳态口径不变。
func TestBakeScaleFirstFrameAfterSizeChange(t *testing.T) {
	if sky.SkyCacheScale != 2 {
		t.Fatalf("稳态烘焙尺度 sky.SkyCacheScale = %d，口径 2（禁改）", sky.SkyCacheScale)
	}
	if sky.SkyCacheBootScale != sky.SkyCacheScale*2 {
		t.Fatalf("首帧尺度 sky.SkyCacheBootScale = %d，期望比稳态粗一档 = %d", sky.SkyCacheBootScale, sky.SkyCacheScale*2)
	}
	if got := sky.BakeScale(true); got != 4 {
		t.Errorf("sky.BakeScale(true) = %d，期望 4", got)
	}
	if got := sky.BakeScale(false); got != 2 {
		t.Errorf("sky.BakeScale(false) = %d，期望 2（稳态）", got)
	}

	sky.SkyCache = sky.SkyCacheType{}
	sky.SkyStats = sky.SkyCacheStats{}
	big := image.Pt(800, 600)
	e0 := sky.SkyCached(big, effect.Clear, true, nil, 0) // 冷启动首烘 = 稳态口径
	if e0 == nil {
		t.Fatal("sky.SkyCached 返回 nil")
	}
	if dx, dy := e0.Img.Bounds().Dx(), e0.Img.Bounds().Dy(); dx != 800/sky.SkyCacheScale || dy != 600/sky.SkyCacheScale {
		t.Fatalf("冷启动首烘尺度不是稳态：%dx%d，期望 %dx%d", dx, dy, 800/sky.SkyCacheScale, 600/sky.SkyCacheScale)
	}
	small := image.Pt(400, 300)
	e1 := sky.SkyCached(small, effect.Clear, true, nil, 0) // 尺寸变化后的首帧 ⇒ 粗一档
	if dx, dy := e1.Img.Bounds().Dx(), e1.Img.Bounds().Dy(); dx != 400/sky.SkyCacheBootScale || dy != 300/sky.SkyCacheBootScale {
		t.Fatalf("尺寸变化首帧尺度 = %dx%d，期望 %dx%d（粗一档）", dx, dy, 400/sky.SkyCacheBootScale, 300/sky.SkyCacheBootScale)
	}
	e2 := sky.SkyCached(small, effect.Clear, true, nil, 0) // 第二帧 ⇒ 回到稳态
	if dx, dy := e2.Img.Bounds().Dx(), e2.Img.Bounds().Dy(); dx != 400/sky.SkyCacheScale || dy != 300/sky.SkyCacheScale {
		t.Fatalf("第二帧尺度 = %dx%d，期望 %dx%d（稳态）", dx, dy, 400/sky.SkyCacheScale, 300/sky.SkyCacheScale)
	}
	e3 := sky.SkyCached(small, effect.Clear, true, nil, 0)
	if e3 != e2 {
		t.Fatalf("第三帧换了图 ⇒ 稳态没命中缓存")
	}
	if b := sky.SkyCacheGet().Builds; b != 3 {
		t.Fatalf("Builds = %d，期望 3（冷启动 + 首帧粗 + 第二帧升级，之后全命中）", b)
	}
	// 同类同尺寸的月相变化（键变、尺寸没变）⇒ 稳态尺度，不吃粗尺度
	e4 := sky.SkyCached(small, effect.Clear, true, f64p(0.5), 0)
	if dx := e4.Img.Bounds().Dx(); dx != 400/sky.SkyCacheScale {
		t.Fatalf("键变（尺寸未变）后尺度 = %d，期望 %d（稳态）", dx, 400/sky.SkyCacheScale)
	}
	t.Logf("尺寸变化首帧 %dx%d → 第二帧 %dx%d → 命中同图",
		e1.Img.Bounds().Dx(), e1.Img.Bounds().Dy(), e2.Img.Bounds().Dx(), e2.Img.Bounds().Dy())
}

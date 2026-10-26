package ui

// P5b 测量件的钉死单测（spec/p5b-dbg.md 判据 3，共 5 条）。
//
// 开关：环境变量 WS_DBG_FRAMES=1 ⇒ 生产构造路径（NewField / NewFieldEntering）
// 也置位 probeCount（接线点 ui/effects.go 的 newField，计数通路 = P1 已有的
// probeCount/draws，见 ui/overlap_probe.go）。
// 未设 ⇒ probeCount 恒 false ⇒ 计数与日志一概不产生。
//
// 手法复用 ui/overlap_probe_test.go:188 的 TestProbeCounterDoesNotChangeOps
// （图元序列用 OpsLen 的 data/refs 长度对比，gioui v0.10.3 的 op.Ops 不导出长度）。

import (
	"os"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/metrics"
)

// dbgSeed 与 P1/P3 探针表同一个固定种子（cmd/overlapprobe/main.go:17 probeSeed），
// 目的是让下面的对照数字能逐字复现。
const dbgSeed int64 = 20261023

// dbgStep 开/关对比里每帧的推进量：同一常数 ⇒ 两条路径的输入序列逐字相同。
const dbgStep = 16 * time.Millisecond

// dbgRainDrawsPerFrame 对照表数字：out/overlap-after-p3.tsv 的 Rain 行
// （w=1080 h=1920 seed=20261023，drawsPerFrame=228，P3 之后未动 Rain）。
const dbgRainDrawsPerFrame = 228

// enableDbgFrames 打开开关（t.Setenv ⇒ 测试结束自动还原，不污染同包其它测试）。
func enableDbgFrames(t *testing.T) {
	t.Helper()
	t.Setenv("WS_DBG_FRAMES", "1")
	if !WSDbgFrames() {
		t.Fatalf("WS_DBG_FRAMES=1 已设但 WSDbgFrames()=false")
	}
}

// disableDbgFrames 关闭开关：先走"真·未设"这条路（os.Unsetenv + 恢复），
// 判据 3 第 1 条要求的就是"环境变量根本没设"这个现场。
func disableDbgFrames(t *testing.T) {
	t.Helper()
	old, had := os.LookupEnv("WS_DBG_FRAMES")
	os.Unsetenv("WS_DBG_FRAMES")
	t.Cleanup(func() {
		if had {
			os.Setenv("WS_DBG_FRAMES", old)
		} else {
			os.Unsetenv("WS_DBG_FRAMES")
		}
	})
	if WSDbgFrames() {
		t.Fatalf("WS_DBG_FRAMES 已 unset 但 WSDbgFrames()=true")
	}
}

// 判据 3 第 1 条：未设 WS_DBG_FRAMES ⇒ DrawCount() 恒 0（默认不计数、零开销）。
func TestDbgFramesOffByDefault(t *testing.T) {
	disableDbgFrames(t)
	for _, env := range []string{"", "0", "true", "yes", "11"} {
		if env != "" {
			t.Setenv("WS_DBG_FRAMES", env)
		} else {
			os.Unsetenv("WS_DBG_FRAMES")
		}
		if WSDbgFrames() {
			t.Fatalf("WS_DBG_FRAMES=%q 被当成启用（口径：只有恰好 \"1\" 才启用）", env)
		}
		for _, isDay := range []bool{true, false} {
			for _, cls := range []effect.Class{effect.Clear, effect.Cloudy, effect.Overcast, effect.Fog, effect.Rain, effect.Thunder, effect.Snow, effect.Dust} {
				f := NewField(cls, isDay, dbgSeed)
				var o op.Ops
				f.Draw(metrics.ProbeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
				if got := f.DrawCount(); got != 0 {
					t.Errorf("env=%q %s/isDay=%v 默认路径 DrawCount()=%d，期望恒 0", env, cls, isDay, got)
				}
				g := NewFieldEntering(cls, isDay, dbgSeed)
				var og op.Ops
				g.Draw(metrics.ProbeGTX(&og, PortraitW, PortraitH), PortraitW, PortraitH)
				if got := g.DrawCount(); got != 0 {
					t.Errorf("env=%q NewFieldEntering %s/isDay=%v DrawCount()=%d，期望恒 0", env, cls, isDay, got)
				}
			}
		}
	}
	// 默认路径不得留下任何计数状态：Draws()（P1 口径）同样恒 0。
	f := NewField(effect.Rain, true, dbgSeed)
	if f.Draws() != 0 {
		t.Errorf("默认路径 Draws()=%d，期望 0", f.Draws())
	}
}

// 判据 3 第 2 条：设了 WS_DBG_FRAMES=1 ⇒ 一次完整绘制调用后 DrawCount()>0，
// 且 ResetDrawCount() 之后归 0。
func TestDbgFramesOnCountsAndReset(t *testing.T) {
	enableDbgFrames(t)
	for _, cls := range []effect.Class{effect.Clear, effect.Cloudy, effect.Overcast, effect.Fog, effect.Rain, effect.Snow} {
		f := NewField(cls, true, dbgSeed) // 生产构造路径：开关在 newField 里接线
		var o op.Ops
		f.Draw(metrics.ProbeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
		n := f.DrawCount()
		if n <= 0 {
			t.Fatalf("%s 开启开关后一帧 DrawCount()=%d，期望 >0", cls, n)
		}
		f.ResetDrawCount()
		if got := f.DrawCount(); got != 0 {
			t.Errorf("%s ResetDrawCount() 后 DrawCount()=%d，期望 0", cls, got)
		}
		// 计数器开着 ⇒ 第二帧继续计数（主循环靠"打印后清零"得到每帧值）。
		f.Draw(metrics.ProbeGTX(new(op.Ops), PortraitW, PortraitH), PortraitW, PortraitH)
		if got := f.DrawCount(); got <= 0 {
			t.Errorf("%s 第二帧 DrawCount()=%d，期望 >0（计数器未被永久关掉）", cls, got)
		}
	}
	// NewFieldEntering（类别切换建场）走同一个 newField ⇒ 同样受开关控制。
	g := NewFieldEntering(effect.Rain, true, dbgSeed)
	var og op.Ops
	g.Draw(metrics.ProbeGTX(&og, PortraitW, PortraitH), PortraitW, PortraitH)
	if g.DrawCount() <= 0 {
		t.Errorf("NewFieldEntering 开启开关后 DrawCount()=%d，期望 >0", g.DrawCount())
	}
}

// 判据 3 第 3 条：**真跑**开/关两次同一序列，绘制产生的图元序列逐字一致
// （复用 TestProbeCounterDoesNotChangeOps 的 OpsLen 手法，不是注释声明）。
func TestDbgFramesToggleKeepsOpsIdentical(t *testing.T) {
	cases := []struct {
		cls   effect.Class
		isDay bool
	}{
		{effect.Clear, true},
		{effect.Clear, false},
		{effect.Cloudy, true},
		{effect.Overcast, false},
		{effect.Fog, true},
		{effect.Rain, true},
		{effect.Thunder, true},
		{effect.Snow, true},
		{effect.Dust, true},
	}
	for _, c := range cases {
		// 开：WS_DBG_FRAMES=1 走生产构造路径（newField 里置位 probeCount）
		enableDbgFrames(t)
		on := NewField(c.cls, c.isDay, dbgSeed)
		if !on.probeCount {
			t.Fatalf("%s/isDay=%v 开关已设但 probeCount=false（接线没挂上）", c.cls, c.isDay)
		}
		var lensOn [][2]int
		for i := 0; i < 3; i++ {
			var o op.Ops
			on.Step(dbgStep, PortraitW, PortraitH)
			on.Draw(metrics.ProbeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
			d, r := metrics.OpsLen(&o)
			if d < 0 || r < 0 {
				t.Fatalf("OpsLen 没取到内部切片（探针失效）")
			}
			lensOn = append(lensOn, [2]int{d, r})
		}
		// 关：环境变量根本没设 ⇒ 同种子同序列
		disableDbgFrames(t)
		off := NewField(c.cls, c.isDay, dbgSeed)
		if off.probeCount {
			t.Fatalf("%s/isDay=%v 未设开关但 probeCount=true", c.cls, c.isDay)
		}
		var lensOff [][2]int
		for i := 0; i < 3; i++ {
			var o op.Ops
			off.Step(dbgStep, PortraitW, PortraitH)
			off.Draw(metrics.ProbeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
			d, r := metrics.OpsLen(&o)
			lensOff = append(lensOff, [2]int{d, r})
		}
		for i := range lensOn {
			if lensOn[i] != lensOff[i] {
				t.Errorf("%s/isDay=%v 第 %d 帧图元序列不一致：开=%v 关=%v",
					c.cls, c.isDay, i, lensOn[i], lensOff[i])
			}
		}
		if off.DrawCount() != 0 {
			t.Errorf("%s/isDay=%v 关闭路径 DrawCount()=%d，期望 0", c.cls, c.isDay, off.DrawCount())
		}
		if on.DrawCount() <= 0 {
			t.Errorf("%s/isDay=%v 开启路径 3 帧累计 DrawCount()=%d，期望 >0", c.cls, c.isDay, on.DrawCount())
		}
	}
}

// 判据 3 第 4 条：Rain 类开启计数后一帧的 DrawCount() 与探针表 drawsPerFrame
// 同量级（±15%，对照 out/overlap-after-p3.tsv 的 Rain 行 = 228）。
func TestDbgFramesRainMatchesProbeTable(t *testing.T) {
	enableDbgFrames(t)
	f := NewField(effect.Rain, true, dbgSeed)
	var o op.Ops
	f.Draw(metrics.ProbeGTX(&o, PortraitW, PortraitH), PortraitW, PortraitH)
	got := f.DrawCount()
	lo := float64(dbgRainDrawsPerFrame) * 0.85
	hi := float64(dbgRainDrawsPerFrame) * 1.15
	if float64(got) < lo || float64(got) > hi {
		t.Errorf("Rain 一帧 DrawCount()=%d 落在 ±15%% 之外（期望 %.0f–%.0f，对照表 228）",
			got, lo, hi)
	}
	// 与探针构造路径同一通路 ⇒ 同一场同帧必须同数（没有第二条计数通路）。
	p := NewFieldProbeCounted(effect.Rain, true, dbgSeed)
	p.Draw(metrics.ProbeGTX(new(op.Ops), PortraitW, PortraitH), PortraitW, PortraitH)
	if p.Draws() != got {
		t.Errorf("生产路径 %d 与探针路径 %d 计数不一致（存在第二条通路？）", got, p.Draws())
	}
}

// 判据 3 的补充：主循环"取本帧值 + 打印后清零"的口径钉死 ——
// 连画三帧逐帧读清，每帧读数都等于单帧绘制次数（不会累加、不会漏清零）。
func TestDbgFramesPerFrameResetSemantics(t *testing.T) {
	enableDbgFrames(t)
	f := NewField(effect.Rain, true, dbgSeed)
	var single int
	for i := 0; i < 3; i++ {
		f.Draw(metrics.ProbeGTX(new(op.Ops), PortraitW, PortraitH), PortraitW, PortraitH)
		n := f.DrawCount()
		f.ResetDrawCount()
		if n <= 0 {
			t.Fatalf("第 %d 帧 DrawCount()=%d，期望 >0", i, n)
		}
		if i == 0 {
			single = n
		}
		if n != single {
			t.Errorf("第 %d 帧 draws=%d 与第 0 帧 %d 不等 ⇒ 读的不是本帧值（累加未清）", i, n, single)
		}
		if f.DrawCount() != 0 {
			t.Errorf("第 %d 帧清零后 DrawCount()=%d，期望 0", i, f.DrawCount())
		}
	}
}

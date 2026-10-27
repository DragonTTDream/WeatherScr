// Package particles 是雨/雪/尘/雷的粒子口径：粒子生成（落位/速度/透明度）、贴图长度档与屏上尺寸档、
// 越界回收窗口（寿命）；消费者 = ui（ui/effects.go 的粒子场 Field 在生成与绘制处调用本包）。
// 本包是 拆分 从 ui/effects.go **逐字迁出**的粒子函数群
// 公式、常量、随机数取用顺序、生成顺序一行没改。
// 对 ui 私有类型的依赖一律改成参数传入（粒子字段值、贴图张数、随机数源），
// 判据 = ui/particles/parity_test.go 的 TestParticlesParity（固定输入对拍 + 200 帧连跑对拍）。
package particles

import (
	"math"
	"math/rand"
)

// ---- 雨丝尺寸档（逐字迁自 ui/effects.go:390-440） ----

// RainStreakCapPx 每张雨丝贴图**短轴（屏上宽）**的上限（px，1080 屏口径）。
// 长轴 = 运动模糊长度 L = vy × 快门时间（既有口径，近处长、远处短），
// 短轴按贴图宽高比等比放大 ⇒ 短轴 = L × 48 / 贴图高。
// 实测七张贴图在包围盒中线上 alpha≥64 的列数占包围盒宽的比例（软带占比，
// 与 bin/prepare_particles.py 同一份源图、同一裁包围盒口径）：
//
//	rain1 0.081 rain2 0.075 rain3 0.086 rain4 0.213
//	rain5 0.140 rain6 0.366 rain7 0.443
//
// 同一套等比放大下，rain7 在近档（L=160 px）的短轴 44 px、软带 19 px，
// 读成一条糊带而不是雨丝（离屏判据 TestRainBottomEdgeNoSplash 的口径：
// 单行实心段 ≥6 px 判为带）。软带占比相差 5.9 倍 ⇒ 上限必须逐张给。
// 取值 = 软带目标宽 2.5 px ÷ 占比，收进 [6, 40]（下限保可见、上限防糊带）：
//
//	rain1 31 rain2 33 rain3 29 rain4 12 rain5 18 rain6 7 rain7 6
//
// 实测（同一判据、同一固定种子）：短轴上限 40/40/40/23/36/14/11 时最长实心段 9 px，
// 换成上表后 5 px（判据 <6），带内粒子层变化像素 95 px（雨丝仍画到下边缘）。
var RainStreakCapPx = []float32{31, 33, 29, 12, 18, 7, 6}

// RainStreakCapFor 第 spr 张雨丝贴图的短轴上限（贴图组缺失时退到最宽一档）。
func RainStreakCapFor(spr int) float32 {
	if spr < 0 {
		spr = 0
	}
	if spr >= len(RainStreakCapPx) {
		spr = len(RainStreakCapPx) - 1
	}
	return RainStreakCapPx[spr]
}

// RainBaseAlpha 基础 alpha 随景深 0.35 → 0.70
// 远淡近浓。
func RainBaseAlpha(depth float64) float64 {
	return 0.35 + 0.35*depth
}

// RainDepthFactor 显式景深因子：
// 返回 (alpha 因子, 长度因子)。远景 alpha×0.35 且长度×0.4，
// 近景全额 ⇒ "近实远虚"达到肉眼可分的量级。
func RainDepthFactor(depth float64) (float64, float64) {
	switch {
	case depth < 0.4:
		return 0.35, 0.40
	case depth <= 0.7:
		return 0.70, 0.75
	default:
		return 1.00, 1.00
	}
}

// 雨丝的快门时间与风（参照 github.com/0magnet/termanim/rain）。
// 尾/头 alpha 比例与线色常量随描边画法一起删除（贴图路径由贴图自身的 alpha 形状给）。
const (
	// RainExposure 快门时间（秒）。线长 = 速度 × 快门时间 = 运动模糊。
	// 参照取 1/20 秒：足够让相邻帧的雨线重叠，下落看起来连续而非频闪。
	// 它是**时间**不是帧数 —— 绑帧率会在掉帧时把每条线拉长，
	// 而那正是雨最不该 visibly 变化的时刻。
	// 现值由的长度口径反解（1080x1920 屏）：
	//	L = vy × 屏高 × 快门，vy = 0.6 + 1.35·depth ∈ [0.6, 1.95] 屏高/秒
	//	长档上限 160 px ⇒ 快门 = 160 ÷ (1.95 × 1920) = 0.04273
	//	短档下限随之 = 0.6 × 1920 × 0.04273 = 49.2 px ≥ 40 px
	// 旧值 0.05 的同一式给出 L ∈ [57.6, 187.2] px ⇒ 长档越出 160 px 上限。
	// 取"改快门"而不是"把 L 钳到 160"：钳位会让 depth>0.79 的那 21% 雨滴
	// 全挤在 160 px 一个点上，长度分布出现尖峰；改快门保持逐 depth 单调。
	RainExposure = 0.04273
	// RainSlantBase 风：水平像素 / 垂直像素。参照取 0.28。
	// 垂直的雨像测试卡；一点倾斜才像天气。
	RainSlantBase = 0.28
)

// WindJitterAmp 每粒子倾角抖动幅度（rad），由粒子索引生成 ⇒ 跨帧稳定。
// 雨丝的倾角主体来自粒子自己的速度（atan2(vx, vy)），这里只加逐粒子的固定抖动。
const WindJitterAmp = 0.03

// ---- 雪花屏上尺寸档（逐字迁自 ui/effects.go:776-792） ----

// ---- C3 雪花屏上尺寸----
// 实测（取证 35.1，/tmp/fxdiag-095505 FX_DEBUG 粒子清单）：pkFlake 源 18/26/36 px，
// 屏上 dw = 2.3–12.8 px、alpha 0.42–0.93 ⇒ 在 1080×1920 上就是 2–13 px 的白点，
// 压在亮雾底上 ⇒ "雪天没有飘雪"成立，根因 = 屏上尺寸太小。
// 改法：生成时**按目标屏宽反解 sc**（屏上 dw 逐朵 = 目标 px，逐字落在 12–26）：
//
//	dw = 源图宽 × sc × (0.55+0.45·depth) × scaleMul ÷ spriteNetDiv
//	⇒ sc = 目标px × spriteNetDiv ÷ (源图宽 × (0.55+0.45·depth) × scaleMul)
//
// 数量与下落速度口径不动。
const (
	// FlakePxLo 雪花屏上尺寸下限（px）。
	FlakePxLo = 12.0
	// FlakePxHi 雪花屏上尺寸上限（px）。
	FlakePxHi = 26.0
	// FlakeAlphaMin 亮雾底上可见的 alpha 下限（Draw 里逐帧兜底）。
	FlakeAlphaMin = 0.60
)

// SpriteNetDiv 是雪花尺寸反解式里的净倍率分母（ui/assets.go:375 的同一名词，
// 分母本体归 ui 的贴图净倍率口径 ⇒ 以参数注入，本包不复制常量，避免双份真值）。
// 说明：规格要求"对 ui 私有依赖改参数传入"，故 SizeFlakeByTarget 收 netDiv 入参。

// SizeFlakeByTarget 把雪花屏上尺寸反解到 [FlakePxLo, FlakePxHi] px。
//
//	屏上 dw = 源图宽 × sc × (0.55+0.45·depth) × scaleMul ÷ netDiv
//	⇒ sc = 目标px × netDiv ÷ (源图宽 × (0.55+0.45·depth) × scaleMul)
//
// 实测改前：源 18/26/36 px、sc 0.30–1.00、depth 因子 0.55–1.0、scaleMul 0.75–1.25
// ⇒ 屏上 dw = 2.3–12.8 px（取证 35.1）⇒ 1080×1920 上是 2–13 px 的白点。
// 数量与下落速度口径不动。
// 返回 (sc, ok)：ok=false 对应旧实现里 `szX <= 0 || den <= 0` 的直接 return
// ⇒ 调用方保持粒子原 sc（旧实现逐字行为）。
func SizeFlakeByTarget(r *rand.Rand, netDiv, srcW, depth, scaleMul float64) (float64, bool) {
	den := srcW * (0.55 + 0.45*depth) * scaleMul
	if srcW <= 0 || den <= 0 {
		return 0, false
	}
	target := FlakePxLo + (FlakePxHi-FlakePxLo)*r.Float64()
	return target * netDiv / den, true
}

// ---- 越界回收窗口（寿命，逐字迁自 ui/effects.go:794-799） ----

// 越界回收的 y 窗口（effects.go 原 Step 的字面量 1.15 / -0.35，提成常量：
// 带外可用区与回收窗口必须同口径，否则放出去在窗口外的粒子会被立刻回收。
const (
	// RecycleYHi 回收窗口上界（归一化屏高）。
	RecycleYHi = 1.15
	// RecycleYLo 回收窗口下界（归一化屏高）。
	RecycleYLo = -0.35
)

// OffscreenCenter 雨/雪/尘/星/雾/光晕的**中心口径**越界判据（逐字 = 迁移前
// advanceOneStep 里的那一行：y 出 [RecycleYLo, RecycleYHi]、x 出 [-0.35, 1.25]）。
// 粒子云按"整朵完全出屏"另判（口径归 ui 的云半宽 hw/hh ⇒ 留在 ui，不在本函数内）。
func OffscreenCenter(x, y float64) bool {
	return y > RecycleYHi || y < RecycleYLo || x > 1.25 || x < -0.35
}

// ---- 粒子生成（逐字迁自 ui/effects.go 的 spawn 分支：1259-1281 / 1317-1341 / 1343-1352） ----

// Precip 是生成函数写回的粒子字段集（逐字对应 ui 侧粒子结构体的同名字段；
// 未列出的字段 = 旧分支没有写的字段，调用方保持原值）。
type Precip struct {
	X, Y   float64
	Vx, Vy float64
	Sc     float64
	Alpha  float64
	Omega  float64
	Amp    float64
	RotV   float64
	Phase  float64
	HasPh  bool // 旧分支是否写了 phase（Snow 分支写了，Rain/Dust 分支没写）
}

// SpawnDrop 造一颗雨滴（Rain / Thunder 全走这一支，ui/effects.go:1259-1281 逐字）。
// 参照实现：github.com/0magnet/termanim/rain（rain.go:176-200）。
// 它的反馈："depth drives everything … **Drops all moving at one
// speed read as falling dashes**; spread the speeds and brightnesses
// over a range and the eye separates them into layers and calls it rain."
// 参照的三条量**全部零随机**：
// vy = h·(0.6 + 1.35·depth) 近处 0.51 s 穿屏、远处 1.67 s（3.25 倍）
// length = vy · exposure(0.05 s) 线长由**速度导出**（运动模糊）
// bright = 0.12 + depth²·0.88 平方压向亮端（8 倍）
// 此前的失误模式：把这些量再乘一个大随机项（p.sc / brightnessMul /
// alphaMul / scaleMul，单调序当场被打乱 ⇒ 肉眼只剩均匀感。
// ⇒ 这里**不乘任何随机项**。
// depth 是调用方在分支之前已经抽好的景深（旧实现：p.depth = r.Float64 在 switch 之前）；
// fresh=false 时 y 取屏幕外起点（-0.05 - 0.15·U），与旧实现逐字相同。
func SpawnDrop(r *rand.Rand, fresh bool, depth float64) Precip {
	var p Precip
	p.X = r.Float64()
	p.Y = r.Float64()
	if !fresh {
		p.Y = -0.05 - 0.15*r.Float64()
	}
	p.Sc = 0.55 + 0.85*r.Float64()
	p.Alpha = 0.30 + 0.45*r.Float64()
	p.Vy = 0.6 + 1.35*depth
	p.Vx = -(RainSlantBase * (0.7 + 0.6*depth)) * p.Vy
	return p
}

// SpawnFlake 造一片雪花（Snow 的 pkFlake 分支，ui/effects.go:1317-1341 逐字）。
// 雪是近处飘得快、远处慢（与雨同向：近快远慢）；横向飘移（
// 慢落带飘移，|vx| 必须明显小于 vy）：
// 取值口径：vy 的最小值 0.0398 屏高/秒（depth=0、scaleMul 下档），
// |vx| 上限 0.008 屏宽/秒 ⇒ 逐粒子 |vx|/vy ≤ 0.20 < 1/3（自检 TestSnowDrift 的判据）。
// scaleMul 按调用方**当时的值**传入：旧实现里粒子的 scaleMul 在 spawn 尾段才赋值
// ⇒ 走到本分支时它恒为 0，sjit 当场取 0.92（迁移逐字保留这一事实，不改口径）。
func SpawnFlake(r *rand.Rand, fresh bool, depth, scaleMul float64) Precip {
	var p Precip
	p.X = r.Float64()
	p.Y = r.Float64()
	if !fresh {
		p.Y = -0.05 - 0.15*r.Float64()
	}
	p.Sc = 0.30 + 0.70*r.Float64()
	p.Alpha = 0.40 + 0.55*r.Float64()
	// 视差速度：景深单调主导 + 小抖动。
	// 旧写法随机项跨度 3.2 倍、景深项 2.6 倍，同量级 ⇒ 排序被打乱。
	sdv := 0.30 + 0.70*depth
	sjit := 0.92 + 0.16*scaleMul/1.25
	p.Vy = 0.13 * sdv * sjit
	p.Vx = -(0.002 + 0.006*depth)
	p.Omega = 0.8 + 2.2*r.Float64()
	p.Amp = 0.10 + 0.2*r.Float64()
	p.RotV = (r.Float64() - 0.5) * 2.6
	// 横向摆动：雪是飘不是落
	p.Phase = r.Float64() * math.Pi * 2
	p.Amp += 0.0
	p.HasPh = true
	return p
}

// SpawnMote 造一团尘（Dust 分支，ui/effects.go:1343-1352 逐字）。
// fresh=false 时 x 取屏幕外起点（-0.1 - 0.2·U），与旧实现逐字相同。
func SpawnMote(r *rand.Rand, fresh bool) Precip {
	var p Precip
	p.X = r.Float64()
	p.Y = 0.08 + r.Float64()*0.88
	if !fresh {
		p.X = -0.1 - 0.2*r.Float64()
	}
	p.Sc = 0.35 + 1.15*r.Float64()
	p.Alpha = 0.10 + 0.26*r.Float64()
	p.Vx = 0.16 + 0.50*r.Float64()
	p.Vy = (r.Float64() - 0.5) * 0.05
	p.RotV = (r.Float64() - 0.5) * 0.5
	return p
}

// ---- 贴图变体 / 长度档挑选（逐字迁自 ui/effects.go:1363-1400） ----

// 近大远小：depth 越大 = 越近 = 越大/越长档。
// （上一版写成 2-int(depth*2.999)，方向反了，导致"雨丝雪片没有
// 透视、像静态噪点"—— 上述判读的根因就是这个符号。
// 旧版 **变体轮换**：云/雾/星/雪按 `index % N` 取变体
// （N = 该类变体精灵数），把"同一形状连拿 N 次"排布打散；
// 雨（pkDrop）仍按 depth 挑长度档（线宽/长度都随景深走），
// 尘（pkMote）保持原随机挑法（未指定）。

// DropSpriteTier 雨（pkDrop）按景深挑长度档。
// 实测七张贴图裁包围盒后的长宽比 2.29–3.66（rain7 = trace_07 最长），近层取到最长的那一档。
// n = 雨丝贴图张数（贴图组缺失时 n=0 ⇒ 不钳位，旧实现逐字）。
func DropSpriteTier(depth float64, n int) int {
	spr := int(depth * 6.999)
	if spr < 0 {
		spr = 0
	}
	if n > 0 && spr >= n {
		spr = n - 1
	}
	return spr
}

// FlakeSpriteIndex 雪（pkFlake）的变体轮换：index % 变体数（旧实现逐字）。
func FlakeSpriteIndex(idx, n int) int { return idx % n }

// MoteSpriteIndex 尘（pkMote）的随机挑法（未指定档 ⇒ 走随机数源，旧实现逐字）。
func MoteSpriteIndex(r *rand.Rand, n int) int { return r.Intn(n) }

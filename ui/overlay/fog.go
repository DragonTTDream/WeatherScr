package overlay

import (
	"image"
	"math/rand"

	"github.com/DragonTTDream/WeatherScr/ui/metrics"
)

// ---- 雾的 sc 区间与浓度倍率（逐字迁自 ui/effects.go:546-553）----
//
// 原注释（同块里的云口径与检索依据留在 ui/effects.go，与云常量同处一块）：
// 雾 sc 上限 2.8（此前 3.2 时"很夸张 很糊"）；历史口径 雾 sc = 1.6 + 2.6·U → 上限 4.2。

const (
	// FogScBase 雾的 sc 下限。
	FogScBase = 1.3
	// FogScSpan 雾的 sc 跨度（上限 2.8 = 1.3 + 1.5）。
	FogScSpan = 1.5 // 上限 2.8（此前 3.2 时"很夸张 很糊"）
	// FogAlphaMul 雾浓度倍率：**运行时参数，不烘进贴图**。
	// 需求：雾比初版淡一档（对比图选定，倍率 0.6）。
	// 上一版把 0.6 烘进 fog*.png 的 alpha（均值 93 → 55），
	// 结果出厂贴图与选定的那张效果图不是同一份东西 ⇒ 贴图保持效果图原样，
	// 淡浓在这里调，出图与出厂永远可比。
	FogAlphaMul = 0.6
)

// ---- 雾团生成（逐字迁自 ui/effects.go 的 spawn 分支：Fog 的 pkFog 档 :1165-1172、
// Snow 的灰幕档 :1201-1213）----

// PlaceY 是雾团的 y 落位口：旧实现里那一句 = bandFreeY(r, bandPadJitN(pkFog, f.sh))，
// 带宽与带避让口径归 ui/kinetics 的调用方（ui 侧），**本包不复制那条口径**，
// 由消费者 ui 以回调注入（回调在旧实现里的取用位置 = x 之后一个随机数，逐字同序）。
type PlaceY func() float64

// FogSpawn 是雾团生成写回的字段集（逐字对应 ui 侧粒子结构的同名字段；
// 未列出的字段 = 旧分支没有写的字段，调用方保持原值）。
// Depth = 旧分支**最后**一行写下的景深（alpha 用的是调用方传进来的旧 depth，口径不动）。
type FogSpawn struct {
	X, Y   float64
	Sc     float64
	Alpha  float64
	Vx, Vy float64
	Depth  float64
}

// SnowFogCurtainN Snow 灰幕的团数（P3 判据 B：20 团 → 3 团）。
// 取法 = idx%86==85 ⇒ 260 个里命中 idx = 85 / 171 / 257（推导写在 spawn 的 Snow 分支）。
const SnowFogCurtainN = 3

// 幕层的索引步长与余数 = 旧 Snow 分支里的字面量 86 / 85（推导见 spawn 的 Snow 分支注释：
// 粒子总数 260 ⇒ 命中位 = s−1, 2s−1, 3s−1，要求 3s−1 ≤ 259 ⇒ s = 86）。
const (
	snowCurtainStride = 86
	snowCurtainMod    = 85
)

// SpawnFog 造一团雾（Fog 的 pkFog 档，ui/effects.go:1165-1172 逐字）。
//
//	上一版 sc 到 4.4 → 单个雾团宽 1056 px，12 个亮色大团叠满全屏，
//	整屏被推成近白（判读"雾像一层均匀蒙版、无纵深"成因）。
//	改：Kenney 烟雾贴图 + 低透明 + 越近越浓，纵深交给 drawHaze 梯度。
//
// y 初始分布避开中央信息带，按半径外扩（同云）。
// 雾 sc 上限 4.2 × 256 px = 1075 px，半高 537 px = 0.28 屏高（1920 px 屏）。
// 雾浓度 = 贴图原样 alpha × 运行时倍率（FogAlphaMul 注释写明为什么不烘贴图）
//
// depth 是调用方在分支之前已经抽好的景深（旧实现：p.depth = r.Float64() 在 switch 之前）；
// 随机数取用顺序 = x → y（落位口）→ sc → alpha → vx → vy → depth，逐字与旧分支同序。
func SpawnFog(r *rand.Rand, depth float64, placeY PlaceY) FogSpawn {
	var q FogSpawn
	q.X, q.Y = r.Float64(), placeY()
	q.Sc = FogScBase + FogScSpan*r.Float64()
	q.Alpha = (0.06 + 0.16*r.Float64()*(0.35+0.65*depth)) * FogAlphaMul
	q.Vx = 0.010 + 0.030*r.Float64()
	q.Vy = 0.002 + 0.006*r.Float64()
	q.Depth = 0.2 + 0.8*r.Float64()
	return q
}

// SpawnSnowCurtain 造一张雪天灰幕（Snow 的 pkFog 档，ui/effects.go:1201-1213 逐字）。
//
// 雪天 = **均匀灰幕（雾精灵）+ 可见雪花**（规格 2 表 + 规格 3）⇒ 满屏均匀、不堆团。
// **3 团的 x = 逐团等分的确定落位**（k = (idx−85)/86 ∈ {0,1,2} ⇒ x = (k+0.5)/3
// = 0.167 / 0.500 / 0.833）：20 团时随机 x 的大数定律把团摊开，降到 3 团后小样本会成片
// （实测 seed=20261023：随机落位 0.218/0.241 两团并排 ⇒ 漂移重叠 dualPct = 61.02% > 判据线 60%）。
// y 仍走现口径 bandFreeY（带避让口径不动 = placeY 注入）；sc/alpha/vx/vy/depth 一行没动。
func SpawnSnowCurtain(r *rand.Rand, depth float64, idx int, placeY PlaceY) FogSpawn {
	var q FogSpawn
	q.X = (float64((idx-snowCurtainMod)/snowCurtainStride) + 0.5) / float64(SnowFogCurtainN)
	q.Y = placeY()
	q.Sc = FogScBase + FogScSpan*r.Float64()
	q.Alpha = (0.06 + 0.16*r.Float64()*(0.35+0.65*depth)) * FogAlphaMul
	q.Vx = 0.010 + 0.030*r.Float64()
	q.Vy = 0.002 + 0.006*r.Float64()
	q.Depth = 0.2 + 0.8*r.Float64()
	return q
}

// ---- 雾团贴图变体（逐字迁自 ui/effects.go:1267-1272 的 pkFog 分支）----

// FogSpriteIndex 雾（pkFog）的变体轮换：index % 变体数；
// 雾贴图组缺失时退到云贴图张数（旧实现逐字：n = len(sp.fog)，n==0 ⇒ n = len(sp.cloud)）。
func FogSpriteIndex(idx, nFog, nCloud int) int {
	n := nFog
	if n == 0 {
		n = nCloud
	}
	return idx % n
}

// ---- 雾团屏上倍率（逐字迁自 ui/effects.go:4148-4153 = spriteScale 的雾分支）----

// FogScale 雾团屏上倍率。
//
// 效果图口径：雾团尺寸 = sc·(0.55+0.45·depth)·scaleMul × fogNetScale(=1.0)，
// 1024 px 源图 ⇒ 屏上 732–2867 px，与效果图（2048 源 × 同式 ÷2）逐档相同。
// 上一版把雾收成常量 fogNetScale 0.143（净宽 128 px = 旧 Kenney 净宽），
// 雾从"整片体积雾"缩成"小团"，与选定效果图不符 ⇒ 恢复逐粒子口径。
//
// netScale = 净倍率常量本体归 ui 的贴图口径（ui/assets.go:377 的 fogNetScale，
// 与云带/雪片的分母同处一张表）⇒ 以参数注入，本包不复制常量，避免双份真值。
func FogScale(sc, depth, scaleMul, netScale float64) float64 {
	return sc * (0.55 + 0.45*depth) * scaleMul * netScale
}

// ---- 雾团屏宽封顶（逐字迁自 ui/effects.go:4944-4964）----

// ClampFogWidthPx 雾团屏宽按类封顶（P7 用户判据"最多的时候也只能首尾相连，不能重叠"）：
// 同带 Σ全宽 + 间隙 ≤ 1 屏宽 ⇒ Snow 3 张灰幕 ⇒ 每张 ≤ (1080−42)/3 = 346 px；
// Fog 1 团（配 1 朵 ≤500 px 的云）⇒ ≤ 540 px。缩的是 sc（绘制倍率本体），
// 逐字不动 alpha / vx / 团数 / 落位口径。实测封顶前：Snow 单张 1135 px、Fog 884 px。
//
// 贴图表与退路尺寸以入参注入（旧实现里 = f.sp.fogSz 与 spriteSize(spritesOnce(), pkFog)，
// 都归 ui 的贴图装配口径）；netScale = FogScale 的净倍率常量，同上。
// 返回封顶后的 sc（旧实现就是就地改 p.sc，其余字段一个不写）。
func ClampFogWidthPx(sc, depth, scaleMul, netScale, capPx float64, spr int, fogSz []image.Point, fallback image.Point) float64 {
	if capPx <= 0 || sc <= 0 {
		return sc
	}
	sz := metrics.SpriteSizeOr(fogSz, spr)
	if sz.X <= 0 {
		sz = fallback
	}
	if sz.X <= 0 {
		return sc
	}
	w := float64(sz.X) * FogScale(sc, depth, scaleMul, netScale)
	if w <= capPx {
		return sc
	}
	// 逐字 = 旧实现的 `p.sc *= capPx / w`（先算 capPx/w，再乘 sc；结合顺序影响末位）
	return sc * (capPx / w)
}

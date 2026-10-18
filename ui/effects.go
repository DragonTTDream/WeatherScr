package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// 背景动效引擎 v2。
//
// v1 的两个缺陷，逐条对应低性能 x86 小主机反馈：
// 1. 粒子是 fillRect 画的实心矩形 → 屏幕上是一堆"透明方块在飘"，
// 云和太阳完全没有形状。v2 改为**启动时程序化生成软边精灵贴图**，
// 逐粒子用 paint.NewImageOp + Affine + PushOpacity 贴图，
// 有径向衰减、有形状、有 alpha 层次。
// 2. 相位取 gtx.Now.Nanosecond()/1e6，每帧跳变 → 星星"奇怪地抖动"。
// v2 用**连续累计时间 t**，闪烁 = base + amp*sin(ω*t + φ)，
// φ 与 ω 每粒子固定，运动是 t 的连续函数。
//
// 贴图只生成一次并常驻，逐帧只做变换与贴图，低性能 x86 小主机 集显扛得住。

// 精灵贴图集合。同类多张变体：逐粒子挑一张，避免"同一个形状复制 N 遍"
// 的重复感（视觉判读：云"像低分辨率素材"、雪"像一张静态噪点图"）。
//
// 旧版删掉两条单张贴图路径：
// - moon / moonGlow（genMoon/genGlow 的单张模糊贴图）：月亮改三层叠加，
// 在 drawMoon 里用 clip.RRect 直接铺纯色圆盘，不再走贴图；
// - drop（genDrop 的实心白线）：雨线改 clip.Stroke + 沿长度只变 alpha 的
// 线性渐变，在 drawRain 里画，不再走贴图。
type sprites struct {
	star        []paint.ImageOp
	starSz      []image.Point
	sun         paint.ImageOp
	sunSz       image.Point
	cloud       []paint.ImageOp
	cloudSz     []image.Point
	cloudIsArt  bool
	fog         []paint.ImageOp
	fogSz       []image.Point
	flake       []paint.ImageOp
	flakeSz     []image.Point
	mote        []paint.ImageOp
	moteSz      []image.Point
	bolt        paint.ImageOp
	boltSz      image.Point
	cloudShadow paint.ImageOp
}

// newSprites 程序化生成全部软边贴图。
func newSprites(seed int64) *sprites {
	rng := rand.New(rand.NewSource(seed))
	s := &sprites{}

	// 星：3 档尺寸，大的带明显星芒
	for _, n := range []int{24, 40, 64} {
		s.starSz = append(s.starSz, image.Pt(n, n))
		s.star = append(s.star, paint.NewImageOp(genStar(image.Pt(n, n))))
	}

	s.sunSz = image.Pt(192, 192)

	// 云：Kenney《Background Elements》现成素材（CC0）。
	// 程序生成的软边团块被视觉判读定为"像高斯模糊的白噪点、像 2D 贴图叠加"，
	// 这是算法形状的固有上限，故换人工设计的插画云。
	cl, clSz, fg, fgSz, gl, glSz := loadEmbedded()
	if len(cl) > 0 {
		s.cloud = cl
		s.cloudSz = clSz
		s.cloudIsArt = true
	} else {
		for i := 0; i < 6; i++ {
			sz := image.Pt(240, 160)
			s.cloudSz = append(s.cloudSz, sz)
			s.cloud = append(s.cloud, paint.NewImageOp(genCloud(sz, rng)))
		}
	}
	if len(fg) > 0 {
		s.fog = fg
		s.fogSz = fgSz
	}
	// 日/地平线光（pkGlow）：Kenney light_01（glow.png）。
	// genGlow 的单张模糊贴图路径已删除；
	// 素材缺失时退到最大的星贴图（退路，不是新增观感）。
	if glSz.X > 0 {
		s.sun = gl
		s.sunSz = glSz
	} else {
		s.sun = s.star[len(s.star)-1]
		s.sunSz = s.starSz[len(s.starSz)-1]
	}

	for _, n := range []int{18, 26, 36} {
		s.flakeSz = append(s.flakeSz, image.Pt(n, n))
		s.flake = append(s.flake, paint.NewImageOp(genFlake(image.Pt(n, n))))
	}

	// 沙尘：横向拉长的流线软条（判读：圆斑"缺乏被风吹拂的流线感"）
	// 判读："颗粒像干草木屑，太长太清晰" —— 改成大量细碎软粒 + 少量
	// 中等长度流带，整体低对比，做出"弥漫浑浊"而不是"一根根草屑"。
	for _, sz := range []image.Point{{90, 34}, {130, 40}, {60, 26}, {170, 48}} {
		s.moteSz = append(s.moteSz, sz)
		s.mote = append(s.mote, paint.NewImageOp(genStreak(sz, rng)))
	}

	s.boltSz = image.Pt(90, 200)
	s.bolt = paint.NewImageOp(genBolt(s.boltSz, rng))

	// 云阴影层：同一形状，暗色
	s.cloudShadow = paint.NewImageOp(genCloudShadow(image.Pt(240, 160), rng))

	return s
}

// 全局贴图集：所有 Field 共用，避免重复上传。
var globalSprites *sprites

func spritesOnce() *sprites {
	if globalSprites == nil {
		globalSprites = newSprites(20261015)
	}
	return globalSprites
}

// ---- 贴图生成 ----

// genStar：径向衰减核心 + 四向衍射星芒。
func genStar(sz image.Point) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	maxR := math.Min(cx, cy)
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy) / maxR
			// 核心：指数衰减
			core := math.Exp(-d * d * 9.0)
			// 星芒：沿 x/y 轴的窄条
			spikeX := math.Exp(-math.Abs(dy)*2.2) * math.Exp(-math.Abs(dx)*0.30)
			spikeY := math.Exp(-math.Abs(dx)*2.2) * math.Exp(-math.Abs(dy)*0.30)
			a := core*0.85 + math.Max(spikeX, spikeY)*0.45
			if a > 1 {
				a = 1
			}
			img.SetNRGBA(x, y, color.NRGBA{255, 252, 240, uint8(a * 255)})
		}
	}
	return img
}

// genGlow / genMoon 已删除（读图判读"像没擦干净的
// 镜头污渍"。月亮改三层叠加（清晰圆盘 + 内晕 + 外晕，同色只变 alpha），
// 在 drawMoon 里用 clip.RRect 铺纯色；日/地平线光（pkGlow）改用 Kenney
// glow.png 现成素材。

// genCloud：12 团叠加成积云，顶亮底暗做出体积感，底边压平。
// 判读意见："像把白色斑点做了重度模糊，没有体积感" —— 用垂直亮度梯度
// 与底部平齐解决，而不是单纯加大模糊。
func genCloud(sz image.Point, rng *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	// 22 团 + 更柔的衰减 + 更碎的边缘：判读"边缘过于锐利、形状呆板像
	// 棉花糖、像 2D 贴图直接叠加"，靠提高团数与降低单团权重打散。
	blobs := make([][3]float64, 0, 22)
	baseY := float64(sz.Y) * 0.70
	for i := 0; i < 22; i++ {
		bx := float64(sz.X) * (0.06 + 0.88*rng.Float64())
		by := baseY - float64(sz.Y)*0.46*rng.Float64()*rng.Float64()
		br := float64(sz.X) * (0.06 + 0.11*rng.Float64())
		blobs = append(blobs, [3]float64{bx, by, br})
	}
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			var acc float64
			for _, b := range blobs {
				d := math.Hypot(float64(x)-b[0], float64(y)-b[1]) / b[2]
				acc += math.Exp(-d * d * 1.5)
			}
			a := acc / 5.2
			if a > 1 {
				a = 1
			}
			// 外缘收边 + 底部平齐切掉
			nx, ny := float64(x)/float64(sz.X), float64(y)/float64(sz.Y)
			edge := (1.0 - smoothstep(0.28, 0.5, math.Abs(nx-0.5))) *
				(1.0 - smoothstep(0.06, 0.34, ny-0.74))
			a *= edge
			// 体积感：顶部受光偏亮，底部偏暗
			shade := 1.06 - 0.34*ny
			r := 236.0 * shade
			g := 240.0 * shade
			b := 246.0 * shade
			if r > 255 {
				r = 255
			}
			if g > 255 {
				g = 255
			}
			if b > 255 {
				b = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r), uint8(g), uint8(b), uint8(a * 255)})
		}
	}
	return img
}

// genFlake：六向晶状雪晶，带软边。
func genFlake(sz image.Point) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	maxR := math.Min(cx, cy)
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy) / maxR
			if d > 1 {
				continue
			}
			ang := math.Atan2(dy, dx)
			// 六向臂
			arm := math.Abs(math.Cos(ang * 3))
			v := (0.30 + 0.70*arm) * (1.0 - smoothstep(0.35, 1.0, d))
			// 中心核
			v = math.Max(v, math.Exp(-d*d*14.0))
			if v > 1 {
				v = 1
			}
			img.SetNRGBA(x, y, color.NRGBA{250, 252, 255, uint8(v * 255)})
		}
	}
	return img
}

// genDrop 已删除（读图判读"纯白实心、塑料感、
// 缺透明度"。雨线改在 drawRain 里画：clip.Stroke 出线宽 + 沿长度
// 只变 alpha 的线性渐变。三档长度沿用原贴图档位（28/48/78 px），
// 由 depth 挑档（近处长）。
var rainLenPx = [3]float64{28, 48, 78}

// rainWidthPx 线宽随景深：
// depth<0.4 → 1 px，0.4–0.7 → 1.5 px，>0.7 → 2 px。
func rainWidthPx(depth float64) float64 {
	switch {
	case depth < 0.4:
		return 1
	case depth <= 0.7:
		return 1.5
	default:
		return 2
	}
}

// rainBaseAlpha 基础 alpha 随景深 0.35 → 0.70
// 远淡近浓。
func rainBaseAlpha(depth float64) float64 {
	return 0.35 + 0.35*depth
}

// rainDepthFactor 显式景深因子：
// 返回 (alpha 因子, 长度因子)。远景 alpha×0.35 且长度×0.4，
// 近景全额 ⇒ "近实远虚"达到肉眼可分的量级。
func rainDepthFactor(depth float64) (float64, float64) {
	switch {
	case depth < 0.4:
		return 0.35, 0.40
	case depth <= 0.7:
		return 0.70, 0.75
	default:
		return 1.00, 1.00
	}
}

// 雨线头/尾比例、快门时间、风（参照 github.com/0magnet/termanim/rain）。
// 两端 **RGB 完全相同** = [225,235,245]，只变 alpha（铁律）。
const (
	// rainTailFrac 尾 = 头的 40%。参照反馈：淡到 0 会丢掉线长感，
	// 完全不淡则画成一根没有方向感的条。此前用 0.15（头的 17.6%）
	// ⇒ 线看起来"断"了、短了。
	rainTailFrac = 0.40
	// rainExposure 快门时间（秒）。线长 = 速度 × 快门时间 = 运动模糊。
	// 参照取 1/20 秒：足够让相邻帧的雨线重叠，下落看起来连续而非频闪。
	// 它是**时间**不是帧数 —— 绑帧率会在掉帧时把每条线拉长，
	// 而那正是雨最不该 visibly 变化的时刻。
	rainExposure = 0.05
	// rainSlantBase 风：水平像素 / 垂直像素。参照取 0.28。
	// 垂直的雨像测试卡；一点倾斜才像天气。
	rainSlantBase = 0.28

	// 与 drawRain 逐字同一套公式（参照 termanim/rain）。
	splashDepth = 0.6  // 只有近层溅
	splashLife  = 0.2  // 秒：够看见，又不至于把底边变成一条常驻亮线
	splashCap   = 512  // 同时存活的水花上限
	crownSpread = 15.0 // 水花向外扩散速度（像素/秒）
)

// splash 一滴落到下边缘后留下的印记：无速度，只有年龄；
// 年龄决定它多宽、多亮（扩散 + 衰减）。
type splash struct {
	x   float64 // 归一化横坐标
	age float64 // 落地后秒数
}

var rainLineRGB = color.NRGBA{R: 225, G: 235, B: 245, A: 0xff}

// windAngle 全场缓慢变化的风向项：
// sin(t/9)，幅度 0.06 rad。雨线倾角 = 该角（0 = 竖直）。
// windAngle 全场缓慢变化的风向项：
// sin(t/9)，幅度 **0.16 rad**（旧版 0.06 在低性能 x86 小主机出图上"几乎垂直"，
// 读图判读缺乏风感。每粒子再叠加 windJitterAmp 的固定抖动。
func windAngle(t float64) float64 {
	return 0.16 * math.Sin(t/9.0)
}

// windJitterAmp 每粒子倾角抖动幅度（rad），由粒子索引生成 ⇒ 跨帧稳定。
const windJitterAmp = 0.03

// genMote：不规则软尘团。
func genMote(sz image.Point, rng *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	maxR := math.Min(cx, cy)
	seeds := [4]float64{rng.Float64() * 6.28, rng.Float64() * 6.28, rng.Float64() * 6.28, rng.Float64() * 6.28}
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy) / maxR
			ang := math.Atan2(dy, dx)
			// 用低频正弦把圆扰动成不规则团
			wob := 1.0 + 0.28*math.Sin(ang*3.0+seeds[0]) + 0.16*math.Sin(ang*5.0+seeds[1])
			a := 1.0 - smoothstep(0.35*wob, 1.0*wob, d)
			a *= math.Exp(-d * d * 1.6)
			if a > 1 {
				a = 1
			}
			img.SetNRGBA(x, y, color.NRGBA{222, 208, 178, uint8(a * 255)})
		}
	}
	return img
}

func smoothstep(a, b, x float64) float64 {
	if b <= a {
		if x < a {
			return 0
		}
		return 1
	}
	t := (x - a) / (b - a)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

// ---- 粒子 ----

type pkind int

const (
	pkStar pkind = iota
	pkFlake
	pkMote
	pkDrop
	pkCloud
	pkFog
	pkGlow
)

type particle struct {
	x, y   float64 // 归一化 0..1
	vx, vy float64 // 归一化速度 / 秒
	vxBase float64 // 生成时的基准横向速度；vx 会被间距控制器在它附近调（见 coupleClouds）
	sc     float64 // 缩放
	rot    float64
	rotV   float64
	alpha  float64
	amp    float64 // 闪烁幅度
	omega  float64 // 闪烁角频率
	phase  float64 // 固定初相
	kind   pkind
	depth  float64 // 视差层（0 远 1 近）
	spr    int     // 同类贴图变体下标（逐粒子挑一张，消除重复感）

	// ---- 消除"复制粘贴"感的**持久实例抖动** ----
	//
	// 判据 = 两次读图：
	// 同一贴图、同一亮度、同一尺寸重复摆放。
	//
	// 三个系数都在生成时按**粒子索引**确定（idxJit，索引作随机种子），
	// 因此同一场稳定、跨帧不闪（Draw 每帧读到的都是同一组值）。
	idx           int     // 粒子在 parts 里的索引 = 抖动种子
	brightnessMul float64 // ∈ [0.65, 1.35]，乘到 alpha 上（上限 1.0）
	alphaMul      float64 // ∈ [0.50, 1.00]
	scaleMul      float64 // ∈ [0.75, 1.25]，尺寸抖动
	mirror        bool    // 奇数索引 → 云/雾取负 X 缩放（水平镜像）
	// splashed 本滴是否已经溅过。雨滴从 y=1.0 走到回收线 1.15 要停留
	// 若干帧，没有这个标记就会每帧溅一次 ⇒ 水花数撞上限、底边糊成
	// 一条实心亮带（参照 termanim/rain 明确警告过这个形态）。
	splashed bool
}

// 实例抖动区间。
const (
	brightnessMulMin  = 0.65
	brightnessMulSpan = 0.70 // 0.65 + 0.70 = 1.35
	alphaMulMin       = 0.50
	alphaMulSpan      = 0.50 // 0.50 + 0.50 = 1.00
	scaleMulMin       = 0.75
	scaleMulSpan      = 0.50 // 0.75 + 0.50 = 1.25
	scaleMulMax       = 1.25
)

// idxJit 用粒子索引作随机种子取 [0,1 的确定值
// "每个粒子在生成时获得持久的实例抖动，用粒子索引作随机种子，保证同一场
// 稳定、跨帧不闪"。splitmix64 终位混淆，纯整数、无状态、无分配：
// 同一 (idx, salt) 永远得到同一个值，故跨帧稳定。
func idxJit(idx int, salt uint64) float64 {
	x := uint64(idx) + 0x9E3779B97F4A7C15 + salt*0xC2B2AE3D27D4E49F
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / float64(1<<53)
}

// 云/雾的 sc 区间（外扩量的"sc 上限"就从这里的
// spawn 常量取，不写死像素数字。
//
// 云 sc = 1.1 + 2.4·U → 上限 3.5；雾 sc = 1.6 + 2.6·U → 上限 4.2
// （effects.go 原 spawn 的字面量，根因分析引用的正是这两个上限）。
//
// 性能修订（实测）：填充率与 CPU 线性，1 个整屏填充 @15 fps = 4.4% CPU。
// Kenney 精灵实测 190×127…266×138，`sc` 上限 3.5 再乘 `scaleMul` 上限 1.25
// ⇒ 单朵 ≈ 875×546 = 0.48 Mpx，16 朵 = 7.6 Mpx = **3.7 个整屏当量 = 15.6% CPU**，
// 是全场最大单项开销。按 termanim 的"深度驱动尺寸"原则收面积：
// 数量减半 + sc 上限 3.5→2.2 ⇒ 填充当量 3.7 → ≈0.9。
const (
	cloudScBase = 1.0 // 1.6 → 1.3 → 1.0（实测确认：缩小云 + 降不透明度）
	// 1.2 → 0.5：低性能 x86 小主机反馈"很夸张 很糊"。
	// 根因是**放大倍率**不是羽化：sc 上限 2.2 × scaleMul 上限 1.25 = 2.75 倍放大，
	// 把 Kenney 云的硬边拉成糊团（同一素材在原生 1:1 下视觉模型判"锐利清晰的卡通硬边"，
	// 素材图判读（Kenney cloud1..9 实测）。现上限 1.5，配合 2× 超采样 ⇒ GPU 侧近似 1:1。
	cloudScSpan = 0.25 // 上限 1.65 → 1.25
	fogScBase   = 1.3
	fogScSpan   = 1.5 // 上限 2.8（此前 3.2 时"很夸张 很糊"）
)

// 越界回收的 y 窗口（effects.go 原 Step 的字面量 1.15 / -0.35，提成常量：
// 带外可用区与回收窗口必须同口径，否则放出去在窗口外的粒子会被立刻回收。
const (
	recycleYHi = 1.15
	recycleYLo = -0.35
	// 云初始 x 的铺放区间 = 回收区（与 y 同一口径）。
	// 原先初始 x 是 0..1 ⇒ 开局全在屏内、速度又接近 ⇒ 同进同出，
	// 一段时间整片天空没有云。
	recycleXHi = 1.15
	recycleXLo = -0.35
)

// 云带纵向分布：CloudBandYTop 起、向下铺开 CloudBandYSpread。
// 原样 = 0.02 / 0.24（只压顶部 26%）。
var (
	CloudBandYTop = 0.02
	// **保持 0.24**（云不影响信息展示）。
	// 此前把它收紧到 0.14，依据是"云带变实后会压到时钟数字"——该依据已作废，两条都不成立：
	// (a) 出处是**视觉模型对离屏图的判读**，不是实测口径；
	//       实现此前已就同一件事被纠正过：云一直在时钟下方，不存在压住时钟数字。
	// (b) 结构上不可能：绘制顺序 天空 → 月亮 → 云/粒子 → 时钟 → 天气文字
	// （render.go:272/282/285/295/298），时钟与天气文字**永远画在云之后**。
	// 0.24 是实测认可过的值（云带 0.24 判读认为可以）。
	CloudBandYSpread = 0.24
)

// Field 是一类天气的粒子场。
type Field struct {
	cls   effect.Class
	isDay bool
	sp    *sprites
	parts []particle
	// 与 drawRain 逐字同一套公式（参照 termanim/rain）。
	splashes []splash
	rng      *rand.Rand
	t        float64 // 连续累计时间（秒）—— 所有动画的唯一自变量
	flash    float64
	next     int
	// sh 屏高（像素）：信息带外扩量的分母。屏高由调用方传入——
	// Step(dt, w, h) 每帧刷新；建场时取竖屏目标 PortraitH。
	sh int
	// moonBoost* 云遮月加浓的计数诊断（见 MoonBoostStats）
	moonBoostFrame int
	moonBoostTotal int64
	// sw 屏宽（像素）：云/雾进场落位要"半宽占屏宽比例"（实测口径）。
	sw int
	// speedScale 位移速度系数（实测口径）：
	// 需求：占用降下来要走低帧率时，位移速度同步降低；
	// 帧率降了速度不动，云彩动起来就会一顿一顿。
	//
	// 机制：位移是时间基准 `p.x += p.vx·dt` ⇒ 帧率从 15 降到 8 时
	// 每帧位移从 vx/15 变成 vx/8（**大 87%**） ⇒ 一顿一顿。
	// 取 speedScale = 目标帧率/15 ⇒ 每帧位移 = vx·(1/fps)·(fps/15) = vx/15
	// **恒定**，与 15 fps 时的逐帧观感完全一致，代价是运动整体变慢。
	// 由调用方按帧预算设置（cmd/scr），默认 1.0 = 不缩放。
	speedScale float64
	// fade 出场淡入淡出系数 0..1（1 = 全显示）。类别切换时旧场淡出、新场淡入，
	// 消除"整片云瞬间刷到屏幕上"（需求：类别切换要平滑过渡，
	// 云缓慢进场，不得突然刷新到屏幕上）。
	fade float64
	// entering = 本次建场是"类别切换"产生的：云从屏幕外左侧**错峰**进场，
	// 不在屏内铺开（开局建场仍全屏铺开，见 spawn 注释）。
	entering bool
	// fps 自适应。
	//
	// 现状：实测进程被标
	// Not Responding（消息循环 >3 秒不泵），40 秒内不足 60 帧（证据）。
	// Step 里维护最近 2 秒的实测 fps（按传入 dt 累计，dt 即调用方实测的
	// 帧间隔，低于 12 fps 时按比例削减活跃粒子数（下限 = 原值的 25%），
	// 回升到 18 fps 以上再逐步恢复。
	//
	// 削减只影响 **Draw 的绘制数量**（parts 的前 active 个），
	// 不改生成器（counts/spawn）与贴图（sprites）。
	active   int     // 当前活跃粒子数 = Draw 绘制 parts[:active]
	fpsAccum float64 // 当前测量窗累计 dt（秒）
	fpsCount int     // 当前测量窗帧数
	fps      float64 // 最近一次窗口实测 fps
}

// NewField 建场。seed 决定随机布局。
func NewField(cls effect.Class, isDay bool, seed int64) *Field {
	return newField(cls, isDay, seed, false)
}

func newField(cls effect.Class, isDay bool, seed int64, entering bool) *Field {
	f := &Field{
		cls:   cls,
		isDay: isDay,
		sp:    spritesOnce(),
		rng:   rand.New(rand.NewSource(seed)),
		// entering 必须在生成粒子**之前**就位：粒子在下面的 spawn 循环里生成，
		// 错峰进场的分支就看这个标志。上一版是 NewField 返回后才置位
		// ⇒ 生成时标志还是 false ⇒ 云照旧全屏铺开（TestEnteringCloudsOffscreen 抓到 8 朵全在屏内）。
		entering: entering,
		sh:       PortraitH, // 建场时未知屏高 → 竖屏目标口径，Step 每帧按实际 h 刷新
		sw:       PortraitW, // 同上（云/雾进场落位的半宽分母）。
		// 建场时 sw=0 会让 airHalfN 走兜底值 ⇒ 带外可用段算不出来 ⇒ 云落进信息带
		// （实测 effects_band_test.go:74 抓到 y=0.3794 压带）。
	}
	n := counts(cls, isDay)
	f.parts = make([]particle, 0, n)
	for i := 0; i < n; i++ {
		f.parts = append(f.parts, f.spawn(true, i))
	}
	f.active = n // 未削减 = 全部
	f.fade = 1   // 默认全显示（零值 0 会让整片场不可见）
	return f
}

// NewFieldEntering 类别切换用的建场：云从屏幕外左侧错峰飘入 + 出场淡入系数 0。
// 与 NewField 的唯一区别就是 entering=true（开局建场仍全屏铺开）。
func NewFieldEntering(cls effect.Class, isDay bool, seed int64) *Field {
	f := newField(cls, isDay, seed, true)
	f.fade = 0
	return f
}

// SetFade 设出场淡入淡出系数（0 = 不可见，1 = 全显示）。
func (f *Field) SetFade(k float64) {
	if k < 0 {
		k = 0
	}
	if k > 1 {
		k = 1
	}
	f.fade = k
}

// SetEntering 结束/开启"错峰进场"模式（淡入完成后由调用方关掉）。
func (f *Field) SetEntering(on bool) { f.entering = on }

// counts 每类粒子数。S1 实测 600 粒子 58.9 fps / 34.9 MB，
// S4 实测 420 粒子 15 fps 时总 CPU 3.57%（≤5% 预算内）。
func counts(cls effect.Class, isDay bool) int {
	switch cls {
	case effect.Clear:
		if isDay {
			// 10 → 16：云改小后（sc 上限 1.5）用数量补天空覆盖。
			// 填充当量：16 × 220×130 × 1.25² ≈ 0.71 Mpx = 0.35 个整屏当量 ≈ 1.5% CPU
			// （原 8 朵 sc 上限 2.2 时是 3.7 个当量）。
			return 16
		}
		return 110
	case effect.Cloudy:
		// 16 → 8（低性能 x86 小主机反馈"顶部一堆一堆重叠的云"）：
		// 带避让把云限制在两条窄带里——实测带上方可用段高 182 px、
		// 带下方仅 56 px，共 238 px；云原生高 121–138 px ⇒ 只能容 1.3 行。
		// 8 → 10：云缩到 0.5× 原生后单朵面积降到 1/4，
		// 数量补回层次。小云多 = 层次多；大云少 = 糊团。
		// 5 → 8：5 朵时低性能 x86 小主机 active 只有 2，两朵都在回收区时整片天空空掉
		// （长时间没有云、分布不平均）。
		// 填充当量核算：8 × 220×130 × 1.25² ≈ 0.35 整屏当量 ≈ 1.6% CPU @15fps。
		return 8
	case effect.Fog:
		// 24 → 20（实测口径："很夸张 很糊"）：此前为补"偏淡"把 18→24，
		// 24 个 sc 上限 3.2 × 256 px = 819 px 宽的大团叠在一起 ⇒ 糊成一片。
		// 浓度改由 drawHaze 梯度承担（薄雾是大气梯度，不是一堆大团）。
		return 20
	case effect.Rain:
		return 320
	case effect.Snow:
		return 240
	case effect.Thunder:
		return 420
	case effect.Dust:
		return 460
	}
	return 0
}

// spawn 按类别造一个粒子。fresh=true 时随机铺满全屏，否则从顶部进。
// idx = 粒子在 parts 里的索引，作实例抖动的随机种子。
func (f *Field) spawn(fresh bool, idx int) particle {
	r := f.rng
	// depth 取**全区间 0..1**（参照 termanim/rain：`depth := r.rng.Float64()`）。
	// 此前取 0.35..1.0，把"最慢、最短、几乎不可见"那一档整档切掉
	// ⇒ 场上只剩中近层，层间落差被削平（失误模式）。
	p := particle{depth: r.Float64(), idx: idx}
	switch f.cls {
	case effect.Clear:
		if f.isDay {
			// 视觉判读（2026-10-16 视觉模型，crop_clear_day_top）：
			// "光晕看得出，画面显得比较空" ⇒ 白天晴天补少量漂移云做内容，
			// 云按索引混排（每 5 个里 1 个云），数量与云带同口径收紧。
			if idx%5 == 0 {
				p.kind = pkCloud
				p.sc = cloudScBase + cloudScSpan*r.Float64()
				p.x, p.y = r.Float64(), bandFreeY(r, bandPadJitN(pkCloud, f.sh))
				// **必须给 vx 与 alpha**（缺陷：白天出现静止的小云）。
				// 根因：这个分支设完 kind/sc/x/y 就 break 出整个 switch，
				// vx 与 alpha 都没赋值 ⇒ vx=0（**完全静止**）、alpha=0，
				// 而 alpha=0 又被下面为遮挡加的 0.92 下限兜成 0.92
				// ⇒ 屏幕上就是"不透明 + 一动不动"小云。两条叠加，逐字对上观感描述。
				p.vx = 0.010 + 0.018*r.Float64()
				p.vxBase = p.vx
				p.alpha = 0.45 + 0.25*r.Float64()
				break
			}
			p.kind = pkGlow
			p.x, p.y = r.Float64(), r.Float64()
			// sc 上限 1.9 → 1.2：光晕是 192×192 精灵，填充当量随 sc² 走
			p.sc = 0.4 + 0.8*r.Float64()
			p.alpha = 0.10 + 0.16*r.Float64()
			p.amp = 0.05
			p.omega = 0.25 + 0.5*r.Float64()
			p.vy = -0.006 - 0.01*r.Float64()
		} else {
			p.kind = pkStar
			p.x, p.y = r.Float64(), r.Float64()*0.85
			// 亮度层次：少数亮星 + 大量暗星（判读"亮度单一像噪点"）
			p.sc = 0.18 + 0.90*r.Float64()*r.Float64()
			p.alpha = 0.18 + 0.72*r.Float64()*r.Float64()
			p.amp = 0.15 + 0.40*r.Float64()
			p.omega = 0.5 + 2.6*r.Float64()
			p.rotV = 0
		}
	case effect.Cloudy:
		p.kind = pkCloud
		// y 初始分布避开中央信息带，且**按精灵半径**外扩。
		// 缺陷 1 根因：只保证中心在带外时，云 sc 上限 3.5 × Kenney 云原生高
		// 118–138 px → 半高最高 241 px（1920 px 屏上 0.126 屏高），中心只要贴着
		// 带上沿，云身就压进带内（离屏图视觉判读，图名 lane_cloudy：
		// "在数字 34 的正上方，有一朵灰蓝色的云直接覆盖在数字上…数字的上半部分
		// 看起来像是'陷'在云里"。
		// 云初始 x 铺在**屏内 0..1**（不是回收区 −0.35..1.15：
		// 实测 8 朵里 7 朵落在 −0.04..−0.06，全挤在屏幕左沿外 ⇒ 一起飘进来，
		// 就是"一起出现一起消失"的成因。
		p.x = r.Float64()
		// 云的初始 y 铺在**上部可用带的可见段** [pad·0.3, 带上沿−pad] = 约 [0.017, 0.188]。
		// 原先铺在整个回收区 [−0.35, 0.1878]：可见部分只占该区间 34%
		// ⇒ 5 朵上部云只有 1~2 朵在屏内，且贴着下沿 0.1878，正好在月亮（0.095）下方
		// ⇒ 离屏图实测月亮行没有云（云依旧从月亮下面过去）。
		// 月亮 0.095 正好是可见段 [0.017, 0.188] 的中点 ⇒ 云从上下两侧横穿月亮。
		{
			y0, _ := infoBandY()
			padC := bandPadJitN(pkCloud, f.sh)
			lo := padC * 0.3
			hi := y0 - padC
			if hi <= lo {
				hi = lo + 0.01
			}
			p.y = lo + r.Float64()*(hi-lo)
		}
		// ↑ 初始 x 从 0..1（开局全在屏内 ⇒ 同进同出、一段时间整片天空没云，
		// 云怎么总是一起出现一起消失，
		// 导致会有很多时间是没有云的，一点也不平均）。
		// 改成铺满整个回收区 [recycleXLo, recycleXHi] ⇒ 进场时间天然错开。
		p.sc = cloudScBase + cloudScSpan*r.Float64()
		// 0.55–0.90 → 0.35–0.60：0.55–0.90 是"发糊"那轮为提对比拉上去的，
		// 结果云变"实"，叠起来读成整块巨团。背景点缀口径 = 半透明点缀，不是实心块。
		// 0.28–0.45 → 0.80–0.95 → **1.0**（缺陷：月亮在云上，
		// 云没有盖住月亮；图层顺序有问题；
		// 云的颜色改变了，月亮依旧是自己的颜色，是云被月亮盖住了）。
		// 低性能小主机四帧拼图实测：α 高的小亮云**能**把眉月下端压暗，
		// α 低的大灰云压不住 ⇒ 遮挡强度就是粒子 α（Kenney 云体贴图核心 α 实测 255）。
		// α=1.0 ⇒ 云覆盖处月面被整块替换 ⇒ 云真正盖住月亮。
		p.alpha = 1.0
		p.vx = 0.006 + 0.022*r.Float64()
		p.vxBase = p.vx
		p.depth = 0.2 + 0.8*r.Float64()
	case effect.Fog:
		// 上一版 sc 到 4.4 → 单个雾团宽 1056 px，12 个亮色大团叠满全屏，
		// 整屏被推成近白（判读"雾像一层均匀蒙版、无纵深"成因）。
		// 改：Kenney 烟雾贴图 + 低透明 + 越近越浓，纵深交给 drawHaze 梯度。
		p.kind = pkFog
		// y 初始分布避开中央信息带，按半径外扩（同云）。
		// 雾 sc 上限 4.2 × 256 px = 1075 px，半高 537 px = 0.28 屏高（1920 px 屏）。
		p.x, p.y = r.Float64(), bandFreeY(r, bandPadJitN(pkFog, f.sh))
		p.sc = fogScBase + fogScSpan*r.Float64()
		p.alpha = 0.06 + 0.16*r.Float64()*(0.35+0.65*p.depth)
		p.vx = 0.010 + 0.030*r.Float64()
		p.vy = 0.002 + 0.006*r.Float64()
		p.depth = 0.2 + 0.8*r.Float64()
	case effect.Rain, effect.Thunder:
		p.kind = pkDrop
		p.x = r.Float64()
		p.y = r.Float64()
		if !fresh {
			p.y = -0.05 - 0.15*r.Float64()
		}
		p.sc = 0.55 + 0.85*r.Float64()
		p.alpha = 0.30 + 0.45*r.Float64()
		// 参照实现：github.com/0magnet/termanim/rain（rain.go:176-200）。
		// 它的反馈："depth drives everything … **Drops all moving at one
		// speed read as falling dashes**; spread the speeds and brightnesses
		// over a range and the eye separates them into layers and calls it rain."
		//
		// 参照的三条量**全部零随机**：
		// vy     = h·(0.6 + 1.35·depth)   近处 0.51 s 穿屏、远处 1.67 s（3.25 倍）
		// length = vy · exposure(0.05 s)  线长由**速度导出**（运动模糊）
		// bright = 0.12 + depth²·0.88     平方压向亮端（8 倍）
		// 此前的失误模式：把这些量再乘一个大随机项（p.sc / brightnessMul /
		// alphaMul / scaleMul，单调序当场被打乱 ⇒ 肉眼只剩均匀感。
		// ⇒ 这里**不乘任何随机项**。
		p.vy = 0.6 + 1.35*p.depth
		p.vx = -(rainSlantBase * (0.7 + 0.6*p.depth)) * p.vy
	case effect.Snow:
		p.kind = pkFlake
		p.x = r.Float64()
		p.y = r.Float64()
		if !fresh {
			p.y = -0.05 - 0.15*r.Float64()
		}
		p.sc = 0.30 + 0.70*r.Float64()
		p.alpha = 0.40 + 0.55*r.Float64()
		// 视差速度：景深单调主导 + 小抖动。
		// 旧写法随机项跨度 3.2 倍、景深项 2.6 倍，同量级 ⇒ 排序被打乱。
		// 雪是近处飘得快、远处慢（与雨同向：近快远慢）。
		sdv := 0.30 + 0.70*p.depth
		sjit := 0.92 + 0.16*p.scaleMul/1.25
		p.vy = 0.13 * sdv * sjit
		p.omega = 0.8 + 2.2*r.Float64()
		p.amp = 0.10 + 0.2*r.Float64()
		p.rotV = (r.Float64() - 0.5) * 2.6
		// 横向摆动：雪是飘不是落
		p.phase = r.Float64() * math.Pi * 2
		p.amp += 0.0
	case effect.Dust:
		p.kind = pkMote
		p.x = r.Float64()
		p.y = 0.08 + r.Float64()*0.88
		if !fresh {
			p.x = -0.1 - 0.2*r.Float64()
		}
		p.sc = 0.35 + 1.15*r.Float64()
		p.alpha = 0.10 + 0.26*r.Float64()
		p.vx = 0.16 + 0.50*r.Float64()
		p.vy = (r.Float64() - 0.5) * 0.05
		p.rotV = (r.Float64() - 0.5) * 0.5
	}
	p.phase = r.Float64() * math.Pi * 2
	p.rot = r.Float64() * math.Pi * 2
	// 近大远小：depth 越大 = 越近 = 越大/越长档。
	// （上一版写成 2-int(depth*2.999)，方向反了，导致"雨丝雪片没有
	// 透视、像静态噪点"—— 上述判读的根因就是这个符号。
	//
	// 旧版 **变体轮换**：云/雾/星/雪按 `index % N` 取变体
	// （N = 该类变体精灵数），把"同一形状连拿 N 次"排布打散；
	// 雨（pkDrop）仍按 depth 挑长度档（线宽/长度都随景深走），
	// 尘（pkMote）保持原随机挑法（未指定）。
	sp := spritesOnce()
	switch p.kind {
	case pkStar:
		p.spr = idx % len(sp.star)
	case pkFlake:
		p.spr = idx % len(sp.flake)
	case pkDrop:
		p.spr = int(p.depth * 2.999)
		if p.spr < 0 {
			p.spr = 0
		}
		if p.spr >= len(rainLenPx) {
			p.spr = len(rainLenPx) - 1
		}
	case pkCloud:
		p.spr = idx % len(sp.cloud)
	case pkFog:
		n := len(sp.fog)
		if n == 0 {
			n = len(sp.cloud)
		}
		p.spr = idx % n
	case pkMote:
		p.spr = r.Intn(len(sp.mote))
	}

	// 持久实例抖动：索引作种子，
	// 同一粒子跨帧恒定 ⇒ 亮度/透明度/尺寸的差异是**稳定**的，不会逐帧闪。
	p.brightnessMul = brightnessMulMin + brightnessMulSpan*idxJit(idx, 1)
	p.alphaMul = alphaMulMin + alphaMulSpan*idxJit(idx, 2)
	p.scaleMul = scaleMulMin + scaleMulSpan*idxJit(idx, 3)
	// 水平镜像：奇数索引对云/雾取负 X 缩放
	p.mirror = idx%2 == 1 && (p.kind == pkCloud || p.kind == pkFog)
	// sw 屏宽（像素）：云/雾进场落位要"半宽占屏宽比例"（实测口径）。
	// 不走 resampleAir —— 防重叠判据是"屏内水平间距"，对刚在屏幕外生成的
	// 进场粒子无意义；进场粒子靠 y 分层与进场时刻天然错开。
	if p.kind == pkCloud || p.kind == pkFog {
		// 云/雾一律不走 resampleAir：那里的重采样用 bandFreeY
		// recycleYLo=-0.35…recycleYHi=1.15，会把刚夹好的 y 又推回屏幕外
		// （实测 TestAirEntryPlacement 抓到 y=-0.1452）。
		if !fresh {
			f.placeAirEntry(&p, r)
			return p
		}
		if f.entering {
			// **类别切换产生的建场**：云从屏幕外左侧**错峰**进场。
			// 需求：类别切换要平滑过渡，云缓慢进场，不得突然刷新到屏幕上。
			// 根因就是 NewField 走 fresh 分支把云一次性铺满全屏，
			// 于是整片云在切换那一帧凭空出现。
			// 错峰：第 i 朵放在 x = −0.35 − 0.10·i（归一化），
			// 以 vx≈0.017/s 计，第 1 朵约 21 s 进屏、第 8 朵约 82 s 进屏
			// ⇒ 天空在约一分多钟里逐渐填满，而不是一帧刷出来。
			p.x = -0.35 - 0.10*float64(p.idx)
			f.clampAirY(&p, r, false) // x 不许被 spreadAir 改回屏内
			return p
		}
		// 开局建场：x 保持全屏铺开（一开场天空就该有云，不该全挤在左边缘），
		// 但 y 同样夹到"整只在屏内"（实测 TestAirEntryPlacement 抓到
		// 初始云 y=1.0511 越出 [0.0453, 0.9547] ⇒ 开局就有半截出屏的云。
		f.clampAirY(&p, r, true)
		return p
	}

	// 同带横向防重叠
	f.resampleAir(&p, r)
	return p
}

// 同带横向防重叠的判据：
// 同一垂直带 = 中心 y 相差 ±0.12×屏高；水平距离下限 = 0.22×屏宽。
const (
	airBandN = 0.12
	airGapN  = 0.22
	airTries = 40
)

// isAir 空中类精灵（云/雾/光）—— 只有它们参与防重叠。
func isAir(k pkind) bool { return k == pkCloud || k == pkFog || k == pkGlow }

// resampleAir 同带横向防重叠：新粒子若与已放置者
// 在同一垂直带（±0.12×屏高）内水平距离 < 0.22×屏宽，则重采样位置，最多试
// airTries 次（试满即接受，避免生成器卡死）。
//
// 云/雾的 y 重采样走 bandFreeY（信息带避让口径不变），光（pkGlow）全屏取 y。
func (f *Field) resampleAir(p *particle, r *rand.Rand) {
	if !isAir(p.kind) {
		return
	}
	for tries := 0; tries < airTries; tries++ {
		ok := true
		for i := range f.parts {
			q := &f.parts[i]
			if !isAir(q.kind) {
				continue
			}
			if math.Abs(q.y-p.y) > airBandN {
				continue
			}
			if math.Abs(q.x-p.x) < airGapN {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		p.x = r.Float64()
		switch p.kind {
		case pkCloud:
			p.y = bandFreeY(r, bandPadJitN(pkCloud, f.sh))
		case pkFog:
			p.y = bandFreeY(r, bandPadJitN(pkFog, f.sh))
		default:
			p.y = r.Float64()
		}
	}
}

// infoBandY 中央信息带（时钟块 + 天气块）的归一化 y 区间。
// 从 ui 的 rect 常量取，不写死数字。
func infoBandY() (float64, float64) {
	y0 := ClockRect.Y
	y1 := WeatherRect.Y + WeatherRect.H
	if y0 < 0 {
		y0 = 0
	}
	if y1 > 1 {
		y1 = 1
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	return y0, y1
}

// maxSpriteH 同类全部贴图变体的**最大原生高**（像素）。
//
// 口径："从 spriteSize() 取原生高"：spriteSize 只返回
// 第 0 个变体（cloudSz[0] = 190×127），而 9 张 Kenney 云原生高 118–138 px 不等
// （ui/embed/cloud1..9.png 实测，判读结论已记入工程记录）→ 只取 [0]
// 会让 cloud6（266×138）的半高超出外扩量，按半径断言的自检必红。
// 故取同类变体的最大原生高（spriteSize 语义的扩展），对不上的地方已记入工程记录。
func maxSpriteH(sp *sprites, kind pkind) int {
	var szs []image.Point
	switch kind {
	case pkCloud:
		szs = sp.cloudSz
	case pkFog:
		szs = sp.fogSz
		if len(szs) == 0 {
			szs = sp.cloudSz // spawn 的同款退路（pick 亦然）
		}
	}
	m := 0
	for _, s := range szs {
		if s.Y > m {
			m = s.Y
		}
	}
	if m <= 0 {
		m = spriteSize(sp, kind).Y
	}
	return m
}

// bandPadN 该 kind 的信息带**外扩量**（归一化屏高）：
//
//	外扩 = (最大 sc × 精灵原生高 / 屏高) / 2      
//
// 原生高取同类变体的最大值（maxSpriteH），sc 上限取 spawn 的常量
// （cloudScBase+cloudScSpan / fogScBase+fogScSpan），屏高由调用方传入 ——
// 不写死像素数字。深度因子 (0.55+0.45·depth) 的最大值是 depth=1 时的 1.0，
// 故 Draw 里 scale = sc×(0.55+0.45·depth) 的半高必然 ≤ 本外扩量。
//
// 只给云（pkCloud）与雾（pkFog）算外扩；雨/雪/尘/星返回 0
// （细线条压在大字上不影响可读性，不动）。
func bandPadN(kind pkind, screenH int) float64 {
	if screenH <= 0 {
		return 0
	}
	var maxSc float64
	switch kind {
	case pkCloud:
		maxSc = cloudScBase + cloudScSpan
	case pkFog:
		maxSc = fogScBase + fogScSpan
	default:
		return 0
	}
	return (maxSc * float64(maxSpriteH(spritesOnce(), kind)) / float64(screenH)) / 2 / float64(spriteSupersample) // 精灵已 2× 超采样 ⇒ 净尺寸口径不变
}

// bandPadJitN 云/雾**含尺寸抖动**的外扩量 = bandPadN × scaleMulMax(1.25)。
//
// 旧版给每个粒子加了 scaleMul ∈ [0.75,1.25] 的持久尺寸抖动，
// Draw 画出来的半高最大是原口径的 1.25 倍。信息带的**可用区**与**回收窗口**
// 必须同步放大到这个口径，否则会出现"中心在带外、精灵身体压进带内"漏洞
// （可用区与回收窗口同口径）。
//
// bandPadN 本身保持公式不变（自检按它断言），
// 放大只发生在落位与回收这两处。
func bandPadJitN(kind pkind, screenH int) float64 {
	return bandPadN(kind, screenH) * scaleMulMax
}

// halfHN 粒子**实际画出来**的半高（归一化 0..1）：与 Draw 里的
// `scale := p.sc * (0.55 + 0.45*p.depth) * p.scaleMul` +
// `dh := float32(sz.Y) * float32(scale)` + `py := p.y*float64(h) - dh/2` 同口径。
// 自检按半径断言用它。
//
// 旧版的尺寸抖动 scaleMul 必须计入：Draw 画多大，这里就算多大，
// 带避让的断言才不会被抖动钻空子。
func (f *Field) halfHN(p *particle, screenH int) float64 {
	if screenH <= 0 {
		return 0
	}
	sz, _, ok := f.pick(p)
	if !ok || sz.Y <= 0 {
		return 0
	}
	scale := spriteScale(p)
	return float64(sz.Y) * scale / float64(screenH) / 2
}

// bandFreeY 在信息带**外扩 pad 之后**的可用区里均匀取 y：
//
//	[recycleYLo, y0-pad] ∪ [y1+pad, recycleYHi]，按区间长度加权落段
//
// 即"把中心推到 带边界 ± 外扩量 之外"。
// 可用区的上下界取 Step 的越界回收窗口（recycleYLo/recycleYHi）：带外空间
// 只有那么大，用 [0,1] 会把粒子放到"放出去即被回收"位置上。
// 屏矮到两侧都放不下时（雾在 1920 px 屏上就是这样：外扩 0.28 > 带上方 0.244），
// 退到"贴到最近的带外边界"（中心 = 带边界 ± pad），精灵整体仍在带外。
func bandFreeY(r *rand.Rand, pad float64) float64 {
	y0, y1 := infoBandY()
	hiA := y0 - pad // 带上方可用区上界
	loB := y1 + pad // 带下方可用区下界
	a := hiA - recycleYLo
	if a < 0 {
		a = 0
	}
	b := recycleYHi - loB
	if b < 0 {
		b = 0
	}
	if a+b <= 0 {
		if r.Float64() < 0.5 {
			return hiA
		}
		return loB
	}
	if r.Float64()*(a+b) < a {
		return recycleYLo + r.Float64()*a
	}
	return loB + r.Float64()*b
}

// Step 推进一个 dt。t 连续累加，动画全部是 t 的连续函数。
func (f *Field) Step(dt time.Duration, w, h int) {
	d := dt.Seconds()
	if d <= 0 {
		return
	}
	f.t += d
	f.sh = h // 屏高由调用方传入（外扩量的分母）
	f.sw = w // 屏宽：云/雾进场落位的半宽分母
	// 位移速度系数（实测口径）：0 / 未设置 = 不缩放
	ss := f.speedScale
	if ss <= 0 {
		ss = 1
	}

	// fps 自适应测量：以传入的 dt 累计出"最近 2 秒"实测帧率
	// （dt 即调用方实测的帧间隔；生产路径 main.go 传 time.Since(last)）。
	// 窗口每累计满 2 秒结算一次并调整活跃粒子数。
	f.fpsAccum += d
	f.fpsCount++
	if f.fpsAccum >= 2.0 {
		f.fps = float64(f.fpsCount) / f.fpsAccum
		f.adaptFps(f.fps)
		f.fpsAccum = 0
		f.fpsCount = 0
	}

	for si := 0; si < len(f.splashes); si++ {
		f.splashes[si].age += d
	}
	// 过期水花必须摘掉：只递增不删除 ⇒ 数量一路涨到 splashCap，
	// 底边糊成一条实心亮带（参照 termanim/rain 警告的正是这个形态）。
	if len(f.splashes) > 0 {
		kept := f.splashes[:0]
		for _, sp := range f.splashes {
			if sp.age < splashLife {
				kept = append(kept, sp)
			}
		}
		f.splashes = kept
	}
	bandY0, bandY1 := infoBandY()
	// 回收窗口按**含尺寸抖动**的外扩口径（bandPadJitN）：
	// 可用区（spawn 落位）与回收窗口必须同口径，否则带边界与回收线之间
	// 会出现"中心在带外、身体压进带内"空隙。
	padCloud := bandPadJitN(pkCloud, h)
	padFog := bandPadJitN(pkFog, h)
	for i := range f.parts {
		p := &f.parts[i]
		p.x += p.vx * d * ss
		p.y += p.vy * d * ss
		p.rot += p.rotV * d * ss
		// 出界回收
		if p.y > recycleYHi || p.y < recycleYLo || p.x > 1.25 || p.x < -0.35 {
			// 落地水花（参照 termanim/rain:Frame）：雨落到下边缘时留一个
			// 扩散+衰减的印记 —— "这才让雨是落在某处，而不是凭空消失"。
			// 参照只让**近层**（depth>0.6）溅：每滴都溅会在底边堆成一条
			// 实心亮带；只溅近层才读得出一颗颗撞击。
			if p.kind == pkDrop && p.y > 1.0 && !p.splashed && p.depth > splashDepth && len(f.splashes) < splashCap {
				f.splashes = append(f.splashes, splash{x: p.x})
				p.splashed = true
			}
			f.parts[i] = f.spawn(false, i)
			continue
		}
		// 云/雾的越界回收按**带边界 ± 外扩量**。
		// 旧版只按中心判越界，于是中心贴着带上沿的云/雾，
		// 精灵身体仍压进带内 210–537 px（缺陷 1 根因）。回收窗口扩到带外 pad
		// 之后，推进过程中精灵中心不会回到带内，精灵整体也不进带。
		if p.kind == pkCloud || p.kind == pkFog {
			pad := padCloud
			if p.kind == pkFog {
				pad = padFog
			}
			if p.y > bandY0-pad && p.y < bandY1+pad {
				f.parts[i] = f.spawn(false, i)
				continue
			}
		}
		// 云/雾横向循环
		if (f.cls == effect.Cloudy || f.cls == effect.Fog) && p.x > 1.3 {
			p.x = -0.3
		}
		if f.cls == effect.Dust && p.x > 1.3 {
			p.x = -0.25
		}
	}
	// 云横向间距控制器（缺陷：云的横向分布不均）。
	if f.cls == effect.Cloudy || f.cls == effect.Rain || f.cls == effect.Thunder {
		f.coupleClouds()
	}
	// 闪电：40~200 帧一次闪光
	if f.cls == effect.Thunder {
		f.next--
		if f.next <= 0 {
			f.flash = 1.0
			f.next = 40 + f.rng.Intn(160)
		}
		f.flash *= math.Exp(-d * 7.0)
	}
}

// adaptFps 按实测 fps 调整活跃粒子数：
//
//	fps < 12  → 按比例削减：active ← active × fps/12，下限 = 总数 × 25%
//	fps > 18  → 逐步恢复：每次 +10% 总数，上限 = 总数
//	12≤fps≤18 → 滞回区，不动（防在阈值附近来回抖动）
//
// 只动 active（Draw 的绘制数量）；parts、spawn、贴图全部不动。
func (f *Field) adaptFps(fps float64) {
	n := len(f.parts)
	if n == 0 {
		f.active = 0
		return
	}
	if f.active <= 0 || f.active > n {
		f.active = n // 零值 Field / 未初始化 → 全画（旧行为）
	}
	minActive := (n + 3) / 4 // 下限 = 原值的 25%
	// **多云类不削减**（云怎么总是一起出现一起消失，
	// 导致会有很多时间是没有云的，一点也不平均；月亮行根本没有云）。
	// 低性能 x86 小主机帧率 <12 ⇒ 8 朵云被砍到 minActive=2 朵：整片天空长时间空掉，
	// 且月亮所在行（0.095h）几乎不会有云经过 ⇒ 读成"月亮在云上面"。
	// 8 朵云按填充当量口径 = 8 × 220×130 × 1.25² ≈ 0.35 整屏当量
	// ≈ 1.6% CPU @15fps，在 ≤5% 预算内，没有削减必要。
	if f.cls == effect.Cloudy {
		minActive = n
	}
	if fps < 12 {
		target := int(float64(f.active) * fps / 12.0) // 按比例削减
		if target < minActive {
			target = minActive
		}
		if target < f.active {
			f.active = target
		}
	} else if fps > 18 {
		step := n / 10
		if step < 1 {
			step = 1
		}
		target := f.active + step
		if target > n {
			target = n
		}
		f.active = target
	}
}

// ActiveCount 当前活跃粒子数 = Draw 实际绘制的粒子数。
// MoonBoostStats 返回"本帧被加浓的云数 / 累计被加浓的云数"（诊断）。
// 用途：低性能 x86 小主机验证 overMoon 到底触发没有 —— 低性能 x86 小主机 12 帧连拍月框内最大亮度恒定 237
// （云体仅 ~210） ⇒ 云盖到月核上时月核没被压暗 ⇒ 加浓没生效，需要实测触发次数。
func (f *Field) MoonBoostStats() (int, int64) {
	return f.moonBoostFrame, f.moonBoostTotal
}

func (f *Field) ActiveCount() int {
	n := len(f.parts)
	if f.active <= 0 || f.active > n {
		return n // 未初始化 → 全画（旧行为）
	}
	return f.active
}

// TotalCount 粒子总数（生成器口径，不随削减变化）。
func (f *Field) TotalCount() int { return len(f.parts) }

// FPS 最近一次测量窗口结算的实测帧率。
func (f *Field) FPS() float64 { return f.fps }

// Draw 画一整帧。
// cloudBandN 阴雨类天空的云带片数
// 读图判读"背景像一块平滑的布，缺乏云层厚度"。
const cloudBandN = 4 // 5 → 4（需求：适当减少云的数量）

// bandPlace 第 i 条云带在时刻 t 的落位与尺寸（屏幕像素坐标，x/y 是精灵左上角）。
//
// 抽成包级函数的原因：绘制与"横向分布是否均匀"探针必须共用**同一套几何**。
// 探针若自己复制一份公式，测出来的就不是屏幕上真实存在的东西。
type bandPlace struct {
	x, y, dw, dh float64
	mirror       bool
}

// bandSpan 所有云带共用的横向循环周期（像素）：屏宽 + 最宽那条带的两倍宽。
// 必须共用：周期不同 ⇒ 相位差随时间漂移 ⇒ 扎堆 / 空白（见 bandPlace 注释）。
func (f *Field) bandSpan(w int) float64 {
	if len(f.sp.cloudSz) == 0 {
		return float64(w)
	}
	sum := 0.0
	for i := 0; i < cloudBandN; i++ {
		fi := i % len(f.sp.cloudSz)
		frac := float64(i) / float64(cloudBandN-1)
		sc := (1.5 + 0.8*frac) / float64(spriteSupersample)
		sum += float64(f.sp.cloudSz[fi].X) * sc
	}
	// 用**平均**带宽而不是最宽：周期 = w + 2·平均dw。
	// 用 max 会让窄带绕一圈的离屏段变长（实测平均覆盖 11.4% → 10.3%）。
	// 这个周期对**每一条**带都满足"进场时整条还在屏幕外"：
	// x = mod(...) − dw，周期起点 x = −dw ⇒ 带跨 [−dw, 0]；周期终点 x = w+2·平均dw−dw > w ⇒ 整条已出右界。
	// （前提：平均dw ≥ 各带 dw 的最小值，窄带的 x 更靠右也不会提前进入屏内。）
	return float64(w) + 2*(sum/float64(cloudBandN))
}

func (f *Field) bandPlace(i int, t float64, w, h int) (bandPlace, bool) {
	if len(f.sp.cloud) == 0 || len(f.sp.cloudSz) == 0 {
		return bandPlace{}, false
	}
	fi := i % len(f.sp.cloud)
	sz := f.sp.cloudSz[fi]
	if sz.X <= 0 || sz.Y <= 0 {
		return bandPlace{}, false
	}
	frac := float64(i) / float64(cloudBandN-1)
	// 缩放口径见 drawCloudBands 注释（1.5+0.8·frac，必须除以 spriteSupersample）。
	sc := (1.5 + 0.8*frac) / float64(spriteSupersample)
	dw := float64(sz.X) * sc
	// **横向均匀性**（缺陷：云的横向分布不均，容易出现某一小段时间
	// 屏幕中大部分是空白或某一段云大量集中出现）
	//
	// 原公式有两处会让 4 条带自己堆到一起或自己散开：
	// (a) 每条带速度不同（0.45→1.0 倍视差） ⇒ 快的绕一圈回来就追上慢的；
	// (b) 每条带的 span 还各自不同（span = w + 2·dw，而 dw 随带宽 285–524 px 变）
	// ⇒ 周期不同 ⇒ 相位差随时间漂移。
	// 实测（ui/hspread_test.go，900 秒逐秒剖面）：平均覆盖从 15% 掉到 2.3%，
	// 单秒最多 8/12 列全空；同时出现相邻两列 35%、三列 28/28/28 的扎堆。
	//
	// 改法：所有带共用**同一个 span 与同一个速度**，相位严格等分（i/N 圈），
	// 于是"等分"不依赖调参、是恒等式：任意时刻第 i 条带的中心都在
	// mod(V·t + i·span/N, span) 上，相邻中心间距恒等于 span/N。
	// 代价（如实记录）：带与带之间的**视差速度差被取消**，深度感改由
	// 尺寸（285→524 px）、不透明度（0.588→0.784）与 y（38→499 px）承担。
	span := f.bandSpan(w)
	// 相位按**左边缘**记（不是中心）：mod=0 时 x = −dw ⇒ 整条带跨 [−dw, 0]，
	// 全在屏幕外；mod→span 时 x = span−dw = w+2·平均dw−dw > w ⇒ 整条已出右界。
	// 若按中心记（x = centre − dw/2），周期起点带跨 [−dw/2, +dw/2] ⇒ 半条已经在屏内
	// ⇒ 云在左边缘凭空冒出来，违反需求"云应该在外面慢慢进来
	// 而不是在眼前突然出现"。
	// 代价：等分的是**左边缘**，中心间距会因各带宽度不同偏差最多 (dw_max−dw_min)/2 ≈ 120 px
	// （占周期 1888 px 的 6%），实测剖面最满列 ≤20%（改前 35%），可接受。
	x := math.Mod(0.06*float64(w)*t*0.725+span*float64(i)/float64(cloudBandN), span) - dw
	return bandPlace{
		x: x, y: float64(h) * (CloudBandYTop + CloudBandYSpread*frac),
		dw: dw, dh: float64(sz.Y) * sc, mirror: i%2 == 1,
	}, true
}

// drawCloudBands 在天空**上部**画大云带，给雨/雷/阴/雾四类提供云层厚度。
//
// 位置约束：云带只落在顶部 2%–26% 高度，**位于时钟信息带之上**，
// 不与时间/天气文字重叠（信息带避让在旧版由 bandFreeY 对粒子实现；
// 云带单张宽达 475–1064 px，逐带避让不现实，故用"整体置于信息带之上"
// 这一更强的约束达成同一目的。
//
// 变换必须 `op.Affine(tr).Push` + `Pop()`（`.Add` 会累积变换）。
func (f *Field) drawCloudBands(gtx layout.Context, w, h int, t float64) {
	if len(f.sp.cloud) == 0 || len(f.sp.cloudSz) == 0 {
		return
	}
	for i := 0; i < cloudBandN; i++ {
		fi := i % len(f.sp.cloud)
		sz := f.sp.cloudSz[fi]
		if sz.X <= 0 || sz.Y <= 0 {
			continue
		}
		frac := float64(i) / float64(cloudBandN-1)
		bp, ok := f.bandPlace(i, t, w, h)
		if !ok {
			continue
		}
		// 缩放 2.5–4.0 ⇒ 云带宽 475–1064 px（本函数注释的原始口径）。
		// **必须除以 spriteSupersample**：精灵已 2× 超采样，
		// 不除就是 950–2128 px ⇒ 单张云带横铺满屏、纵向从 26% 高度拖到 84%，
		// 就是"画面整体被超大的云占据"的成因。
		// 此前只给粒子绘制的 4 处尺度加了除数，漏了这一处独立公式。
		// 2.5+1.5·frac → **1.5+0.8·frac**（需求：云单朵尺寸太大，
		// 缩小大体积云）：净宽 237–532 px → **142–306 px**（1080 px 屏上从
		// 22%–49% 屏宽降到 13%–28%，最大那张砍掉 42% 宽度 ⇒ 面积砍掉 67%。
		// **云带必须遮得住日/月**（需求：大云应该在太阳或月亮的
		// 上面遮盖，而不是云被太阳遮盖）。
		// 逐像素对照实测（同一帧开/关粒子层相减）：云带经过日/月行时只把天空
		// 增亮 40~75 级、把 250 级的日面从 250 压到 244 ⇒ 日/月整块从云里透出来
		// ⇒ 读成"太阳盖在云上面"。原 alpha = (40+30·frac)/255 = **0.157–0.275**。
		// 提到 (150+50·frac)/255 = **0.588–0.784**：
		// 盖在 250 级日面上 → 216×0.70 + 250×0.30 = 226（日面 250→226，降 24 级）
		// 同一条云带在暗天空上 → 216×0.70 + 90×0.30 = 178
		// ⇒ 覆盖处与云带自身都形成清晰台阶，silhouette 读得出。
		// 尺寸口径不动（带宽 285–524 px，已按需求缩小大体积云、砍过 42% 宽）。
		// **alpha 随索引递减（顶部带最实）**——此前是递增 0.588→0.784，方向反了。
		// 实测（ui/bandcover_test.go）：与月盘 y 117..247 px 相交的**只有带0(38..229)
		// 与带1(192..413)**，而递增口径下这两条恰好是 alpha 最低的 0.588 / 0.654。
		// 合成亮度算术（云体 216、月核 246、暗天空 90）：
		// 云盖月面 − 云盖暗天空 = (246−90)·(1−alpha) = 156·(1−alpha)
		// alpha=0.588 → 差 64 级；0.654 → 54 级 ⇒ 月面从云里透出来 ⇒ 读成"月亮盖住云"
		// （缺陷：部分大云没有遮盖月亮，反而被月亮遮盖）。
		// 反向后：带0=0.941(差 9 级)、带1=0.891(19 级)、带2=0.840(25 级)、带3=0.789(34 级)。
		// 只有真正会盖到日/月的两条被提到遮得住的区间；下面两条不压盘，
		// 保持较低 alpha 以免在时钟区域堆成更重的实心块。
		alpha := (240.0 - 25.0*frac) / 255.0
		// 云带纵向铺开范围：0.24 把 4 条云带压在顶部 26% 内，
		// 范围做成可调量，供出图对比挑选（指定认可 0.24）。
		sc := (1.5 + 0.8*frac) / float64(spriteSupersample)
		sx := sc
		if bp.mirror { // 奇数索引水平镜像（机制复用）
			sx = -sc
		}
		alpha *= f.fade // 类别切换交叉淡出/淡入
		tr := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(sx), float32(sc))).
			Offset(f32.Pt(float32(bp.x), float32(bp.y)))
		ts := op.Affine(tr).Push(gtx.Ops)
		st := paint.PushOpacity(gtx.Ops, float32(alpha))
		cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
		f.sp.cloud[fi].Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
		st.Pop()
		ts.Pop()
	}
}

func (f *Field) Draw(gtx layout.Context, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	t := f.t

	// 阴雨类云层厚度：画在粒子之前，
	// 粒子在云带前面飞，形成纵深。
	switch f.cls {
	case effect.Rain, effect.Thunder, effect.Cloudy, effect.Fog:
		f.drawCloudBands(gtx, w, h, t)
	}

	// 雾霭梯度：底部浓、顶部淡的空气透视（判读"雾像均匀蒙版、无纵深"）
	if f.cls == effect.Fog {
		f.drawHaze(gtx, w, h, t)
	}

	// 闪光（雷暴）
	if f.flash > 0.01 {
		a := uint8(math.Min(255, f.flash*110))
		st := paint.PushOpacity(gtx.Ops, float32(f.flash))
		paint.Fill(gtx.Ops, color.NRGBA{235, 240, 255, a})
		st.Pop()
	}

	// 绘制数量 = 前 active 个粒子（fps 自适应）。
	// 削减只影响这里：parts 全量仍被 Step 推进、spawn 照常回收，
	// 生成器与贴图不受影响。
	active := f.ActiveCount()
	for i := 0; i < active; i++ {
		p := &f.parts[i]
		alpha := p.alpha
		if p.amp > 0 {
			// 连续闪烁：base + amp*sin(ω t + φ)
			alpha += p.amp * math.Sin(p.omega*t+p.phase)
		}
		// 持久实例抖动：亮度系数乘到 alpha 上、
		// 透明度系数再乘一次。两个系数都是索引决定的常量 ⇒ 跨帧稳定。
		alpha *= p.brightnessMul
		alpha *= p.alphaMul
		alpha *= f.fade // 类别切换交叉淡出/淡入
		// **云的不透明度下限 0.92**（缺陷：月亮依旧在云层上，
		// 而不是被云遮挡；还是月亮在云上，云没有盖住月亮）。
		// 实测根因：p.alpha 虽然设成 1.0，但上面两行实例抖动把它打折到
		// [0.65,1.35]×[0.50,1.00] = **0.33–1.0**（均值 0.75）。
		// α=0.33 盖在 246 级月面上只压到 221×0.33+246×0.67 = 238
		// ⇒ 月面几乎没变暗，眉月整条轮廓从云里透出来 ⇒ 读成"月亮在云前面"。
		// 图层顺序本身是对的（render.go:272 skyBlit → :282 moonBlit → :285 Field.Draw，
		// 云画在月亮之后，所以遮不住的唯一机制就是这里的 α 被抖下去。
		// 0.92 ⇒ 云盖到处 221×0.92+246×0.08 = 223（月面 246→223，降 23 级），
		// 云自身在暗天空上是 205 ⇒ 覆盖处形成清晰台阶， silhouette 读得出。
		// 尺寸口径不受影响：云实际只有 95×63 px（spriteScale 常量 0.25），
		// 不会再回到"整块巨团"那个问题。
		if p.kind == pkCloud && f.cls != effect.Clear && alpha < 0.92 {
			// 下限限定在 Cloudy/Rain/Thunder/Fog：这些类别的云要盖住日/月。
			// Clear 类的云是点缀（alpha 0.45–0.70），套上下限就变成不透明团。
			alpha = 0.92
		}
		// **云遮月要遮得住**
		// "两次跳变并没有改变月亮在云层上面的事实"。
		// 月亮已画在云之前，但云 α 只有 0.28–0.45：盖到 246 级的月面上只压到 ~185，
		// 眉月的锐利轮廓从云里透出来 ⇒ 眼睛判成"月亮在云前面"。
		// 口径：与月亮外接矩形相交的那朵云，α 抬到 ≥0.85（云遮月时更厚、背面被打亮，
		// 物理上也说得通。只按粒子的矩形相交判定 ⇒ 每帧每朵一次矩形比较，零像素成本。
		if alpha <= 0.01 {
			continue
		}
		// 雨线：不走贴图，走描边 + 沿长度只变 alpha
		// 的线性渐变。基础 alpha 由 depth 给（0.35→0.70），在 drawRain 里算，
		// 这里只把实例抖动系数传进去。
		if p.kind == pkDrop {
			f.drawRain(gtx, w, h, p, p.brightnessMul*p.alphaMul)
			continue
		}
		// 空气透视：越远越淡（判读"雨丝缺乏近大远小""雪像屏幕灰尘"
		// 的共性根因是远近粒子一样亮
		if p.kind == pkFlake || p.kind == pkMote {
			alpha *= 0.25 + 0.75*p.depth
		}
		if alpha > 1 {
			alpha = 1
		}

		sz, img, ok := f.pick(p)
		if !ok {
			continue
		}

		// 目标尺寸：贴图尺寸 × 缩放 × 视差 × 实例尺寸抖动（scaleMul）
		scale := spriteScale(p)
		dw := float32(sz.X) * float32(scale)
		dh := float32(sz.Y) * float32(scale)
		if dw < 2 || dh < 2 {
			continue
		}
		px := float32(p.x*float64(w)) - dw/2
		py := float32(p.y*float64(h)) - dh/2

		// 变换链（局部贴图坐标 → 屏幕坐标）：缩放 → 绕自身中心旋转 → 平移到位置。
		// f32.Affine2D 的方法是 Offset（不是 Translate），Rotate 签名是 (origin, radians)。
		fx := dw / float32(sz.X)
		fy := dh / float32(sz.Y)
		cx, cy := dw/2, dh/2
		if p.mirror {
			// 水平镜像：奇数索引对云/雾取负 X 缩放。
			// 负缩放将图形翻到 -X 侧，故平移补一个 dw 把包围盒搬回原位。
			fx = -fx
			px += dw
		}
		tr := f32.Affine2D{}.
			Scale(f32.Point{}, f32.Point{X: fx, Y: fy}).
			Offset(f32.Pt(px, py))
		if p.rotV != 0 {
			tr = f32.Affine2D{}.
				Scale(f32.Point{}, f32.Point{X: fx, Y: fy}).
				Rotate(f32.Point{X: cx, Y: cy}, float32(p.rot)).
				Offset(f32.Pt(px, py))
		}
		// 变换必须 Push/Pop 成对：gioui 的 Affine.Add 是"乘到当前变换上且
		// 不压栈"（op.go:196-206 原文：Add is like Push except it doesn't
		// push the current transformation to the stack，逐粒子 Add 会让
		// 变换累积叠加，第 2 个粒子起全部跑到屏幕外 —— 实测 scale 0.55 时
		// 整屏只剩 1 个精灵（1342 亮像素，包围盒 40x67）。
		// **云阴影层已删除**（云边缘出现边线，观感很差。原实现：genCloudShadow 生成的 240×160
		// **固定形状暗色椭圆**垫在云体贴图之下、向下偏移 10% 高度、α×0.55。
		// 它是程序生成的团块，与 Kenney 云体贴图的轮廓不匹配
		// ⇒ 从云体贴图边缘露出来 = 看到的"边线"。Kenney 云贴图自带明暗两面。
		// 同轮删除：overMoon 加浓（α→0.85）与 cloudY 六成偏置 —— 加浓基于错误诊断，
		// 它让落在月亮上的小云与周围大云亮度不一致
		// （小云与大云差异过大，成因指向加浓——该判断正确）。
		// 月亮位置从 0.13h 移到 **0.30h** = 低性能 x86 小主机 12 帧连拍量到的云质量中心
		// （云像素峰值 0.30–0.40 屏高，月亮原先在其上方 0.17 屏高 ⇒ "大云都在月亮下面"）。

		ts := op.Affine(tr).Push(gtx.Ops)
		st := paint.PushOpacity(gtx.Ops, float32(alpha))
		cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
		img.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
		st.Pop()
		ts.Pop()
	}

	// 闪电（雷暴）：闪光起头时画一道，随 flash 淡出
	if f.cls == effect.Thunder && f.flash > 0.25 {
		f.drawBolt(gtx, w, h, f.flash)
	}

	// 月亮（晴夜）：**已烘进天空纹理**（ui.renderSkyCPU → bakeCelestial），
	// 每帧零成本。原每帧 64 圆盘 = 5.7 个整屏当量，是 Clear 夜的最大单项开销。
	// 保留 drawMoon 函数本体供离线路径与回退使用，生产路径不再逐帧调用。

	// 落地水花（参照 termanim/rain:draw）：扩散 + 衰减的印记，
	// 让雨"落在某处"。参照反馈：早期溅起的水还在地板之上。
	for _, sp := range f.splashes {
		frac := 1 - sp.age/splashLife
		if frac <= 0 {
			continue
		}
		// 参照是终端字符画：1 个字符就是它的全部视觉单位。搬到 1080×1920
		// 上照抄成 2 px ⇒ 看不见（视觉模型判"没有水花"）。
		// 但按屏高等比放大（u=h/50 ⇒ 34 px 高）后，21 个水花 × 每个 3 块
		// 铺满底边 ⇒ 又糊成一条连续亮带（实测底部 34 px 行均值 +47），
		// **正是参照警告的形态**："a floor of wide bars is a line rather
		// than a scatter of impacts"。
		// ⇒ 取中间口径：单颗约 8×10 px、峰值 alpha 降到 0.7，
		// 目标是读得出"一颗颗撞击"，既不看不见也不连成线。
		a := 0.35 + frac*0.35
		c := color.NRGBA{R: rainLineRGB.R, G: rainLineRGB.G, B: rainLineRGB.B,
			A: uint8(a * 255)}
		x := sp.x * float64(w)
		e := sp.age * crownSpread * 1.2 // 扩散速度：像素/秒（当前屏口径）
		const wid, hgt = 8, 10
		paintRect(gtx, int(x)-wid/2, h-hgt, int(x)+wid/2, h, c)
		if e > 0 {
			paintRect(gtx, int(x-e)-wid/2, h-hgt, int(x-e)+wid/2, h, c)
			paintRect(gtx, int(x+e)-wid/2, h-hgt, int(x+e)+wid/2, h, c)
			if sp.age < splashLife/2 {
				paintRect(gtx, int(x-e)-wid/2, h-2*hgt, int(x-e)+wid/2, h-hgt, c)
				paintRect(gtx, int(x+e)-wid/2, h-2*hgt, int(x+e)+wid/2, h-hgt, c)
			}
		}
	}
}

// paintRect 用最简单的方式在屏幕坐标画一个实心矩形（水花用，无变换）。
func paintRect(gtx layout.Context, x0, y0, x1, y1 int, c color.NRGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	cl := clip.Rect(image.Rect(x0, y0, x1, y1)).Push(gtx.Ops)
	paint.Fill(gtx.Ops, c)
	cl.Pop()
}

// 月白：三层**同色，只变 alpha**。
var moonColor = color.NRGBA{R: 245, G: 242, B: 230, A: 0xff}

// 月亮径向渐变参数。
//
// 2026-10-16 两轮视觉核对的实测教训：
// - 旧版三层实心盘 ⇒ "三个硬边同心圆，像灰色的靶子"；
// - 旧版第一版 26 环、每环画**绝对 alpha** ⇒ 读图仍是"一圈一圈的
// 硬边同心环"。根因：叠加后相邻环之间的 alpha 落差 = 该环自身 alpha，
// 近核处约 15/255 ≈ 6% 一跳 ⇒ 台阶肉眼可见。
//
// 正确做法：把目标**累积**曲线 A(r) = A0·exp(-decay·(r/rc−1)) 离散化，
// 每环只画**增量** `a_i = A(r_i) − A(r_{i+1})`，由外到内叠加。
// 叠加后中心累积 = A(rc) = A0、向外单调降到 A(外)，且相邻落差被压到
// 环数 N 的粒度上：N=64 时最大落差 ≈ 120×(1−e^(−3/63)) ≈ 6/255 ≈ 2.4%，
// 环间距 3.4/63 ≈ 0.054 rc ≈ 2.6 px ⇒ 台阶不可见。
const (
	moonRingN      = 64    // 环数（越大台阶越细）
	moonRingSpread = 3.4   // 最外环半径 = r_core × (1 + 3.4)
	moonRingA0     = 120.0 // 核心处累积 alpha
	moonRingDecay  = 3.0   // 累积曲线衰减率
	moonDiscAlpha  = 235   // 核心圆盘 alpha
)

// drawMoon 月亮：核心圆盘 + 64 环增量叠加的径向衰减光晕 + 3 个淡坑纹理。
// 夜间才画（Draw 里的判据不变）；pkGlow 类保留给地平线光。
func (f *Field) drawMoon(gtx layout.Context, w, h int, t float64) {
	cx := float32(w) * 0.72
	cy := float32(h) * 0.13
	// 半径口径沿用原 moonSz 96 px 贴图（1080 屏上半径 48 px），随屏宽走。
	r := float32(w) * 0.045
	pulse := float32(1.0 + 0.04*math.Sin(0.35*t)) // 原有的轻微呼吸，保留
	rc := r * pulse

	// 由外到内（i 从 N-1 递减到 0），每环画增量 alpha。
	for i := moonRingN - 1; i >= 0; i-- {
		frac := float64(i) / float64(moonRingN-1)
		next := float64(i+1) / float64(moonRingN-1)
		ri := rc * (1 + moonRingSpread*float32(frac))
		// 增量 = A(r_i) − A(r_{i+1})；最外环之外视为 0。
		aCur := moonRingA0 * float32(math.Exp(-moonRingDecay*frac))
		aNext := moonRingA0 * float32(math.Exp(-moonRingDecay*next))
		ai := aCur - aNext
		if ai < 1 {
			continue
		}
		disc(gtx, cx, cy, ri, uint8(ai))
	}
	disc(gtx, cx, cy, rc, moonDiscAlpha)

	// 表面纹理：固定系数 ⇒ 跨帧稳定不闪。
	// 2026-10-16 视觉核对：alpha 26 的三坑"几乎没有"，提到 46 并补到 5 处，
	// 其中两处是月海（大片暗区），比小坑更容易被肉眼读到。
	craters := []struct{ dx, dy, rr float32 }{
		{-0.34, -0.21, 0.18},
		{0.26, 0.30, 0.26},
		{0.10, -0.40, 0.13},
		{-0.12, 0.22, 0.34}, // 月海
		{0.34, -0.14, 0.22}, // 月海
	}
	for _, c := range craters {
		discColor(gtx, cx+c.dx*rc, cy+c.dy*rc, c.rr*rc,
			color.NRGBA{R: 210, G: 208, B: 196, A: 46})
	}
}

// disc 铺一个半径 rad 的纯色圆盘：正方形 clip.RRect，四角半径 = rad ⇒ 正圆，
// 只填一种颜色（无变换、无渐变）。
func disc(gtx layout.Context, cx, cy, rad float32, a uint8) {
	discColor(gtx, cx, cy, rad, color.NRGBA{moonColor.R, moonColor.G, moonColor.B, a})
}

// discColor 是 disc 的通用版：任意颜色。
func discColor(gtx layout.Context, cx, cy, rad float32, c color.NRGBA) {
	if rad <= 0 {
		return
	}
	rr := gtx.Dp(unit.Dp(rad))
	rect := image.Rect(int(cx-rad), int(cy-rad), int(cx+rad), int(cy+rad))
	cl := clip.RRect{Rect: rect, SE: rr, SW: rr, NW: rr, NE: rr}.Push(gtx.Ops)
	paint.Fill(gtx.Ops, c)
	cl.Pop()
}

// drawRain 雨线（读图判读"纯白实心、塑料感、缺透明度"）：
//
// 1. 沿长度 alpha 渐隐：clip.Stroke 出线形，paint.LinearGradientOp 沿长度铺，
// 两端 **RGB 完全相同** = [225,235,245]，只变 alpha：头部 0.85 → 尾部 0.15；
// 2. 线宽随景深：depth<0.4 → 1 px，0.4–0.7 → 1.5 px，>0.7 → 2 px；
// 3. 基础 alpha 随景深 0.35 → 0.70（远淡近浓），再乘实例抖动；
// 4. 倾角 = 全场缓慢风向项 windAngle(t) = 0.06·sin(t/9) rad。
//
// 头部 = 下落方向的前端（下端），尾部 = 上端。
//
// 注意：gioui 的线性渐变**不支持变换**（v0.10.3 gpu/internal/rendertest/
// render_test.go:246 `t.Skip("linear gradients don't support transformations")`，
// 故这里**不加 Affine**：线形用 clip.Stroke 的路径表达，渐变端点直接给屏幕坐标。
func (f *Field) drawRain(gtx layout.Context, w, h int, p *particle, mul float64) {
	// 景深分离。
	//
	// 2026-10-16 两轮实测（FX_DEBUG 原文）：加因子后读图仍"粗细浓淡基本一致"。
	// 根因不在因子，在**与景深无关的随机项**：
	// - 长度里乘了 `p.sc`（每粒子随机缩放） ⇒ depth=0.51 算出 len=59.7，
	// 比 depth=0.93 的 len=40.8 还长，远近排序被随机项彻底打乱；
	// - alpha 里乘了 `brightnessMul×alphaMul`（∈0.325–1.35，与景深无关）
	// ⇒ headA 落在 0.209–0.628 的无序散布。
	// 修法：雨线的长度与 alpha **只由景深驱动**，随机项降为 ±10% 的轻微抖动。
	// 参照 github.com/0magnet/termanim/rain：三条量**零随机、全部由 depth 导出**。
	// bright = 0.12 + depth²·0.88      （平方压向亮端，8 倍跨度）
	// length = vy · exposure           （运动模糊：线长 = 快门时间内移动的距离）
	// 尾 = 头的 40%                    （参照反馈：淡到 0 会丢掉线长感，
	// 不淡就画成一根没有方向感的条
	// 此前乘的 p.sc / brightnessMul / alphaMul / scaleMul 全部**不再参与**，
	// 它们是与景深无关的大随机项，会把单调序打乱。
	headA := 0.12 + p.depth*p.depth*0.88
	tailA := headA * rainTailFrac
	width := rainWidthPx(p.depth)
	// 线长 = 速度 × 快门时间，换算到像素（p.vy 的单位是"屏高/秒"）。
	L := p.vy * float64(h) * rainExposure
	if L < 3 {
		L = 3
	}
	cx, cy := p.x*float64(w), p.y*float64(h)
	// 倾角 = 全场风向项 + 每粒子固定抖动。抖动由已有的
	// scaleMul（∈[0.75,1.25]，生成时定死）线性映射而来 ⇒ 跨帧稳定不闪，
	// 不必新增字段。
	ang := windAngle(f.t) + (p.scaleMul-1.0)/0.25*windJitterAmp
	sx, sy := math.Sin(ang)*L/2, math.Cos(ang)*L/2
	hx, hy := cx+sx, cy+sy   // 头部（下端，alpha 0.85×base）
	txf, tyf := cx-sx, cy-sy // 尾部（上端，alpha 0.15×base）

	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(txf), float32(tyf)))
	path.LineTo(f32.Pt(float32(hx), float32(hy)))
	spec := path.End()

	st := clip.Stroke{Path: spec, Width: float32(width)}.Op().Push(gtx.Ops)
	paint.LinearGradientOp{
		Stop1:  f32.Pt(float32(hx), float32(hy)),
		Color1: color.NRGBA{rainLineRGB.R, rainLineRGB.G, rainLineRGB.B, uint8(headA * 255)},
		Stop2:  f32.Pt(float32(txf), float32(tyf)),
		Color2: color.NRGBA{rainLineRGB.R, rainLineRGB.G, rainLineRGB.B, uint8(tailA * 255)},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

// DebugStats 打印前若干粒子的实际绘制参数，用于离线定位"粒子画不出来"，
// 并作为旧版自检的证据行（抖动系数、雨线两端 alpha、镜像位）。
func (f *Field) DebugStats(n int) string {
	if n > len(f.parts) {
		n = len(f.parts)
	}
	out := ""
	for i := 0; i < n; i++ {
		p := &f.parts[i]
		sz, _, _ := f.pick(p)
		scale := spriteScale(p)
		out += fmt.Sprintf("p%d kind=%d x=%.3f y=%.3f sz=%dx%d sc=%.2f scale=%.3f dw=%.1f dh=%.1f alpha=%.2f amp=%.2f bmul=%.3f amul=%.3f smul=%.3f mirror=%v spr=%d\n",
			i, p.kind, p.x, p.y, sz.X, sz.Y, p.sc, scale,
			float32(sz.X)*float32(scale), float32(sz.Y)*float32(scale),
			p.alpha, p.amp, p.brightnessMul, p.alphaMul, p.scaleMul, p.mirror, p.spr)
		if p.kind == pkDrop {
			// 与 drawRain 逐字同一套公式（参照 termanim/rain）。
			// 自检证据必须来自真实绘制路径。
			headA := 0.12 + p.depth*p.depth*0.88
			out += fmt.Sprintf("    rain: depth=%.2f len=%.1f width=%.1f headA=%.3f tailA=%.3f vy=%.3f ang=%.4f\n",
				p.depth, p.vy*float64(f.sh)*rainExposure,
				rainWidthPx(p.depth), headA, headA*rainTailFrac,
				p.vy,
				windAngle(f.t)+(p.scaleMul-1.0)/0.25*windJitterAmp)
		}
	}
	return out
}

// JitterStats 全场实例抖动的分布摘要
// "渲染日志里粒子的 brightnessMul/alphaMul 分布"证据：
// 每类的 min/mean/max，以及镜像粒子数与变体覆盖数。
func (f *Field) JitterStats() string {
	type acc struct {
		n             int
		min, max, sum float64
	}
	var b, a, s acc
	mirrorN := 0
	vars := map[pkind]map[int]int{}
	for i := range f.parts {
		p := &f.parts[i]
		for _, tc := range []struct {
			v float64
			c *acc
		}{{p.brightnessMul, &b}, {p.alphaMul, &a}, {p.scaleMul, &s}} {
			v, c := tc.v, tc.c
			if c.n == 0 || v < c.min {
				c.min = v
			}
			if c.n == 0 || v > c.max {
				c.max = v
			}
			c.sum += v
			c.n++
		}
		if p.mirror {
			mirrorN++
		}
		if vars[p.kind] == nil {
			vars[p.kind] = map[int]int{}
		}
		vars[p.kind][p.spr]++
	}
	line := fmt.Sprintf("JITTER n=%d bmul[min=%.3f mean=%.3f max=%.3f] amul[min=%.3f mean=%.3f max=%.3f] smul[min=%.3f mean=%.3f max=%.3f] mirror=%d",
		len(f.parts),
		b.min, b.sum/float64(b.n), b.max,
		a.min, a.sum/float64(a.n), a.max,
		s.min, s.sum/float64(s.n), s.max,
		mirrorN)
	for _, k := range []pkind{pkCloud, pkFog, pkStar, pkFlake, pkDrop, pkMote, pkGlow} {
		m := vars[k]
		if len(m) == 0 {
			continue
		}
		line += fmt.Sprintf(" kind%d变体=%d", k, len(m))
	}
	return line + "\n"
}

// pick 按粒子取对应变体的贴图与尺寸。
func (f *Field) pick(p *particle) (image.Point, paint.ImageOp, bool) {
	clamp := func(i int, n int) int {
		if i < 0 {
			return 0
		}
		if i >= n {
			return n - 1
		}
		return i
	}
	switch p.kind {
	case pkStar:
		i := clamp(p.spr, len(f.sp.star))
		return f.sp.starSz[i], f.sp.star[i], true
	case pkGlow:
		return f.sp.sunSz, f.sp.sun, true
	case pkCloud:
		i := clamp(p.spr, len(f.sp.cloud))
		return f.sp.cloudSz[i], f.sp.cloud[i], true
	case pkFog:
		if len(f.sp.fog) == 0 {
			i := clamp(p.spr, len(f.sp.cloud))
			return f.sp.cloudSz[i], f.sp.cloud[i], true
		}
		i := clamp(p.spr, len(f.sp.fog))
		return f.sp.fogSz[i], f.sp.fog[i], true
	case pkFlake:
		i := clamp(p.spr, len(f.sp.flake))
		return f.sp.flakeSz[i], f.sp.flake[i], true
	case pkMote:
		i := clamp(p.spr, len(f.sp.mote))
		return f.sp.moteSz[i], f.sp.mote[i], true
	}
	return image.Point{}, paint.ImageOp{}, false
}

func spriteSize(sp *sprites, k pkind) image.Point {
	switch k {
	case pkStar:
		return sp.starSz[0]
	case pkGlow:
		return sp.sunSz
	case pkCloud:
		return sp.cloudSz[0]
	case pkFog:
		if len(sp.fog) > 0 {
			return sp.fogSz[0]
		}
		return sp.cloudSz[0]
	case pkFlake:
		return sp.flakeSz[0]
	case pkMote:
		return sp.moteSz[0]
	}
	return image.Point{}
}

// genStreak：横向拉长的流线软条，用于沙尘 —— 判读指出圆斑"缺乏被风吹拂的
// 流线感"，横向拉伸 + 左端淡出做出被风拖出来的方向性。
func genStreak(sz image.Point, rng *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	wob := rng.Float64() * 6.28
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			nx := (float64(x) - cx) / cx
			ny := (float64(y) - cy) / cy
			// 纵向薄、横向长
			vert := math.Exp(-ny * ny * 3.2)
			horz := 1.0 - smoothstep(0.40, 1.0, math.Abs(nx))
			// 左端淡出 = 风的来向
			tail := 0.45 + 0.55*(0.5*nx+0.5)
			a := vert * horz * tail
			// 沿长度方向的浓淡起伏
			a *= 0.72 + 0.28*math.Sin(nx*3.0+wob)
			if a > 1 {
				a = 1
			}
			if a < 0 {
				a = 0
			}
			a *= 0.72
			img.SetNRGBA(x, y, color.NRGBA{222, 200, 162, uint8(a * 255)})
		}
	}
	return img
}

// genBolt：折线闪电，带辉光。雷暴必须看得见闪电（判读："没有看到闪电"）。
func genBolt(sz image.Point, rng *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	// 生成一条从上到下的折线
	pts := [][2]float64{{float64(sz.X) * 0.55, 0}}
	x := float64(sz.X) * 0.55
	for y := 1.0; y < float64(sz.Y); y += float64(sz.Y) / 9.0 {
		x += (rng.Float64() - 0.5) * float64(sz.X) * 0.55
		if x < 6 {
			x = 6
		}
		if x > float64(sz.X)-6 {
			x = float64(sz.X) - 6
		}
		pts = append(pts, [2]float64{x, y})
	}
	segs := make([][4]float64, 0, len(pts)-1)
	for i := 0; i < len(pts)-1; i++ {
		segs = append(segs, [4]float64{pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]})
	}
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			d := 1e9
			for _, sg := range segs {
				d = math.Min(d, distSeg(float64(x), float64(y), sg))
			}
			// 芯线 + 外辉光
			core := math.Exp(-d * d * 1.6)
			glow := math.Exp(-d * d * 0.06)
			a := core*0.95 + glow*0.35
			if a > 1 {
				a = 1
			}
			if a <= 0.004 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{250, 248, 255, uint8(a * 255)})
		}
	}
	return img
}

func distSeg(px, py float64, sg [4]float64) float64 {
	x1, y1, x2, y2 := sg[0], sg[1], sg[2], sg[3]
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	if l2 <= 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

// drawBolt 画一道闪电，亮度跟随闪光衰减。
func (f *Field) drawBolt(gtx layout.Context, w, h int, flash float64) {
	sz := f.sp.boltSz
	sc := float32(1.6) + float32(w)/1080.0*1.2
	dw, dh := float32(sz.X)*sc, float32(sz.Y)*sc
	px := float32(w) * 0.58
	py := float32(h) * 0.10
	ts := op.Affine(f32.Affine2D{}.
		Scale(f32.Point{}, f32.Point{X: dw / float32(sz.X), Y: dh / float32(sz.Y)}).
		Offset(f32.Pt(px, py))).Push(gtx.Ops)
	st := paint.PushOpacity(gtx.Ops, float32(math.Min(1.0, flash*1.4)))
	cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
	f.sp.bolt.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	st.Pop()
	ts.Pop()
}

// Jitter 一个粒子的实例抖动三元组（自检口径）。
type Jitter struct{ B, A, S float64 }

// JitOf 第 i 个粒子的抖动系数。自检用它验证"跨帧不闪"：抖动由索引决定，
// 推进任意帧后必须与建场时逐位相同。
func (f *Field) JitOf(i int) Jitter {
	if i < 0 || i >= len(f.parts) {
		return Jitter{}
	}
	p := &f.parts[i]
	return Jitter{p.brightnessMul, p.alphaMul, p.scaleMul}
}

// AirOverlapStats 同带横向重叠对数与空中粒子数（自检）：
// 同带 = |Δy| ≤ 0.12，重叠 = |Δx| < 0.22。
func (f *Field) AirOverlapStats() (pairs, air int) {
	for i := range f.parts {
		if !isAir(f.parts[i].kind) {
			continue
		}
		air++
		for j := i + 1; j < len(f.parts); j++ {
			if !isAir(f.parts[j].kind) {
				continue
			}
			if math.Abs(f.parts[i].y-f.parts[j].y) <= airBandN &&
				math.Abs(f.parts[i].x-f.parts[j].x) < airGapN {
				pairs++
			}
		}
	}
	return
}

// SetFlash 仅供离线渲染测试：强制设定闪光强度，验证闪电是否可见。
func (f *Field) SetFlash(v float64) { f.flash = v }

// genCloudShadow：云的暗色底面，用于垫在亮体下方做体积感。
func genCloudShadow(sz image.Point, rng *rand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sz.X, sz.Y))
	blobs := make([][3]float64, 0, 22)
	baseY := float64(sz.Y) * 0.70
	for i := 0; i < 22; i++ {
		bx := float64(sz.X) * (0.06 + 0.88*rng.Float64())
		by := baseY - float64(sz.Y)*0.46*rng.Float64()*rng.Float64()
		br := float64(sz.X) * (0.06 + 0.11*rng.Float64())
		blobs = append(blobs, [3]float64{bx, by, br})
	}
	for y := 0; y < sz.Y; y++ {
		for x := 0; x < sz.X; x++ {
			var acc float64
			for _, b := range blobs {
				d := math.Hypot(float64(x)-b[0], float64(y)-b[1]) / b[2]
				acc += math.Exp(-d * d * 1.5)
			}
			a := acc / 5.2
			nx, ny := float64(x)/float64(sz.X), float64(y)/float64(sz.Y)
			edge := (1.0 - smoothstep(0.28, 0.5, math.Abs(nx-0.5))) *
				(1.0 - smoothstep(0.06, 0.34, ny-0.74))
			a *= edge
			// 暗面：偏冷偏暗
			img.SetNRGBA(x, y, color.NRGBA{44, 52, 66, uint8(a * 255)})
		}
	}
	return img
}

// drawHaze 底部浓、顶部淡的雾霭梯度，做出空气透视纵深。
func (f *Field) drawHaze(gtx layout.Context, w, h int, t float64) {
	cl := clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops)
	// 两端同色、只变 alpha：gioui 的渐变按端点色插值，两端 RGB 不同
	// 会把整屏插成白色（实测背景变成 255,255,255）。
	paint.LinearGradientOp{
		Stop1:  f32.Pt(0, float32(h)*0.30),
		Color1: color.NRGBA{198, 206, 214, 0},
		Stop2:  f32.Pt(0, float32(h)),
		Color2: color.NRGBA{198, 206, 214, 96},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
}

// airHalfN 云/雾精灵的半宽/半高（归一化）：与绘制口径逐字一致
// （`scale = p.sc × (0.55 + 0.45·depth) × p.scaleMul`，effects.go:867/1164）。
func (f *Field) airHalfN(p *particle) (hw, hh float64) {
	sp := spritesOnce()
	sz := spriteSize(sp, p.kind)
	if p.kind == pkCloud && p.spr >= 0 && p.spr < len(sp.cloudSz) {
		sz = sp.cloudSz[p.spr]
	} else if p.kind == pkFog && p.spr >= 0 && p.spr < len(sp.fogSz) {
		sz = sp.fogSz[p.spr]
	}
	scale := spriteScale(p)
	if f.sw <= 0 || f.sh <= 0 {
		return 0.2, 0.1
	}
	hw = (float64(sz.X) * scale / 2) / float64(f.sw)
	hh = (float64(sz.Y) * scale / 2) / float64(f.sh)
	return
}

// placeAirEntry 云/雾的**进场落位**（低性能 x86 小主机反馈）：
//
//	反馈 1"有云会突然刷新出来，应该在外面慢慢进来而不是在眼前突然出现"
//	反馈 2"有的云在顶上，然后只有云的下面一小部分在屏幕内"
//
// 根因：回收走 `spawn(false)` ⇒ x 在 [0,1] 全屏随机 + y 走 `bandFreeY`
// （取值区间 recycleYLo=-0.35 … recycleYHi=1.15） ⇒ 中心可以落在屏幕外，
// 于是只有半截在屏内；而"随机 x + 立刻可见"就是眼前突然出现。
//
// 改法：
// - x 从**屏幕外**进入：vx>0 从左边缘外进（x = -hw，身体完全在屏外），
// vx<0 从右边缘外进（x = 1+hw）。进场即不可见，随漂移慢慢进入。
// - y 夹到**整个精灵都在屏内**：y ∈ [hh, 1-hh]，再与信息带避让区取交
// （带上方 [y0-pad-hh, y0-pad] 与带下方 [y1+pad, y1+pad+hh] 的可用段）。
func (f *Field) placeAirEntry(p *particle, r *rand.Rand) {
	hw, hh := f.airHalfN(p)
	if p.vx >= 0 {
		p.x = -hw
	} else {
		p.x = 1 + hw
	}
	// 雾：**只要求整只在屏内，不做带避让**。
	// 几何无解（实测）：雾 sc 上限 3.2 × 256 px ⇒ 半高 0.213 屏高，
	// 信息带 0.244–0.822 占 0.578 屏高，带外可用 0.422 < 2×0.213 = 0.426。
	// 旧代码靠把中心放到屏幕外（bandFreeY 区间 -0.35…1.15）来"避让"，
	// 那正是低性能 x86 小主机反馈"只有云的下面一小部分在屏幕内"来源。
	// 雾是 alpha 0.06–0.22 的大气薄雾，压过文字是纵深本身。
	//
	// 云同口径（上游原文 upstream/core/settings/default_settings.py:1523-1540）：
	// 'weather': {'bg_color': [35,35,35,255], 'bg_opacity': 0.3, 'show_background': True}
	// ⇒ 上游信息面板**自带 30% 不透明度深色底**，动画画在它下面 ⇒ 云不需要避让信息带。
	// 本复刻已有这层底（ui/render.go:621），绘制顺序 天空→粒子→fillPanel→文字
	// （render.go:263/270/277） ⇒ 带避让与这层底是重复机制，删。
	// 保留带避让的实测代价：带 0.244–0.822 占 578 px，云原生高 121–138 px
	// ⇒ 带外只剩顶部 182 px + 底部 56 px = 238 px，只容 1.3 行
	// ⇒ 8 朵全挤顶部叠成一块（连通块宽 735 px）⇒ 顶部一堆一堆重叠的云。
	if p.kind == pkFog || p.kind == pkCloud {
		if p.kind == pkCloud {
			// **云的落位 = 月亮行**（需求：云要在月亮上面，不是部分；
			// 云不得从月亮下面过去；云不得一起出现一起消失）。
			// 原实现把 y 铺满整屏 [hh, 1−hh] ⇒ 实测落位 0.459/0.613/0.806/0.912，
			// 全在月亮（0.095）下方 ⇒ 云永远从月亮底下飘过去。
			// 月盘半径 moonRcFrac 是**屏宽**单位 ⇒ y 轴口径要乘 W/H：0.060×1080/1920 = 0.0338。
			// 直接拿 0.060 当 y 偏移会把范围放大约 2 倍（实测云落在 0.019/0.020/0.021，
			// 而月盘 y 范围只有 0.061–0.129 ⇒ 这些云这辈子碰不到月亮。
			rcY := moonRcFrac * float64(f.sw) / float64(f.sh)
			loY := 0.095 - rcY - hh
			hiY := 0.095 + rcY + hh
			if loY < hh {
				loY = hh
			}
			if hiY > 1-hh {
				hiY = 1 - hh
			}
			if hiY <= loY {
				hiY = loY + 0.01
			}
			p.y = loY + r.Float64()*(hiY-loY)
			// 进场 x 在屏幕外**铺开**：原实现固定 p.x = −hw ⇒ 8 朵云全挤在同一个
			// 左沿起点 ⇒ 同进同出，中间整片天空空掉（就是"一起出现一起消失"）。
			if p.vx >= 0 {
				p.x = -hw - r.Float64()*0.35
			} else {
				p.x = 1 + hw + r.Float64()*0.35
			}
			return
		}
		if 1-2*hh > 0 {
			p.y = hh + r.Float64()*(1-2*hh)
		} else {
			p.y = 0.5
		}
		f.spreadAir(p, r, false) // 回收：x 固定在屏幕外，只有 y 可调
		return
	}
	pad := bandPadJitN(p.kind, f.sh)
	y0, y1 := infoBandY()
	// 可用段：带上方（精灵整体在带上方且整体在屏内）/ 带下方（同理）
	type seg struct{ lo, hi float64 }
	segs := make([]seg, 0, 2)
	// 带上方：中心 y ≤ y0-pad-hh，且 y ≥ hh
	if hi := y0 - pad - hh; hi >= hh {
		segs = append(segs, seg{hh, hi})
	}
	// 带下方：中心 y ≥ y1+pad+hh，且 y ≤ 1-hh
	if lo := y1 + pad + hh; lo <= 1-hh {
		segs = append(segs, seg{lo, 1 - hh})
	}
	if len(segs) == 0 {
		// 带太宽放不下整只精灵 ⇒ 退化为"整只在屏内"，允许压带（比半截出屏好）
		if 1-2*hh > 0 {
			p.y = hh + r.Float64()*(1-2*hh)
		} else {
			p.y = 0.5
		}
		return
	}
	total := 0.0
	for _, s := range segs {
		total += s.hi - s.lo
	}
	t := r.Float64() * total
	for _, s := range segs {
		w := s.hi - s.lo
		if t <= w {
			p.y = s.lo + t
			return
		}
		t -= w
	}
	p.y = segs[len(segs)-1].hi
}

// SetSpeedScale 设置位移速度系数（实测口径）。
// 调用方口径：speedScale = 目标帧率 / 15（15 fps ⇒ 1.0，8 fps ⇒ 0.533）。
// 传 0 或负数 = 恢复不缩放。
func (f *Field) SetSpeedScale(k float64) {
	if k <= 0 {
		k = 1
	}
	f.speedScale = k
}

// clampAirY 只把云/雾中心 y 夹到"整只在屏内"并与信息带避让区取交，
// 不动 x（开局建场用）。区间口径同 placeAirEntry。
// overMoon 该空气粒子的矩形是否与月亮外接矩形相交（含光晕范围）。
// 月亮几何与 ui/render.go 的 moonLayerGet 逐字同口径：心 (0.72w, 0.13h)，
// 外沿 = rc×(1+2.2)，rc = 0.060w。
func (f *Field) overMoon(p *particle) bool {
	if f.sw <= 0 || f.sh <= 0 {
		return false
	}
	hw, hh := f.airHalfN(p)
	mcX, mcY := 0.72, 0.095
	mR := 0.060 * (1.0 + 2.2) // 归一化到屏宽；y 方向按屏宽换算
	padX, padY := mR, mR*float64(f.sw)/float64(f.sh)
	return !(p.x+hw < mcX-padX || p.x-hw > mcX+padX || p.y+hh < mcY-padY || p.y-hh > mcY+padY)
}

func (f *Field) clampAirY(p *particle, r *rand.Rand, allowX bool) {
	_, hh := f.airHalfN(p)
	// 雾：**只要求整只在屏内，不做带避让**。
	// 几何无解（实测）：雾 sc 上限 3.2 × 256 px ⇒ 半高 0.213 屏高，
	// 信息带 0.244–0.822 占 0.578 屏高，带外可用 0.422 < 2×0.213 = 0.426。
	// 旧代码靠把中心放到屏幕外（bandFreeY 区间 -0.35…1.15）来"避让"，
	// 那正是低性能 x86 小主机反馈"只有云的下面一小部分在屏幕内"来源。
	// 雾是 alpha 0.06–0.22 的大气薄雾，压过文字是纵深本身。
	//
	// 云同口径（上游原文 upstream/core/settings/default_settings.py:1523-1540）：
	// 'weather': {'bg_color': [35,35,35,255], 'bg_opacity': 0.3, 'show_background': True}
	// ⇒ 上游信息面板**自带 30% 不透明度深色底**，动画画在它下面 ⇒ 云不需要避让信息带。
	// 本复刻已有这层底（ui/render.go:621），绘制顺序 天空→粒子→fillPanel→文字
	// （render.go:263/270/277） ⇒ 带避让与这层底是重复机制，删。
	// 保留带避让的实测代价：带 0.244–0.822 占 578 px，云原生高 121–138 px
	// ⇒ 带外只剩顶部 182 px + 底部 56 px = 238 px，只容 1.3 行
	// ⇒ 8 朵全挤顶部叠成一块（连通块宽 735 px）⇒ 顶部一堆一堆重叠的云。
	if p.kind == pkCloud {
		// **开局建场同口径：云落在月亮行**（与 placeAirEntry 同一判据，
		// 需求：云要在月亮上面，不是部分。
		// 原实现 y 铺满整屏 [hh, 1−hh] ⇒ 实测 0.269/0.328/0.441/0.634/0.736/0.865/0.904，
		// 月亮在 0.095 ⇒ 开局就没有云在月亮行。
		rcY := moonRcFrac * float64(f.sw) / float64(f.sh) // 月盘半径的 **y 轴**口径
		loY := 0.095 - rcY - hh
		hiY := 0.095 + rcY + hh
		if loY < hh {
			loY = hh
		}
		if hiY > 1-hh {
			hiY = 1 - hh
		}
		if hiY <= loY {
			hiY = loY + 0.01
		}
		p.y = loY + r.Float64()*(hiY-loY)
		f.spreadAir(p, r, allowX) // 开局建场 allowX=true：x 与 y 都可自由取；错峰进场 allowX=false：x 由调用方排好，不许动
		return
	}
	if p.kind == pkFog {
		if 1-2*hh > 0 {
			p.y = hh + r.Float64()*(1-2*hh)
		} else {
			p.y = 0.5
		}
		f.spreadAir(p, r, allowX) // 开局建场 allowX=true：x 与 y 都可自由取；错峰进场 allowX=false：x 由调用方排好，不许动
		return
	}
	pad := bandPadJitN(p.kind, f.sh)
	y0, y1 := infoBandY()
	type seg struct{ lo, hi float64 }
	segs := make([]seg, 0, 2)
	if hi := y0 - pad - hh; hi >= hh {
		segs = append(segs, seg{hh, hi})
	}
	if lo := y1 + pad + hh; lo <= 1-hh {
		segs = append(segs, seg{lo, 1 - hh})
	}
	if len(segs) == 0 {
		if 1-2*hh > 0 {
			p.y = hh + r.Float64()*(1-2*hh)
		} else {
			p.y = 0.5
		}
		return
	}
	total := 0.0
	for _, s := range segs {
		total += s.hi - s.lo
	}
	t := r.Float64() * total
	for _, s := range segs {
		w := s.hi - s.lo
		if t <= w {
			p.y = s.lo + t
			return
		}
		t -= w
	}
	p.y = segs[len(segs)-1].hi
}

// spriteScale 精灵绘制尺度。
//
// 云/雾 = **1.0 固定（原生 1:1）**，其余类沿用深度口径。
//
// 依据（三轮实测 + 视觉模型判读）：
// - Kenney 云精灵实测是锐利平涂（半透明像素仅 1.75–1.96%，边缘只有 1 px 的
// alpha=127 斜坡，TestEdgeProfileIsRamp 剖面 [127,255,255,…]；
// - 尺度抖动 scaleMul ∈ [0.75,1.25] 让贴图倍率在 0.6–1.25 之间乱跳：
// 缩小把超采样做出的 3 px 斜坡压回 2 px ⇒ 视觉模型判"有锯齿台阶"；
// 放大则把硬边拉成糊团 ⇒ 判"发糊发虚"。**尺度在抽奖，观感不可控。**
// - Kenney 这类精灵包就是按原生尺寸设计的，1:1 使用是这类素材的标准用法。
// 云的尺寸变化交给 9 张不同形状（177–266 px 宽），不交给缩放。
// - 2× 超采样后 1:1 贴图 ⇒ 边缘是 3 px 斜坡（剖面 [131,163,231,255,…]）。
func spriteScale(p *particle) float64 {
	if p.kind == pkCloud || p.kind == pkFog {
		// 精灵数据**已经**是 2× 超采样后的尺寸 ⇒ 原生 1:1 对应 1/spriteSupersample。
		// 云再乘 0.5：原生 177–266 px = 屏宽 16–25%，单朵就是大块，
		// 8 朵叠起来读成 535×571 px 的巨块（实测 cur_cloudy_d1.png 最大连通块）。
		// 背景点缀要的是"小而多、有层次"，不是"大而实"。
		// 0.5× 原生 = 88–133 px = 屏宽 8–12%。
		return 0.5 / float64(spriteSupersample)
	}
	return p.sc * (0.55 + 0.45*p.depth) * p.scaleMul / float64(spriteSupersample)
}

// spreadAir 给云/雾挑一个**不与已有同类粒子重叠**的落位。
//
// 实测依据（TestCrowdSweep，40 个种子）：均匀随机落位下
// **平均最小边缘间隙 = −180.9 px，最差 −233.3 px**（负值 = 重叠）。
// 云宽只有 177–266 px ⇒ 重叠 70–100% = 融成一块
// ⇒ 低性能 x86 小主机反馈"顶部一堆云！画面整体被超大的云占据"。
// 顶部数量平均只有 1.98 朵 ⇒ 拥挤不是数量，**是重叠**。
//
// 做法：拒绝采样。在屏内随机取候选 y（x 对回收粒子固定在屏幕外，
// 只有 y 可调，取"与所有已有粒子的最小边缘间隙"最大的候选；
// 连续 12 次候选都达不到 minGap 就接受当前最优（不追求完美解）。
func (f *Field) spreadAir(p *particle, r *rand.Rand, allowX bool) {
	if p.kind != pkCloud && p.kind != pkFog {
		return
	}
	w, h := f.sw, f.sh
	if w <= 0 || h <= 0 {
		return
	}
	hw, hh := f.airHalfN(p)
	if 1-2*hh <= 0 {
		return
	}
	minGap := 0.10 * float64(w) // 108 px @1080 宽
	// **云的 y 采样区间 = 月亮行**（需求：云要在月亮上面，不是部分；
	// 云不得从月亮下面过去）。
	// spreadAir 是云落位的**真正决定者**：它按整屏 [hh, 1−hh] 采样 64 次挑间距最优者，
	// 把上游 clampAirY/placeAirEntry 设好的 y 又换成整屏值
	// （实测落位 0.269/0.328/0.441/0.634/0.736/0.865/0.904，月亮在 0.095）。
	// 云身半高 hh，月盘中心 0.095 半径 moonRcFrac ⇒ 中心落在
	// [0.095−rc−hh, 0.095+rc+hh] 才盖得到月盘。
	yLo, yHi := hh, 1-2*hh
	if p.kind == pkCloud {
		// 月盘半径 moonRcFrac 是**屏宽**单位 ⇒ y 轴口径 = ×W/H = 0.060×1080/1920 = 0.0338。
		rcY := moonRcFrac * float64(w) / float64(h)
		loY := 0.095 - rcY - hh
		hiY := 0.095 + rcY + hh
		if loY < hh {
			loY = hh
		}
		if hiY > 1-hh {
			hiY = 1 - hh
		}
		if hiY <= loY {
			hiY = loY + 0.01
		}
		// yHi 是**跨度**不是上界（采样写 `y := yLo + r*yHi`）——
		// 上一版写 `yHi = hiY - hh` 把上界错当跨度，范围又外扩了一截 hh。
		yLo, yHi = loY, hiY-loY
	}
	bestY, bestX, bestGap := p.y, p.x, math.Inf(-1)
	for tries := 0; tries < 64; tries++ {
		y := yLo + r.Float64()*yHi
		x := p.x
		if allowX && hw > 0 {
			x = hw + r.Float64()*(1-2*hw)
		}
		g := math.Inf(1)
		for i := range f.parts {
			q := &f.parts[i]
			if q.kind != p.kind || q == p {
				continue
			}
			qw, qh := f.airHalfN(q)
			dy := math.Abs(y-q.y) * float64(h)
			dx := math.Abs(x-q.x) * float64(w)
			// 边缘间隙按**各自轴向半尺寸**扣：
			// 上一版把半高 hh 同时用在水平方向 ⇒ 半宽 110 px > 半高 65 px
			// ⇒ 水平重叠被系统性低估 ⇒ 云横向叠成 535 px 巨块没被拦住。
			// 轴向扣减后与 TestCrowdSweep 的半宽口径一致。
			gap := math.Hypot(dx, dy) - (hw*float64(w)+qw*float64(w))/2 - (hh*float64(h)+qh*float64(h))/2
			if gap < g {
				g = gap
			}
		}
		if g > bestGap {
			bestGap, bestY, bestX = g, y, x
		}
		if g >= minGap {
			break
		}
	}
	p.y = bestY
	if allowX {
		p.x = bestX
	}
}

// CelestialCover 统计当前 active 的云与日/月盘（圆心 0.72,0.095，半径 moonRcFrac）的**真实相交**。
//
// 为什么要有这个函数（缺陷：拿"随机挑的几帧"判断遮挡——那几帧里
// 根本没有云压在日/月盘上 ⇒ 结论无效。
// 这个函数让每一张出图都自带证据：出图前先算出该帧云对盘面的覆盖率。
//
// 口径：圆盘上用 24×24 网格采样（只保留圆内点），统计被**任一**云精灵矩形覆盖的比例。
// 云矩形用真实绘制尺寸：贴图尺寸 × spriteScale（云 = 常量 0.5/spriteSupersample，与 p.sc 无关）。
func (f *Field) CelestialCover(w, h int) (clouds int, coverPct float64) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	sp := spritesOnce()
	sc := 0.5 / float64(spriteSupersample)
	rcx := moonRcFrac
	rcy := moonRcFrac * float64(w) / float64(h)
	cx, cy := 0.72, 0.095

	type rect struct{ x0, y0, x1, y1 float64 }
	var rects []rect
	for i := 0; i < f.ActiveCount(); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		sz := sp.cloudSz[0]
		if p.spr >= 0 && p.spr < len(sp.cloudSz) {
			sz = sp.cloudSz[p.spr]
		}
		hw := float64(sz.X) * sc / 2 / float64(w)
		hh := float64(sz.Y) * sc / 2 / float64(h)
		if p.x+hw < cx-rcx || p.x-hw > cx+rcx || p.y+hh < cy-rcy || p.y-hh > cy+rcy {
			continue
		}
		rects = append(rects, rect{p.x - hw, p.y - hh, p.x + hw, p.y + hh})
	}
	if len(rects) == 0 {
		return 0, 0
	}
	const N = 24
	total := 0
	covered := 0
	for iy := 0; iy < N; iy++ {
		for ix := 0; ix < N; ix++ {
			// 采样点落在圆盘内才算数
			dx := (float64(ix)+0.5)/N*2 - 1
			dy := (float64(iy)+0.5)/N*2 - 1
			if dx*dx+dy*dy > 1 {
				continue
			}
			px := cx + dx*rcx
			py := cy + dy*rcy
			total++
			for _, r := range rects {
				if px >= r.x0 && px <= r.x1 && py >= r.y0 && py <= r.y1 {
					covered++
					break
				}
			}
		}
	}
	if total == 0 {
		return len(rects), 0
	}
	return len(rects), 100 * float64(covered) / float64(total)
}

// 云横向间距控制器参数。
const (
	cloudXLo     = -0.35 // 与 recycleXLo 同口径：云的活动窗口左界
	cloudXHi     = 1.35  // 右界（云在 x>1.3 处回绕到 -0.3）
	cloudCoupleK = 0.05  // 间距误差 → 速度修正的增益（归一化单位）
	cloudVxMin   = 0.004 // 速度钳位下界
	cloudVxMax   = 0.030 // 速度钳位上界
)

// coupleClouds 按"前后空隙"微调每朵云的横向速度，让云自己散开、自己填缝。
//
// 为什么需要它（缺陷：云的横向分布不均，容易出现某一小段时间屏幕中
// 大部分是空白或某一段云大量集中出现：8 朵云的基准速度是 0.006–0.028
// （**4.7 倍差**），快的会追上慢的 ⇒ 一边堆成团、另一边空出来，两个症状同时出现。
// 落位阶段本来有最小间隙保证（spreadAir），但它只在**生成时**生效，漂移中不复查。
//
// 规则（交通流式平滑）：
//
//	vx_i = vxBase_i + K · (gapAhead_i − gapBehind_i) / target
//
// 前面挤、后面空 → 该项为负 → 这朵减速 → 前面的把它甩开、后面的追上来填缝 ⇒ 两侧同时收敛。
// 前面空、后面挤 → 该项为正 → 加速离开拥挤侧、钻进空侧。
// 速度只在 [vxBase·附近] 的钳位区间内动，基准速度分布（视差）整体保留。
//
// 入屏位置**不动**：云仍然从屏幕外左侧 x=-0.3 进场
// （需求：云应该在外面慢慢进来，而不是在眼前突然出现），
// 均匀性只靠速度调，绝不靠把云瞬移到屏幕中间。
func (f *Field) coupleClouds() {
	n := f.ActiveCount()
	idx := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if f.parts[i].kind == pkCloud && f.parts[i].vxBase > 0 {
			idx = append(idx, i)
		}
	}
	m := len(idx)
	if m < 3 {
		return
	}
	sort.Slice(idx, func(a, b int) bool { return f.parts[idx[a]].x < f.parts[idx[b]].x })
	width := cloudXHi - cloudXLo
	target := width / float64(m)
	adj := make([]float64, m)
	for k := 0; k < m; k++ {
		cur := f.parts[idx[k]].x
		ahead := f.parts[idx[(k+1)%m]].x - cur
		behind := cur - f.parts[idx[(k-1+m)%m]].x
		if k == m-1 {
			ahead += width // 首尾相接（回绕）
		}
		if k == 0 {
			behind += width
		}
		adj[k] = cloudCoupleK * (ahead - behind) / target
	}
	for k := 0; k < m; k++ {
		p := &f.parts[idx[k]]
		vx := p.vxBase + adj[k]
		if vx < cloudVxMin {
			vx = cloudVxMin
		}
		if vx > cloudVxMax {
			vx = cloudVxMax
		}
		p.vx = vx
	}
}

// CoverageProfile 把屏幕横向切成 cols 个竖条，返回每条被云（**云带 + 粒子云**）覆盖的比例。
//
// 为什么要有它（缺陷：云的横向分布不均，容易出现某一小段时间屏幕中
// 大部分是空白或某一段云大量集中出现：横向均匀性是个**随时间变化**的性质，
// 单帧图判断不了，必须逐秒把整条剖面打出来。
// 几何来源与绘制共用 bandPlace / spriteScale，保证测的就是屏幕上真实画出来的东西。
func (f *Field) CoverageProfile(w, h, cols int) []float64 {
	if w <= 0 || h <= 0 || cols <= 0 {
		return nil
	}
	type rect struct{ x0, y0, x1, y1 float64 }
	var rects []rect
	if f.cls == effect.Rain || f.cls == effect.Thunder || f.cls == effect.Cloudy || f.cls == effect.Fog {
		for i := 0; i < cloudBandN; i++ {
			bp, ok := f.bandPlace(i, f.t, w, h)
			if !ok {
				continue
			}
			rects = append(rects, rect{bp.x, bp.y, bp.x + bp.dw, bp.y + bp.dh})
		}
	}
	sc := 0.5 / float64(spriteSupersample)
	for i := 0; i < f.ActiveCount(); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		sz := f.sp.cloudSz[0]
		if p.spr >= 0 && p.spr < len(f.sp.cloudSz) {
			sz = f.sp.cloudSz[p.spr]
		}
		hw := float64(sz.X) * sc / 2
		hh := float64(sz.Y) * sc / 2
		cx := p.x * float64(w)
		cy := p.y * float64(h)
		rects = append(rects, rect{cx - hw, cy - hh, cx + hw, cy + hh})
	}
	const rows = 40
	out := make([]float64, cols)
	for c := 0; c < cols; c++ {
		x := (float64(c) + 0.5) * float64(w) / float64(cols)
		hit := 0
		for r := 0; r < rows; r++ {
			y := (float64(r) + 0.5) * float64(h) / float64(rows)
			for _, rc := range rects {
				if x >= rc.x0 && x <= rc.x1 && y >= rc.y0 && y <= rc.y1 {
					hit++
					break
				}
			}
		}
		out[c] = float64(hit) / float64(rows)
	}
	return out
}

// CelestialCoverAll 盘面上被**云带 + 粒子云**覆盖的比例（含"大云"口径）。
//
// 与 CelestialCover 的区别：那个只统计粒子，而"大云"指 drawCloudBands
// 画的那 4 条带（宽 285–524 px）。此前拿只含粒子的数字回答"大云遮住了日月"，
// 口径不对 ⇒ 补这个含云带的版本。
func (f *Field) CelestialCoverAll(w, h int) (bands int, coverPct float64) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	cx, cy := 0.72*float64(w), 0.095*float64(h)
	rcx := moonRcFrac * float64(w)
	rcy := moonRcFrac * float64(w) / float64(h)
	var rects [cloudBandN + 64][4]float64
	n := 0
	if f.cls == effect.Rain || f.cls == effect.Thunder || f.cls == effect.Cloudy || f.cls == effect.Fog {
		for i := 0; i < cloudBandN; i++ {
			bp, ok := f.bandPlace(i, f.t, w, h)
			if !ok {
				continue
			}
			// 只算与盘所在行有交集的带（y 区间相交才谈得上遮挡）
			if bp.y > cy+rcy || bp.y+bp.dh < cy-rcy {
				continue
			}
			rects[n] = [4]float64{bp.x, bp.y, bp.x + bp.dw, bp.y + bp.dh}
			n++
			bands++
		}
	}
	sc := 0.5 / float64(spriteSupersample)
	for i := 0; i < f.ActiveCount() && n < len(rects); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		sz := f.sp.cloudSz[0]
		if p.spr >= 0 && p.spr < len(f.sp.cloudSz) {
			sz = f.sp.cloudSz[p.spr]
		}
		hw := float64(sz.X) * sc / 2
		hh := float64(sz.Y) * sc / 2
		cxp, cyp := p.x*float64(w), p.y*float64(h)
		if cyp+hh < cy-rcy || cyp-hh > cy+rcy {
			continue
		}
		rects[n] = [4]float64{cxp - hw, cyp - hh, cxp + hw, cyp + hh}
		n++
	}
	const g = 24
	total, covered := 0, 0
	for a := 0; a < g; a++ {
		for b := 0; b < g; b++ {
			dx := (2*float64(a)/float64(g-1) - 1)
			dy := (2*float64(b)/float64(g-1) - 1)
			if dx*dx+dy*dy > 1 {
				continue
			}
			total++
			px, py := cx+dx*rcx, cy+dy*rcy
			for k := 0; k < n; k++ {
				r := rects[k]
				if px >= r[0] && px <= r[2] && py >= r[1] && py <= r[3] {
					covered++
					break
				}
			}
		}
	}
	if total == 0 {
		return bands, 0
	}
	return bands, 100 * float64(covered) / float64(total)
}

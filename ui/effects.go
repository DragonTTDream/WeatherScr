package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/DragonTTDream/WeatherScr/effect"
)

import (
	"github.com/DragonTTDream/WeatherScr/ui/kinetics"
	"github.com/DragonTTDream/WeatherScr/ui/metrics"
	"github.com/DragonTTDream/WeatherScr/ui/screenfit"
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
// 线性渐变，不再走贴图；该描边画法已在规格 1.3 里换成 rain1..7 贴图。
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
	flakeImg    []*image.NRGBA // 雪贴图的原始像素（自检取用：证明来自 readPNG 的解码结果）
	flakeIsArt  bool           // true = 雪是 embed 贴图；false = 退到 genFlake 程序晶形
	mote        []paint.ImageOp
	moteSz      []image.Point
	rain        []paint.ImageOp // 雨丝贴图 rain1..7（Kenney trace_01..07）
	rainSz      []image.Point
	bolt        paint.ImageOp
	boltSz      image.Point
	boltOps     []paint.ImageOp // 闪电贴图 bolt1..3（Kenney spark_01..03）
	boltSzOps   []image.Point
	boltIsArt   bool
	cloudShadow paint.ImageOp

	// 运行时着色的贴图副本缓存（按 (lift, 贴图下标) 惰性构建）。
	// 依据：规格 2"云精灵体色：晴档提白、雨/雷档压暗（运行时着色，不改贴图文件）"。
	tintMu  sync.Mutex
	tintMap map[tintKey]tintEntry
}

// tintKey = 副本缓存键（lift 取整，fog 区分云/雾两套贴图）。
type tintKey struct {
	lift int
	idx  int
	fog  bool
}

type tintEntry struct {
	op  paint.ImageOp
	sz  image.Point
	img *image.NRGBA // 副本本体像素（CPU 侧合成用：云影判据量的就是这张）
	ok  bool
}

// tintedSprite 返回把体色整体加 lift 级后的贴图副本（alpha 通道不动）。
//
// lift = 0 时直接回落到原贴图（不占内存）。副本 = 与 loadEmbedded 同一条加载链
// （readPNG 反预乘 → featherAlpha(1)），只多一步 RGB 平移 ⇒ 边缘口径与原图逐字一致。
// 逐张惰性构建并缓存：一个类只会用到它自己那 3–4 张，构建量 = 实际用过的 (lift, 张) 组合。
func (s *sprites) tintedSprite(lift float64, idx int, fog bool) (paint.ImageOp, image.Point, bool) {
	l := int(math.Round(lift))
	if l == 0 {
		if fog {
			if idx >= 0 && idx < len(s.fog) {
				return s.fog[idx], s.fogSz[idx], true
			}
			return paint.ImageOp{}, image.Point{}, false
		}
		if idx >= 0 && idx < len(s.cloud) {
			return s.cloud[idx], s.cloudSz[idx], true
		}
		return paint.ImageOp{}, image.Point{}, false
	}
	k := tintKey{lift: l, idx: idx, fog: fog}
	s.tintMu.Lock()
	defer s.tintMu.Unlock()
	if s.tintMap == nil {
		s.tintMap = map[tintKey]tintEntry{}
	}
	if e, ok := s.tintMap[k]; ok {
		return e.op, e.sz, e.ok
	}
	name := "cloud" + itoa(idx+1) + ".png"
	if fog {
		name = "fog" + itoa(idx+1) + ".png"
	}
	src := spriteImg(name)
	if src == nil {
		s.tintMap[k] = tintEntry{}
		return paint.ImageOp{}, image.Point{}, false
	}
	n := featherAlpha(toNRGBA(src), 1) // 与 loadEmbedded 同口径
	for i := 0; i < len(n.Pix); i += 4 {
		n.Pix[i+0] = addClamp8(n.Pix[i+0], l)
		n.Pix[i+1] = addClamp8(n.Pix[i+1], l)
		n.Pix[i+2] = addClamp8(n.Pix[i+2], l)
		// alpha 不动：体色只改明度，透明度口径逐字保持
	}
	e := tintEntry{op: paint.NewImageOp(n), sz: n.Bounds().Size(), img: n, ok: true}
	s.tintMap[k] = e
	return e.op, e.sz, true
}

// tintedImg 取 tintedSprite 那份副本的**本体像素**（同一条缓存，不另建一份）。
//
// 用途：云影判据要在 CPU 上把影合成到天空纹理上量亮度，必须用 GPU 真正上传的那张
// （lift=-255 ⇒ RGB 全 0、alpha 逐字不动），不能在探针里重抄一遍羽化/着色。
func (s *sprites) tintedImg(lift float64, idx int, fog bool) (*image.NRGBA, bool) {
	l := int(math.Round(lift))
	if l == 0 {
		name := "cloud" + itoa(idx+1) + ".png"
		if fog {
			name = "fog" + itoa(idx+1) + ".png"
		}
		img := spriteImg(name)
		return img, img != nil
	}
	_, _, ok := s.tintedSprite(lift, idx, fog)
	if !ok {
		return nil, false
	}
	s.tintMu.Lock()
	defer s.tintMu.Unlock()
	e, ok := s.tintMap[tintKey{lift: l, idx: idx, fog: fog}]
	if !ok {
		return nil, false
	}
	return e.img, e.img != nil
}

// addClamp8 = v+lift 收进 [0,255]。
func addClamp8(v uint8, lift int) uint8 {
	p := int(v) + lift
	if p > 255 {
		return 255
	}
	if p < 0 {
		return 0
	}
	return uint8(p)
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

	// 雪 = embed 贴图（规格 1.2：禁程序化画雪晶）。
	// 实测六向晶在屏上尺度读成雨丝（屏上 12–26 px 的六向细臂 = 一根根短斜线），
	// 换成 Kenney circle_05 的柔焦发光圆点后才是"点状雪"。
	// genFlake 只保留为**贴图缺失时的退路**（出厂路径走不到它）。
	if ops, szs, imgs, ok := loadSpriteSet("snow", 3); ok {
		s.flake, s.flakeSz, s.flakeImg = ops, szs, imgs
		s.flakeIsArt = true
	} else {
		for _, n := range []int{18, 26, 36} {
			s.flakeSz = append(s.flakeSz, image.Pt(n, n))
			s.flake = append(s.flake, paint.NewImageOp(genFlake(image.Pt(n, n))))
		}
	}

	// 雨 = embed 贴图（规格 1.3：竖向柔化长条 trace_01..07，按景深挑档）。
	if ops, szs, _, ok := loadSpriteSet("rain", 7); ok {
		s.rain, s.rainSz = ops, szs
	}

	// 闪电 = embed 贴图（规格 1.5：spark_01..03 的白热闪电枝）。
	// genBolt 的折线程序闪电保留为退路（贴图缺失时 s.bolt 仍是它）。
	if ops, szs, _, ok := loadSpriteSet("bolt", 3); ok {
		s.boltOps, s.boltSzOps, s.boltIsArt = ops, szs, true
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
// 缺透明度"。雨线也已在规格 1.3 里从描边路径换成贴图路径（rain1..7），
// 描边画法与它的三档长度、线宽函数一并删除（长度口径见 rainStreakCapPx）。

// rainStreakCapPx 每张雨丝贴图**短轴（屏上宽）**的上限（px，1080 屏口径）。
//
// 长轴 = 运动模糊长度 L = vy × 快门时间（既有口径，近处长、远处短），
// 短轴按贴图宽高比等比放大 ⇒ 短轴 = L × 48 / 贴图高。
// 实测七张贴图在包围盒中线上 alpha≥64 的列数占包围盒宽的比例（软带占比，
// 与 bin/prepare_particles.py 同一份源图、同一裁包围盒口径）：
//
//	rain1 0.081  rain2 0.075  rain3 0.086  rain4 0.213
//	rain5 0.140  rain6 0.366  rain7 0.443
//
// 同一套等比放大下，rain7 在近档（L=160 px）的短轴 44 px、软带 19 px，
// 读成一条糊带而不是雨丝（离屏判据 TestRainBottomEdgeNoSplash 的口径：
// 单行实心段 ≥6 px 判为带）。软带占比相差 5.9 倍 ⇒ 上限必须逐张给。
// 取值 = 软带目标宽 2.5 px ÷ 占比，收进 [6, 40]（下限保可见、上限防糊带）：
//
//	rain1 31  rain2 33  rain3 29  rain4 12  rain5 18  rain6 7  rain7 6
//
// 实测（同一判据、同一固定种子）：短轴上限 40/40/40/23/36/14/11 时最长实心段 9 px，
// 换成上表后 5 px（判据 <6），带内粒子层变化像素 95 px（雨丝仍画到下边缘）。
var rainStreakCapPx = []float32{31, 33, 29, 12, 18, 7, 6}

// rainStreakCapFor 第 spr 张雨丝贴图的短轴上限（贴图组缺失时退到最宽一档）。
func rainStreakCapFor(spr int) float32 {
	if spr < 0 {
		spr = 0
	}
	if spr >= len(rainStreakCapPx) {
		spr = len(rainStreakCapPx) - 1
	}
	return rainStreakCapPx[spr]
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

// 雨丝的快门时间与风（参照 github.com/0magnet/termanim/rain）。
// 尾/头 alpha 比例与线色常量随描边画法一起删除（贴图路径由贴图自身的 alpha 形状给）。
const (
	// rainExposure 快门时间（秒）。线长 = 速度 × 快门时间 = 运动模糊。
	// 参照取 1/20 秒：足够让相邻帧的雨线重叠，下落看起来连续而非频闪。
	// 它是**时间**不是帧数 —— 绑帧率会在掉帧时把每条线拉长，
	// 而那正是雨最不该 visibly 变化的时刻。
	//
	// 现值由规格 1.3 的长度口径反解（1080x1920 屏）：
	//
	//	L = vy × 屏高 × 快门，vy = 0.6 + 1.35·depth ∈ [0.6, 1.95] 屏高/秒
	//	长档上限 160 px ⇒ 快门 = 160 ÷ (1.95 × 1920) = 0.04273
	//	短档下限随之 = 0.6 × 1920 × 0.04273 = 49.2 px ≥ 40 px
	//
	// 旧值 0.05 的同一式给出 L ∈ [57.6, 187.2] px ⇒ 长档越出 160 px 上限。
	// 取"改快门"而不是"把 L 钳到 160"：钳位会让 depth>0.79 的那 21% 雨滴
	// 全挤在 160 px 一个点上，长度分布出现尖峰；改快门保持逐 depth 单调。
	rainExposure = 0.04273
	// rainSlantBase 风：水平像素 / 垂直像素。参照取 0.28。
	// 垂直的雨像测试卡；一点倾斜才像天气。
	rainSlantBase = 0.28
)

// windJitterAmp 每粒子倾角抖动幅度（rad），由粒子索引生成 ⇒ 跨帧稳定。
// 雨丝的倾角主体来自粒子自己的速度（atan2(vx, vy)），这里只加逐粒子的固定抖动。
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
// **粒子云 sc = 屏宽倍率（2026-10-20 改大）**：净倍率分母 = bandNetDiv，
// 与云带同一个分母 ⇒ 换算逐字可比：
//
//	粒子云屏宽 = 源图 512 px × p.sc × scaleMul / bandNetDiv
//	p.sc = 1.5 + 0.8·U ∈ [1.5, 2.3] ⇒ 512×1.5 = 768 px … 512×2.3 = 1178 px
//	云带屏宽 = 源图 512 px × (1.5 + 0.8·frac) / bandNetDiv = 768 … 1178 px
//
// 两式同量级、逐档相同 ⇒ 粒子云与云带同量级（依据：稳态下 8 朵 256 px 的
// 圆团与 4 条 768–1178 px 的带同时在场，256 px 那一档读成"一小坨一小坨"）。
// 朵数按面积反比同步下调（单朵面积 ≈ 现 9 倍），见 counts。
//
// 历史口径（保留记录）：云 sc = 1.1 + 2.4·U → 上限 3.5；雾 sc = 1.6 + 2.6·U → 上限 4.2
// （effects.go 原 spawn 的字面量，根因分析引用的正是这两个上限）。
//
// 性能修订（实测）：填充率与 CPU 线性，1 个整屏填充 @15 fps = 4.4% CPU。
// Kenney 精灵实测 190×127…266×138，`sc` 上限 3.5 再乘 `scaleMul` 上限 1.25
// ⇒ 单朵 ≈ 875×546 = 0.48 Mpx，16 朵 = 7.6 Mpx = **3.7 个整屏当量 = 15.6% CPU**，
// 是全场最大单项开销。按 termanim 的"深度驱动尺寸"原则收面积：
// 数量减半 + sc 上限 3.5→2.2 ⇒ 填充当量 3.7 → ≈0.9。
const (
	cloudScBase = 1.5 // 粒子云改大：1.0 → 1.5（屏宽 512×1.5 = 768 px = 云带最窄一档）
	// 1.2 → 0.5：低性能 x86 小主机反馈"很夸张 很糊"。
	// 根因是**放大倍率**不是羽化：sc 上限 2.2 × scaleMul 上限 1.25 = 2.75 倍放大，
	// 把 Kenney 云的硬边拉成糊团（同一素材在原生 1:1 下视觉模型判"锐利清晰的卡通硬边"，
	// 素材图判读（Kenney cloud1..9 实测）。现上限 1.5，配合 2× 超采样 ⇒ GPU 侧近似 1:1。
	cloudScSpan = 0.8 // 粒子云改大：0.25 → 0.8（sc 上限 2.3 ⇒ 屏宽 512×2.3 = 1178 px = 云带最宽一档）
	fogScBase   = 1.3
	fogScSpan   = 1.5 // 上限 2.8（此前 3.2 时"很夸张 很糊"）
	// fogAlphaMul 雾浓度倍率：**运行时参数，不烘进贴图**。
	// 需求：雾比初版淡一档（对比图选定，倍率 0.6）。
	// 上一版把 0.6 烘进 fog*.png 的 alpha（均值 93 → 55），
	// 结果出厂贴图与选定的那张效果图不是同一份东西 ⇒ 贴图保持效果图原样，
	// 淡浓在这里调，出图与出厂永远可比。
	fogAlphaMul = 0.6
)

// ---- 逐类云形态口径（2026-10-20 云规则改动，规格 2 的表逐字落地）----
//
// 屏宽 = 屏上像素宽（1080 px 屏口径，与云带同一 px 口径）。粒子云屏宽 =
// 源图 512 px × p.sc × scaleMul ÷ bandNetDiv ⇒ p.sc 由**目标屏宽反解**
// （sc 是纯倍率，屏宽是口径本身，抖动 scaleMul 只改形状不改屏宽）。
//
//	类                屏宽 px     体色（运行时着色）       alpha        纵向位置
//	晴 clear          300–600     白（提亮 +140）         0.85–0.95    视差：小=远=靠上
//	多云见日 partly   300–700     白偏亮（+115）          0.80–0.92    视差
//	多云 cloudy      500–900     中灰（+45）             0.75–0.90    视差：大=近=靠下
//	阴 overcast      768–1178    中暗灰（+10）           0.85–0.95    压顶
//	雨/雷 rain/thunder 768–1178  暗灰（−32 ⇒ 降 25–40）  0.88–0.96    压顶
//	雪 snow          均匀灰幕（雾精灵 1024 px 源，lift −70） 现 fogAlphaMul  满屏均匀不堆团
//	尘 dust          现口径                             现口径        现口径
//
// 检索依据（出处逐条，规格 0 要求写进注释）：
//
//	雨/雷 = 又厚又黑的云："大雨来临前，天空通常会变得阴沉沉，云也越来越黑"
//	"造成降水的云最主要有两种，一种叫积雨云，一种叫雨层云"
//	  https://www.weather.com.cn/hainan/zyqxxx/05/2869945.shtml
//	晴天云 = 白棉团、边缘清楚："积云…通常具有如同棉花的形状…颜色为白色到浅灰色，
//	常见于大晴天"
//	  https://xinwen.bjd.com.cn/content/s64a8e0f1e4b03d11a64d1566.html
//	视差口径（远=上=小=慢、近=下=大=快）："background images move past the camera
//	more slowly than foreground"
//	  https://wikiwand.com/en/Parallax_scrolling
//	云速随高度（薄高云快、厚低云慢）："higher clouds typically moving much faster
//	than those near the surface"/"high thin cirrus streaks fly fast while low
//	stratus creeps along"
//	  https://popsci.blog/fast-moving-clouds-meaning
//	  https://calendar-holidays.com/how-fast-do-clouds-travel
//
// 体色 = **运行时着色**（在贴图 RGB 上加一个常量，ui/embed/*.png 逐字节不动）：
// 实测 9 张云精灵实心像素（alpha>200）反预乘均色 = (92,93,95)…(114,115,119)，
// 现档云体就是这层深灰 ⇒ 晴/多云见日的云读成"与雾天没区别"的淡灰大幕。
// lift 逐档：晴 +140（→ 232…255 白）、多云见日 +115（→ 207…234 白偏亮）、
// 多云 +45（→ 137…164 中灰）、阴 +10（→ 102…129 中暗灰）、
// 雨/雷 −32（→ 60…87，比现档降 32 级，落在规格要求的 25–40 区间内）。
type cloudForm struct {
	wLo, wHi float64 // 屏宽区间（px，1080 屏口径）
	lift     float64 // 体色提亮/压暗量（加到贴图 RGB，运行时着色）
	aLo, aHi float64 // alpha 区间
	pressTop bool    // true = 压顶（云中心 y 落在画面上缘带）
	idx      []int   // 按类挑选的云精灵下标（0 基）
}

// 云精灵按类挑选（按 9 张贴图的**实测内容**分配，不是按文件名顺序）：
//
//	实测口径 = alpha>40 的实心占比 fill、实心区平均列高 meanColH、bbox、最大 alpha
//	cloud1 378×255 fill .639 meanColH 242 maxA  95  → 紧凑厚团（边缘软）
//	cloud2 259×214 fill .461 meanColH 120 maxA  61  → 最小的小棉团
//	cloud3 459×321 fill .655 meanColH 301 maxA 252  → 最厚最实（积云）
//	cloud4 356×245 fill .669 meanColH 238 maxA  81  → 紧凑厚团
//	cloud5 468×468 fill .286 meanColH 134 maxA 229  → 大块散点积云
//	cloud6 498×420 fill .403 meanColH 201 maxA 255  → 顶厚（上/下质量比 2.18）
//	cloud7 425×288 fill .075 meanColH  32 maxA  77  → 细条薄卷云（上/下比 5.11）
//	cloud8 466×449 fill .193 meanColH  90 maxA 137  → 散点薄云
//	cloud9 507×499 fill .320 meanColH 162 maxA 244  → 大朵散积云
//
// ⇒ 薄小那几张 = cloud7/2/8（fill ≤ .193、meanColH ≤ 120）；
//
//	中档 = cloud1/4/9；厚大那几张（积云系）= cloud3/6/5（fill ≥ .286 且 maxA ≥ 229）。
//
// cloudIdxAllowed = **R3 贴图白名单**（B3，evidence/p8-numbers-20261023.md:26-33）：
// 可用 = cloud1 / cloud2 / cloud4 ⇒ 0 基索引 **{0,1,3}**；
// 需求逐字"cloud3-5-6-7-8-9不使用" ⇒ 原三档表 cloudIdxThin{6,1,7} / cloudIdxMid{0,3,8} /
// cloudIdxThick{2,5,4} 合并为单一表，三处 cloudForm.idx 全部指它（上面那段 9 张实测内容表保留做停用依据）。
var cloudIdxAllowed = []int{0, 1, 3}

// kineticsClassOf 把 effect.Class 映射到运动侧档名（kinetics 不吃 effect 包 ⇒ 映射写在消费点）。
// Snow/Dust 无粒子云 ⇒ 走 RainThunder 占位口径（现口径的占位值就是 768–1178，见 cloudFormOf 头注）。
func kineticsClassOf(cls effect.Class) kinetics.Class {
	switch cls {
	case effect.Clear:
		return kinetics.ClassClear
	case effect.PartlyCloudy:
		return kinetics.ClassPartlyCloudy
	case effect.Cloudy:
		return kinetics.ClassCloudy
	case effect.Overcast:
		return kinetics.ClassOvercast
	case effect.Rain, effect.Thunder:
		return kinetics.ClassRainThunder
	case effect.Fog:
		return kinetics.ClassFogCloud
	}
	return kinetics.ClassRainThunder
}

// cloudFormOf 返回该天气类的云形态口径。雪/雾/尘没有粒子云（雪走雾精灵均匀灰幕、
// 雾走雾精灵、尘走 mote），返回现口径占位值（wLo/wHi = 云带最宽档，不参与生成）。
func cloudFormOf(cls effect.Class) cloudForm {
	// R4 逐类**最终显示宽度**（B3，evidence/p8-numbers-20261023.md:11-15）：
	// lo = ceil(0.444·W) ⇒ @1080 = 480 px（小于下限的云在生成侧直接不出）；
	// hi = (W − gap)/2   ⇒ @1080 = 529 px（同带 2 朵 + 21 px 间隙恰好铺满 1080）。
	// 口径宽取 B2 的 screenfit 基准（竖屏目标 1080）；Rain/Thunder 档 768–1178 逐字不动（kinetics 内豁免）。
	loPx, hiPx := kinetics.WidthFor(kineticsClassOf(cls), screenfit.Baseline().W)
	lo, hi := float64(loPx), float64(hiPx)
	switch cls {
	case effect.Clear:
		// B3：原 300–420（"薄快厚慢"排序用的薄档）整档低于 R4 下限 ⇒ 抬到 480–529。
		// 速度排序不再由档间宽度差承担（各档同带），由 kinetics 的档内速度式 + 进场排队承担。
		return cloudForm{wLo: lo, wHi: hi, lift: 140, aLo: 0.85, aHi: 0.95, pressTop: false, idx: cloudIdxAllowed}
	case effect.PartlyCloudy:
		return cloudForm{wLo: lo, wHi: hi, lift: 115, aLo: 0.80, aHi: 0.92, pressTop: false, idx: cloudIdxAllowed}
	case effect.Cloudy:
		return cloudForm{wLo: lo, wHi: hi, lift: 45, aLo: 0.75, aHi: 0.90, pressTop: false, idx: cloudIdxAllowed}
	case effect.Overcast:
		return cloudForm{wLo: lo, wHi: hi, lift: 10, aLo: 0.85, aHi: 0.95, pressTop: true, idx: cloudIdxAllowed}
	case effect.Rain, effect.Thunder:
		return cloudForm{wLo: lo, wHi: hi, lift: -32, aLo: 0.88, aHi: 0.96, pressTop: true, idx: cloudIdxAllowed}
	case effect.Fog:
		// B3：云档 480–529；雾团封顶 clampFogWidthPx 不动（不动清单）。
		return cloudForm{wLo: lo, wHi: hi, lift: 0, aLo: 0.92, aHi: 1.0, pressTop: false, idx: cloudIdxAllowed}
	}
	return cloudForm{wLo: lo, wHi: hi, lift: 0, aLo: 0.92, aHi: 1.0, pressTop: false, idx: cloudIdxAllowed}
}

// ---- C1 云速按薄厚（规格 1 第 1 条，公式逐字）----
//
//	vx = vxThin − (vxThin − vxThick)·min(1, dw/dwRef)
//
// dw = 该朵云的屏宽占比（屏宽 px ÷ 屏宽 px，与 spreadAir 的 hw 同口径）；
// dwRef = 云最大屏宽 = 1178 px ÷ 1080 px = 1.0915（现口径最宽一档，见 cloudFormOf 表）。
// vxThin = 0.014 = 现云速上界（spawn 旧式 0.003 + 0.011·U 在 U=1 处的值）；
// vxThick = 0.003 = 现云速下界（同一式在 U=0 处的值）⇒ 速度区间与现口径逐字同宽，
// 只是把"随机抽速度"换成"按屏宽定速度"。
//
// 换算（1080 px 屏、speedScale=1、15 fps）：
//
//	薄小云 300 px → dw=0.2778 → vx=0.01121 → 12.1 px/s = 0.81 px/帧
//	中档  600 px → dw=0.5556 → vx=0.00839 →  9.1 px/s
//	厚大云 1178 px → dw=1.0915 → vx=0.00300 →  3.2 px/s
//
// 依据：薄高云快、厚低云慢（https://popsci.blog/fast-moving-clouds-meaning 、
// https://calendar-holidays.com/how-fast-do-clouds-travel ）；
// 视差近快远慢（https://wikiwand.com/en/Parallax_scrolling ）。
const (
	// B3/R1（evidence/p8-numbers-20261023.md:17-23）：内插端点 = kinetics 的速度档，
	// 快端逐字冻结在现云速上界 0.0140（"最快的云不能比现在更快"），慢端 0.0022
	// （拉开速度差只靠**调慢**，不调快；三档慢速 0.0085/0.0040/0.0022 见 kinetics 常量）。
	cloudVxThin  = kinetics.VxTier0
	cloudVxThick = kinetics.VxTier3
)

// cloudVxByThickness 按屏宽占比取速度（薄快厚慢，单调递减）。
func cloudVxByThickness(dw float64) float64 {
	k := dw / screenfit.RefWidth
	if k > 1 {
		k = 1
	}
	if k < 0 {
		k = 0
	}
	return cloudVxThin - (cloudVxThin-cloudVxThick)*k
}

// ---- C3 雪花屏上尺寸（规格 3）----
//
// 实测（取证 35.1，/tmp/fxdiag-095505 FX_DEBUG 粒子清单）：pkFlake 源 18/26/36 px，
// 屏上 dw = 2.3–12.8 px、alpha 0.42–0.93 ⇒ 在 1080×1920 上就是 2–13 px 的白点，
// 压在亮雾底上 ⇒ "雪天没有飘雪"成立，根因 = 屏上尺寸太小。
//
// 改法：生成时**按目标屏宽反解 sc**（屏上 dw 逐朵 = 目标 px，逐字落在 12–26）：
//
//	dw = 源图宽 × sc × (0.55+0.45·depth) × scaleMul ÷ spriteNetDiv
//	⇒ sc = 目标px × spriteNetDiv ÷ (源图宽 × (0.55+0.45·depth) × scaleMul)
//
// 数量与下落速度口径不动（规格 3 逐字）。
const (
	flakePxLo     = 12.0
	flakePxHi     = 26.0
	flakeAlphaMin = 0.60 // 亮雾底上可见的 alpha 下限（Draw 里逐帧兜底）
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

// 压顶档（阴 / 雨 / 雷，判据 = cloudFormOf().pressTop）的带纵向口径：
// 带左上角从屏上缘外 0.12 屏高起、逐带只下移 0.10（现口径 0.02 / 0.24 的对比值）。
// 取负起点的理由：压顶要的是**带中心**贴住画面上缘，而带自身半高 0.10–0.25 屏高，
// 起点取 0 时带中心最低只到 0.10 屏高、最高到 0.41 屏高 ⇒ 四条带里三条压在中部。
const (
	cloudBandPressTop    = -0.12
	cloudBandPressSpread = 0.10
)

// Field 是一类天气的粒子场。
type Field struct {
	cls   effect.Class
	isDay bool
	sp    *sprites
	parts []particle
	rng   *rand.Rand
	t     float64 // 连续累计时间（秒）—— 所有动画的唯一自变量
	flash float64
	// ---- 闪电闪现的时间口径（规格 1.5）----
	//
	// 旧口径是**帧数**：f.next = 40 + rng(160) 帧，按 15 fps 折算 = 2.7–13.3 秒，
	// 帧率一变间隔就跟着变（掉帧到 8 fps 时间隔直接拉长 87%）。
	// 现全部走**秒**：间隔 6–12 秒随机，单次闪现 0.15–0.30 秒。
	boltOn    bool    // 当前这一刻是否在闪现中
	boltStart float64 // 本次闪现的起始 t（秒）
	boltDur   float64 // 本次闪现的时长（秒，落在 0.15–0.30）
	boltNext  float64 // 距下一次闪现还要等的秒数（按码分级，见 boltGapForCode）
	boltCount int     // 已触发的闪现次数（自检计数）
	boltDrawn int     // drawBolt 真正画出的次数（自检：非雷暴类必须为 0）
	boltIdx   int     // 本次闪现用第几张闪电贴图
	boltX     float64 // 本次闪现的落位（归一化屏宽）= 局域闪光的锚点
	boltY     float64 // 本次闪现的落位（归一化屏高）= 局域闪光的锚点
	boltLog   []float64
	boltDurs  []float64
	// wxCode = 该场对应的 WMO 码（调用方每帧 SetWeatherCode 传进来）。
	// 闪现间隔按码分级（95/96/97/99 四档，其余码不闪），见 boltGapForCode。
	wxCode int
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
	// ---- B3 进场排队（R1"决不能同出同进"，evidence/p8-numbers-20261023.md:24）----
	//
	// 速度差只解决追赶，同出同进由进场排队解决 ⇒ 进场位发射前过 kinetics.EntryOK：
	// 同帧已有上一条进场 ⇒ |Δx| 必须 ≥ 上一条半宽，不合格就把这朵**保持停在屏外**（parked），
	// 下一帧再试（不发射）。entryLog 是进场流水，探针（ui/kinetics_rules_test.go）直接读它判样本数。
	entryHas      bool            // 是否已有过进场记录
	entryLastStep int64           // 上一条进场的固定步号（"同帧"判据 = 步号相同）
	entryLastX    float64         // 上一条进场位中心 x（px 口径，与 kinetics.EntryOK 同单位）
	entryLastHalf float64         // 上一条云的半宽（px）
	entryLog      []cloudEntryRec // 进场流水（只增，量级 = 每朵云一次进场 ≈ 数十次/小时）
	parkedCloud   map[int]bool    // 被闸门挡住、保持停在屏外的云索引（parts 下标）
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
	active int // 当前活跃粒子数 = Draw 绘制 parts[:active]
	// fpsAccum = 当前测量窗累计 dt。**按 time.Duration 整纳秒累加**：浮点秒累加会把
	// 10 × 0.2 s 加到 1.9999999999999998 ⇒ "满 2 秒"那一轮要到第 11 帧才结算
	// （实测 5 fps 连喂 30 帧只结算 2 轮）⇒ 削减轮数比"每满 2 秒一轮"少一轮。
	// 口径不动的部分：窗口长度 2 秒、吃实测 dt、每满一轮结算一次（A2 闸门根因）。
	fpsAccum time.Duration // 当前测量窗累计 dt（整纳秒）
	fpsCount int           // 当前测量窗帧数
	fps      float64       // 最近一次窗口实测 fps
	// ---- 批次 A2 / C1：位移改固定步长 ----
	//
	// 旧口径：每帧位移 = vx·实测dt·speedScale ⇒ 帧时长 61↔451 ms 的抖动
	// 直接变成位移忽大忽小（"一顿一顿"）。新口径：位移只吃 fixedStep，
	// 实测 dt 只进累加器 acc 并按整步消费，单帧最多补 fixedStepCatchup 步。
	acc time.Duration // 帧累加器：吃实测 dt，按 fixedStep 整步扣
	// steps = 已推进的固定步数。t 由步数导出（t = steps × fixedStep），
	// 云带横移因此只由步数驱动，不吃墙钟（C1 第 3 条）。
	steps int64
	// frameIdx = 建场以来的帧计数（C3 启动期拆开：BootCap 的输入）。
	frameIdx int
	// recoverStreak = 连续 fps>18 的轮数（C2 恢复滞回：满 3 轮才开始恢复，
	// 期间任何一轮 fps≤18 把计数清零）。
	recoverStreak int
	// ---- P1 云/雾重叠探针（spec/p1-probe.md，只测量不改行为）----
	//
	// probeCount = 绘制计数器的开关，**只在探针构造路径置位**
	// （ui/overlap_probe.go 的 NewFieldProbeCounted / SetProbeCounting）。
	// 普通 NewField / NewFieldEntering 路径恒为 false ⇒ 绘制路径里的
	// `if f.probeCount { f.draws++ }` 分支恒假：零行为变化，开销 = 一次布尔判。
	//
	// draws = 累计"贴图提交"次数：一个 `精灵.Add(gtx.Ops)` + `paint.PaintOp{}.Add`
	// 组合算 1 次（计数点逐处见 out/notes-p1-probe.md 的 文件:行 清单）。
	probeCount bool
	draws      int
}

// fixedStep 固定步长（C1 第 1 条）：位移的唯一步长，基准写死 1/60 s，
// **不跟随目标帧率**（目标帧率只经 speedScale 生效，见 SetSpeedScale 口径）。
const fixedStep = time.Second / 60

// fixedStepCatchup 追赶上限（C1 第 2 条）：单帧最多补 3 步 ⇒ 长帧不爆冲，
// 多余的累加丢弃并把 acc 置 0。
const fixedStepCatchup = 3

// recoverArmRounds 恢复滞回轮数（C2 第 3 条）：连续 3 轮 fps>18 才开始恢复。
const recoverArmRounds = 3

// fpsWindow fps 自适应测量窗长度：每满 2 秒结算一轮（削减/恢复各算一轮）。
// 整 Duration 比较，不走浮点秒 ⇒ "每满 2 秒一轮"对任何 dt 都精确成立。
const fpsWindow = 2 * time.Second

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
	// P5b 测量件（spec/p5b-dbg.md 第 1 条）：WS_DBG_FRAMES=1 时生产构造路径
	// 也开启绘制计数。走的是 P1 已有的 probeCount/draws 通路（ui/overlap_probe.go
	// 的 NewFieldProbeCounted 同一开关字段），**没有第二条计数通路**。
	// 未设该环境变量 ⇒ WSDbgFrames()=false ⇒ probeCount 保持 false ⇒
	// 绘制路径的自增分支恒假 ⇒ 与 P5b 之前的产物逐字一致。
	f.probeCount = metrics.WSDbgFrames()
	n := counts(cls, isDay)
	f.parts = make([]particle, 0, n)
	for i := 0; i < n; i++ {
		f.parts = append(f.parts, f.spawn(true, i))
	}
	f.active = n // 未削减 = 全部
	f.fade = 1   // 默认全显示（零值 0 会让整片场不可见）
	if cls == effect.Thunder {
		// 建场时码还没传进来（SetWeatherCode 每帧传），间隔按码分级 ⇒ 先挂在场外，
		// 等码到位由 SetWeatherCode 抽第一次的等待时间。开局不闪。
		f.boltNext = math.Inf(1)
	}
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

// counts 每类粒子数（B3/R4：粒子云朵数按 kinetics.BandCapacity 封顶 2 朵）。
//
// 封顶只作用在"粒子云所在档"（Cloudy / PartlyCloudy / Overcast / Fog 的云半档 / Clear 白天）：
// Clear 夜间 110 是星、Rain/Thunder 的 220/300 是雨丝、Snow 的 260 是雪片 + 3 团灰幕、
// Dust 的 320 是尘 ⇒ 那些档的粒子不是云，朵数上限对它们无意义（countsRaw 原值不动）。
// 现口径里真正被封顶的只有 Clear 白天 4→2 与 Cloudy 3→2（PartlyCloudy/Overcast/Fog 本来就 ≤2）。
func counts(cls effect.Class, isDay bool) int {
	n := countsRaw(cls, isDay)
	if !cloudTierClass(cls, isDay) {
		return n
	}
	nMax, _, _ := kinetics.BandCapacity(screenfit.Baseline().W)
	if n > nMax {
		return nMax
	}
	return n
}

// cloudTierClass 该档的粒子数是否就是粒子云朵数（= 要不要按 BandCapacity 封顶）。
func cloudTierClass(cls effect.Class, isDay bool) bool {
	switch cls {
	case effect.Cloudy, effect.PartlyCloudy, effect.Overcast, effect.Fog:
		return true
	case effect.Clear:
		return isDay // 夜间 110 是星不是云（countsRaw 注释逐字）
	}
	return false
}

// countsRaw 每类粒子数。S1 实测 600 粒子 58.9 fps / 34.9 MB，
// S4 实测 420 粒子 15 fps 时总 CPU 3.57%（≤5% 预算内）。
//
// **粒子云改大 → 朵数按面积反比下调（2026-10-20）**：粒子云屏宽从 256 px 提到
// 768–1178 px（与云带同量级，见 spriteScale），单朵面积 ≈ 现 9 倍 ⇒
// 云量四档的朵数按 1/9 面积反比下调（Cloudy 8→3、PartlyCloudy 4→2、
// Overcast 13→5、Clear 白天 16→4），覆盖总量与改大前同量级。
// 夜间晴天的 110 是星，不是云 ⇒ 不动。Fog/Rain/Snow/Thunder/Dust 不动。
func countsRaw(cls effect.Class, isDay bool) int {
	switch cls {
	case effect.Clear:
		if isDay {
			// 16 → 4：云屏宽 256 → 768–1178 px，单朵面积 ×9 ⇒ 数量按面积反比降到 1/4。
			// 填充当量：4 × (512×1.9)×(420×1.9) ≈ 4 × 0.78 Mpx = 3.1 Mpx
			// ≈ 1.5 个整屏当量（1080×1920 = 2.07 Mpx；1 整屏当量 @15fps ≈ 4.4% CPU）。
			return 4
		}
		return 110
	case effect.Cloudy:
		// 8 → 3：屏宽 ×3–4.6（面积 ×9–21）⇒ 数量按面积反比降到 3/8。
		// 填充当量：3 × 0.78 Mpx ≈ 2.3 Mpx ≈ 1.1 整屏当量 ≈ 4.9% CPU @15fps
		// （旧口径 8 × 220×130 × 1.25² ≈ 0.35 整屏当量 ≈ 1.6% CPU）。
		// 朵数下限：低性能 x86 小主机口径"active 只有 2 时整片天空空掉"，
		// 且 adaptFps 对云量三档不削减（minActive = n，见 adaptFps 注释）⇒ 3 朵全在场。
		return 3
	case effect.PartlyCloudy:
		// 云量分档拆自 Cloudy（取证 34.16.2）：**多云见日**那一档。
		// 数量 = 现 Cloudy(3) 的约一半 = 2，太阳画盘、云稀。
		return 2
	case effect.Overcast:
		// **阴天无日**那一档。
		// P3 判据 B（out/notes-p2-budget.md 逐类表：粒子云 5→2 朵 ⇒ 4.37 层当量 → 约 0.62）：
		// 云密改由云带承担（带 6→4 同批），粒子云只留"点缀"层。
		return 2
	case effect.Fog:
		// P3 判据 B（预算表：雾团 20→2 团 ⇒ 31.53 屏宽当量 → 约 3.15，
		// 按 bandSpan 口径 1.13 层；带 4→2 后合计约 1.77 层）。
		// 浓度口径不变：仍由 drawHaze 梯度承担（薄雾是大气梯度，不是一堆大团）。
		return 2
	case effect.Rain:
		// 320 → 220（批次 A2 / C5a，用户定档 a）：降图元上限。
		// 本档无 idx% 分组依赖（Rain 的粒子全走 pkDrop，贴图按 depth 挑档，
		// 见 spawn 的 Rain/Thunder 分支与 pkDrop 分支）⇒ 改数不打散任何分组。
		return 220
	case effect.Snow:
		// 240 → 260：补**均匀灰幕**（雾精灵，规格 2 表：雪 = 均匀灰幕 + 可见雪花）。
		// 幕层按 idx%86==85 取 ⇒ 260 个里正好 3 团（P3 判据 B：20 团 → 3 团，命中 idx = 85/171/257）；
		// 240 片雪的数量口径不动（规格 3"数量与下落速度口径不变"）。
		return 260
	case effect.Thunder:
		// 420 → 300（批次 A2 / C5a）：与 Rain 同分支造粒子（全 pkDrop），
		// 无 idx% 分组依赖；闪电闪现按码分级、与粒子数无关（见 boltGapForCode）。
		return 300
	case effect.Dust:
		// 460 → 320（批次 A2 / C5a）：pkMote 的贴图是 r.Intn 随机挑，
		// 连"按总数取模"的口径都没有 ⇒ 无 idx% 分组依赖。
		return 320
	}
	return 0
}

// snowFogCurtainN Snow 灰幕的团数（P3 判据 B：20 团 → 3 团）。
// 取法 = idx%86==85 ⇒ 260 个里命中 idx = 85 / 171 / 257（推导写在 spawn 的 Snow 分支）。
const snowFogCurtainN = 3

// spawnCloudTier 粒子云档（Cloudy / PartlyCloudy / Overcast，P3 起再加 Fog 的一半）
// 共用的粒子云初始化。整段口径逐字不变（含 **RNG 取数顺序** x → y → sc → alpha → depth，
// 顺序一动，同 seed 的落位序列就整体漂移），只是从 spawn 的 switch 里搬出来，
// 让 Fog 的一半能复用同一条云口径（不复制第二份公式）。
func (f *Field) spawnCloudTier(p *particle) {
	r := f.rng
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
	// 0.28–0.45 → 0.80–0.95 → 1.0 → **逐类 alpha 区间（规格 2 表，2026-10-20）**：
	//
	//	多云见日 0.80–0.92 / 多云 0.75–0.90 / 阴 0.85–0.95
	//
	// 旧口径 1.0 是"云遮月"那轮顶到顶格的产物（α=1.0 ⇒ 云覆盖处月面被整块替换），
	// 三档共用一个 α ⇒ 多云见日/多云/阴三档在透明度上长得一样（"云和雾天没区别"）。
	// 遮挡强度仍由 α 承担（贴图核心 α 实测 255），逐档区间见 cloudFormOf。
	// sc 与 vx 在 switch 之后按**目标屏宽**统一反解（见 sizeCloudByClass）。
	cf := cloudFormOf(f.cls)
	p.alpha = cf.aLo + (cf.aHi-cf.aLo)*r.Float64()
	// 云速按薄厚分层（C1 第 1 条）：旧口径 0.003+0.011·U 是**随机抽速度**，
	// 与云的尺寸无关 ⇒ 大云可以跑得比小云快 ⇒ "后出现的云追上先出现的云"
	// 与"厚云跑在白云下面"两条现象的机制。现按屏宽定速度（薄快厚慢）：
	// vx = vxThin − (vxThin−vxThick)·min(1, dw/dwRef)，见 cloudVxByThickness。
	// 速度区间逐字沿用现口径的上下界（0.003 / 0.014），只换自变量。
	p.vx = cloudVxByThickness(0)
	p.vxBase = p.vx
	p.depth = 0.2 + 0.8*r.Float64()
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
				// 速度改按薄厚（C1，见 cloudVxByThickness）：晴天云 300–600 px ⇒
				// vx 0.0084–0.0112，落在旧口径 0.005–0.014 之内（薄小 ⇒ 快档）。
				// alpha 0.45–0.70 → **0.85–0.95**（规格 2 表：晴天 = 小白棉团、边缘清楚；
				// 旧 0.45–0.70 的大片淡灰 = "晴天的云和雾天没区别"的成因，取证 35.1）。
				// sc 与 vx 在 switch 之后按**目标屏宽**统一反解（见 sizeCloudByClass）。
				p.vx = cloudVxByThickness(0)
				p.vxBase = p.vx
				p.alpha = 0.85 + 0.10*r.Float64()
				break
			}
			if !glowParticlesOn {
				// 预览工装：WS_GLOWP=0 ⇒ 不画 Kenney 光斑小圆圈（"几个圈"的成因之一）
				p.kind = pkStar
				p.x, p.y = r.Float64(), r.Float64()*0.85
				p.sc = 0.18 + 0.90*r.Float64()*r.Float64()
				p.alpha = 0.18 + 0.72*r.Float64()*r.Float64()
				p.amp = 0.15 + 0.40*r.Float64()
				p.omega = 0.5 + 2.6*r.Float64()
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
	case effect.Cloudy, effect.PartlyCloudy, effect.Overcast:
		// 三档共用同一套云的贴图与运动口径（差异只在数量与太阳可见性，
		// 见 counts 与 bakeCelestial）。
		f.spawnCloudTier(&p)
	case effect.Fog:
		// 上一版 sc 到 4.4 → 单个雾团宽 1056 px，12 个亮色大团叠满全屏，
		// 整屏被推成近白（判读"雾像一层均匀蒙版、无纵深"成因）。
		// 改：Kenney 烟雾贴图 + 低透明 + 越近越浓，纵深交给 drawHaze 梯度。
		//
		// P3 判据 B：雾团 20 → 2。判据 3 钉 `counts(Fog)==2`，判据 4 的覆盖率口径 =
		// CoverageProfile = **云带 + 粒子云**（雾团在这条口径里计 0）⇒
		// "2 条带 + 2 团雾"的实测覆盖率 35.42% < 下限 35.7%（实跑原文见 out/notes-p3.md）。
		// 规格的补救"把云数加一档（Fog 2→3）"在 counts==2 的钉下只能**换种不换数**：
		// idx%2==1 那一档走粒子云（spawnCloudTier，与 Cloudy/Overcast 同一条云口径），
		// idx%2==0 那一档保留雾幕 ⇒ "雾 = 灰幕 + 薄雾"的定义性视觉还剩一层。
		if idx%2 == 1 {
			f.spawnCloudTier(&p)
			break
		}
		p.kind = pkFog
		// y 初始分布避开中央信息带，按半径外扩（同云）。
		// 雾 sc 上限 4.2 × 256 px = 1075 px，半高 537 px = 0.28 屏高（1920 px 屏）。
		p.x, p.y = r.Float64(), bandFreeY(r, bandPadJitN(pkFog, f.sh))
		p.sc = fogScBase + fogScSpan*r.Float64()
		// 雾浓度 = 贴图原样 alpha × 运行时倍率（fogAlphaMul 注释写明为什么不烘贴图）
		p.alpha = (0.06 + 0.16*r.Float64()*(0.35+0.65*p.depth)) * fogAlphaMul
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
		// 雪天 = **均匀灰幕（雾精灵）+ 可见雪花**（规格 2 表 + 规格 3）。
		// 依据："上天同云，雨雪雰雰"＝下雪的云在天空中是均匀一色的
		// （https://www.chinanews.com.cn/m/cul/2016/07-19/7944311.shtml ）
		// ⇒ 满屏均匀、不堆团；现 Snow 档只有 240 片雪、没有任何幕层
		// （counts(Snow)=240 全给 pkFlake，hasCloudBands(Snow)=false），
		// 加上屏上只有 2.3–12.8 px 的白点 ⇒ "雪天为什么没有飘雪"。
		// 幕层按索引取（每 86 个取 1 个 = 3 团，P3 判据 B 由 20 团降下），
		// 落位/浓度走**雾的现口径**（placeAirEntry 的 pkFog 分支 + fogAlphaMul）。
		// 幕层按索引取，P3 判据 B 把团数 20 → 3：步长 13 → **86**，取模余数 12 → **85**
		// （推导：粒子总数 260 ⇒ idx ∈ [0,259]；命中位 = s−1, 2s−1, 3s−1，
		//   要求 3s−1 ≤ 259 ⇒ s ≤ 86.33 ⇒ 取最大整数 s = 86 ⇒ 命中 idx = 85 / 171 / 257，
		//   正好 3 团，且相邻两团间距最大化（86 个粒子一跳）。
		//   对照：s = 87 时命中 86 / 173，第三团落在 idx = 260（越界）⇒ 只有 2 团，
		//   与"3 团"的钉（判据 3）不符——这一版就是这么踩到后改回 86 的，实测记录在 out/notes-p3.md）。
		// 预算依据：out/notes-p2-budget.md 逐类表 Snow 雾幕 20 团 = 29.90 屏宽当量 →
		// 3 团 = 4.49（÷bandSpan ⇒ 约 1.69 层）；雪粒 260 的口径一行没动。
		if idx%86 == 85 {
			p.kind = pkFog
			// **3 团的 x = 逐团等分的确定落位**（k = (idx−85)/86 ∈ {0,1,2} ⇒ x = (k+0.5)/3
			// = 0.167 / 0.500 / 0.833）。依据 = 本分支原话的定板口径
			// "雪 = 均匀灰幕…⇒ 满屏均匀、不堆团"（规格 2 表 + 规格 3）：
			// 20 团时随机 x 的大数定律把团摊开，降到 3 团后小样本会成片
			// （实测 seed=20261023：随机落位 0.218/0.241 两团并排 ⇒ 漂移重叠
			// dualPct = 61.02% > 判据线 60%，见 out/notes-p3.md 的判据 2 实跑）。
			// y 仍走现口径 bandFreeY（带避让口径不动）；sc/alpha/vx/vy/depth 一行没动。
			p.x = (float64((idx-85)/86) + 0.5) / float64(snowFogCurtainN)
			p.y = bandFreeY(r, bandPadJitN(pkFog, f.sh))
			p.sc = fogScBase + fogScSpan*r.Float64()
			p.alpha = (0.06 + 0.16*r.Float64()*(0.35+0.65*p.depth)) * fogAlphaMul
			p.vx = 0.010 + 0.030*r.Float64()
			p.vy = 0.002 + 0.006*r.Float64()
			p.depth = 0.2 + 0.8*r.Float64()
			break
		}
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
		// 横向飘移（规格 1.2：慢落带飘移，|vx| 必须明显小于 vy）。
		// 旧口径 vx 恒 0（雪直上直下），"雪是飘不是落"只写在注释里、没有速度分量。
		// 取值口径：vy 的最小值 0.0398 屏高/秒（depth=0、scaleMul 下档），
		// |vx| 上限 0.008 屏宽/秒 ⇒ 逐粒子 |vx|/vy ≤ 0.20 < 1/3（自检 TestSnowDrift 的判据）。
		p.vx = -(0.002 + 0.006*p.depth)
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
		// 按景深挑档（规格 1.3：近处长条）。实测七张贴图裁包围盒后的长宽比
		// 2.29–3.66（rain7 = trace_07 最长），近层取到最长的那一档。
		p.spr = int(p.depth * 6.999)
		if p.spr < 0 {
			p.spr = 0
		}
		if n := len(sp.rain); n > 0 && p.spr >= n {
			p.spr = n - 1
		}
	case pkCloud:
		// **云精灵按类挑选**（规格 2"晴/多云见日优先用薄小那几张，雨/雷优先用
		// 厚大那几张"）：下标表 cloudIdxThin/cloudIdxMid/cloudIdxThick 按 9 张
		// 贴图的实测内容分配（fill/meanColH/maxA 实测值写在 cloudIdxThin 注释）。
		// 旧口径 idx % 9 让晴天云和雨云抽到同一批形状（厚积云抽给晴天 = 淡灰大幕）。
		cfi := cloudFormOf(f.cls).idx
		if n := len(cfi); n > 0 {
			p.spr = cfi[idx%n]
		} else {
			p.spr = idx % len(sp.cloud)
		}
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
	// ---- 逐类屏宽 / 按薄厚速度 / 雪花屏上尺寸 ----
	//
	// 必须在 scaleMul 与 spr 就位之后做：屏宽是口径本身（规格 2 表、规格 3），
	// sc 是纯倍率 ⇒ 由目标屏宽**反解**，抖动 scaleMul 与贴图原生宽只影响形状。
	if p.kind == pkCloud {
		f.sizeCloudByClass(&p)
	}
	// P7 雾团封顶：与 sizeCloudByClass **同一时机**——scaleMul 与 spr 就位之后。
	// 指名根因：封顶原先写在 switch 里 sc 赋值处（:1180/:1242），而 p.scaleMul 在
	// :1342 才赋值、pkCloud 的反解也在其后 ⇒ 封顶算出的宽被随后的 scaleMul 作废
	// （实测 Snow 三张灰幕 hw 0.569/0.586/1.025 = 1233/1268/2214 px 原样未动）。
	if p.kind == pkFog {
		fogCapPx := 540.0
		if f.cls == effect.Snow {
			fogCapPx = 346.0
		}
		f.clampFogWidthPx(&p, fogCapPx)
	}
	if p.kind == pkFlake && f.cls == effect.Snow {
		f.sizeFlakeByTarget(&p)
	}
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
			//
			// 错峰口径（2026-10-20 粒子云改大后重定）：第 i 朵放在 x = −hw_i − 0.10·i，
			// 即"整朵贴在屏幕左沿之外，再按索引退 0.10 屏宽（108 px）"。
			// 自检 TestEnteringCloudsOffscreen 要求 x + hw ≤ 0 ⇒ x ≤ −hw，本式满足。
			// 旧口径 x = −0.35 − 0.10·i 是**固定起点**：改大后 hw = 0.267–0.681，
			// 小朵云（hw=0.30）的 −0.35 只比屏外起点退 0.05，大朵云（hw=0.64）的
			// −0.35 落在 hw 之内（x+hw = 0.29 > 0 ⇒ 建场那一刻就在屏内 = 闪现）。
			// 现以每朵自身的 hw 为基准，错峰量 = 0.10·i 屏宽（108 px/朵）。
			// 以 vx≈0.008/s 计：hw=0.372 的第 0 朵约 47 s 进屏，
			// hw=0.483 的第 1 朵约 68 s，hw=0.639 的第 2 朵约 89 s。
			hw, _ := f.airHalfN(&p)
			p.x = -hw - 0.10*float64(p.idx)
			f.clampAirY(&p, r, false) // x 不许被 spreadAir 改回屏内
			return p
		}
		// 开局建场：x 保持全屏铺开（一开场天空就该有云，不该全挤在左边缘），
		// 但 y 同样夹到"整只在屏内"（实测 TestAirEntryPlacement 抓到
		// 初始云 y=1.0511 越出 [0.0453, 0.9547] ⇒ 开局就有半截出屏的云。
		// P3：Snow 的 3 团灰幕 x 已在 Snow 分支按团序等分排好（"满屏均匀、不堆团"），
		// 不许 spreadAir 的 64 次随机抽样再把它抽回并排 ⇒ allowX=false
		// （clampAirY 的现成分支："x 由调用方排好，不许动"，其余类逐字不变）。
		allowX := !(f.cls == effect.Snow && p.kind == pkFog)
		f.clampAirY(&p, r, allowX)
		// P6b：铺放态也要防相叠。旧代码在这里直接 return ⇒ 开局第一帧就是探针
		// 口径的"铺放态"，实测 Fog 含粒子双层占比 28.70% > 10% 判据线（旧防重叠
		// 只作用于回收，且判据只看中心距 0.22 不看宽度）。
		// Snow 的 3 团灰幕 x 已按团序等分（allowX=false）⇒ 不许重排，跳过。
		if isAir(p.kind) && allowX {
			f.resampleAir(&p, r)
			f.clampAirY(&p, r, true) // 重摆 y 后回到"整只在屏内"窗口
		}
		return p
	}

	// 同带横向防重叠
	f.resampleAir(&p, r)
	return p
}

// 同带横向防重叠的判据：
// 同一垂直带 = 中心 y 相差 ±0.12×屏高；水平距离下限 = 0.22×屏宽。
//
// P6b（含粒子/雾团占比判据：铺放态 Fog 实测 28.70% > 10% 的成因）：
// 旧下限 0.22 是**只看中心距**的常量，不看宽度 ⇒ 宽 400–700 px 的雾团
// 中心距 237 px 照样整块相叠（探针铺放态口径：`cmd/overlapprobe` 的 dualPct 列）。
// 现加 airSlotGapN = **中心距下限 = 两半宽之和 + 21 px(0.02·屏宽)**，
// 与 DriftOverlapIncl 的真实落位口径同一几何；airGapN 保留为兜底下限（贴图缺失时 hw=0）。
const (
	airBandN    = 0.12
	airGapN     = 0.22
	airTries    = 40
	airSlotGapN = 0.02
)

// isAir 空中类精灵（云/雾/光）—— 只有它们参与防重叠。
func isAir(k pkind) bool { return k == pkCloud || k == pkFog || k == pkGlow }

// airGapNeededN 两朵空中粒子"不相叠"所需的最小中心距（归一化屏宽）。
// = 两半宽之和 + airSlotGapN，并与 airGapN 取大（贴图缺失时 hw=0 的兜底）。
func (f *Field) airGapNeededN(a, b *particle) float64 {
	ha, _ := f.airHalfN(a)
	hb, _ := f.airHalfN(b)
	if need := ha + hb + airSlotGapN; need > airGapN {
		return need
	}
	return airGapN
}

// placeAirInGap 随机抽样 40 次仍不相叠时的**确定性落位**：
// 在同带（|Δy| ≤ airBandN）现有层的空档里挑最大的那个，把本朵放到空档中点；
// 空档放不下（需要的中心距 > 空档长度）⇒ 按现成进场位口径挪到**屏外左侧**
// （x = −hw − 0.10·idx，与 placeAirEntry / driftRecycleOffscreen 同一口径），
// 进场时间天然错开。绕回口径 = 共用周期 span（bandSpan），与云带同一几何。
// visibleOnly=true 用于开局建场：落位中心必须在可见窗 [hw, 1−hw] 内
// （"一开场天空就该有云"）；false 用于回收/漂移处置：整个周期 [0, spanN) 的档都可用。
func (f *Field) placeAirInGap(p *particle, visibleOnly bool) {
	hwP, _ := f.airHalfN(p)
	spanN := 1.0
	if f.sw > 0 {
		spanN = f.bandSpan(f.sw) / float64(f.sw)
	}
	type seg struct{ c, w float64 }
	segs := make([]seg, 0, 12)
	for i := range f.parts {
		q := &f.parts[i]
		if q == p || !isAir(q.kind) {
			continue
		}
		if math.Abs(q.y-p.y) > airBandN {
			continue
		}
		hq, _ := f.airHalfN(q)
		if hq <= 0 {
			continue
		}
		c := math.Mod(q.x, spanN)
		if c < 0 {
			c += spanN
		}
		segs = append(segs, seg{c: c, w: 2 * hq})
	}
	if len(segs) == 0 {
		p.x = 0.5
		return
	}
	bestLen, bestCenter := -1.0, 0.0
	need := 2*hwP + 2*airSlotGapN
	for k := range segs {
		start := math.Mod(segs[k].c+segs[k].w/2, spanN)
		nx := segs[(k+1)%len(segs)]
		end := math.Mod(nx.c-nx.w/2, spanN)
		length := end - start
		if length < 0 {
			length += spanN
		}
		center := math.Mod(start+length/2, spanN)
		// 空档在整个周期 [0, spanN) 上都可用：粒子绘制走 metrics.WrapPeriod(x, span) 绕回口径
		// （driftLayers 同一口径）⇒ 落在 1 之外 = 屏右外，随漂移绕回左沿进场，
		// 不是"永不进场"。上一版把中心限制在 [hw, 1−hw] 的可见段 ⇒ 宽雾团被全部
		// 推到屏外，Fog 覆盖率 47.50% 掉到 21.25%（实测）。
		if visibleOnly && (center < hwP || center > 1.0-hwP) {
			continue
		}
		if length > bestLen {
			bestLen, bestCenter = length, center
		}
	}
	if bestLen >= need {
		p.x = bestCenter
		return
	}
	// 同带放不下 ⇒ **换一条净空 y 带**（|Δy| > airBandN 即不受间隙约束），x 留在屏内保覆盖。
	// 机理：两团各 0.64 屏宽在同一带里要不相叠，就必须有一团在屏外 ⇒ Fog 瞬时覆盖率
	// 从 47.50% 掉到 21.25%（实测）；把第二团挪开一个带间距就两全。
	if hh := f.halfHN(p, f.sh); hh > 0 && hh < 0.5 {
		bestY, bestClear := 0.0, -1.0
		for i := 0; i <= 100; i++ {
			y := hh + (1.0-2*hh)*float64(i)/100.0
			clear := 1.0
			for k := range f.parts {
				q := &f.parts[k]
				if q == p || !isAir(q.kind) {
					continue
				}
				if d := math.Abs(q.y - y); d < clear {
					clear = d
				}
			}
			if clear > bestClear {
				bestClear, bestY = clear, y
			}
		}
		if bestClear > airBandN {
			p.y = bestY
			p.x = 0.35 + 0.30*float64(p.idx%2) + 0.05*float64(p.idx%3)
			if hwP > 0 {
				if p.x < hwP {
					p.x = hwP
				}
				if p.x > 1.0-hwP {
					p.x = 1.0 - hwP
				}
			}
			return
		}
	}
	p.x = -hwP - 0.10*float64(p.idx)
}

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
			// P6b：宽度感知（旧口径只看中心距 0.22 ⇒ 宽雾团整块相叠，铺放态 Fog 28.70%）
			if math.Abs(q.x-p.x) < f.airGapNeededN(p, q) {
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
	// 试满仍不相叠 ⇒ 确定性落位（可见窗/整周期档 → 换净空带 → 屏外进场位）
	// 开局建场 = **可见窗内紧排**（P7 用户判据"最多的时候也只能首尾相连"⇒
	// 满密度那一刻所有层都该在屏内、首尾相接，覆盖率同时拉满）；
	// 回收/漂移处置走整个周期 [0, spanN) 的档。宽度已由 cloudFormOf 表与
	// clampFogWidthPx 封到"同带 Σ全宽 ≤ 1 屏宽"⇒ 可见窗必然装得下。
	f.placeAirInGap(p, f.steps == 0)
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
	var netDiv float64
	switch kind {
	case pkCloud:
		maxSc = cloudScBase + cloudScSpan
		// 粒子云的净倍率分母 = bandNetDiv（与云带共用，见 spriteScale 的 pkCloud 分支）：
		// 绘制 scale = p.sc × scaleMul ÷ bandNetDiv，不含深度因子 (0.55+0.45·depth)。
		netDiv = bandNetDiv
	case pkFog:
		maxSc = fogScBase + fogScSpan
		netDiv = spriteNetDiv // 雾沿用 ÷spriteNetDiv 的既有净口径
	default:
		return 0
	}
	return (maxSc * float64(maxSpriteH(spritesOnce(), kind)) / float64(screenH)) / 2 / netDiv
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

// Step 推进一帧（批次 A2 / C1）。
//
// 位移不吃实测帧 dt：acc += dt，然后 for acc >= fixedStep { advanceOneStep(fixedStep);
// acc -= fixedStep }，追赶上限 3 步（长帧不爆冲，多余累加丢弃并置 acc = 0）。
// 帧时长 61↔451 ms 的抖动因此不再变成位移忽大忽小（"一顿一顿"）。
//
// 口径不动的部分：屏宽/屏高刷新、fps 自适应测量（照旧吃实测 dt，每满 2 秒结算一次）、
// speedScale 的取值与设置口径（只改消费端：乘在固定步长上）。
func (f *Field) Step(dt time.Duration, w, h int) {
	d := dt.Seconds()
	if d <= 0 {
		return
	}
	f.sh = h     // 屏高由调用方传入（外扩量的分母）
	f.sw = w     // 屏宽：云/雾进场落位的半宽分母
	f.frameIdx++ // 启动期帧计数（C3 BootCap 的输入）

	// 横屏云宽复查（缺陷 L1/L4，spec/b4-landscape.md:18-19）：建场（NewField）时屏未知
	// ⇒ 云按竖屏目标 1080 口径反解（480–529 px）；同一批云到了 1920 横屏就是
	// 0.25–0.28 W 的小云（横屏基线"最窄云 多云 207 px"同源，troubleshooting/横屏适配缺陷.md:13）。
	f.enforceLandscapeCloudWidth()

	// fps 自适应测量：以传入的 dt 累计出"最近 2 秒"实测帧率
	// （dt 即调用方实测的帧间隔；生产路径 main.go 传 time.Since(last)）。
	// 窗口每累计满 2 秒结算一次并调整活跃粒子数。
	f.fpsAccum += dt
	f.fpsCount++
	if f.fpsAccum >= fpsWindow {
		f.fps = float64(f.fpsCount) / f.fpsAccum.Seconds()
		f.adaptFps(f.fps)
		f.fpsAccum = 0
		f.fpsCount = 0
	}

	// ---- 云横向间距控制器 + 同道不超车钳位：不在这里跑 ----
	//
	// 批次 A2 / C1：控制器搬到 advanceOneStep 里、**每步跑一次**（在本步位移之前），
	// 帧循环里不再调 ⇒ 速度序列只由步数导出。理由（A2 闸门红测试 TestFixedStepNoWallClock）：
	// 控制器每帧跑一次时，帧数（= 墙钟驱动的量）直接进入 vx ⇒ 同步数、异墙钟的
	// 两个场位移逐位不等（实测 12 步、4 个长帧 vs 12 个 fixedStep 帧：粒子 0 的 x 差 3.08e-04）。
	// "每帧速度变化 ≤ cloudVxSlew"这条自检判据由**单步上限 = cloudVxSlew ÷ 追赶上限**保住：
	// 一帧最多补 fixedStepCatchup 步 ⇒ 每帧 |Δvx| ≤ fixedStepCatchup × cloudVxSlewStep = cloudVxSlew。
	f.acc += dt
	if f.acc >= fixedStepCatchup*fixedStep {
		// 长帧（累加已够 3 步以上）：只补到追赶上限，多余累加丢弃并置 0。
		for k := 0; k < fixedStepCatchup; k++ {
			f.advanceOneStep(fixedStep)
		}
		f.acc = 0
		return
	}
	for f.acc >= fixedStep {
		f.advanceOneStep(fixedStep)
		f.acc -= fixedStep
	}
}

// advanceOneStep 推进**一个固定步长**（C1 第 3 条）：粒子位移、云带横移、
// 出界回收、闪电推进全部只吃 step，不吃实测帧 dt。
// speedScale 语义保持（15 fps ⇒ 1.0，8 fps ⇒ 0.533），乘在固定步长上。
// 动画自变量 t = 已推进步数 × fixedStep ⇒ 由步数导出，与墙钟无关
// （云带横移 bandPlace 的位移项吃的就是这个 t）。
func (f *Field) advanceOneStep(step time.Duration) {
	d := step.Seconds()
	f.steps++
	f.t = float64(f.steps) * d
	// ---- 云横向间距控制器 + 同道不超车钳位：在**本步位移之前**跑（C1：跟着步数跑）----
	//
	// 顺序理由（实测：放在位移之后跑时，2000 步里抓到 1–5 次同道超车，见 out/notes）：
	// 钳位约束的是"下一步要走的位移"。放在位移之前 ⇒ 本步的位移由"当前配置"下的钳位值驱动，
	// "同带内 vx 随 x 单调不增"这条性质对真正要走的这一步成立 ⇒ 超车在条件上封死。
	if f.cls == effect.Cloudy || f.cls == effect.PartlyCloudy ||
		f.cls == effect.Overcast || f.cls == effect.Rain || f.cls == effect.Thunder {
		f.coupleClouds()
	}
	// ---- P3 判据 D：漂移期复查（每个固定步走一遍入口，节流在函数里）----
	//
	// 原话（coupleClouds 注释）："落位阶段本来有最小间隙保证（spreadAir），
	// 但它只在**生成时**生效，漂移中不复查" ⇒ 这条就是"漂移中的复查"。
	// 调用点跟着步数跑（与 coupleClouds 同一口径，不吃墙钟），
	// 内部按 driftCheckEverySteps 节流 ⇒ 未命中的步只做一次取模判断。
	f.crowdSweep()
	// ---- B3 进场排队：parked 的云每步再试一次（闸门放行才发射进场位）----
	f.releaseParkedClouds()
	// 位移速度系数（实测口径）：0 / 未设置 = 不缩放
	ss := f.speedScale
	if ss <= 0 {
		ss = 1
	}
	bandY0, bandY1 := infoBandY()
	// 回收窗口按**含尺寸抖动**的外扩口径（bandPadJitN）：
	// 可用区（spawn 落位）与回收窗口必须同口径，否则带边界与回收线之间
	// 会出现"中心在带外、身体压进带内"空隙。
	// 粒子云不走这条回收窗口（落位 = 月亮行，见下面注释），只取雾的外扩量。
	padFog := bandPadJitN(pkFog, f.sh) // 屏高 = Step 每帧刷进 f.sh 的值（口径不动）
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind == pkCloud && f.parkedCloud[i] {
			// B3 parked：保持停在屏外（不位移、不回收），由 releaseParkedClouds 逐帧再试。
			continue
		}
		p.x += p.vx * d * ss
		p.y += p.vy * d * ss
		p.rot += p.rotV * d * ss
		// 出界回收
		//
		// **粒子云按"整朵完全出屏"判**（现象：云会在没彻底出屏幕前就突然消失，
		// 取证 34.16.3 批 5 / m00970 第 2 条）：
		// 回收判据 = 中心 ± 自身半宽 hw / 半高 hh 完全离开可视区
		// （x−hw > 1、x+hw < 0、y−hh > 1、y+hh < 0），
		// 也就是 x 向上下界按 dw 展开、y 向同理。
		// 旧口径按**中心**判（x>1.25 / x<−0.35 / y 出 [−0.35, 1.15]）：
		// 云半宽 0.052（112 px 云在 1080 px 屏上）⇒ 中心到 1.052 时整朵已经出屏，
		// 却要等到 1.25 才回收 ⇒ 中间 0.2 屏宽 = 216 px 的行程里
		// 云是整朵挂在屏幕外的（看不见），回收那一刻读成"没出屏就突然消失"。
		// 其余类（雨/雪/尘/星/雾/光晕）沿用既有中心口径，
		// 雾的落位与带避让另有口径（placeAirEntry / bandPadJitN），一行没动。
		offscreen := p.y > recycleYHi || p.y < recycleYLo || p.x > 1.25 || p.x < -0.35
		if p.kind == pkCloud {
			hw, hh := f.airHalfN(p)
			// **按运动方向判"出屏"**（2026-10-20 粒子云改大）：
			// vx>0 只看右边界（x−hw > 1），vx<0 只看左边界（x+hw < 0）。
			// 两条定板要同时成立：
			//   (a) 不许在没彻底出屏幕前突然消失 ⇒ 出屏判据必须按整朵展开；
			//   (b) 云要在屏幕外慢慢进来 ⇒ 进场位 x = −hw（vx>0）本来就整朵在屏外，
			//       若"完全出屏"不分方向地判，进场位与"出屏"是同一个条件
			//       （x+hw ≤ 0 ⇔ 整朵在屏外）⇒ 刚放好的进场云当场被回收、
			//       重新放到 x=−hw ⇒ 错峰量被吃掉（实测 5 朵云全部在第 2 秒进屏）。
			// 判据按方向展开后：进场侧不回收（云从屏幕外慢慢飘进来），
			// 出屏侧整朵出屏才回收（自检 TestRecycleFullyOffscreen 两段照旧）。
			if p.vx >= 0 {
				offscreen = p.x-hw > 1.0 || p.y-hh > 1.0 || p.y+hh < 0.0
			} else {
				offscreen = p.x+hw < 0.0 || p.y-hh > 1.0 || p.y+hh < 0.0
			}
		}
		if offscreen {
			f.parts[i] = f.spawn(false, i)
			continue
		}
		// 雾的越界回收按**带边界 ± 外扩量**。
		// 旧版只按中心判越界，于是中心贴着带上沿的雾，
		// 精灵身体仍压进带内 210–537 px（缺陷 1 根因）。回收窗口扩到带外 pad
		// 之后，推进过程中精灵中心不会回到带内，精灵整体也不进带。
		//
		// **粒子云不适用这条（2026-10-20 粒子云改大）**：
		// 云的落位口径是**月亮行**（placeAirEntry / clampAirY / spreadAir 三处同一判据，
		// y = 0.095 ± 月盘半径 ± 半高），压带由信息面板自带的 30% 深色底压暗
		// （上游 default_settings.py:1523-1540 的 weather.bg_opacity 0.3 +
		// show_background True；本复刻 ui/render.go 的 fillPanel 已实现，
		// 绘制顺序 天空→粒子→fillPanel→文字）。
		// 改大后云半高 0.111–0.383 屏高，带外可用段只有 0.422 屏高 ⇒
		// "整只在带外"几何无解（2×0.383 = 0.766 > 0.422），与雾同一处境。
		// 实测后果：这条守卫在 pad=0.104 时就把月亮行的云成片打掉
		// （45 s 内 8 朵云共回收 2952 次），pad 随屏宽涨到 0.306 后
		// 守卫区间 (−0.139, 1.205) 覆盖全部落位 ⇒ 每帧全部回收 ⇒ 屏上零朵云。
		// 云的出屏回收只走上面的"整朵完全出屏"判据（需求口径：不许没出屏就消失）。
		if p.kind == pkFog {
			pad := padFog
			if p.y > bandY0-pad && p.y < bandY1+pad {
				f.parts[i] = f.spawn(false, i)
				continue
			}
		}
		// 云/雾横向循环
		//
		// **粒子云不参与回绕（2026-10-20 粒子云改大）**：回绕点 x>1.3 是"中心口径"，
		// 改大后云半宽 hw = 0.267–0.681 屏宽 ⇒ 中心到 1.3 时云体还压着屏幕右沿
		// （左沿在 1.3−hw = 0.62…1.03），此刻瞬移到 −0.3 等于
		// "在没彻底出屏幕前突然消失"＋"在左沿突然出现的半截云"，两条定板都撞。
		// 自检 TestAirEntryRecycleIsOffscreen 把回收后的 x 钉在 x ≤ −hw，
		// 回绕给出的 x = −0.3 在 hw > 0.3 时直接越界（实测 x=−0.3000、hw=0.3398）。
		// 云已经有同口径的替身：上面的"整朵完全出屏"回收（x−hw > 1.0 ⇒ spawn(false)
		// → placeAirEntry → x = −hw），进场位天然在屏幕外，不需要回绕。
		if f.cls == effect.Fog && p.kind == pkFog && p.x > 1.3 {
			p.x = -0.3
		}
		if f.cls == effect.Dust && p.x > 1.3 {
			p.x = -0.25
		}
	}
	// 闪电（雷暴）：时间口径的闪现（规格 1.5，见 stepBolt）
	if f.cls == effect.Thunder {
		f.stepBolt(d)
	}
}

// 闪电闪现的口径常量。
//
// 时长口径不动（0.15–0.30 秒）；**间隔按码分级**（D4）：
//
//	95 = Thunder, slight or moderate, without hail（中弱）      ⇒ 15–40 s
//	96 = Thunder, slight or moderate, with hail（中弱 + 冰雹）  ⇒ 10–25 s
//	97 = Thunder, heavy, without hail（强）                     ⇒ 6–15 s
//	99 = Thunder, heavy, with hail（最强）                      ⇒ 6–15 s
//	其余码（非雷暴）                                            ⇒ 不闪
//
// 检索出处（WMO CODE TABLE 4677 的码义）：
// "https://github.com/wmo-im/wmo4677" 与 NOAA 的码表页
// "https://www.nco.ncep.noaa.gov/pmb/docs/grib2/grib2_doc/grib2_table4-208.shtml"。
// 实测 Open-Meteo 没有 thunder_probability 这个字段（响应报 invalid String value），
// ⇒ 频率只能由码分级，不能由概率字段驱动。
const (
	boltDurLo    = 0.15 // 单次闪现时长下限（秒）
	boltDurHi    = 0.30 // 上限（秒）
	boltGap95Lo  = 15.0 // 码 95 的间隔下限（秒）
	boltGap95Hi  = 40.0 // 码 95 的间隔上限
	boltGap96Lo  = 10.0 // 码 96
	boltGap96Hi  = 25.0
	boltGap97Lo  = 6.0 // 码 97（强）
	boltGap97Hi  = 15.0
	boltGap99Lo  = 6.0 // 码 99（最强）
	boltGap99Hi  = 15.0
	boltGapFloor = 6.0  // 三档里最小的间隔 ⇒ 不许频闪的下限
	boltAlphaMax = 0.75 // 峰值 alpha 上限
	boltLenFrac  = 0.28 // 长轴 = 0.28 屏高（旧折线闪电的屏上长轴 560 px / 1920 = 0.29，同量级）
	// 落位区：云带画面上沿所在的上部天空带。
	// 实测压顶档（雨/雷）的云带左上角 -0.12…-0.02 屏高、带高 0.24–0.57 屏高
	// ⇒ 带体在屏内的可见上沿从 0 起，闪电取 0.06–0.20 屏高 = 落在带体上沿之上/之中，
	// 旧口径是写死的 (0.58w, 0.10h) 一个点。
	boltXLo = 0.15
	boltXHi = 0.85
	boltYLo = 0.06
	boltYHi = 0.20
)

// boltGapForCode 码 → 闪现间隔区间（秒）。ok=false ⇒ 该码不闪。
//
// 97 与 99 同档（6–15 s）：码义表里 97 = Thunder, heavy, without hail（强）、
// 99 = Thunder, heavy, with hail（最强），两档都是"强"，
// 分级表只列了 95/96/99 ⇒ 97 按码义表落进最强那一档（见函数头注释的出处）。
func boltGapForCode(code int) (lo, hi float64, ok bool) {
	switch code {
	case 95:
		return boltGap95Lo, boltGap95Hi, true
	case 96:
		return boltGap96Lo, boltGap96Hi, true
	case 97:
		return boltGap97Lo, boltGap97Hi, true
	case 99:
		return boltGap99Lo, boltGap99Hi, true
	}
	return 0, 0, false
}

// stepBolt 推进闪电的时间线（只在 Thunder 调用）。
//
// 四条硬口径：
//
//	1 单次闪现 0.15–0.30 秒：闪现期间 flash 走一条"起-平台-落"的包络，
//	   到点归零，不是一帧的脉冲；
//	2 间隔按码分级（见 boltGapForCode）：下一次的等待时间在**上一次结束时**才抽，
//	   抽的是秒数，与帧率无关（旧口径抽帧数 ⇒ 帧率一变间隔就变）；
//	3 不许频闪：包络在 0.15 秒的上升段与 0.45 秒的下降段之间是平台，
//	   逐帧连续变化（flash 是 t 的连续函数），不存在逐帧跳变；
//	4 非雷暴码 ⇒ 不闪（正在落的那一次让它自然落完，不再起新的）。
//
// 随机源 = f.rng（建场时的确定性种子，见 NewField 的 rand.New(seed.NewSource(seed))），
// 不用 math/rand 的全局函数。
func (f *Field) stepBolt(d float64) {
	lo, hi, ok := boltGapForCode(f.wxCode)
	if f.boltOn {
		u := (f.t - f.boltStart) / f.boltDur
		if u >= 1.0 {
			f.boltOn = false
			f.flash = 0
			if ok {
				f.boltNext = lo + (hi-lo)*f.rng.Float64()
			}
			return
		}
		f.flash = boltEnvelope(u)
		return
	}
	if !ok {
		f.flash = 0
		return
	}
	f.boltNext -= d
	if f.boltNext > 0 {
		f.flash = 0
		return
	}
	f.boltOn = true
	f.boltStart = f.t
	f.boltDur = boltDurLo + (boltDurHi-boltDurLo)*f.rng.Float64()
	f.boltIdx = f.rng.Intn(3)
	f.boltX = boltXLo + (boltXHi-boltXLo)*f.rng.Float64()
	f.boltY = boltYLo + (boltYHi-boltYLo)*f.rng.Float64()
	f.boltCount++
	// 事件记录（自检用，定长上限，不随运行时长无限增长）
	if len(f.boltLog) < 256 {
		f.boltLog = append(f.boltLog, f.t)
		f.boltDurs = append(f.boltDurs, f.boltDur)
	}
	f.flash = boltEnvelope(0)
}

// boltEnvelope 闪现包络：u∈[0,1] → 0…1…0。
// 前 15% 升起、15%–55% 平台、后 45% 落下（两端 smoothstep ⇒ 无台阶）。
func boltEnvelope(u float64) float64 {
	if u <= 0 {
		return 0
	}
	if u >= 1 {
		return 0
	}
	return smoothstep(0.0, 0.15, u) * (1.0 - smoothstep(0.55, 1.0, u))
}

// NextActive 一轮帧率自适应后的活跃粒子数（C2 抽出的纯函数：无随机、无副作用）。
//
// 口径（规格 A2/C2，全部数值确定）：
//
//	死区 12 ≤ fps ≤ 18   ⇒ 原值不动
//	削减 fps < 12         ⇒ 比例目标 = cur×fps/12，下限 minActive = (total+3)/4，
//	                        且**单轮位移不超 lim = max(4, total/20)**（斜坡，不一次砍 43%）
//	恢复 fps > 18         ⇒ 每轮 +lim，上限 total（3 轮滞回在调用方 adaptFps）
//	云量档 total ≤ 8      ⇒ 永不削减（adaptFps 的"云量三档 minActive = n"同口径：
//	                        Cloudy 3 / PartlyCloudy 2 / Overcast 5 / Clear 白天 4 全 ≤ 8）
//
// cur 越界（0 或 > total）按"未削减 = 全部"处理（与 Field.active 的零值口径一致）。
func NextActive(cur, total int, fps float64) int {
	if total <= 0 {
		return 0
	}
	if cur <= 0 || cur > total {
		cur = total
	}
	if total <= 8 {
		// 云量档：削减机制针对的是"云被砍到 2 朵 ⇒ 整片天空空掉"（见 adaptFps 注释）。
		return total
	}
	lim := total / 20
	if lim < 4 {
		lim = 4
	}
	switch {
	case fps < 12:
		target := int(float64(cur) * fps / 12.0)  // 按比例削减
		if min := (total + 3) / 4; target < min { // 下限 = 总数 25%
			target = min
		}
		if cur-target > lim { // 限幅：单轮削减不超 lim
			target = cur - lim
		}
		if target < cur {
			return target
		}
		return cur
	case fps > 18:
		target := cur + lim // 限幅：单轮恢复不超 lim
		if target > total {
			target = total
		}
		return target
	}
	return cur // 死区
}

// adaptFps 按实测 fps 调整活跃粒子数（消费端，数值口径全在纯函数 NextActive）：
//
//	fps < 12  → 削减：单轮位移限幅 lim = max(4, total/20)，下限 = 总数 × 25%
//	fps > 18  → 恢复：**连续 3 轮**才开始（滞回），每轮 +≤lim，上限 = 总数
//	12≤fps≤18 → 滞回区，不动（防在阈值附近来回抖动）
//
// 滞回计数在 Field 上（recoverStreak）：期间任何一轮 fps ≤ 18 把计数清零。
// 云量三档 Cloudy/PartlyCloudy/Overcast 保留既有口径 minActive = n（永不削减）。
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
	// 云量拆出的两档**同口径**（PartlyCloudy / Overcast）：削减机制针对的是
	// "云被砍到 2 朵 ⇒ 整片天空空掉"，与档无关。
	if f.cls == effect.Cloudy || f.cls == effect.PartlyCloudy || f.cls == effect.Overcast {
		minActive = n
	}
	if fps < 12 {
		f.recoverStreak = 0 // 恢复计数清零（滞回：削减轮打断连续恢复轮）
		target := NextActive(f.active, n, fps)
		if target < minActive {
			target = minActive
		}
		if target < f.active {
			f.active = target
		}
		return
	}
	if fps > 18 {
		f.recoverStreak++
		if f.recoverStreak < recoverArmRounds {
			return // 连续不足 3 轮 fps>18 ⇒ 不开始恢复（防振荡）
		}
		if target := NextActive(f.active, n, fps); target > f.active {
			f.active = target
		}
		return
	}
	f.recoverStreak = 0 // 死区同样清零：恢复只认"连续"
}

// ActiveCount 当前活跃粒子数 = Draw 实际绘制的粒子数。
// MoonBoostStats 返回"本帧被加浓的云数 / 累计被加浓的云数"（诊断）。
// 用途：低性能 x86 小主机验证 overMoon 到底触发没有 —— 低性能 x86 小主机 12 帧连拍月框内最大亮度恒定 237
// （云体仅 ~210） ⇒ 云盖到月核上时月核没被压暗 ⇒ 加浓没生效，需要实测触发次数。
func (f *Field) MoonBoostStats() (int, int64) {
	return f.moonBoostFrame, f.moonBoostTotal
}

// BootCap 启动期的活跃粒子上限（C3 第 1 条，纯函数、数值确定）。
//
// 启动期要把"一次性全量重排"拆开：尺寸切换当帧做全量烘焙 + 全量粒子
// （实测 n=1 layout=1.261 s、n=7 layout=1.385 s），前 120 帧按三档放行：
//
//	frameIndex <  60 ⇒ 80
//	60 ≤ frameIndex < 120 ⇒ min(total, 160)
//	frameIndex ≥ 120      ⇒ total（稳态，不封顶）
//
// frameIndex 从 0 起算（第 1 帧 = 0）。上限只压"画多少"，不改生成器口径
// （parts/counts 不动），与 C2 的削减取小。
func BootCap(frameIndex int, total int) int {
	if total <= 0 {
		return 0
	}
	if frameIndex < 0 {
		frameIndex = 0
	}
	switch {
	case frameIndex < 60:
		if total < bootCapFirst {
			return total
		}
		return bootCapFirst
	case frameIndex < 120:
		if total < bootCapSecond {
			return total
		}
		return bootCapSecond
	}
	return total
}

// 启动期三档的数值（C3 第 1 条钉死口径）。
const (
	bootCapFirst  = 80
	bootCapSecond = 160
)

// ActiveCount 当前活跃粒子数 = Draw 实际绘制的粒子数。
//
// = min(削减后的 active, 启动期上限 BootCap)（C3 第 1 条：与 C2 的削减取小）。
// 未步过帧的场（frameIdx == 0，建场后直接读）不套启动期上限 ⇒ 建场口径 = 全部，
// 与旧行为一致；跑起来后第一帧起启动期上限生效。
func (f *Field) ActiveCount() int {
	n := len(f.parts)
	act := n
	if f.active > 0 && f.active <= n {
		act = f.active // 已削减
	}
	if f.frameIdx > 0 {
		if capN := BootCap(f.frameIdx-1, n); act > capN {
			act = capN // 启动期拆开：前 60 帧只画 80 个
		}
	}
	return act
}

// TotalCount 粒子总数（生成器口径，不随削减变化）。
func (f *Field) TotalCount() int { return len(f.parts) }

// FPS 最近一次测量窗口结算的实测帧率。
func (f *Field) FPS() float64 { return f.fps }

// Draw 画一整帧。
// cloudBandN 阴雨类天空的云带片数。
//
// P6 判据 1（spec/p6-noverlap.md 设计第 1 条，定死不许自行调参）：N = 3
// （Overcast / Cloudy / Rain / Thunder）。
//
// 旧值 4（5 → 4，需求"适当减少云的数量"）：等相位按共用 span 加、按逐带 spanI 取模
// （旧 bandPlace 根因，见其注释）⇒ 实测 SCR_CROWD dual=55.7%。
const cloudBandN = 3 // P6 设计第 1 条：4 → 3

// bandCount 本场的云带片数（P6 设计第 1 条，定死）：
//
//	Cloudy / Rain / Thunder = 3（= cloudBandN）
//	Overcast                = 3（P6：4 → 3）
//	PartlyCloudy            = 2（保持）
//	Fog                     = 2（保持，P3 判据 3 逐字）
//
// 云带是"云层厚度"层，粒子云是"点缀"层。P6 把压顶四档统一成 N=3 的理由：
// 不重叠的可证性（见 bandPlace 注释）只对"等宽 T + 等相位 + 共用 span"的 N 条带成立，
// Overcast 单独多一条带会把间隙从 (w−T)/3 = 26.7 px 压成 span/4 − T = 770−1000 < 0（相撞）。
func (f *Field) bandCount() int {
	switch f.cls {
	case effect.PartlyCloudy:
		return 2
	case effect.Overcast:
		// P6 设计第 1 条：Overcast 4 → 3（判据测试 bandCount() 逐类 = 3/3/3/3/2）。
		return 3
	case effect.Fog:
		// P3 判据 3 逐字要求 Fog `bandCount()==2`（配套预算表"雾的带 4→2"）；
		// 规格 A 的括注"其余类走默认 4"与判据 3/预算表冲突，取判据 3（见 out/notes-p3.md 冲突条目）。
		return 2
	}
	return cloudBandN
}

// bandPlace 第 i 条云带在时刻 t 的落位与尺寸（屏幕像素坐标，x/y 是精灵左上角）。
//
// 抽成包级函数的原因：绘制与"横向分布是否均匀"探针必须共用**同一套几何**。
// 探针若自己复制一份公式，测出来的就不是屏幕上真实存在的东西。
type bandPlace struct {
	x, y, dw, dh float64
	mirror       bool
}

// bandWidthPx P6 设计第 2 条"等宽"（定死，不许自行调参）：
//
//	T = round(w × 1000 / 1080)   （1080 屏 ⇒ 1000 px）
//	硬上限 T ≤ 0.95·w
//
// 带宽不再随 frac 变化（旧 sc = (1.5+0.8·frac)/bandNetDiv ⇒ 逐带 768…1178 px =
// 重叠根因，见 bandPlace 注释）。粒子云 p.sc 口径**不动**（P6"不做什么"，只收云带）。
func bandWidthPx(w int) float64 {
	t := math.Round(float64(w) * 1000.0 / 1080.0)
	if cap := 0.95 * float64(w); t > cap {
		t = cap
	}
	return t
}

// landscapeCloudWidthRangePx 横屏云宽区间（缺陷 L1/L4，spec/b4-landscape.md:18-19）：
// 云宽走 kinetics 的屏宽分制口径（下限 0.444W、间隙 0.0194W、同带 ≤2 朵，
// evidence/p8-numbers-20261023.md:11-15），并把"屏幕左右边距 = 间隙同口径"一起留出
// ⇒ 云永不铺满整屏（L1 根因是单片云铺满 1920 宽）：
//
//	lo = ceil(0.444·W) = kinetics 下限            ⇒ @1920 = 853 px
//	hi = min(kinetics FloorHi = (W − gap)/2, (W − 3·gap)/2) = (W − 3·gap)/2  ⇒ @1920 = 904 px
//	     （3·gap = 同带 2 朵之间的一条间隙 + 左右两条边距，均 0.0194W 同口径）
//
// 竖屏不走这条口径（竖屏云带宽 = bandWidthPx 逐字不变，480/529/21 @1080 不动）。
func landscapeCloudWidthRangePx(w int) (lo, hi int) {
	_, lo, gap := kinetics.BandCapacity(w)
	hi = kinetics.FloorHi(w)
	if cap := (w - 3*gap) / 2; cap < hi {
		hi = cap
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// bandWidthFor 云带宽度（横屏 = kinetics 口径的区间上限，竖屏 = bandWidthPx 逐字）。
// 双朝向判定 = w > h（spec/b4-landscape.md:34），屏高由调用方传入
// （bandPlace 有 h；bandSpan 走 Field 每帧刷新的 f.sh，见 Step）。
func (f *Field) bandWidthFor(w, h int) float64 {
	if IsLandscape(w, h) {
		_, hi := landscapeCloudWidthRangePx(w)
		return float64(hi)
	}
	return bandWidthPx(w)
}

// bandSpan 所有云带共用的横向循环周期（像素）。
//
// P6 设计第 3 条（常量式）：span = w + 2·T，T = bandWidthPx(w)。
// 必须共用：周期不同 ⇒ 相位差随时间漂移 ⇒ 扎堆 / 空白（见 bandPlace 注释）。
// 旧口径 "w + 2·平均dw"（逐带 sc 求和）随等宽改造退化为 w + 2·T 的同一数值，
// 直接改常量式，消除"平均带宽"这一漂移源。
// 进场口径（左边缘相位，bandPlace 注释）：周期起点 x = −dw ⇒ 带跨 [−T, 0] 全在屏外；
// 周期终点 x = span − dw = w + T ≥ w ⇒ 整条带已出右界（一个像素都不剩在屏内）。
func (f *Field) bandSpan(w int) float64 {
	if len(f.sp.cloudSz) == 0 {
		return float64(w)
	}
	// 周期公式 span = w + 2·T 不动（P6 设计第 3 条）；T 的朝向口径见 bandWidthFor。
	return float64(w) + 2*f.bandWidthFor(w, f.sh)
}

func (f *Field) bandPlace(i int, t float64, w, h int) (bandPlace, bool) {
	if len(f.sp.cloud) == 0 || len(f.sp.cloudSz) == 0 {
		return bandPlace{}, false
	}
	n := f.bandCount()
	fi := i % len(f.sp.cloud)
	sz := f.sp.cloudSz[fi]
	if sz.X <= 0 || sz.Y <= 0 {
		return bandPlace{}, false
	}
	frac := float64(i) / float64(n-1)
	// P6 设计第 2 条：等宽 T ⇒ sc = T / sz.X（bandPlace 与 drawCloudBands 两处调用点
	// 统一这一口径），dw = sz.X × sc = T 逐带相同；高度 dh = sz.Y × sc 保精灵比例。
	sc := f.bandWidthFor(w, h) / float64(sz.X)
	dw := float64(sz.X) * sc
	// P6 设计第 3 条：相位与周期**共用同一个 span = w + 2·T**（旧逐带 spanI 删除）。
	span := f.bandSpan(w)
	// ---- 不重叠的可证性（P6 设计第 4 条）----
	//
	// 等相位（i/N 圈）+ 共用 span ⇒ 中心间距恒 = span/N（与 t 无关）：
	//   中心_i = mod(V·t + span·i/N, span) + T/2。
	// 两带重叠（最坏 = 相邻带，带宽同为 T）：
	//   overlap = max(0, T − span/N) = max(0, T − (w+2T)/N)。
	//   N = 3 ⇒ overlap = max(0, (T − w)/3)，而硬上限 T ≤ 0.95w ≤ w ⇒ 重叠恒为 0，
	//   间隙 = span/N − T = (w − T)/3（w=1080、T=1000 ⇒ 26.7 px）。
	// 旧落位式（根因，spec 点名到行，旧行号：旧 ui/effects.go:2168 落位式 /
	//   旧 :2098 bandSpan = w + 2·平均dw / 旧 :2157 逐带 spanI / 旧 :2130 逐带 sc）：
	//   x = mod(0.03·w·t·0.725 + span·i/n, spanI) − dw
	//   把"等相位"按**共用 span** 加出来，却对**逐带周期** spanI = w + 2·dw_i 取模，
	//   带宽 dw_i 逐带不同（sc = (1.5+0.8·frac)/bandNetDiv ⇒ 768…1178 px）
	//   ⇒ 相邻带中心间距在 [0, spanI) 内不均，随 t 漂移必然相撞 ⇒ 实测 dual=55.7%。
	//
	// 相位按**左边缘**记（不是中心）：mod=0 时 x = −dw ⇒ 整条带跨 [−dw, 0]，全在屏幕外；
	// mod→span 时 x = span − dw = w + dw ⇒ 整条已出右界（旧 :2158–2164 注释意图不变）。
	// 若按中心记（x = centre − dw/2），周期起点带跨 [−dw/2, +dw/2] ⇒ 半条已经在屏内
	// ⇒ 云在左边缘凭空冒出来，违反需求"云应该在外面慢慢进来而不是在眼前突然出现"。
	// 等宽改造后这条代价消失：dw 逐带相同 ⇒ 左边缘等分 ⇔ 中心等分，间距恒 = span/N。
	//
	// 速度口径不动（P6"不做什么"）：0.03·w·t·0.725（云速 ×0.5 档，乘 0.725 系数不变）。
	x := math.Mod(0.03*float64(w)*t*0.725+span*float64(i)/float64(n), span) - dw
	// ---- 压顶档的带纵向位置（规格 2 表：阴 / 雨 / 雷 = 压顶）----
	//
	// P6"不做什么"：y 口径逐字不动（旧 :2178–2181 压顶档、frac = i/(n−1)）。
	// 现口径 y = 0.02 + 0.24·frac（带左上角，归一化屏高）：四条带的**中心** y 实测
	// 0.119 / 0.196 / 0.290 / 0.412 屏高（dh/2 = 0.0996…0.152 屏高），
	// 中位数 0.243 ⇒ 比晴天粒子云的中心（月亮行 0.079…0.208，中位 0.144）更低
	// ⇒ "厚重的云应该更多在上面"不成立。
	// 压顶档（cloudFormOf().pressTop = 阴/雨/雷）改成左上角从屏上缘外起、逐带只下移 0.10：
	// 带中心 y 实测 0.020 / 0.075 / 0.127 / 0.131 屏高（Rain，4 条），中位 0.101 ⇒ 落在晴天之下。
	// 带左上角为负 = 带上半截在屏幕外 ⇒ 画出来贴住上缘，读成"压顶"而不是"整条挂在天上"。
	top, spread := CloudBandYTop, CloudBandYSpread
	if cloudFormOf(f.cls).pressTop {
		top, spread = cloudBandPressTop, cloudBandPressSpread
	}
	return bandPlace{
		x: x, y: float64(h) * (top + spread*frac),
		dw: dw, dh: float64(sz.Y) * sc, mirror: i%2 == 1,
	}, true
}

// BandRegionPx 云带在屏幕上的纵向区间（px，含精灵高度）：面板/信息层的**带区间接口**。
//
// 落位逐条取 bandPlace 的**同一套几何**（绘制与本接口同源 ⇒ 版式避让测的就是画出来的东西）。
// 无云带类别（Clear/Snow/Dust）返回 (0,0) ⇒ 调用方退化为屏高比例口径。
// 只读口径：本函数不动带数学（bandPlace/bandSpan 的公式与常量逐字不变）。
func (f *Field) BandRegionPx(w, h int) (top, bottom int) {
	if w <= 0 || h <= 0 || !f.hasCloudBands() {
		return 0, 0
	}
	n := f.bandCount()
	first := true
	for i := 0; i < n; i++ {
		bp, ok := f.bandPlace(i, f.t, w, h)
		if !ok {
			return 0, 0
		}
		t0 := int(math.Round(bp.y))
		b0 := int(math.Round(bp.y + bp.dh))
		if first {
			top, bottom = t0, b0
			first = false
			continue
		}
		if t0 < top {
			top = t0
		}
		if b0 > bottom {
			bottom = b0
		}
	}
	return top, bottom
}

// drawCloudBands 在天空**上部**画大云带，给雨/雷/阴/雾四类提供云层厚度。
//
// 位置约束：云带只落在顶部 2%–26% 高度，**位于时钟信息带之上**，
// 不与时间/天气文字重叠（信息带避让在旧版由 bandFreeY 对粒子实现；
// 云带单张宽达 475–1064 px，逐带避让不现实，故用"整体置于信息带之上"
// 这一更强的约束达成同一目的。
//
// 变换必须 `op.Affine(tr).Push` + `Pop()`（`.Add` 会累积变换）。
// bandAlphaMax 云带 alpha 上限：Overcast 比其余档提高（阴天那一档要"云带压得更实"，
// 取证 34.16.2 的 Overcast 定义 = 云密度 1.6 倍 + 云带 alpha 上限提高）。
// 其余档上限 240/255 = 0.941（既有口径，带0 实测 0.941）；Overcast 取 255/255 = 1.000。
//
// 抽成函数的理由：云影层与云本体必须用同一个 alpha（影 = 本体 alpha × cloudShadowK），
// 两处各写一份就会漂移（自检 TestCloudShadowAlphaMatches 逐条比对两条链）。
func (f *Field) bandAlphaMax() float64 {
	if f.cls == effect.Overcast {
		return 255.0
	}
	return 240.0
}

// bandAlphaOf 第 i 条云带本体的 alpha（含类别切换的 fade 乘子）。
// alpha 随索引递减（顶部带最实）：(alphaMax − 25·frac)/255 × fade。
func (f *Field) bandAlphaOf(i, n int) float64 {
	if n <= 1 {
		return f.bandAlphaMax() / 255.0 * f.fade
	}
	frac := float64(i) / float64(n-1)
	return (f.bandAlphaMax() - 25.0*frac) / 255.0 * f.fade
}

// cloudAlphaOf 第 p 朵粒子云本体的 alpha（含闪烁、实例抖动、fade、
// 非晴档的 alpha 下限、闪光期的局域抬升），与 Draw 里云本体那条链逐字同口径。
//
// 抽出来的理由同 bandAlphaOf：云影层要用同一个值。
func (f *Field) cloudAlphaOf(p *particle) float64 {
	alpha := p.alpha
	if p.amp > 0 {
		alpha += p.amp * math.Sin(p.omega*f.t+p.phase)
	}
	alpha *= p.brightnessMul
	alpha *= p.alphaMul
	alpha *= f.fade
	if f.cls != effect.Clear && alpha < cloudFormOf(f.cls).aLo {
		alpha = cloudFormOf(f.cls).aLo
	}
	if f.flash > 0.01 {
		if m := f.flashMaskAt(p.x, p.y); m > 0 {
			alpha *= 1.0 + flashBoostMax*m*f.flash
		}
	}
	if alpha > 1 {
		alpha = 1
	}
	if alpha < 0 {
		return 0
	}
	return alpha
}

// cloudShadowPlace 一朵云投在天空上的一片影：用第 spr 张云精灵、
// 左上角 (x,y)、屏幕尺寸 dw×dh、alpha = 云本体 alpha × cloudShadowK。
type cloudShadowPlace struct {
	spr    int
	x, y   float64
	dw, dh float64
	mirror bool
	alpha  float64
}

// cloudShadowPlaces 本帧要投的全部影：每朵激活的粒子云 + 每条云带。
//
// 几何与 alpha 只有这一份：GPU 绘制（drawCloudShadows）与判据探针（CPU 合成）
// 都读它，两处各写一份就会漂移。
// 只在白天档返回内容：夜间没有阳光可挡，且会把夜空压黑。
func (f *Field) cloudShadowPlaces(w, h int, t float64) []cloudShadowPlace {
	if !f.isDay {
		return nil
	}
	out := make([]cloudShadowPlace, 0, 16)
	// ---- 云带 ----
	// P3 判据 C（空影守卫）：守卫必须与"本体画不画"共用 hasCloudBands() 这一判据
	// （Draw / CoverageProfile / CelestialCoverAll 同一判据，探针 P1 抓到
	// Clear 白天 / Snow / Dust 本体零条带、每帧仍投 4 次带影 = 纯浪费）。
	if n := f.bandCount(); n > 0 && f.hasCloudBands() && len(f.sp.cloud) > 0 && len(f.sp.cloudSz) > 0 {
		for i := 0; i < n; i++ {
			fi := i % len(f.sp.cloud)
			sz := f.sp.cloudSz[fi]
			if sz.X <= 0 || sz.Y <= 0 {
				continue
			}
			bp, ok := f.bandPlace(i, t, w, h)
			if !ok {
				continue
			}
			// 半影：绕本体中心放大 cloudShadowSizeMul 倍 ⇒ 本体中心与影中心重合。
			// 本体尺寸直接取 bandPlace 给的 dw/dh（同一条落位公式，不另抄一份缩放）。
			sc := bp.dw / float64(sz.X) * cloudShadowSizeMul
			dw, dh := float64(sz.X)*sc, float64(sz.Y)*sc
			out = append(out, cloudShadowPlace{
				spr: fi,
				x:   float64(bp.x) + bp.dw/2 - dw/2,
				y:   float64(bp.y) + bp.dh/2 - dh/2,
				dw:  dw, dh: dh,
				mirror: bp.mirror,
				alpha:  f.bandAlphaOf(i, n) * cloudShadowK,
			})
		}
	}
	// ---- 粒子云 ----
	active := f.ActiveCount()
	ncl := len(f.sp.cloudSz)
	if ncl > 0 {
		for i := 0; i < active && i < len(f.parts); i++ {
			p := &f.parts[i]
			if p.kind != pkCloud {
				continue
			}
			fi := p.spr
			if fi < 0 {
				fi = 0
			}
			if fi >= ncl {
				fi = fi % ncl
			}
			sz := f.sp.cloudSz[fi]
			if sz.X <= 0 || sz.Y <= 0 {
				continue
			}
			scale := spriteScale(p) * cloudShadowSizeMul
			dw, dh := float64(sz.X)*scale, float64(sz.Y)*scale
			if dw < 2 || dh < 2 {
				continue
			}
			out = append(out, cloudShadowPlace{
				spr:    fi,
				x:      p.x*float64(w) - dw/2,
				y:      p.y*float64(h) - dh/2,
				dw:     dw,
				dh:     dh,
				mirror: p.mirror,
				alpha:  f.cloudAlphaOf(p) * cloudShadowK,
			})
		}
	}
	return out
}

// drawCloudShadows 云影层：在画云本体**之前**把 cloudShadowPlaces 投在天空上。
//
// 精灵用 tintedSprite(lift=-255) 的副本：RGB 逐通道 −255 钳到 0 = 纯黑 (0,0,0)，
// alpha 通道逐字不动 ⇒ 影的形状与云本体完全一致（旧的 genCloudShadow 程序团块
// 与云体贴图轮廓不匹配、从边缘露出来 = 看到的"边线"，那条已删除，这里不再走那条路）。
func (f *Field) drawCloudShadows(gtx layout.Context, w, h int, t float64) {
	places := f.cloudShadowPlaces(w, h, t)
	for _, pl := range places {
		if pl.alpha <= 0.01 {
			continue
		}
		sop, osz, ok := f.sp.tintedSprite(-255, pl.spr, false)
		if !ok {
			continue
		}
		fx := pl.dw / float64(osz.X)
		fy := pl.dh / float64(osz.Y)
		px, py := pl.x, pl.y
		if pl.mirror {
			fx = -fx
			px += pl.dw
		}
		tr := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(fx), float32(fy))).
			Offset(f32.Pt(float32(px), float32(py)))
		ts := op.Affine(tr).Push(gtx.Ops)
		st := paint.PushOpacity(gtx.Ops, float32(pl.alpha))
		cl := clip.Rect(image.Rect(0, 0, osz.X, osz.Y)).Push(gtx.Ops)
		sop.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		if f.probeCount {
			f.draws++ // P1 探针计数：云影层（云带影 + 粒子云影，drawCloudShadows）
		}
		cl.Pop()
		st.Pop()
		ts.Pop()
	}
}

func (f *Field) drawCloudBands(gtx layout.Context, w, h int, t float64) {
	if len(f.sp.cloud) == 0 || len(f.sp.cloudSz) == 0 {
		return
	}
	n := f.bandCount()
	for i := 0; i < n; i++ {
		fi := i % len(f.sp.cloud)
		sz := f.sp.cloudSz[fi]
		if sz.X <= 0 || sz.Y <= 0 {
			continue
		}
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
		// alpha 口径抽到 bandAlphaOf（云影层要用同一个值，两处不能各写一份）。
		alpha := f.bandAlphaOf(i, n)
		// 云带纵向铺开范围：0.24 把 4 条云带压在顶部 26% 内，
		// 范围做成可调量，供出图对比挑选（指定认可 0.24）。
		// P6 设计第 2 条（绘制调用点 2/2）：sc = T / sz.X 与 bandPlace 同一口径
		// （= bp.dw / sz.X，等宽 T ⇒ 逐带相同；上面注释里的 1.5+0.8·frac 口径作废，
		// alpha/y 的 frac 口径不动，在 bandAlphaOf 与 bandPlace 内部各算各的）。
		sc := bp.dw / float64(sz.X)
		sx := sc
		if bp.mirror { // 奇数索引水平镜像（机制复用）
			sx = -sc
		}
		// （fade 已在 bandAlphaOf 里乘过：类别切换交叉淡出/淡入）
		// 云带的体色提亮由之后的局域提亮层承担（drawFlash 画在云与粒子之后）。
		// 这里**不抬云 alpha**：实测抬 alpha 会把云覆盖处的复合色推向云体色，
		// 在亮天空底上就是变暗（离屏差分实测 (390,80) 处 −8.7 级），
		// 与"闪光把云提亮"相反。
		tr := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(sx), float32(sc))).
			Offset(f32.Pt(float32(bp.x), float32(bp.y)))
		ts := op.Affine(tr).Push(gtx.Ops)
		st := paint.PushOpacity(gtx.Ops, float32(alpha))
		cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
		f.sp.cloud[fi].Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		if f.probeCount {
			f.draws++ // P1 探针计数：云带本体（drawCloudBands）
		}
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

	// 云影层：画在云本体**之前**（层序 sky（含阳光）→ cloud shadow → 云本体 → 其余）。
	// 只在白天档启用：夜间没有阳光可挡，且会把夜空压黑（见 drawCloudShadows）。
	f.drawCloudShadows(gtx, w, h, t)

	// 阴雨类云层厚度：画在粒子之前，
	// 粒子在云带前面飞，形成纵深。
	if f.hasCloudBands() {
		f.drawCloudBands(gtx, w, h, t)
	}

	// 雾霭梯度：底部浓、顶部淡的空气透视（判读"雾像均匀蒙版、无纵深"）
	if f.cls == effect.Fog {
		f.drawHaze(gtx, w, h, t)
	}

	// 闪光（雷暴）：**局域提亮层画在云与粒子之后**（见 drawFlash 的注释），
	// 这里只留口径说明，不再整屏白洗。

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
		if p.kind == pkCloud && f.cls != effect.Clear && alpha < cloudFormOf(f.cls).aLo {
			// 下限 = **该类的 alpha 区间下界**（规格 2 表：多云见日 0.80 / 多云 0.75 /
			// 阴 0.85 / 雨·雷 0.88）。旧口径是写死的 0.92：三档共用一个下限 ⇒
			// 表里的逐档 alpha 被兜底抹平（多云 0.75–0.90 整档被抬成 ≥0.92）。
			// 下限限定在非 Clear：这些类别的云要盖住日/月（图层律不动）。
			alpha = cloudFormOf(f.cls).aLo
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
		// 雨（pkDrop）= 贴图雨丝（规格 1.3）。透明度口径 = 景深驱动的 headA
		// （远淡近浓，0.12 → 1.00），长度口径 = vy × 快门时间（运动模糊）。
		// 程序化雨线（clip.Stroke + 线性渐变）已删除。
		if p.kind == pkDrop {
			sz, img, ok := f.pick(p)
			if !ok {
				continue
			}
			headA := (0.12 + p.depth*p.depth*0.88) * p.brightnessMul * p.alphaMul * f.fade
			f.drawPrecipSprite(gtx, w, h, p, img, sz, headA, true)
			continue
		}
		// 空气透视：越远越淡（判读"雨丝缺乏近大远小""雪像屏幕灰尘"
		// 的共性根因是远近粒子一样亮
		if p.kind == pkFlake || p.kind == pkMote {
			alpha *= 0.25 + 0.75*p.depth
		}
		// **雪花 alpha 下限 0.60**（规格 3：alpha 下限保证在亮雾底上可见）。
		// 实测改前：p.alpha 0.40–0.95 经实例抖动 [0.65,1.35]×[0.50,1.00] 与
		// 空气透视 (0.25+0.75·depth) 连乘后 = **0.03–0.95** ⇒ 远层雪片几乎全透明，
		// 叠在雪天基色 (168,184,200) 与灰幕上 = 看不见（取证 35.1：alpha 0.42–0.93
		// 是抖动**前**的生成值）。0.60 = 亮雾底上可辨的最小档（未逐档取证，见 notes）。
		if p.kind == pkFlake && f.cls == effect.Snow && alpha < flakeAlphaMin {
			alpha = flakeAlphaMin
		}
		if alpha > 1 {
			alpha = 1
		}
		// **闪光局域化（D3 第 3 条）：云 alpha 跟着提亮**。
		// 锚点半径内的云在闪光时变实（alpha 抬升），半径外不动；
		// 体色的提亮由之后的局域提亮层承担（drawFlash 画在云与粒子之后）。
		if f.flash > 0.01 && p.kind == pkCloud {
			if m := f.flashMaskAt(p.x, p.y); m > 0 {
				alpha *= 1.0 + flashBoostMax*m*f.flash
				if alpha > 1 {
					alpha = 1
				}
			}
		}
		// 雪（Snow 的 pkFlake）= 贴图柔焦雪点（规格 1.2）。
		// 旧写法在这里改画覆盖目录/内嵌的 rain.png：雪粒子拿的是雨丝贴图，
		// 屏幕上落下来的就是雨丝（"雪怎么下的是雨"的直接机制）。
		// 本分支删除后，雪只走 pick() 给的 snow1..3。

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
		if f.probeCount {
			f.draws++ // P1 探针计数：粒子本体（含雾团，Draw 粒子循环）
		}
		cl.Pop()
		st.Pop()
		ts.Pop()
	}

	// 闪光（雷暴）：**局域提亮层**，画在云带与粒子之后
	// ⇒ 半径内的天空与云一起被抬（画在粒子之前就只有天空被抬、云不抬）。
	f.drawFlash(gtx, w, h)

	// 闪电（雷暴）：闪光起头时画一道，随 flash 淡出。
	// 可见阈值 0.25 → boltFlashMin(0.12)：低幅度闪光时也要看得见电。
	if f.cls == effect.Thunder && f.flash > boltFlashMin {
		f.drawBolt(gtx, w, h, f.flash)
	}

	// 月亮（晴夜）：**已烘进天空纹理**（ui.renderSkyCPU → bakeCelestial），
	// 每帧零成本。原每帧 64 圆盘 = 5.7 个整屏当量，是 Clear 夜的最大单项开销。
	// 保留 drawMoon 函数本体供离线路径与回退使用，生产路径不再逐帧调用。
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

// ── 降水粒子贴图覆盖（预览工装）──────────────────────────
//
// 依据：素材预览工装规格 1.5。覆盖目录里有 rain.png ⇒ 工装把这张贴图喂给
// drawPrecipSprite 做离屏判读；出厂路径不吃它（雨走 rain1..7、雪走 snow1..3）。
//
// 不许动的口径：粒子速度（vy）、密度（数量与生成器）、透明度
// （雨 = 景深驱动的 headA = 0.12 + depth²·0.88）全部沿用现有公式，
// 只把"画什么形状"换成贴图。
//
// 贴图长轴约定：贴图的竖直方向（Y）= 下落方向，短轴按贴图自身宽高比等比导出
// 后按张封顶（rainStreakCapPx）。旋转 = atan2(vx, vy) = 粒子自己的下落方向。

const rainOverrideFile = "rain.png"

// rainTexMemo 逐帧路径的查表缓存：按覆盖目录键控，目录不变就纯查表。
var rainTexMemo = struct {
	mu     sync.Mutex
	dir    string
	filled bool
	img    paint.ImageOp
	sz     image.Point
	ok     bool
}{}

// rainTexOverride 预览工装的雨丝贴图：**覆盖目录里的同名文件优先，其次内嵌 embed/rain.png**。
// 未启用 / 不存在 / 解码失败 ⇒ false。
//
// 现状：出厂绘制路径已改走 pick() 给的 rain1..7（规格 1.3），
// 本函数只服务覆盖目录预览工装（assetdir.go 的 assetImageAny 链路）与它的自检。
func rainTexOverride() (paint.ImageOp, image.Point, bool) {
	dir := assetDir()
	rainTexMemo.mu.Lock()
	defer rainTexMemo.mu.Unlock()
	if rainTexMemo.dir != dir || !rainTexMemo.filled {
		_, img, sz, ok := assetImageAny(rainOverrideFile)
		rainTexMemo.dir = dir
		rainTexMemo.filled = true
		rainTexMemo.img, rainTexMemo.sz, rainTexMemo.ok = img, sz, ok
	}
	return rainTexMemo.img, rainTexMemo.sz, rainTexMemo.ok
}

// drawPrecipSprite 把一个降水粒子画成贴图（规格 1.3 的雨丝路径）。
//
// useLen = true（雨/雷）：长轴 = 线长口径 L = vy × 快门时间（运动模糊），
// 短轴按贴图宽高比等比导出，再按贴图逐张封顶（rainStreakCapPx）。
// useLen = false：尺寸 = 贴图尺寸 × spriteScale(p) = 通用精灵尺寸口径
// （覆盖目录预览工装走这一支）。
//
// 旋转：gioui 的 Rotate 矩阵把 (0,1) 转到 (−sin, cos)，
// 而雨丝的下落方向向量是 (vx, vy) ⇒ 角度取 −atan2(vx, vy)。
// 变换链与 Draw 里的通用精灵路径同构（Scale → Rotate → Offset，Push/Pop 成对）。
func (f *Field) drawPrecipSprite(gtx layout.Context, w, h int, p *particle, img paint.ImageOp, sz image.Point, alpha float64, useLen bool) {
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	if alpha <= 0.01 {
		return
	}
	var dw, dh float32
	if useLen {
		L := p.vy * float64(h) * rainExposure
		if L < 3 {
			L = 3
		}
		dh = float32(L)
		dw = float32(float64(dh) * float64(sz.X) / float64(sz.Y))
		// 短轴按贴图逐张封顶（见 rainStreakCapPx 的实测占比表）：
		// 等比放大时软带占比 0.44 的那张贴图在近档会画成 15 px 宽的糊带。
		if cap := rainStreakCapFor(p.spr); dw > cap {
			dw = cap
		}
	} else {
		scale := spriteScale(p)
		dw = float32(float64(sz.X) * scale)
		dh = float32(float64(sz.Y) * scale)
	}
	if dw < 1 || dh < 1 {
		return
	}
	fx := dw / float32(sz.X)
	fy := dh / float32(sz.Y)
	cx, cy := dw/2, dh/2
	px := float32(p.x*float64(w)) - dw/2
	py := float32(p.y*float64(h)) - dh/2
	// 旋转 = **粒子自己的下落方向**（规格 1.3）：
	// 速度 (vx, vy) 里已经带着风斜率（rainSlantBase·(0.7+0.6·depth)，雨丝向左下），
	// 取 atan2(vx, vy) 就是这条下落方向与竖直方向的夹角；gioui 的 Rotate 把
	// (0,1) 转到 (−sin, cos)，故角度取负才与 (vx, vy) 同向。
	// 每粒子再叠加 windJitterAmp 的固定抖动（由 scaleMul 映射，跨帧稳定）。
	ang := math.Atan2(p.vx, p.vy) + (p.scaleMul-1.0)/0.25*windJitterAmp
	tr := f32.Affine2D{}.
		Scale(f32.Point{}, f32.Point{X: fx, Y: fy}).
		Rotate(f32.Point{X: cx, Y: cy}, float32(-ang)).
		Offset(f32.Pt(px, py))
	ts := op.Affine(tr).Push(gtx.Ops)
	st := paint.PushOpacity(gtx.Ops, float32(alpha))
	cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if f.probeCount {
		f.draws++ // P1 探针计数：drawPrecipSprite 降水贴图
	}
	cl.Pop()
	st.Pop()
	ts.Pop()
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
			// 与贴图雨丝逐字同一套口径（drawPrecipSprite 的 useLen 分支）。
			// 自检证据必须来自真实绘制路径。
			headA := (0.12 + p.depth*p.depth*0.88) * p.brightnessMul * p.alphaMul
			L := p.vy * float64(f.sh) * rainExposure
			if L < 3 {
				L = 3
			}
			dw := float64(L) * float64(sz.X) / float64(sz.Y)
			if cap := rainStreakCapFor(p.spr); dw > float64(cap) {
				dw = float64(cap)
			}
			out += fmt.Sprintf("    rain: depth=%.2f len=%.1f width=%.1f headA=%.3f vy=%.3f ang=%.4f\n",
				p.depth, L, dw, headA, p.vy,
				math.Atan2(p.vx, p.vy)+(p.scaleMul-1.0)/0.25*windJitterAmp)
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
		// **体色运行时着色**（规格 2：晴档提白、雨/雷档压暗，不改贴图文件）：
		// gioui v0.10.3 的 ImageOp 没有颜色乘子 ⇒ 取按 lift 生成的贴图副本
		// （见 sprites.tintedSprite；lift=0 时回落原贴图，零副本）。
		if op, sz, ok := f.sp.tintedSprite(cloudFormOf(f.cls).lift, i, false); ok {
			return sz, op, true
		}
		return f.sp.cloudSz[i], f.sp.cloud[i], true
	case pkFog:
		if len(f.sp.fog) == 0 {
			i := clamp(p.spr, len(f.sp.cloud))
			return f.sp.cloudSz[i], f.sp.cloud[i], true
		}
		i := clamp(p.spr, len(f.sp.fog))
		// 雪档的雾 = **中灰均匀**（规格 2 表）：雾贴图实测是纯白
		// （fog1..4 实心像素反预乘均色 (255,255,255)、alpha 178），
		// 雪天天空基色 (168,184,200) ⇒ 白幕 + 白点 = 没有对比 ⇒ 压成中灰 −70。
		// 其余档 lift=0 ⇒ 走原贴图（现口径不动）。
		lift := 0.0
		if f.cls == effect.Snow {
			lift = -70
		}
		if op, sz, ok := f.sp.tintedSprite(lift, i, true); ok {
			return sz, op, true
		}
		return f.sp.fogSz[i], f.sp.fog[i], true
	case pkFlake:
		i := clamp(p.spr, len(f.sp.flake))
		return f.sp.flakeSz[i], f.sp.flake[i], true
	case pkDrop:
		// 雨丝贴图（规格 1.3）。贴图组缺失时不画（程序化雨线已按规格删除，
		// 不保留第二套画法）。
		if n := len(f.sp.rain); n > 0 {
			i := clamp(p.spr, n)
			return f.sp.rainSz[i], f.sp.rain[i], true
		}
		return image.Point{}, paint.ImageOp{}, false
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

// drawBolt 画一道闪电（规格 1.5：贴图 bolt1..3），亮度跟随闪现包络。
//
// 三条口径：
//
//	1 贴图 = embed/bolt1..3（Kenney spark_01..03 的白热闪电枝），
//	   逐次闪现换一张（boltIdx 在闪现开始时抽好，闪现期间不变 ⇒ 不逐帧跳）；
//	   贴图组缺失时退到 genBolt 的程序折线（退路，出厂路径走不到）；
//	2 落位 = 闪现开始时抽好的随机点（boltX/boltY，见常量注释），
//	   长轴 = 0.28 屏高，短轴按贴图宽高比等比；
//	3 峰值 alpha ≤ 0.75（boltAlphaMax）：透明度 = 0.75 × 包络，包络峰值 1。
func (f *Field) drawBolt(gtx layout.Context, w, h int, flash float64) {
	f.boltDrawn++
	img, sz := f.sp.bolt, f.sp.boltSz
	if i := f.boltSpriteIndex(); i >= 0 {
		img, sz = f.sp.boltOps[i], f.sp.boltSzOps[i]
	}
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	dw, dh := boltRectPx(sz, w, h)
	px := f.boltX*float64(w) - dw/2
	py := f.boltY*float64(h) - dh/2
	a := boltAlphaMax * math.Min(1.0, math.Max(0.0, flash))
	if a <= 0.01 {
		return
	}
	ts := op.Affine(f32.Affine2D{}.
		Scale(f32.Point{}, f32.Point{X: float32(dw / float64(sz.X)), Y: float32(dh / float64(sz.Y))}).
		Offset(f32.Pt(float32(px), float32(py)))).Push(gtx.Ops)
	st := paint.PushOpacity(gtx.Ops, float32(a))
	cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if f.probeCount {
		f.draws++ // P1 探针计数：drawBolt 闪电本体
	}
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
// 同带 = |Δy| ≤ 0.12；P6b 起重叠判据 = 中心距 < 两半宽之和 + 间隙（宽度感知，
// 与 resampleAir / DriftOverlapIncl 同一几何；旧口径 0.22 只看中心距）。
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
				math.Abs(f.parts[i].x-f.parts[j].x) < f.airGapNeededN(&f.parts[i], &f.parts[j]) {
				pairs++
			}
		}
	}
	return
}

// ---- D3 闪光局域化 ----
//
// 旧口径 = 整屏白洗（PushOpacity(flash) + paint.Fill 满屏 alpha）：
// 屏幕上每个像素一起变白，云与天空同涨同落，读成"整块屏幕闪一下"。
// 现口径 = 以闪电锚点为中心的局部提亮：
//
//	锚点 = 本次闪电的落点 (boltX, boltY)（闪现开始时抽好、闪现期间不动）；
//	       锚点没抽过时取云带顶部的随机 x、y = 0.12 屏高
//	形状 = 闪电本体（boltGlowMask：当前那张闪电贴图高斯模糊烘出的形状掩膜），
//	       不再是圆（旧圆罩见下面 boltGlow 的口径说明）
//	幅度 = 中心 0.30 上限（预览基准：0.30 档 = 非背景像素 44.8% 的观感）
//	时长 = 单次 0.15–0.30 s 不动（走 boltEnvelope，见 stepBolt）
//	对象 = 提亮范围内的天空与云：云 alpha 在粒子循环里按 flashMaskAt
//	       （0.35 屏宽的 cos² 圆）抬升（云变实），
//	       体色由提亮层抬（提亮层画在云与粒子之后，落在云像素上）
//
// dev 开关 FX_FLASH 的语义 = 强制一次局域闪光（见 SetFlash）。
const (
	flashRadiusFrac = 0.35 // 云 alpha 抬升掩膜的半径 = 0.35 × 屏宽（云变实的范围，不是光的形状）
	flashAmpMax     = 0.30 // 提亮幅度上限（预览基准 0.30 档，不动）
	flashBoostMax   = 0.60 // 云 alpha 的抬升系数上限（掩膜满处 alpha ×(1+0.60)，不动）
	flashAnchorY    = 0.12 // 锚点没抽过时用的 y（云带顶部）
)

// ---- 闪电形状光罩 boltGlow ----
//
// 需求原话：闪电的光线是圆的，而发光体就是闪电本身。
// 实测根因：提亮掩膜 = 圆罩贴图（Kenney Light Masks circle_a 的角向平均剖面，
// 宽 0.70 倍屏宽）⇒ 提亮形状是一个圆，与闪电本体的走向没有任何关系。
//
// 现口径 = 把本次闪现用的那张闪电贴图（bolt1..3 之一）的 alpha 做高斯模糊
// （σ = boltGlowSigmaFrac × 屏宽 = 0.03 倍屏宽）烘成形状掩膜 boltGlow：
//
//	形状 = 闪电本体（沿闪电走向），掩膜按最大值归一 ⇒ 最亮处就是闪电本体；
//	锚点 = 闪电落点 (boltX, boltY)（贴图中心 = 落点，口径不动）；
//	绘制矩形 = 闪电本体那一块（长轴 0.28 屏高 = boltLenFrac，短轴按贴图宽高比）；
//	幅度 = flash × flashAmpMax（0.30 上限不动）；
//	云 alpha 抬升（flashMaskAt 的 0.35 屏宽 cos² 圆 + 系数 0.60）不动。
//
// 圆罩贴图退役：embed 目录里的文件保留，代码里不再引用（见 out/notes 的说明）。
const (
	boltGlowSigmaFrac = 0.03 // 高斯模糊 σ = 0.03 倍屏宽（屏幕像素）
	boltGlowMaskN     = 192  // 掩膜栅格边长（像素）
	// 闪电本体的可见阈值：0.25 → 0.12。
	// 实测根因：本体只在 flash>0.25 才画，而闪现包络在 0.25 以下的区间占整次的
	// 大半 ⇒ 低幅度闪光时"只见亮不见电"。
	boltFlashMin = 0.12
)

// ---- 云影层（云遮挡阳光）----
//
// 需求："你那个光线居然没有被云给影响。又是以前的毛病，云没有盖住影响背后的阳光。"
// 实测根因：云画在天空纹理之上（层序 sky → field → …），云精灵 alpha < 1
// ⇒ 天空从云后面透出来，云覆盖处的阳光没有被削弱。
//
// 口径：在场层画云本体**之前**，为每朵粒子云与每条云带在天空上投软阴影——
// 同一张精灵、颜色 (0,0,0)、alpha = 云 alpha × cloudShadowK、尺寸放大 1.15 倍（半影）。
// 层序：sky（含阳光）→ cloud shadow → 云本体 → 其余（现有层序不动，只插这一层）。
// 只在白天档启用：夜间没有阳光可挡，且会把夜空压黑。
const (
	cloudShadowK       = 0.45 // 出厂值：影 alpha = 云 alpha × 0.45
	cloudShadowSizeMul = 1.15 // 半影：影比云本体大 15%
)

// flashMaskAt 局域提亮在归一化坐标 (x,y) 处的掩膜 0..1。
// 距离按**屏宽**归一（半径是 0.35 倍屏宽，纵向同分母 ⇒ 各向同性的圆）。
func (f *Field) flashMaskAt(x, y float64) float64 {
	d := math.Hypot(x-f.boltX, y-f.boltY) / flashRadiusFrac
	if d >= 1.0 {
		return 0
	}
	c := math.Cos(d * math.Pi * 0.5)
	return c * c
}

// boltSpriteIndex 本次闪现用的闪电贴图下标（与 drawBolt 的选图口径同一份，两处不各写一份）。
// 返回 -1 = 闪电贴图组没落地（drawBolt 走 genBolt 的程序折线退路）。
func (f *Field) boltSpriteIndex() int {
	n := len(f.sp.boltOps)
	if n == 0 {
		return -1
	}
	i := f.boltIdx
	if i >= n {
		i = 0
	}
	return i
}

// boltRectPx 闪电本体在屏幕上的绘制矩形（与 drawBolt 同口径）：
// 长轴 = boltLenFrac × 屏高，短轴按贴图宽高比等比。
func boltRectPx(sz image.Point, w, h int) (float64, float64) {
	dh := boltLenFrac * float64(h)
	return dh * float64(sz.X) / float64(sz.Y), dh
}

// gaussKernel 归一化一维高斯核（σ 单位 = 栅格像素，半径 3σ，端点钳到 0）。
func gaussKernel(sigma float64) []float64 {
	r := int(math.Ceil(3.0 * sigma))
	if r < 1 {
		r = 1
	}
	k := make([]float64, 2*r+1)
	sum := 0.0
	for i := range k {
		x := float64(i - r)
		k[i] = math.Exp(-x * x / (2.0 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

// boltGlowKey 形状掩膜的缓存键（同一张闪电 + 同一幅面只烘一次）。
type boltGlowKey struct {
	idx, w, h int
}

type boltGlowEntry struct {
	op        paint.ImageOp
	mask      *image.NRGBA // 掩膜本体（自检取用：判据要能算出屏幕上那一层的形状）
	dw, dh    float64      // 屏幕上的绘制矩形（与闪电本体同一块）
	peakAtC   float64      // 归一后掩膜在矩形中心处的取值（自检打印用）
	builtMSec float64
	ok        bool
}

var boltGlowCache struct {
	mu  sync.Mutex
	ent map[boltGlowKey]*boltGlowEntry
}

// boltGlowMask 把第 idx 张闪电贴图（bolt1..3）的 alpha 高斯模糊烘成形状掩膜。
//
// 步骤（全部在屏幕像素口径下做，与屏幕上看到的一致）：
//  1. 在 boltGlowMaskN 的栅格上双线性采出闪电本体的 alpha（栅格铺满闪电的绘制矩形）；
//  2. separable 高斯模糊，σ = boltGlowSigmaFrac × 屏宽 换算到栅格像素；
//  3. 按最大值归一 ⇒ 最亮处 = 闪电本体，掩膜峰值对应提亮幅度 flashAmpMax；
//  4. RGB 写固定的冷白（与太阳泛光/宽光同一条口径：源图 RGB 一个字节都不写进屏幕），
//     alpha = 掩膜值。
//
// 缓存 = (贴图下标, 屏宽, 屏高)，逐帧零生成成本；一次烘的成本 = 栅格² × 核长 × 2。
// 贴图没落地 ⇒ ok=false（调用方不画提亮层，退回"只见闪电本体"）。
func boltGlowMask(idx, w, h int) *boltGlowEntry {
	k := boltGlowKey{idx: idx, w: w, h: h}
	boltGlowCache.mu.Lock()
	defer boltGlowCache.mu.Unlock()
	if boltGlowCache.ent == nil {
		boltGlowCache.ent = map[boltGlowKey]*boltGlowEntry{}
	}
	if e, ok := boltGlowCache.ent[k]; ok {
		return e
	}
	t0 := time.Now()
	e := &boltGlowEntry{}
	boltGlowCache.ent[k] = e
	name := "bolt" + itoa(idx+1) + ".png"
	src := spriteImg(name)
	if src == nil {
		return e
	}
	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	dw, dh := boltRectPx(sb.Size(), w, h)
	if dw <= 0 || dh <= 0 {
		return e
	}
	n := boltGlowMaskN
	if n < 8 {
		n = 8
	}
	srcA := make([]float64, n*n)
	at := func(x, y int) float64 {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x > int(sw)-1 {
			x = int(sw) - 1
		}
		if y > int(sh)-1 {
			y = int(sh) - 1
		}
		return float64(src.Pix[src.PixOffset(sb.Min.X+x, sb.Min.Y+y)+3]) / 255.0
	}
	for j := 0; j < n; j++ {
		fy := (float64(j) + 0.5) / float64(n) * sh
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		for i := 0; i < n; i++ {
			fx := (float64(i) + 0.5) / float64(n) * sw
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			v := (at(x0, y0)*(1-tx)+at(x0+1, y0)*tx)*(1-ty) +
				(at(x0, y0+1)*(1-tx)+at(x0+1, y0+1)*tx)*ty
			srcA[j*n+i] = v
		}
	}
	// σ：屏幕像素 → 栅格像素（栅格铺满 dw，故 1 栅格像素 = dw/n 屏幕像素）
	sig := boltGlowSigmaFrac * float64(w) * float64(n) / dw
	if sig < 0.5 {
		sig = 0.5
	}
	kk := gaussKernel(sig)
	tmp := make([]float64, n*n)
	blurred := make([]float64, n*n)
	conv := func(in, out []float64, horiz bool) {
		half := (len(kk) - 1) / 2
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				var s float64
				for t, kv := range kk {
					o := t - half
					ii, jj := i, j
					if horiz {
						ii = i + o
						if ii < 0 {
							ii = 0
						} else if ii >= n {
							ii = n - 1
						}
					} else {
						jj = j + o
						if jj < 0 {
							jj = 0
						} else if jj >= n {
							jj = n - 1
						}
					}
					s += kv * in[jj*n+ii]
				}
				out[j*n+i] = s
			}
		}
	}
	conv(srcA, tmp, true)
	conv(tmp, blurred, false)
	peak := 0.0
	for _, v := range blurred {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return e
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			a := blurred[j*n+i] / peak
			if a > 1.0 {
				a = 1.0
			}
			p := img.PixOffset(i, j)
			img.Pix[p+0] = 235
			img.Pix[p+1] = 240
			img.Pix[p+2] = 255
			img.Pix[p+3] = uint8(math.Round(a * 255))
		}
	}
	e.op = paint.NewImageOp(img)
	e.mask = img
	e.dw, e.dh = dw, dh
	e.peakAtC = blurred[(n/2)*n+n/2] / peak
	e.builtMSec = float64(time.Since(t0).Microseconds()) / 1000.0
	e.ok = true
	return e
}

// drawFlash 画局域提亮层：以闪电落点 (f.boltX, f.boltY) 为中心、
// 铺在闪电本体那块矩形上（长轴 0.28 屏高、短轴按贴图宽高比）的**形状**提亮，
// 峰值幅度 = flash × 0.30（flash 是闪现包络 0..1）。
//
// 掩膜 = boltGlowMask（当前那张闪电贴图高斯模糊烘出的形状掩膜）⇒ 提亮沿闪电走向，
// 不再是圆罩（需求：发光体就是闪电）。锚点口径不动。
//
// 绘制链与云精灵同构（Scale → Offset，Push/Pop 成对）；
// 掩膜是 straight alpha 的 NRGBA，gioui 上传时预乘 ⇒ src-over 叠加 = 向白色抬。
func (f *Field) drawFlash(gtx layout.Context, w, h int) {
	if f.flash <= 0.01 {
		return
	}
	amp := f.flash * flashAmpMax
	if amp <= 0.002 {
		return
	}
	idx := f.boltSpriteIndex()
	if idx < 0 {
		return
	}
	e := boltGlowMask(idx, w, h)
	if !e.ok {
		return
	}
	sz := e.op.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	cx, cy := f.boltX*float64(w), f.boltY*float64(h)
	tr := f32.Affine2D{}.
		Scale(f32.Point{}, f32.Pt(float32(e.dw/float64(sz.X)), float32(e.dh/float64(sz.Y)))).
		Offset(f32.Pt(float32(cx-e.dw/2), float32(cy-e.dh/2)))
	ts := op.Affine(tr).Push(gtx.Ops)
	st := paint.PushOpacity(gtx.Ops, float32(amp))
	cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
	e.op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if f.probeCount {
		f.draws++ // P1 探针计数：drawFlash 局域提亮层
	}
	cl.Pop()
	st.Pop()
	ts.Pop()
}

// SetWeatherCode 传入该场对应的 WMO 码（调用方每帧传，值变了才生效）。
//
// 码决定闪现间隔的档位（boltGapForCode）：95 ⇒ 15–40 s、96 ⇒ 10–25 s、
// 97/99 ⇒ 6–15 s、其余码 ⇒ 不闪。码换档时重抽第一次的等待时间
// （正在落的那一次不受影响，让它按包络落完）。
func (f *Field) SetWeatherCode(code int) {
	if code == f.wxCode {
		return
	}
	f.wxCode = code
	lo, hi, ok := boltGapForCode(code)
	if !ok {
		f.boltNext = math.Inf(1) // 非雷暴码 ⇒ 不闪
		return
	}
	if !f.boltOn {
		f.boltNext = lo + (hi-lo)*f.rng.Float64()
	}
}

// WeatherCode 读出当前码（判读与自检用）。
func (f *Field) WeatherCode() int { return f.wxCode }

// SetFlash 仅供离线渲染测试：强制设定闪光强度（FX_FLASH = 强制一次局域闪光）。
// 锚点没抽过（boltX/boltY 全零）⇒ 按口径补一个：云带顶部的随机 x、y = 0.12 屏高。
func (f *Field) SetFlash(v float64) {
	if v > 0 && f.boltX == 0 && f.boltY == 0 {
		f.boltX = boltXLo + (boltXHi-boltXLo)*f.rng.Float64()
		f.boltY = flashAnchorY
	}
	f.flash = v
}

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
	if f.probeCount {
		f.draws++ // P1 探针计数：drawHaze 雾霭梯度
	}
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

// ---- B3 进场排队闸门（R1"决不能同出同进"，spec/b3-kinetics.md §3 最后一条）----
//
// cloudEntryRec 一条进场流水：步号 + 粒子索引 + 进场那一刻的中心 x 与半宽（**px 口径**，
// 与 kinetics.EntryOK 的入参同单位）。placeAirEntry / driftRecycleOffscreen 发射进场位时写入，
// 探针 ui/kinetics_rules_test.go 直接读 Field.entryLog 判"同帧进场样本数"。
type cloudEntryRec struct {
	Step   int64   // 固定步号（f.steps 口径 = int64）
	Idx    int     // parts 下标
	Xpx    float64 // 进场那一刻的中心 x（px）
	HalfPx float64 // 该朵半宽（px）
}

// cloudParkGapN 保持 parked 时**额外**退到屏外的距离（屏宽占比，@1080 = 54 px）。
// 只决定"下一帧再试"时这朵云停在哪：进场位之外再多退 0.05 屏宽，位移/回收判据都不碰它
// （vx≥0 的出屏判据是 x−hw>1.0、vx<0 的是 x+hw<0， parked 位恒不满足）。
const cloudParkGapN = 0.05

// cloudEntryPlace 现成进场位口径（逐字沿用 placeAirEntry / driftRecycleOffscreen 的两条）：
// vx≥0 ⇒ 整朵贴屏幕左沿外 x = −hw；vx<0 ⇒ 贴右沿外 x = 1+hw。
func (f *Field) cloudEntryPlace(p *particle) (x, hw float64) {
	hw, _ = f.airHalfN(p)
	if p.vx >= 0 {
		x = -hw
	} else {
		x = 1 + hw
	}
	return
}

// cloudEntryGateOK 闸门判据：同帧（同一步号）已有上一条进场 ⇒ 必须过 kinetics.EntryOK。
// 上一条不在同一帧 ⇒ 不是"同出同进"，放行（R1 允许追上）。
func (f *Field) cloudEntryGateOK(xEntry, hw float64) bool {
	if !f.entryHas || f.entryLastStep != f.steps {
		return true
	}
	w, _ := screenfit.Fallback(f.sw, 1)
	return kinetics.EntryOK(xEntry*float64(w), hw*float64(w), f.entryLastX, f.entryLastHalf, w)
}

// emitCloudEntry 发射进场位：摆 x + 记进场流水 + 从 parked 名单除名。
func (f *Field) emitCloudEntry(p *particle, xEntry, hw float64) {
	w, _ := screenfit.Fallback(f.sw, 1)
	p.x = xEntry
	f.entryHas = true
	f.entryLastStep = f.steps
	f.entryLastX = xEntry * float64(w)
	f.entryLastHalf = hw * float64(w)
	f.entryLog = append(f.entryLog, cloudEntryRec{Step: f.steps, Idx: p.idx, Xpx: p.x * float64(w), HalfPx: hw * float64(w)})
	delete(f.parkedCloud, p.idx)
}

// parkCloudEntry 闸门不合格 ⇒ 保持 parked：整朵停在进场位之外，下一帧再试（本帧不发射）。
func (f *Field) parkCloudEntry(p *particle, xEntry, hw float64) {
	if f.parkedCloud == nil {
		f.parkedCloud = make(map[int]bool)
	}
	f.parkedCloud[p.idx] = true
	if p.vx >= 0 {
		p.x = xEntry - cloudParkGapN
	} else {
		p.x = xEntry + cloudParkGapN
	}
	_ = hw
}

// releaseParkedClouds 每个固定步把 parked 名单里最前面那一朵再试一次：闸门放行才发射。
// 一帧只发射一朵 ⇒ 同帧进场对恒为 0（"排队"的字面口径）。
func (f *Field) releaseParkedClouds() {
	if len(f.parkedCloud) == 0 {
		return
	}
	for i := range f.parts {
		if !f.parkedCloud[i] {
			continue
		}
		p := &f.parts[i]
		if p.kind != pkCloud {
			delete(f.parkedCloud, i)
			continue
		}
		x, hw := f.cloudEntryPlace(p)
		if !f.cloudEntryGateOK(x, hw) {
			continue
		}
		f.emitCloudEntry(p, x, hw)
		return
	}
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
	// 停车也要在**绕回周期里错峰**：与同带同类层保持 ≥ 半宽和 + 间隙，
	// 否则两张幕布都停在左沿外 = 周期里相叠（实测 Snow 400 步含粒子 43.74%）。
	if p.vx >= 0 {
		p.x = -hw
		for j := range f.parts {
			q := &f.parts[j]
			if q == p || q.kind != p.kind || !isAir(q.kind) {
				continue
			}
			if math.Abs(q.y-p.y) > airBandN {
				continue
			}
			hq, _ := f.airHalfN(q)
			if need := q.x - (hw + hq + airSlotGapN); need < p.x && q.x > p.x {
				p.x = need
			}
		}
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
			// 进场 x = **整朵恰好贴在屏幕外**（vx>0 → x = −hw，vx<0 → x = 1+hw）：
			// 右（左）边缘正好落在屏幕左（右）边界上 ⇒ 进场那一刻零可见宽度，
			// 不闪现（自检：TestAirEntryRecycleIsOffscreen 要求 x ≤ −hw，
			// TestAirEntryPlacement 要求 x ≥ −hw ⇒ 两条合起来把进场位钉在 x = −hw）。
			//
			// 旧口径在这里再减一个固定 0.35 屏宽做"铺开"，依据是"8 朵云全挤在
			// 同一个左沿起点 ⇒ 同进同出"。改大后 hw = 0.267–0.681 屏宽，
			// −hw−0.35 = −0.62…−1.03 ⇒ 云要在屏外走掉大半屏才进屏，
			// 且直接撞上面两条自检的边界。
			// 现口径：错峰由**每朵自身的 hw**（尺寸抖动 scaleMul 与 sc 导出，
			// 逐朵差最多 0.41 屏宽 = 445 px）与 vx 分布（0.003–0.014，4.7 倍）承担：
			// 以 vx≈0.008/s 计，hw 相差 0.41 ⇒ 进屏时刻相差约 51 s。
			// B3 进场排队闸门（R1"决不能同出同进"）：进场位发射前过 kinetics.EntryOK，
			// 同帧不合格 ⇒ 保持 parked（整朵停在进场位之外），releaseParkedClouds 下一帧再试。
			x, hwE := f.cloudEntryPlace(p)
			if !f.cloudEntryGateOK(x, hwE) {
				f.parkCloudEntry(p, x, hwE)
				return
			}
			f.emitCloudEntry(p, x, hwE)
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

// cloudSpriteSz 该朵云用的贴图原生尺寸（按类挑选后的下标）。
func cloudSpriteSz(spr int) image.Point {
	sp := spritesOnce()
	if spr >= 0 && spr < len(sp.cloudSz) {
		return sp.cloudSz[spr]
	}
	if len(sp.cloudSz) > 0 {
		return sp.cloudSz[0]
	}
	return image.Pt(512, 384)
}

// cloudDrawWidthPx 该朵云**画出来**的屏宽（px），与 Draw 的 dw 逐字同口径：
// dw = 源图宽 × spriteScale = 源图宽 × p.sc × scaleMul ÷ bandNetDiv。
func cloudDrawWidthPx(p *particle) float64 {
	return float64(cloudSpriteSz(p.spr).X) * spriteScale(p)
}

// sizeCloudByClass 按规格 2 的逐类屏宽区间反解 p.sc，并按薄厚定速度（C1 第 1 条）。
//
// 反解式（与 spriteScale 的 pkCloud 分支互逆）：
//
//	屏宽 px = 源图宽 × p.sc × scaleMul ÷ bandNetDiv
//	⇒ p.sc = 屏宽 px × bandNetDiv ÷ (源图宽 × scaleMul)
//
// ⇒ 逐朵屏宽**逐字落在表内区间**（抖动 scaleMul 被反解掉，不再把区间撑宽）。
// 速度：dw 归一化 = 屏宽 px ÷ 屏宽 px（与 airHalfN 的 hw 同口径），
// vx = cloudVxByThickness(dw) ⇒ 薄小快、厚大慢，且逐朵确定（不随机）。
// enforceLandscapeCloudWidth 横屏在场云的宽度复查：实画宽越出横屏 kinetics 区间
// （lo = 0.444W = 853 px @1920、hi = (W − 3×0.0194W)/2 = 904 px @1920）的云，
// 按 sizeCloudByClass 的**同一套反解式**重解 p.sc（速度沿用 cloudVxByThickness 原式，
// 只是随新宽度取点 ⇒ 粒子运动口径逐字不动）。落在区间内的云逐条不动。
func (f *Field) enforceLandscapeCloudWidth() {
	if !IsLandscape(f.sw, f.sh) {
		return
	}
	lo, hi := landscapeCloudWidthRangePx(f.sw)
	for i := range f.parts {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		w := cloudDrawWidthPx(p)
		if w >= float64(lo) && w <= float64(hi) {
			continue
		}
		szX := float64(cloudSpriteSz(p.spr).X)
		if szX <= 0 || p.scaleMul <= 0 {
			continue
		}
		dwPx := float64(lo) + (float64(hi)-float64(lo))*f.rng.Float64()
		p.sc = dwPx * bandNetDiv / (szX * p.scaleMul)
		p.vx = cloudVxByThickness(dwPx / float64(f.sw))
		p.vxBase = p.vx
	}
}

func (f *Field) sizeCloudByClass(p *particle) {
	cf := cloudFormOf(f.cls)
	lo, hi := cf.wLo, cf.wHi
	if IsLandscape(f.sw, f.sh) {
		// 缺陷 L1/L4（spec/b4-landscape.md:18-19）：横屏逐朵**最终显示宽度**走
		// kinetics 的屏宽分制（下限 0.444W=853 px @1920，上限留边距 ⇒ 904 px @1920）。
		fw, _ := screenfit.Fallback(f.sw, f.sh)
		l, hiPx := landscapeCloudWidthRangePx(fw)
		lo, hi = float64(l), float64(hiPx)
	}
	dwPx := lo + (hi-lo)*f.rng.Float64()
	szX := float64(cloudSpriteSz(p.spr).X)
	if szX <= 0 || p.scaleMul <= 0 {
		return
	}
	p.sc = dwPx * bandNetDiv / (szX * p.scaleMul)
	fw, _ := screenfit.Fallback(f.sw, 1)
	sw := float64(fw)
	p.vx = cloudVxByThickness(dwPx / sw)
	p.vxBase = p.vx
}

// sizeFlakeByTarget 把雪花屏上尺寸反解到 [12, 26] px（规格 3）。
//
//	屏上 dw = 源图宽 × sc × (0.55+0.45·depth) × scaleMul ÷ spriteNetDiv
//	⇒ sc = 目标px × spriteNetDiv ÷ (源图宽 × (0.55+0.45·depth) × scaleMul)
//
// 实测改前：源 18/26/36 px、sc 0.30–1.00、depth 因子 0.55–1.0、scaleMul 0.75–1.25
// ⇒ 屏上 dw = 2.3–12.8 px（取证 35.1）⇒ 1080×1920 上是 2–13 px 的白点。
// 数量与下落速度口径不动（规格 3 逐字）。
func (f *Field) sizeFlakeByTarget(p *particle) {
	sp := spritesOnce()
	szX := float64(sp.flakeSz[0].X)
	if p.spr >= 0 && p.spr < len(sp.flakeSz) {
		szX = float64(sp.flakeSz[p.spr].X)
	}
	den := szX * (0.55 + 0.45*p.depth) * p.scaleMul
	if szX <= 0 || den <= 0 {
		return
	}
	target := flakePxLo + (flakePxHi-flakePxLo)*f.rng.Float64()
	p.sc = target * spriteNetDiv / den
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
	if p.kind == pkCloud {
		// **粒子云屏宽 = 云带同量级（2026-10-20 改大）**：
		// 净倍率 = p.sc × scaleMul ÷ bandNetDiv，与云带共用同一个分母。
		// 换算（源图宽 × 倍率 = 屏宽，1080 px 屏）：
		//
		//	512 px × 1.5 / 1.00 = 768 px … 512 px × 2.3 / 1.00 = 1178 px
		//	云带 512 px × (1.5 + 0.8·frac) / 1.00 = 768 px … 1178 px（逐档相同）
		//
		// 依据：稳态下粒子云 256 px（旧常量 cloudNetScale 0.50）与云带 768–1178 px
		// 相差 3–4.6 倍 ⇒ 8 朵圆团读成"一小坨一小坨"。改后两者同量级。
		// scaleMul ∈ [0.75, 1.25] 是既有的逐朵尺寸抖动，一并计入：
		// 逐朵屏宽 = 512 × p.sc × scaleMul ∈ [576, 1469] px（名义档 768–1178 px 的抖动带）。
		return p.sc * p.scaleMul / bandNetDiv
	}
	if p.kind == pkFog {
		// 效果图口径：雾团尺寸 = sc·(0.55+0.45·depth)·scaleMul × fogNetScale(=1.0)，
		// 1024 px 源图 ⇒ 屏上 732–2867 px，与效果图（2048 源 × 同式 ÷2）逐档相同。
		// 上一版把雾收成常量 fogNetScale 0.143（净宽 128 px = 旧 Kenney 净宽），
		// 雾从"整片体积雾"缩成"小团"，与选定效果图不符 ⇒ 恢复逐粒子口径。
		return p.sc * (0.55 + 0.45*p.depth) * p.scaleMul * fogNetScale
	}
	// 其余类（星/雪/尘/光晕）：贴图没有被超采样，÷2 就是它们的既有净口径。
	return p.sc * (0.55 + 0.45*p.depth) * p.scaleMul / spriteNetDiv
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
// 云矩形用真实绘制尺寸：贴图尺寸 × spriteScale（云 = p.sc × scaleMul ÷ bandNetDiv，
// 逐朵取，见 spriteScale 的 pkCloud 分支）。
// 盘半径口径 = moonRcFrac（月盘 0.060），**不含光晕**（光晕 0.192 会把矩形虚大、占比虚高）。
func (f *Field) CelestialCover(w, h int) (clouds int, coverPct float64) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	sp := spritesOnce()
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
		// 逐朵取绘制同口径的净倍率（spriteScale：p.sc × scaleMul ÷ bandNetDiv），
		// 不再取常量 cloudNetScale —— 常量口径量不到屏幕上真实画出来的尺寸。
		sc := spriteScale(p)
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
	cloudXLo = -0.35 // 与 recycleXLo 同口径：云的活动窗口左界
	cloudXHi = 1.35  // 右界（云在 x>1.3 处回绕到 -0.3）
	// 间距误差 → 速度目标的增益。
	// 实测根因：旧值 0.05 相对钳位区间 [0.002,0.015] 过大 —— 间距误差的归一化量
	// 在 ±1 之间，0.05 的增益每帧都把 vx 直接打到上钳位或下钳位，且逐帧翻向，
	// 速度在 0.002 与 0.015 之间（7 倍）逐帧跳 = 肉眼看到的"一抽一抽"。
	// 现口径三样一起把修正压进钳位区间：增益降到 0.012 + 死区 ±15% + 逐帧变化上限。
	cloudCoupleK = 0.012 // 间距误差 → 速度目标的增益（归一化单位）
	// 死区：前后间距误差在 ±15% 之内不修正。
	// 理由：目标速度是逐帧重算的，间距在目标值附近抖动时旧口径逐帧反向修正，
	// 这正是抽动的直接来源；死区把"没必要修"的那一段直接置零。
	cloudGapDead = 0.15
	// 低通斜率：每帧速度变化上限（归一化单位/帧）。
	// 速度只朝目标值挪，一步最多 0.0004 ⇒ 从下钳位爬到上钳位要 32 帧以上，
	// 逐帧跳变在条件上不成立。
	cloudVxSlew = 0.0004
	// cloudVxSlewStep 单步速度变化上限 = cloudVxSlew ÷ 追赶上限（A2 / C1）。
	// 控制器跟着固定步数跑 ⇒ 一帧最多补 fixedStepCatchup 步，每帧合计变化上限
	// 仍是 cloudVxSlew（自检 TestCloudVxSmooth 的判据逐字不变）。
	cloudVxSlewStep = cloudVxSlew / fixedStepCatchup
	// 钳位区间同步 ×0.5（云速整体降一半，与 spawn 的 vx 生成式同一口径）：
	// 0.004/0.030 → **0.002/0.015**。区间本身是定板口径，不动。
	cloudVxMin = 0.002 // 速度钳位下界（0.004 × 0.5）
	cloudVxMax = 0.015 // 速度钳位上界（0.030 × 0.5）
)

// coupleClouds 按"前后空隙"微调每朵云的横向速度，让云自己散开、自己填缝。
//
// 为什么需要它（缺陷：云的横向分布不均，容易出现某一小段时间屏幕中
// 大部分是空白或某一段云大量集中出现：8 朵云的基准速度是 0.006–0.028
// （**4.7 倍差**），快的会追上慢的 ⇒ 一边堆成团、另一边空出来，两个症状同时出现。
// 落位阶段本来有最小间隙保证（spreadAir），但它只在**生成时**生效，漂移中不复查。
//
// 规则（低通，不是逐帧重算）：
//
//	target_i = vxBase_i + K · clamp0( gapAhead_i − gapBehind_i ) / target
//	vx_i    += clamp( target_i − vx_i, ±cloudVxSlew )
//	vx_i     = clamp( vx_i, cloudVxMin, cloudVxMax )
//
// 三条口径（对应"移动慢的云一抽一抽"）：
//
//	死区 cloudGapDead：间距误差在 ±15% 之内不修正 ⇒ 间距在目标附近抖动时不逐帧反向；
//	增益 cloudCoupleK 0.05 → 0.012 ⇒ 目标落在钳位区间之内，不再每帧打到端点；
//	低通 cloudVxSlew：每帧速度变化上限 ⇒ 速度是爬过去的，不是逐帧跳过去的。
//
// 前面挤、后面空 → 该项为负 → 这朵减速 → 前面的把它甩开、后面的追上来填缝 ⇒ 两侧同时收敛。
// 前面空、后面挤 → 该项为正 → 加速离开拥挤侧、钻进空侧。
// 速度只在 [cloudVxMin, cloudVxMax] 的钳位区间内动，基准速度分布（视差）整体保留。
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
	if m == 0 {
		return
	}
	sort.Slice(idx, func(a, b int) bool { return f.parts[idx[a]].x < f.parts[idx[b]].x })
	adj := make([]float64, m) // m<3 时全 0：没有"前后间距"可修，目标就是基准速度
	tgt := make([]float64, m)
	if m >= 3 {
		width := cloudXHi - cloudXLo
		target := width / float64(m)
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
			// 死区：间距误差的归一化量在 ±cloudGapDead 之内 ⇒ 不修正。
			err := (ahead - behind) / target
			if math.Abs(err) <= cloudGapDead {
				err = 0.0
			}
			adj[k] = cloudCoupleK * err
		}
	}
	for k := 0; k < m; k++ {
		tgt[k] = f.parts[idx[k]].vxBase + adj[k]
	}
	// ---- 同道不超车（规格 1 第 2 条，逐字："同一横向带内，后车速度不得超过前车速度"）----
	//
	// 前车 = 同带内 x 更大的最近一朵；带 = 中心 y 相差 airBandN(=0.12 屏高) 之内。
	// 钳位 = 后车的**目标** ← min(目标, 前车终值)：前车更快时后车被压到同一速度 ⇒
	// 间距只增不减，超车（两朵的相对次序翻转）在条件上就不成立。跨带不约束（规格逐字）。
	//
	// 遍历顺序 = 从最前车往后（idx 已按 x 升序排好，故倒序遍历）：
	// 前车先定稿，后车钳的是前车的**终值** ⇒ 钳位沿队列向后传递，
	// 同带内 vx 随 x 单调不增（超车要求后车更快，被逐对封死）。
	//
	// 钳位作用在目标上、再走低通 ⇒ "不超车"与"每帧变化 ≤ cloudVxSlew"两条同时成立
	// （旧写法直接改 vx，一帧就能从 0.015 掉到 0.002，正是抽动的另一半来源）。
	//
	// 与薄快厚慢（C1 第 1 条）的关系：薄云基准速度大，排在厚云后面时被压到厚云的速度。
	// 规格的原话是"就算要追上，也应该是轻云追上厚云"：轻云在后、厚云在前时，
	// 前车更慢 ⇒ 后车被压到前车速度 ⇒ 轻云以厚云的速度跟在其后，间距恒定，永不越过它；
	// 厚云在后时本来就更慢 ⇒ 钳位不动它。
	for k := m - 1; k >= 0; k-- {
		p := &f.parts[idx[k]]
		front, frontDx := -1, math.Inf(1)
		for j := 0; j < m; j++ {
			if j == k {
				continue
			}
			q := &f.parts[idx[j]]
			if q.x <= p.x {
				continue
			}
			if math.Abs(q.y-p.y) > airBandN {
				continue
			}
			if d := q.x - p.x; d < frontDx {
				frontDx, front = d, j
			}
		}
		if front >= 0 {
			if vq := f.parts[idx[front]].vx; tgt[k] > vq {
				tgt[k] = vq
			}
		}
		// 低通：朝目标挪，**一步**最多 cloudVxSlewStep = cloudVxSlew ÷ 追赶上限；再进钳位区间。
		// （每帧合计 ≤ cloudVxSlew：一帧最多补 fixedStepCatchup 步。）
		d := tgt[k] - p.vx
		if d > cloudVxSlewStep {
			d = cloudVxSlewStep
		} else if d < -cloudVxSlewStep {
			d = -cloudVxSlewStep
		}
		vx := p.vx + d
		if vx < cloudVxMin {
			vx = cloudVxMin
		} else if vx > cloudVxMax {
			vx = cloudVxMax
		}
		p.vx = vx
	}
}

// hasCloudBands 本场是否画云带（Draw / CoverageProfile / CelestialCoverAll 共用同一判据，
// 三处必须一致，否则探针量的就不是屏幕上画出来的东西）。
func (f *Field) hasCloudBands() bool {
	switch f.cls {
	case effect.Rain, effect.Thunder, effect.Cloudy, effect.PartlyCloudy, effect.Overcast, effect.Fog:
		return true
	}
	return false
}

// ---- P3 判据 D：漂移期复查（运行时可观测 + 节流处置）----
//
// 现状（原话，ui/effects.go 的 coupleClouds 注释）：
//
//	"落位阶段本来有最小间隙保证（spreadAir），但它只在**生成时**生效，漂移中不复查"
//
// ⇒ 本节把"漂移中的复查"补出来：每 N 个固定步按**当前真实落位**扫一次重数，
//
//	超预算就对同带内重叠最严重的那一对做处置。
//
// P6 设计第 7 条（定死）：处置 = **回收重进场**（driftRecycleOffscreen：挪到屏外进场位 +
// spreadAir 挑 y），P3 的"只降 alpha"缓解（particle.crowdDim 字段、cloudAlphaOf 的降 alpha 块、
// crowdSweep 加档 / crowdDimRelax 退档、driftDim* 常量）按 spec 指名删除，行号见 out/notes-p6.md。
const (
	// driftCheckEverySteps 节流 N = 30 个固定步（fixedStep = 1/60 s ⇒ 0.5 s 一次）。
	// N 的理由（口径，不拍数字）：一个检查间隔内两层相对位移的上界 =
	//   云↔云带：(cloudVxMax 0.015 + 带速 0.03×0.725 = 0.02175) × 0.5 s = 0.0184 屏宽 = 19.9 px
	//   云↔云 ：(cloudVxMax−cloudVxMin = 0.013) × 0.5 s = 0.0065 屏宽 = 7.0 px
	// 判据线量级 = 整周期（1080 屏上 3026 px）上的 ≥2 层占比 ⇒ 一个间隔最多推进 20 px
	// = 周期的 0.66% ⇒ 漏检窗口 = 0.5 s，处置最迟在下个间隔落地。
	// 代价：一次扫描 = O(层数²) 对（云量档 4 带 + 3 云 = 7 层 ⇒ 21 对）+ 一次事件排序，
	// 60 fps 下每 30 步一次。N 取 10 ⇒ 进 60 fps 热路径而几何只多测 3 倍（收益 0）；
	// N 取 120（2 s）⇒ 一次重叠峰可被整段放过。
	driftCheckEverySteps = 30
	// driftDualBudgetPct 处置触发线 = 判据线本身。
	// P6 判据（spec/p6-noverlap.md 目标口径，逐字）：含粒子云/雾团的双层占比 ≤10%
	// ⇒ 触发线从 P3 的 60% 收紧到 10%（与测试线 budgetDualLine 同口径）。
	driftDualBudgetPct = 10.0
	// P6 设计第 7 条：P3 的 driftDimStep/driftDimMax/driftDimDecay（"只降 alpha"缓解三常量）
	// 随该缓解一并删除（处置改回收，见 crowdSweep / driftRecycleOffscreen）。
)

// driftLayer 是 metrics.Layer 的别名（R5 迁移：类型本体逐字迁出到 ui/metrics/drift.go，
// 原定义 = ui/effects.go:4718-4725）。字段与语义一字未改 ⇒ 类型别名，不是新类型。
type driftLayer = metrics.Layer

// driftLayers 按**当前真实落位**把云带 / 粒子云 / 雾团展开成 span 周期内的横轴区间。
// 几何与绘制逐条共用同一份公式：带 = bandPlace(i, f.t)（与 drawCloudBands 同源），
// 粒子云 = cloudDrawWidthPx（与 Draw 的 dw 同口径），雾团 = fogSz × spriteScale
// （与 CoverageProfile / AirWidthsPx 同口径）。x 取当前的 p.x ⇒ 不是等相位假设。
func (f *Field) driftLayers(w, h int) ([]driftLayer, float64) {
	w, h = screenfit.Fallback(w, h)
	span := f.bandSpan(w)
	out := make([]driftLayer, 0, 16)
	if f.hasCloudBands() {
		for i := 0; i < f.bandCount(); i++ {
			bp, ok := f.bandPlace(i, f.t, w, h)
			if !ok {
				continue
			}
			out = append(out, driftLayer{
				Start: metrics.WrapPeriod(bp.x, span),
				Width: bp.dw,
				Pi:    -1,
				Cy:    (bp.y + bp.dh/2) / float64(h),
			})
		}
	}
	n := f.ActiveCount()
	for i := 0; i < n && i < len(f.parts); i++ {
		p := &f.parts[i]
		var dw float64
		switch p.kind {
		case pkCloud:
			dw = cloudDrawWidthPx(p)
		case pkFog:
			sz := metrics.SpriteSizeOr(f.sp.fogSz, p.spr)
			if sz.X <= 0 {
				continue
			}
			dw = float64(sz.X) * spriteScale(p)
		default:
			continue
		}
		if dw <= 0 {
			continue
		}
		out = append(out, driftLayer{
			Start: metrics.WrapPeriod(p.x*float64(w)-dw/2, span),
			Width: dw,
			Pi:    i,
			Cy:    p.y,
		})
	}
	return out, span
}

// DriftOverlapBandOnly P6 设计第 6 条的**带-only**口径：只用 bandPlace 真落位算
// 云带彼此的重叠（不含粒子）。设计上恒为 0（bandPlace 注释的可证性），这里是"量出来证明"
// 的出口：探针与 SCR_CROWD 打 dualBandOnly，判据测试断言 ≤5%。
// layers = Σ带宽 ÷ span = N·T/span（设计第 5 条覆盖率线 ≥0.90 用这一口径，
// 与现成 AvgLayersOf（ui/overlap_probe.go）同一算式）。
func (f *Field) DriftOverlapBandOnly() (layers float64, dualPct float64) {
	ls, span := f.driftLayers(f.sw, f.sh)
	bands := make([]driftLayer, 0, len(ls))
	for _, l := range ls {
		if l.Pi < 0 {
			bands = append(bands, l)
		}
	}
	return metrics.DriftLayersStats(bands, span)
}

// DriftOverlapIncl P6 设计第 6 条的**含粒子云/雾团**口径：层集 = 现 DriftOverlap 的
// driftLayers 全集（带 + 粒子云 + 雾团），layers = Σ宽÷span 口径不变；
// dual = 同带粒子对重叠（metrics.DriftDualParticlePairs，口径理由见 ui/metrics/drift.go 的注释）。
func (f *Field) DriftOverlapIncl() (layers float64, dualPct float64) {
	ls, span := f.driftLayers(f.sw, f.sh)
	layers, _ = metrics.DriftLayersStats(ls, span)
	return layers, metrics.DriftDualParticlePairs(ls, span, airBandN)
}

// DriftOverlap = DriftOverlapIncl 的兼容出口（P3 判据 2 的"实测值"、判据 3 的 400 步判据
// 与 main.go SCR_CROWD 都经它）。P6 起 dual 口径 = 同带粒子对（DriftOverlapIncl 注释）；
// 带-only 口径走 DriftOverlapBandOnly。
func (f *Field) DriftOverlap() (layers float64, dualPct float64) {
	return f.DriftOverlapIncl()
}

// driftWorstPair 同带内（|Δy| ≤ airBandN，与 coupleClouds 的"同道"同一判据）
// 重叠长度最大的一对**可处置粒子层**（pi ≥ 0）：云带不可挪（带几何 = bandPlace 的
// 等相位恒等式，处置带 = 改带口径 = 越权），故只在粒子云 / 雾团之间挑。
// 返回 (前车, 后车, 重叠长度)；无可处置对时 (-1,-1,0)。
func driftWorstPair(ls []driftLayer, span float64) (int, int, float64) {
	bestI, bestJ, best := -1, -1, 0.0
	for i := 0; i < len(ls); i++ {
		if ls[i].Pi < 0 {
			continue
		}
		for j := i + 1; j < len(ls); j++ {
			if ls[j].Pi < 0 {
				continue
			}
			if math.Abs(ls[i].Cy-ls[j].Cy) > airBandN {
				continue
			}
			ov := metrics.DriftArcOverlap(ls[i], ls[j], span)
			if ov <= best {
				continue
			}
			front, rear := ls[i].Pi, ls[j].Pi
			if ls[j].Start > ls[i].Start {
				front, rear = ls[j].Pi, ls[i].Pi
			}
			best, bestI, bestJ = ov, front, rear
		}
	}
	return bestI, bestJ, best
}

// driftClassOn 云/雾档 = 场上有漂移中的粒子云或雾团的档（判据 D 说的"云/雾类"）：
// 云带五档（Cloudy/PartlyCloudy/Overcast/Rain/Thunder）+ 雾（pkFog）两档（Fog/Snow）。
// Clear 白天只有 1 朵云 + 3 个光斑（无从重叠）、Dust 无云无雾 ⇒ 不进复查路径。
func driftClassOn(cls effect.Class) bool {
	switch cls {
	case effect.Cloudy, effect.PartlyCloudy, effect.Overcast, effect.Rain, effect.Thunder, effect.Fog, effect.Snow:
		return true
	}
	return false
}

// driftRecycleOffscreen P6 设计第 7 条的处置 = **挪到屏外重进场**（现成回收口径）：
//
//	x 摆到进场位（vx>0 → x = −hw、vx<0 → x = 1+hw，整只贴屏外，与 placeAirEntry
//	的回收进场位逐字同式；回收窗常量 recycleXLo/recycleYLo 口径），
//	y 走现成 spreadAir（拒绝采样挑与已有同类粒子最小间隙最大的落位，allowX=false ⇒
//	x 固定在屏幕外、只有 y 可调，与 placeAirEntry 的雾回收支路同一调用式）。
//
// 不用 spawn(false) 整朵重生的理由（沿用 P3 取证的那条速度判据，处置不许撞它）：
//
//	spawn ⇒ vx 从钳位区间瞬移回基准（0.003–0.014），一帧 |Δvx| 最大 0.011 ≫
//	cloudVxSlew(0.0004) ⇒ 撞 TestCloudVxSmooth 每帧变化上限。本函数只挪位、
//	逐字不动 vx/vxBase/alpha/sc（P6"不做什么"：粒子数/alpha/速度口径）。
//
// moveAirToFreeBand 把一朵空中层挪进**净空 y 带**（与所有同类层 |Δy| > airBandN + 余量）：
// 宽团（雾/灰幕，hw 0.41–1.03）在同一带里必然相叠，横向又铺不开 ⇒ 换带是唯一
// 既消重叠又保覆盖的处置。实测：回收走 spreadAir 重摆 y 会把宽团塞进窄带，
// Fog 800 步含粒子 25.44%、Snow 400 步 48.67% 都由这条路径产生。
// 返回 false = 没有净空带（调用方走屏外进场位）。
func (f *Field) moveAirToFreeBand(p *particle) bool {
	hh := f.halfHN(p, f.sh)
	if hh <= 0 || hh >= 0.5 {
		return false
	}
	margin := airBandN + airSlotGapN
	// 搜索区**不受"整只在屏内"限制**：宽幕（hw/hh 到 0.29–1.03）在 [hh, 1−hh] 里
	// 只剩 0.424 高，装不下 3 张 pairwise > 0.14 ⇒ 必有一张被推去停车，
	// 而停车位在绕回周期里彼此相叠（实测 Snow 400 步三张全停在 x −0.62…−0.87）。
	// 幕布类允许边缘出血，搜索区取 [0.06, 0.94]。
	// 搜索区 = **完整可见**（y ∈ [hh, 1−hh]，TestCloudFogAvoidInfoBand 硬要求）
	// 且**避让中央信息带**（与 bandFreeY 同口径：[hh, y0−pad] ∪ [y1+pad, 1−hh]）。
	// 两块宽团（Fog 实测 0.82 与 0.98 屏宽）在同一带里不可能都全可见 ⇒
	// 唯一同时满足"不相叠 + 不掉覆盖率"的出路是把第二块放到信息带另一侧的可用带。
	pad := bandPadJitN(p.kind, f.sh)
	y0, y1 := infoBandY()
	loA, hiA := hh, y0-pad
	loB, hiB := y1+pad, 1.0-hh
	bestClear, bestY := -1.0, 0.0
	for k := 0; k <= 120; k++ {
		y := 0.0
		if k <= 60 {
			if hiA <= loA {
				continue
			}
			y = loA + (hiA-loA)*float64(k)/60.0
		} else {
			if hiB <= loB {
				continue
			}
			y = loB + (hiB-loB)*float64(k-61)/59.0
		}
		clear := 2.0
		for j := range f.parts {
			q := &f.parts[j]
			if q == p || q.kind != p.kind || !isAir(q.kind) {
				continue
			}
			if d := math.Abs(q.y - y); d < clear {
				clear = d
			}
		}
		if clear > bestClear {
			bestClear, bestY = clear, y
		}
	}
	if bestClear <= margin {
		return false
	}
	p.y = bestY
	f.placeAirXInBand(p)
	return true
}

func (f *Field) driftRecycleOffscreen(i int) {
	if i < 0 || i >= len(f.parts) {
		return
	}
	p := &f.parts[i]
	if p.kind != pkCloud && p.kind != pkFog {
		return
	}
	hw, _ := f.airHalfN(p)
	// P6b：先试换净空带（保覆盖），找不到净空带才走屏外进场位。
	if f.moveAirToFreeBand(p) {
		return
	}
	if p.kind == pkCloud {
		// B3 进场排队闸门（与 placeAirEntry 同一判据、同一发射函数）：云走 EntryOK，
		// 不合格保持 parked；雾不在 R1 的"云"口径里 ⇒ 雾支路逐字不动（下面 else）。
		x, hwE := f.cloudEntryPlace(p)
		if !f.cloudEntryGateOK(x, hwE) {
			f.parkCloudEntry(p, x, hwE)
		} else {
			f.emitCloudEntry(p, x, hwE)
		}
	} else if p.vx >= 0 {
		p.x = -hw
	} else {
		p.x = 1 + hw
	}
	f.spreadAir(p, f.rng, false)
}

// crowdSweep 漂移期复查（P3 判据 D 第 2 条 + P6 设计第 7 条）：每个固定步进来一次，
// 按 driftCheckEverySteps = 30 节流（spec 逐字"每 30 步复查一次"，沿用现成节流）。
//
// 机理（处置方式 = P6 设计第 7 条：回收重进场；P3 的"只降 alpha"缓解已按 spec 删除）：
//
//	含粒子双层占比（metrics.DriftDualParticlePairs，同带粒子对口径）超预算（10%）
//	⇒ 取同带内重叠最严重的一对（driftWorstPair，前车道与后车道都处置），
//	走 driftRecycleOffscreen 挪到屏外重进场；预算内 ⇒ 不做任何事（alpha 不碰）。
func (f *Field) crowdSweep() {
	if !driftClassOn(f.cls) {
		return
	}
	if f.steps <= 0 || f.steps%driftCheckEverySteps != 0 {
		return
	}
	ls, span := f.driftLayers(f.sw, f.sh)
	if metrics.DriftDualParticlePairs(ls, span, airBandN) < driftDualBudgetPct {
		return
	}
	front, rear, ov := driftWorstPair(ls, span)
	if ov <= 0 {
		return
	}
	f.driftRecycleOffscreen(front)
	f.driftRecycleOffscreen(rear)
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
	if f.hasCloudBands() {
		for i := 0; i < f.bandCount(); i++ {
			bp, ok := f.bandPlace(i, f.t, w, h)
			if !ok {
				continue
			}
			rects = append(rects, rect{bp.x, bp.y, bp.x + bp.dw, bp.y + bp.dh})
		}
	}
	for i := 0; i < f.ActiveCount(); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		sz := f.sp.cloudSz[0]
		if p.spr >= 0 && p.spr < len(f.sp.cloudSz) {
			sz = f.sp.cloudSz[p.spr]
		}
		// 逐朵取绘制同口径净倍率（spriteScale），与 Draw 完全一致
		sc := spriteScale(p)
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
	// 数组上限 = 云带片数的上限（bandCount 最大 6 = Overcast）+ 粒子上限 64
	var rects [10 + 64][4]float64
	n := 0
	if f.hasCloudBands() {
		for i := 0; i < f.bandCount(); i++ {
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
	for i := 0; i < f.ActiveCount() && n < len(rects); i++ {
		p := &f.parts[i]
		if p.kind != pkCloud {
			continue
		}
		sz := f.sp.cloudSz[0]
		if p.spr >= 0 && p.spr < len(f.sp.cloudSz) {
			sz = f.sp.cloudSz[p.spr]
		}
		// 逐朵取绘制同口径净倍率（spriteScale），与 Draw 完全一致
		sc := spriteScale(p)
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

// placeAirXInBand 换带之后给本层在新带的可见窗里挑最大空档的落位；
// 新带没有其他层 ⇒ 取可见窗中点。
func (f *Field) placeAirXInBand(p *particle) {
	hwP, _ := f.airHalfN(p)
	if hwP >= 0.5 {
		p.x = 0.5
		return
	}
	lo, hi := hwP, 1.0-hwP
	if hi <= lo {
		p.x = 0.5
		return
	}
	marks := []float64{lo, hi}
	for j := range f.parts {
		q := &f.parts[j]
		if q == p || !isAir(q.kind) {
			continue
		}
		if math.Abs(q.y-p.y) > airBandN {
			continue
		}
		hq, _ := f.airHalfN(q)
		marks = append(marks, q.x-hq-airSlotGapN, q.x+hq+airSlotGapN)
	}
	bestLen, bestC := -1.0, (lo+hi)/2
	for k := 0; k+1 < len(marks); k++ {
		a, b := marks[k], marks[k+1]
		if b < a {
			a, b = b, a
		}
		c := (a + b) / 2
		if c < lo || c > hi {
			continue
		}
		if b-a > bestLen {
			bestLen, bestC = b-a, c
		}
	}
	p.x = bestC
}

// clampFogWidthPx 雾团屏宽按类封顶（P7 用户判据"最多的时候也只能首尾相连，不能重叠"）：
// 同带 Σ全宽 + 间隙 ≤ 1 屏宽 ⇒ Snow 3 张灰幕 ⇒ 每张 ≤ (1080−42)/3 = 346 px；
// Fog 1 团（配 1 朵 ≤500 px 的云）⇒ ≤ 540 px。缩的是 sc（绘制倍率本体），
// 逐字不动 alpha / vx / 团数 / 落位口径。实测封顶前：Snow 单张 1135 px、Fog 884 px。
func (f *Field) clampFogWidthPx(p *particle, capPx float64) {
	if capPx <= 0 || p.sc <= 0 {
		return
	}
	sz := metrics.SpriteSizeOr(f.sp.fogSz, p.spr)
	if sz.X <= 0 {
		sz = spriteSize(spritesOnce(), pkFog)
	}
	if sz.X <= 0 {
		return
	}
	w := float64(sz.X) * spriteScale(p)
	if w <= capPx {
		return
	}
	p.sc *= capPx / w
}

package overlay

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// ---- 闪光叠加与闪电本体（逐字迁自 ui/effects.go：常量 /，
// 掩膜，绘制矩形，高斯核，形状掩膜，
// 提亮层，闪电本体）----
// ---- D3 闪光局域化 ----
// 旧口径 = 整屏白洗（PushOpacity(flash) + paint.Fill 满屏 alpha）：
// 屏幕上每个像素一起变白，云与天空同涨同落，读成"整块屏幕闪一下"。
// 现口径 = 以闪电锚点为中心的局部提亮：
//
//	锚点 = 本次闪电的落点 (boltX, boltY)（闪现开始时抽好、闪现期间不动）；
//	 锚点没抽过时取云带顶部的随机 x、y = 0.12 屏高
//	形状 = 闪电本体（BoltGlowMask：当前那张闪电贴图高斯模糊烘出的形状掩膜），
//	 不再是圆（旧圆罩见下面 boltGlow 的口径说明）
//	幅度 = 中心 0.30 上限（预览基准：0.30 档 = 非背景像素 44.8% 的观感）
//	时长 = 单次 0.15–0.30 s 不动（走 boltEnvelope，见 stepBolt）
//	对象 = 提亮范围内的天空与云：云 alpha 在粒子循环里按 flashMaskAt
//	 （0.35 屏宽的 cos² 圆）抬升（云变实），
//	 体色由提亮层抬（提亮层画在云与粒子之后，落在云像素上）
//
// dev 开关 FX_FLASH 的语义 = 强制一次局域闪光（见 SetFlash）。
const (
	FlashRadiusFrac = 0.35 // 云 alpha 抬升掩膜的半径 = 0.35 × 屏宽（云变实的范围，不是光的形状）
	FlashAmpMax     = 0.30 // 提亮幅度上限（预览基准 0.30 档，不动）
	FlashBoostMax   = 0.60 // 云 alpha 的抬升系数上限（掩膜满处 alpha ×(1+0.60)，不动）
	FlashAnchorY    = 0.12 // 锚点没抽过时用的 y（云带顶部）
)

// ---- 闪电形状光罩 boltGlow ----
// 需求原话：闪电的光线是圆的，而发光体就是闪电本身。
// 实测根因：提亮掩膜 = 圆罩贴图（Kenney Light Masks circle_a 的角向平均剖面，
// 宽 0.70 倍屏宽）⇒ 提亮形状是一个圆，与闪电本体的走向没有任何关系。
// 现口径 = 把本次闪现用的那张闪电贴图（bolt1..3 之一）的 alpha 做高斯模糊
// （σ = BoltGlowSigmaFrac × 屏宽 = 0.03 倍屏宽）烘成形状掩膜 boltGlow：
//
//	形状 = 闪电本体（沿闪电走向），掩膜按最大值归一 ⇒ 最亮处就是闪电本体；
//	锚点 = 闪电落点 (boltX, boltY)（贴图中心 = 落点，口径不动）；
//	绘制矩形 = 闪电本体那一块（长轴 0.28 屏高 = BoltLenFrac，短轴按贴图宽高比）；
//	幅度 = flash × FlashAmpMax（0.30 上限不动）；
//	云 alpha 抬升（flashMaskAt 的 0.35 屏宽 cos² 圆 + 系数 0.60）不动。
//
// 圆罩贴图退役：embed 目录里的文件保留，代码里不再引用。
const (
	BoltGlowSigmaFrac = 0.03 // 高斯模糊 σ = 0.03 倍屏宽（屏幕像素）
	BoltGlowMaskN     = 192  // 掩膜栅格边长（像素）
	// 闪电本体的可见阈值：0.25 → 0.12。
	// 实测根因：本体只在 flash>0.25 才画，而闪现包络在 0.25 以下的区间占整次的
	// 大半 ⇒ 低幅度闪光时"只见亮不见电"。
	BoltFlashMin = 0.12
)

const (
	BoltAlphaMax = 0.75 // 峰值 alpha 上限
	BoltLenFrac  = 0.28 // 长轴 = 0.28 屏高（旧折线闪电的屏上长轴 560 px / 1920 = 0.29，同量级）
)

// FlashMaskAt 局域提亮在归一化坐标 (x,y) 处的掩膜 0..1。
// 距离按**屏宽**归一（半径是 0.35 倍屏宽，纵向同分母 ⇒ 各向同性的圆）。
// 锚点 = 本次闪现的落点（旧实现里读 boltX/boltY ⇒ 迁出后由调用方注入，值口径不动）。
func FlashMaskAt(boltX, boltY, x, y float64) float64 {
	d := math.Hypot(x-boltX, y-boltY) / FlashRadiusFrac
	if d >= 1.0 {
		return 0
	}
	c := math.Cos(d * math.Pi * 0.5)
	return c * c
}

// BoltRectPx 闪电本体在屏幕上的绘制矩形（与 drawBolt 同口径）：
// 长轴 = BoltLenFrac × 屏高，短轴按贴图宽高比等比。
func BoltRectPx(sz image.Point, w, h int) (float64, float64) {
	dh := BoltLenFrac * float64(h)
	return dh * float64(sz.X) / float64(sz.Y), dh
}

// GaussKernel 归一化一维高斯核（σ 单位 = 栅格像素，半径 3σ，端点钳到 0）。
func GaussKernel(sigma float64) []float64 {
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

// GlowEntry 形状掩膜的缓存条目（字段只改名不改类型/顺序/语义 = 迁移前的 boltGlowEntry；
// 搬到导出包后字段必须导出，消费者 ui 与自检才能取到同一批值）。
type GlowEntry struct {
	Op        paint.ImageOp
	Mask      *image.NRGBA // 掩膜本体（自检取用：判据要能算出屏幕上那一层的形状）
	Dw, Dh    float64      // 屏幕上的绘制矩形（与闪电本体同一块）
	PeakAtC   float64      // 归一后掩膜在矩形中心处的取值（自检打印用）
	BuiltMSec float64
	Ok        bool
}

var boltGlowCache struct {
	mu  sync.Mutex
	ent map[boltGlowKey]*GlowEntry
}

// BoltGlowMask 把第 idx 张闪电贴图（bolt1..3）的 alpha 高斯模糊烘成形状掩膜。
// 步骤（全部在屏幕像素口径下做，与屏幕上看到的一致）：
// 1. 在 BoltGlowMaskN 的栅格上双线性采出闪电本体的 alpha（栅格铺满闪电的绘制矩形）；
// 2. separable 高斯模糊，σ = BoltGlowSigmaFrac × 屏宽 换算到栅格像素；
// 3. 按最大值归一 ⇒ 最亮处 = 闪电本体，掩膜峰值对应提亮幅度 FlashAmpMax；
// 4. RGB 写固定的冷白（与太阳泛光/宽光同一条口径：源图 RGB 一个字节都不写进屏幕），
// alpha = 掩膜值。
// 缓存 = (贴图下标, 屏宽, 屏高)，逐帧零生成成本；一次烘的成本 = 栅格² × 核长 × 2。
// 贴图没落地 ⇒ Ok=false（调用方不画提亮层，退回"只见闪电本体"）。
// 贴图查询口以回调注入（旧实现里 = spriteImg("bolt"+itoa(idx+1)+".png")，
// 内嵌贴图与 itoa 归 ui 的资产口径 ⇒ 缓存键与烘焙过程一行不动）。
func BoltGlowMask(idx, w, h int, load func(idx int) *image.NRGBA) *GlowEntry {
	k := boltGlowKey{idx: idx, w: w, h: h}
	boltGlowCache.mu.Lock()
	defer boltGlowCache.mu.Unlock()
	if boltGlowCache.ent == nil {
		boltGlowCache.ent = map[boltGlowKey]*GlowEntry{}
	}
	if e, ok := boltGlowCache.ent[k]; ok {
		return e
	}
	t0 := time.Now()
	e := &GlowEntry{}
	boltGlowCache.ent[k] = e
	src := load(idx)
	if src == nil {
		return e
	}
	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	dw, dh := BoltRectPx(sb.Size(), w, h)
	if dw <= 0 || dh <= 0 {
		return e
	}
	n := BoltGlowMaskN
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
	sig := BoltGlowSigmaFrac * float64(w) * float64(n) / dw
	if sig < 0.5 {
		sig = 0.5
	}
	kk := GaussKernel(sig)
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
	e.Op = paint.NewImageOp(img)
	e.Mask = img
	e.Dw, e.Dh = dw, dh
	e.PeakAtC = blurred[(n/2)*n+n/2] / peak
	e.BuiltMSec = float64(time.Since(t0).Microseconds()) / 1000.0
	e.Ok = true
	return e
}

// DrawFlash 画局域提亮层：以闪电落点 (boltX, boltY) 为中心、
// 铺在闪电本体那块矩形上（长轴 0.28 屏高、短轴按贴图宽高比）的**形状**提亮，
// 峰值幅度 = flash × 0.30（flash 是闪现包络 0..1）。
// 掩膜 = BoltGlowMask（当前那张闪电贴图高斯模糊烘出的形状掩膜）⇒ 提亮沿闪电走向，
// 不再是圆罩（需求：发光体就是闪电）。锚点口径不动。
// 绘制链与云精灵同构（Scale → Offset，Push/Pop 成对）；
// 掩膜是 straight alpha 的 NRGBA，gioui 上传时预乘 ⇒ src-over 叠加 = 向白色抬。
func DrawFlash(gtx layout.Context, w, h int, flash, boltX, boltY float64, idx int, load func(idx int) *image.NRGBA, probeCount bool, draws *int) {
	if flash <= 0.01 {
		return
	}
	amp := flash * FlashAmpMax
	if amp <= 0.002 {
		return
	}
	// idx = 本次闪现用的闪电贴图下标（旧实现里现抽 = f.boltSpriteIndex()，
	// 选图口径归 Field 的闪现状态 ⇒ 由调用方注入，-1 的退路判据一行没动）。
	if idx < 0 {
		return
	}
	e := BoltGlowMask(idx, w, h, load)
	if !e.Ok {
		return
	}
	sz := e.Op.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	cx, cy := boltX*float64(w), boltY*float64(h)
	tr := f32.Affine2D{}.
		Scale(f32.Point{}, f32.Pt(float32(e.Dw/float64(sz.X)), float32(e.Dh/float64(sz.Y)))).
		Offset(f32.Pt(float32(cx-e.Dw/2), float32(cy-e.Dh/2)))
	ts := op.Affine(tr).Push(gtx.Ops)
	st := paint.PushOpacity(gtx.Ops, float32(amp))
	cl := clip.Rect(image.Rect(0, 0, sz.X, sz.Y)).Push(gtx.Ops)
	e.Op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if probeCount && draws != nil {
		(*draws)++ // P1 探针计数：drawFlash 局域提亮层
	}
	cl.Pop()
	st.Pop()
	ts.Pop()
}

// DrawBolt 画一道闪电，亮度跟随闪现包络。
// 三条口径：
//
//	1 贴图 = embed/bolt1..3（Kenney spark_01..03 的白热闪电枝），
//	 逐次闪现换一张（boltIdx 在闪现开始时抽好，闪现期间不变 ⇒ 不逐帧跳）；
//	 贴图组缺失时退到 genBolt 的程序折线（退路，出厂路径走不到）；
//	2 落位 = 闪现开始时抽好的随机点（boltX/boltY，见常量注释），
//	 长轴 = 0.28 屏高，短轴按贴图宽高比等比；
//	3 峰值 alpha ≤ 0.75（BoltAlphaMax）：透明度 = 0.75 × 包络，包络峰值 1。
//
// 迁移处置（纯搬移）：
//
//	计次 boltDrawn++ = Field 的自检计数 ⇒ 留在调用方（旧实现里它就是函数体第一行，
//	 在贴图/幅度判据之前，无条件加一 ⇒ 调用方在调用之前加，逐字同序）；
//	选图（f.sp.bolt / boltOps + f.boltSpriteIndex()）= Field 的贴图装配与闪现状态 ⇒ 留在调用方，
//	 本函数收**已选好的** img/sz；
//	探针计数 draws = P1 探针的 Field 计数器 ⇒ 以指针注入，累加位置一行不动。
func DrawBolt(gtx layout.Context, w, h int, flash, boltX, boltY float64, img paint.ImageOp, sz image.Point, probeCount bool, draws *int) {
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	dw, dh := BoltRectPx(sz, w, h)
	px := boltX*float64(w) - dw/2
	py := boltY*float64(h) - dh/2
	a := BoltAlphaMax * math.Min(1.0, math.Max(0.0, flash))
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
	if probeCount && draws != nil {
		(*draws)++ // P1 探针计数：drawBolt 闪电本体
	}
	cl.Pop()
	st.Pop()
	ts.Pop()
}

// DrawHaze 底部浓、顶部淡的雾霭梯度，做出空气透视纵深。
func DrawHaze(gtx layout.Context, w, h int, t float64, probeCount bool, draws *int) {
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
	if probeCount && draws != nil {
		(*draws)++ // P1 探针计数：drawHaze 雾霭梯度
	}
	cl.Pop()
}

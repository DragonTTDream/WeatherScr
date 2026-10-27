package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync"

	"gioui.org/op/paint"

	embedfs "github.com/DragonTTDream/WeatherScr/ui/embed"
)

// 背景特效素材（ui/embed，内嵌即默认路径）：
//	cloud1..9.png OpenGameArt "Clouds with Transparency"（WickedInsignia, CC0）
//	 体积云，448 宽（高 310–474）
//	fog1..4.png OpenGameArt "Thick Fog"（lfa, CC0），896×448，
//	 alpha 通道 ×0.60（浓淡对比图选定的那一档）
//	moon.png NASA SVS CGI Moon Kit（公有领域，LRO 数据），384×384，alpha 全 255
//	rain.png 本项目自绘（MIT），192×192
//	glow.png Kenney "Particle Pack" light_01（CC0）
//	detail_*.png 上游详情行图标，来源见 embed/ATTRIBUTION.md
// 阳光形态三张（Kenney CC0，包清单与缩放口径见 assets/LICENSE-kenney.txt）：
//	sunrays.png Kenney "Smoke Particles" flash05.png，缩到 512 宽（512×521，LANCZOS，alpha 保留）
//	 = 晴 / 多云见日档在太阳位置加性绘制的星芒（ui/render.go 的 bakeSunRays）
//	sunglow.png Kenney "Light Masks" circle_a.png，缩到 1024 宽（1024×1024）
//	 = 灰蒙档（云量 ≥85）的宽光软形状底（ui/render.go 的 bakeSunGlow，
//	 绘制宽 1.6 倍烘焙宽，rim 在屏外；屏内加性光量走解析两项）
//	flashglow.png Kenney "Light Masks" circle_a.png，512 宽（512×512，源图原尺寸）
//	 = 退役：闪电提亮改走闪电本体烘出的形状掩膜（ui/effects.go 的
//	 boltGlowMask），圆罩不再被任何代码引用；文件保留在 embed 目录
// 三张都走覆盖目录工装口径：WS_ASSET_DIR 里有同名文件时优先用它（ReadPNG / assetImageAny）。
// 出厂二进制不读任何工装变量（闸门逐字证明）。
// 许可与来源逐条记录：NOTICE 的第三方表 + assets/LICENSE-ogazoo.txt +
// assets/LICENSE-kenney.txt + ui/embed/ATTRIBUTION.md。
// 为什么用现成素材而不是继续程序生成：需求是从社区取现成的精美素材，
// 不用程序生成（需求 4）。程序生成的软边团块经视觉判读
// 判定为"像高斯模糊的白噪点、像均匀蒙版、有贴图感"——这是算法生成形状
// 的固有上限，调参调不出来。体积云是人工设计的形状，
// 边缘平滑、有前后层次，直接解决该问题。

// LoadEmbedded 把嵌入的 PNG 读成贴图集合。
// 云/雾精灵加载时做 alpha 羽化（FeatherAlpha）：低性能 x86 实机反馈"云的锯齿状边缘
// 十分明显"，而 v0.10.3 的滤镜只有 Linear/Nearest（默认已是 Linear），
// ⇒ 锯齿来自精灵自身硬 alpha 边缘被放大 2.2 倍后的台阶，只能在加载时压成斜坡。
func LoadEmbedded() (clouds []paint.ImageOp, cloudSz []image.Point, fogs []paint.ImageOp, fogSz []image.Point, glow paint.ImageOp, glowSz image.Point) {
	for i := 1; i <= 9; i++ {
		img, ok := ReadPNG("embed/cloud" + Itoa(i) + ".png")
		if !ok {
			continue
		}
		n := UpscaleCatmull(FeatherAlpha(ToNRGBA(img), 1), SpriteSupersample)
		clouds = append(clouds, paint.NewImageOp(n))
		cloudSz = append(cloudSz, n.Bounds().Size())
	}
	for i := 1; i <= 4; i++ {
		img, ok := ReadPNG("embed/fog" + Itoa(i) + ".png")
		if !ok {
			continue
		}
		n := UpscaleCatmull(FeatherAlpha(ToNRGBA(img), 1), SpriteSupersample)
		fogs = append(fogs, paint.NewImageOp(n))
		fogSz = append(fogSz, n.Bounds().Size())
	}
	if img, ok := ReadPNG("embed/glow.png"); ok {
		glow = paint.NewImageOp(ToNRGBA(img))
		glowSz = img.Bounds().Size()
	}
	return
}

// ToNRGBA 统一成 *image.NRGBA（FeatherAlpha 需要直接访问 Pix）。
func ToNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bb >> 8), A: uint8(a >> 8),
			})
		}
	}
	return out
}

// ReadPNG 是全部嵌入精灵的**唯一解码口**：解码后就地反预乘。
// 实测根因：ui/embed/glow.png、ui/embed/fog1.png 的 RGB/alpha 比值均值
// 1.31 / 1.35（最小 1.08，最大 1.72） ⇒ 这些 PNG 是**预乘编码**；
// Go 的 image/png 解成 NRGBA（非预乘口径） ⇒ 每个半透明像素被 α/255 压暗
// ⇒ 光晕外圈出现"低 alpha 的黑环"（实测反馈：
// "夜晚的外围的光晕出现明显的边界"。
// 样本（glow.png 水平中线）：
//
//	(81,81,81,50) (94,94,94,62) (112,112,112,77) (124,124,124,90) (136,136,136,101)
//
// RGB 与 alpha 同步衰减，反预乘后 RGB 应恒为 255。
// 只在**加载时**做一次（本函数是加载路径，不在每帧路径里），使全部精灵
// （cloud1..9 / fog1..4 / glow / star / flake / drop / mote / sun 以及本批新增的
// detail_* / cond_*统一受益。
// 覆盖目录（预览工装）：WS_ASSET_DIR 非空且该目录里有同名文件时，
// 本函数改从该目录读；目录里没有的名字照旧走 embedFS。
// 反预乘口径两条路径完全相同（ToNRGBA → Unpremultiply），
// 负缓存语义不变（LoadSprite 对加载失败的负缓存照旧）。
// 未启用覆盖目录时 assetLookupPath 恒 false ⇒ 本函数与改动前逐字节等价。
// 依据：素材预览工装节；实现见 ui/assetdir.go。
func ReadPNG(name string) (image.Image, bool) {
	if p, ok := overrideLookup(name); ok {
		f, err := os.Open(p)
		if err != nil {
			return nil, false
		}
		im, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, false
		}
		return Unpremultiply(ToNRGBA(im)), true
	}
	b, err := embedRead(name)
	if err != nil {
		return nil, false
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, false
	}
	return Unpremultiply(ToNRGBA(im)), true
}

// Unpremultiply 把预乘编码的像素还原成非预乘（straight alpha）：
// a==0 时 RGB 置 0；a>0 时 R = clamp(R*255/a)，G/B 同理。
// 就地修改（调用方给的是刚解码出来的图，没有共享所有权）。
// 根因与样本数据见 ReadPNG 的注释。
func Unpremultiply(n *image.NRGBA) *image.NRGBA {
	if n == nil {
		return nil
	}
	b := n.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			i := n.PixOffset(b.Min.X+x, b.Min.Y+y)
			a := int(n.Pix[i+3])
			if a == 0 {
				n.Pix[i+0] = 0
				n.Pix[i+1] = 0
				n.Pix[i+2] = 0
				continue
			}
			n.Pix[i+0] = Mul255Over(n.Pix[i+0], a)
			n.Pix[i+1] = Mul255Over(n.Pix[i+1], a)
			n.Pix[i+2] = Mul255Over(n.Pix[i+2], a)
		}
	}
	return n
}

// Mul255Over = v*255/a 收进 [0,255]（四舍五入）。
func Mul255Over(v uint8, a int) uint8 {
	p := float64(v) * 255.0 / float64(a)
	if p >= 255 {
		return 255
	}
	if p <= 0 {
		return 0
	}
	return uint8(math.Round(p))
}

func Itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return ""
}

// GlowRaw 返回 Kenney light_01（glow.png）的原始像素，供 CPU 烘焙采样。
// 太阳用它而不是程序画的圆盘：美术做的径向衰减（中心 alpha 251 → r=127 归零）
// 比程序圆盘+羽化更不像"白色圆片叠模糊滤镜"（视觉判读 2026-10-16）。
func GlowRaw() *image.NRGBA {
	img, ok := ReadPNG("embed/glow.png")
	if !ok {
		return nil
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	// 统一转成 NRGBA 以便逐像素采样
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bb >> 8), A: uint8(a >> 8),
			})
		}
	}
	return out
}

// FeatherAlpha 只对**边缘像素**做 (2r+1)^2 盒均值，只改 alpha 不动 RGB。
// 两轮低性能 x86 实机反馈定出的口径：
//
//	反馈 A：云的锯齿状边缘十分明显 ⇒ 需要压掉放大 2.2 倍后的台阶；
//	反馈 B：很夸张、很糊，某些边缘是平的（像圆弧强行被拉平）
//
// ⇒ 第一版对**整张精灵**做 r=2 的盒均值，被放大 2.2 倍后边缘糊成一团，
//
//	而盒滤波是低通：圆顶极值被邻域平均掉 ⇒ 圆弧顶部出现平台（"被拉平"）。
//	这是本次改动引入的伪影，不是素材的问题。
//
// 现口径：**只处理边缘像素**（3×3 邻域内同时存在 alpha=0 与 alpha>0 的像素），
// 半径 1。内部实心区完全不动 ⇒ 形状与圆顶保形；台阶只发生在边缘 1 px 上，
// 也只在那里补。
func FeatherAlpha(src *image.NRGBA, r int) *image.NRGBA {
	if src == nil || r < 1 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return src
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	// 整张先原样复制（含 alpha），随后只覆盖边缘像素
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			si := src.PixOffset(b.Min.X+x, b.Min.Y+y)
			di := out.PixOffset(x, y)
			out.Pix[di+0] = src.Pix[si+0]
			out.Pix[di+1] = src.Pix[si+1]
			out.Pix[di+2] = src.Pix[si+2]
			out.Pix[di+3] = src.Pix[si+3]
		}
	}
	at := func(x, y int) int {
		return int(src.Pix[src.PixOffset(b.Min.X+x, b.Min.Y+y)+3])
	}
	for y := 0; y < h; y++ {
		y0 := y - r
		if y0 < 0 {
			y0 = 0
		}
		y1 := y + r
		if y1 > h-1 {
			y1 = h - 1
		}
		for x := 0; x < w; x++ {
			x0 := x - r
			if x0 < 0 {
				x0 = 0
			}
			x1 := x + r
			if x1 > w-1 {
				x1 = w - 1
			}
			// 边缘判据：邻域内同时有不透明与透明
			var hasZero, hasSolid bool
			for yy := y0; yy <= y1; yy++ {
				for xx := x0; xx <= x1; xx++ {
					if at(xx, yy) == 0 {
						hasZero = true
					} else {
						hasSolid = true
					}
				}
			}
			if !hasZero || !hasSolid {
				continue // 内部实心区 / 全透明区：完全不动
			}
			var s int
			for yy := y0; yy <= y1; yy++ {
				for xx := x0; xx <= x1; xx++ {
					s += at(xx, yy)
				}
			}
			a := s / ((x1 - x0 + 1) * (y1 - y0 + 1))
			if a > 255 {
				a = 255
			}
			out.Pix[out.PixOffset(x, y)+3] = uint8(a)
		}
	}
	return out
}

// SpriteSupersample 精灵超采样倍率 = **1**（B 版素材落地后）。
// 历史口径（2）：Kenney 云精灵顶边 alpha 剖面 = [127, 255, 255, 255, …]，
// 边缘只有 1 px 的半透明斜坡，其余是硬实心（TestEdgeProfileIsRamp 实测）。
// GPU 侧 v0.10.3 只有 FilterLinear，把 177–266 px 的原生小图放大 2.2 倍后
// 每个源像素变成 2.2 px 的软方块 ⇒ 沿曲线边界就是肉眼可读的台阶。
// 加载时先按三次核（Catmull-Rom）高质量放大 2 倍，绘制时把 scale 除以同一系数
// ⇒ GPU 近似 1:1 贴图，台阶在 CPU 的高质量重采样阶段就被消掉。
// 现在取 1 的依据（内存口径，逐张实测）：
//
//	B 版源图 = 云 448 宽（高 310–474）、雾 896×448。
//	屏上云宽 88–133 px ⇒ 源图已是它的 3.4–5.1 倍，GPU 走的是**缩小**采样，
//	"放大源像素成软方块"这个台阶机制本身不存在了，超采样换不来任何东西。
//	解码像素（RGBA 4 B/px）：
//	 新素材 + supersample=2 = 30.68 Mpx = **122.8 MB**（云 24.26 + 雾 6.42）
//	 新素材 + supersample=1 = 3.12 Mpx = **12.5 MB**
//	 旧素材（Kenney）+ supersample=2 = 2.03 Mpx = 8.1 MB（对照）
//	122.8 MB 单是云/雾贴图就顶穿 120 MB 的稳态 RSS 预算
//	（180 s 窗口实测 RSS 104.7 → 114.0 MB，取证 34.14），
//	⇒ 口径"保证视觉效果然后降低尺寸"= 视觉口径不动、把 ×4 的解码放大砍掉。
//
// 注意：**绘制尺寸不随本常量走**，见下面 SpriteNetDiv / CloudNetScale /
// FogNetScale / BandNetDiv（超采样降 1 后若继续除以 SpriteSupersample，
// 全部精灵会放大 2 倍）。
const SpriteSupersample = 1

// 净绘制倍率（相对**源图像素尺寸**）。
// 旧口径把这些尺寸公式统一写成 "除以 SpriteSupersample"，在 supersample=2 时
// 净尺寸 = 0.5×原生。supersample 降到 1 后若照旧除以它，星/雪/尘/光晕
// （它们从未被超采样，除以 2 就是它们的既有口径）会整体放大 2 倍，
// 云/雾也会从 112 px 涨到 224 px ⇒ 画面口径全变。
// ⇒ 把"除以 2"从超采样倍率里拆出来固定成常量，屏上尺寸逐处保持现状：
//
//	云 448 px 宽 × CloudNetScale 0.25 = 112 px —— 既有口径 88–133 px 之内
//	雾 896 px 宽 × FogNetScale 0.143 = 128 px —— 旧净宽 = Kenney 256 原生 × 0.5
//	带 448 px 宽 × (1.5+0.8·frac)/BandNetDiv 4 = 168–258 px —— 旧净宽 142–306 px
//	其余精灵（星/雪/尘/光晕/雨丝）沿用 ÷SpriteNetDiv = ÷2 的既有口径
//
// **口径改判：屏上尺寸回到选定效果图那一份，内存不跟着涨。**
// 上面这组常量把新素材"逐档对齐"到旧 Kenney 尺寸（注释里的"不因换素材变大"），
// 结果出厂画面与选定的那张效果图不是同一个东西：效果图是 supersample=2（源图 2× 放大）
// 下出的，屏上尺寸 = 云 256 px、云带 768–1178 px、雾团 732–2867 px；
// 落地后 supersample 降 1 并按旧尺寸收 ⇒ 云 112 px、带 168–258 px、雾 128 px，
// 云带缩到效果图的 1/4.5 ⇒ 大块体积云读成一堆小圆团。
// 现按"效果图的屏上尺寸 ÷ 现在的源图尺寸"定净倍率，**supersample 保持 1**
// （照效果图那份口径要 2× 放大贴图 = 精灵像素 4 倍 ≈ +42 MB，超内存预算）：
//
//	云 512 px 宽 × 0.50 = 256 px （效果图 1024 × 0.25 = 256 px）
//	带 512 px 宽 × (1.5+0.8·frac)/1.0 = 768–1178 px（效果图 1024 × /2 = 768–1178 px）
//	雾 1024 px 宽 × sc·(0.55+0.45·depth)·scaleMul × 1.0（效果图 2048 × 同式 ÷2）
//
// 代价（如实记）：源图 512 画到 768–1178 px = 1.5–2.3× 放大 ⇒ 边缘比效果图软；
// 云带填充面积 ×4.5 ⇒ CPU 上升（帧率自适应会压粒子数兜底，实测见）。
// **粒子云口径改判（粒子云改大）**：粒子云的屏宽不再由
// CloudNetScale 这个常量决定，改走与云带**同一个分母 BandNetDiv**：
//
//	粒子云 512 px 宽 × p.sc·scaleMul / BandNetDiv，p.sc ∈ [1.5, 2.3]
//	 = 512 × [1.5, 2.3] = 768–1178 px（未乘 scaleMul 抖动时），与云带逐档相同
//
// 依据：稳态下粒子云 256 px、云带 768–1178 px，相差 3–4.6 倍 ⇒
// 屏上同时有 8 朵 256 px 的圆团与 4 条 768–1178 px 的带，圆团读成"小坨"。
// 朵数按面积反比同步下调（单朵面积 ≈ 现 9 倍），见 effects.go counts。
// CloudNetScale 保留作 supersample=2 时代的换算记录，绘制路径已不读它。
const (
	SpriteNetDiv  = 2.0
	CloudNetScale = 0.50 // 旧粒子云口径：512 源 → 256 px（现绘制路径已不读，见上）
	FogNetScale   = 1.00 // 效果图口径：与 sc·(0.55+0.45·depth)·scaleMul 相乘，1024 源 → 732–2867 px
	BandNetDiv    = 1.00 // 效果图口径：512 源 → 768–1178 px（云带与粒子云共用）
)

// UpscaleCatmull 把图放大 f 倍（只支持整数倍），alpha 通道用 Catmull-Rom 核。
// RGB 直接取最近源像素（精灵是平涂色，插值 RGB 会引入暗边）。
func UpscaleCatmull(src *image.NRGBA, f int) *image.NRGBA {
	if src == nil || f < 2 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w*f, h*f
	out := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	at := func(x, y int) int {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x > w-1 {
			x = w - 1
		}
		if y > h-1 {
			y = h - 1
		}
		return int(src.Pix[src.PixOffset(b.Min.X+x, b.Min.Y+y)+3])
	}
	cw := func(t float64) float64 { // Catmull-Rom 核
		abs := t
		if abs < 0 {
			abs = -abs
		}
		switch {
		case abs <= 1:
			return ((1.5*abs-2.5)*abs)*abs + 1.0
		case abs < 2:
			return ((-0.5*abs+2.5)*abs-4.0)*abs + 2.0
		}
		return 0
	}
	for y := 0; y < nh; y++ {
		// 源坐标（像素中心对齐）
		sy := (float64(y)+0.5)/float64(f) - 0.5
		y0 := int(math.Floor(sy))
		for x := 0; x < nw; x++ {
			sx := (float64(x)+0.5)/float64(f) - 0.5
			x0 := int(math.Floor(sx))
			tx, ty := sx-float64(x0), sy-float64(y0)
			var sum, wsum float64
			for j := -1; j <= 2; j++ {
				wy := cw(float64(j) - ty)
				for i := -1; i <= 2; i++ {
					wv := wy * cw(float64(i)-tx)
					if wv == 0 {
						continue
					}
					sum += wv * float64(at(x0+i, y0+j))
					wsum += wv
				}
			}
			a := 0.0
			if wsum != 0 {
				a = sum / wsum
			}
			if a < 0 {
				a = 0
			}
			if a > 255 {
				a = 255
			}
			// RGB 取最近源像素
			rx, ry := x0, y0
			if tx > 0.5 {
				rx = x0 + 1
			}
			if ty > 0.5 {
				ry = y0 + 1
			}
			if rx < 0 {
				rx = 0
			}
			if rx > w-1 {
				rx = w - 1
			}
			if ry < 0 {
				ry = 0
			}
			if ry > h-1 {
				ry = h - 1
			}
			si := src.PixOffset(b.Min.X+rx, b.Min.Y+ry)
			di := out.PixOffset(x, y)
			out.Pix[di+0] = src.Pix[si+0]
			out.Pix[di+1] = src.Pix[si+1]
			out.Pix[di+2] = src.Pix[si+2]
			out.Pix[di+3] = uint8(a)
		}
	}
	return out
}

// ── 详情行图标与状况图标──────────────────
// 素材逐字节复制自上游 ui/assets/weather/source/presented/（含 detail/ 子目录），
// 许可与来源记录见 ui/embed/ATTRIBUTION.md。
// 上游的 detail/*.png 与 presented/*.png 全是 250×250（实测 2026-10-16）。

// 上游低性能 x86 实机数字（低性能 x86 实机 settings_v2.json：font_size 40、detail_icon_size 16、
// icon_size 192；实测：
//
//	detailFontSize = max(8, int(font_size*0.5)) = max(8, 20) = 20 （weather.py:850-851）
//	detailIconSize = max(30, detail_icon_size) = max(30, 16) = 30 （weather.py:866-867）
const (
	DetailFontPx = 30 // 需求：详情行文字要放大（上游公式值 20 太小）⇒ 取 30
	DetailIconPx = 40 // 同上：图标随字号同步放大（上游 max(30, detail_icon_size)=30 是上游口径，非本实现的上线口径）
	// DetailIconSrcPx 上游素材的源边长（缩放系数 = 30.0/250.0、192.0/250.0）。
	DetailIconSrcPx = 250
	// DetailGapPx 上游 WeatherPresentation.qml:443 `anchors.leftMargin: 1.0`
	// 与 `width: metricIcon.width + 1.0 + metricText.implicitWidth`。
	DetailGapPx = 1
)

// DetailRowHeight 上游 WeatherPresentation.qml:393-397 的行高公式：
// max(68.0, detailIconSize + 38.0, detailFontSize*3.2)
// = max(68, 30+38, 20*3.2=64) = 68 px。
func DetailRowHeight() int {
	h := DetailRowMinH
	if v := DetailIconPx + 38; v > h {
		h = v
	}
	if v := int(float64(DetailFontPx) * 3.2); v > h {
		h = v
	}
	return h
}

// DetailRowMinH 上游行高公式的下限常量 68.0（QML:394）。
const DetailRowMinH = 68

// WindGlyphFrac = detail_wind.png 非透明像素包围盒占画布的比例（实测 2026-10-16，
// 方案 (a)。
// 实测包围盒 (34,62)-(228,203) ⇒ 宽 195/250 = 0.780、高 142/250 = 0.568，
// 取主导边（宽）0.780 作为 k。
// 上游 fillMode: PreserveAspectFit 把 250×250 画布整体缩进图标方框，画布里的
// 字形只占 0.78 ⇒ 40 px 图标下字形 ≈ 31 px，读起来像"-5"与数字黏在一起。
const WindGlyphFrac = 0.780

// WindIconDrawPx 风图标的绘制边长 = DetailIconPx / WindGlyphFrac（上限 56 px），
// 使**字形**视觉尺寸与 DetailIconPx 一致（方案 (a)）。
// 40 / 0.780 = 51.28 → 51 px（未触 56 上限）。素材本身不动（不重绘、不裁剪）。
func WindIconDrawPx() int {
	v := int(math.Round(float64(DetailIconPx) / WindGlyphFrac))
	if v > 56 {
		v = 56
	}
	return v
}

type SpriteEntry struct {
	op  paint.ImageOp
	sz  image.Point
	img *image.NRGBA // 反预乘后的原始像素，供 CPU 烘焙采样（bakeMoon）
	ok  bool
}

var SpriteCache = struct {
	mu sync.Mutex
	m  map[string]*SpriteEntry
}{m: map[string]*SpriteEntry{}}

// LoadSprite 加载并缓存一张嵌入精灵（走 ReadPNG 的反预乘口径）。
// 只在首次调用时解码 ⇒ 反预乘只发生在加载时，不进每帧路径（需求）。
// 缺失的素材也缓存（负缓存），逐帧调用不会反复读 embedFS。
func LoadSprite(name string) (paint.ImageOp, image.Point, bool) {
	SpriteCache.mu.Lock()
	defer SpriteCache.mu.Unlock()
	if e, ok := SpriteCache.m[name]; ok {
		return e.op, e.sz, e.ok
	}
	img, ok := ReadPNG("embed/" + name)
	if !ok {
		SpriteCache.m[name] = &SpriteEntry{}
		return paint.ImageOp{}, image.Point{}, false
	}
	n := ToNRGBA(img)
	e := &SpriteEntry{op: paint.NewImageOp(n), sz: n.Bounds().Size(), img: n, ok: true}
	SpriteCache.m[name] = e
	return e.op, e.sz, true
}

// LoadSpriteSet 按前缀加载一组粒子贴图（snow1..3 / rain1..7 / bolt1..3）。
// 解码口只有 LoadSprite 一个（= ReadPNG 的反预乘口径），本函数不新增解码路径。
// 任意一张缺 ⇒ 整组返回 false：逐组退路，屏幕上不会出现"半套贴图"
// （三张雪里两张是贴图、一张是程序晶形那种混合态）。
func LoadSpriteSet(prefix string, n int) ([]paint.ImageOp, []image.Point, []*image.NRGBA, bool) {
	var ops []paint.ImageOp
	var szs []image.Point
	var imgs []*image.NRGBA
	for i := 1; i <= n; i++ {
		name := prefix + Itoa(i) + ".png"
		op, sz, ok := LoadSprite(name)
		if !ok {
			return nil, nil, nil, false
		}
		img := SpriteImg(name)
		if img == nil {
			return nil, nil, nil, false
		}
		ops = append(ops, op)
		szs = append(szs, sz)
		imgs = append(imgs, img)
	}
	return ops, szs, imgs, true
}

// SpriteImg 取缓存精灵的**反预乘原始像素**（不复制、只读）。
// 用途：云/雾体色的运行时着色需要贴图本体（gioui v0.10.3 的 paint.ImageOp
// 没有颜色乘子：ImageOp 的字段是 Filter/uniform/color/src，MulColor 是 v0.11 才加的），
// 所以"按天气类提白/压暗"只能在内存里做贴图副本 ⇒ 需要本体像素。
// **素材文件逐字节不动**。
func SpriteImg(name string) *image.NRGBA {
	SpriteCache.mu.Lock()
	defer SpriteCache.mu.Unlock()
	if e, ok := SpriteCache.m[name]; ok {
		return e.img
	}
	img, ok := ReadPNG("embed/" + name)
	if !ok {
		SpriteCache.m[name] = &SpriteEntry{}
		return nil
	}
	n := ToNRGBA(img)
	SpriteCache.m[name] = &SpriteEntry{op: paint.NewImageOp(n), sz: n.Bounds().Size(), img: n, ok: true}
	return n
}

// EmbedHas 判断嵌入素材是否存在（上游 _file_source 的"在不在"语义，
// rendering/quick/widgets/weather.py:144-147。
func EmbedHas(p string) bool {
	f, err := embedOpen(p)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// DetailSprite 返回详情行某一项的图标贴图与源尺寸（rain/humidity/wind）。
// 素材名逐字照抄上游 _DETAIL_ICON_FILES（weather.py:70-74）的三项，
// 前缀改成本复刻的 detail_（需求 1 的命名）。
func DetailSprite(kind string) (paint.ImageOp, image.Point, bool) {
	name, ok := DetailIconFiles[kind]
	if !ok {
		return paint.ImageOp{}, image.Point{}, false
	}
	return LoadSprite(name)
}

// DetailIconFiles 上游 _DETAIL_ICON_FILES 的复刻（detail/ → detail_ 前缀）。
var DetailIconFiles = map[string]string{
	"rain":     "detail_umbrella.png",
	"humidity": "detail_humidity.png",
	"wind":     "detail_wind.png",
}

// ── 月相──────────────────────────────
// 8 张月相素材（moon_*.png，250×250）已删除：月亮由独立层解析绘制
// （ui/render.go 的 bakeMoonAt / moonLayerGet），素材在运行时没有调用者。
// 相位到 8 等分序号的区间口径保留在下面 IsMoonNew 里（新月不画月面的判据）。

// IsMoonNew 相位是否落在 new 区间（新月无可见月面 ⇒ bakeMoon 不画月亮）。
// 8 等分每格宽 0.125，中心对齐 8 个标准相位 ⇒ 区间 = [中心−0.0625, 中心+0.0625：
//
//	[0.9375,1∪[0,0.0625)→new [0.0625,0.1875)→waxing-crescent
//	[0.1875,0.3125→first-quarter [0.3125,0.4375)→waxing-gibbous
//	[0.4375,0.5625→full [0.5625,0.6875)→waning-gibbous
//	[0.6875,0.8125→waning-crescent [0.8125,0.9375)→last-quarter
//
// 序号 = floor((p+0.0625)/0.125) mod 8（new 跨 0 界用 mod 收口），new = 序号 0。
func IsMoonNew(p float64) bool {
	q := p - math.Floor(p) // 折回 [0,1
	if q < 0 {
		q += 1
	}
	idx := int(math.Floor((q + 0.0625) / 0.125))
	idx %= 8
	if idx < 0 {
		idx += 8
	}
	return idx == 0
}

// MoonIllumination 照度 = (1 − cos(2π·phase)) / 2。
// 为什么自己算：实测 daily=moon_illumination / lunar_illumination / lunar_phase /
// illumination / moon_altitude / moon_azimuth **全部拒绝**，/v1/astronomy 端点
// 404 不存在 ⇒ 照度只能从 moon_phase 派生。
// 实测校验：phase=0.174 ⇒ illum = 0.270（27%，盈眉月）。
// 边界：illum(0)=0、illum(0.25)=0.5、illum(0.5)=1、illum(0.75)=0.5。
func MoonIllumination(phase float64) float64 {
	return (1.0 - math.Cos(2.0*math.Pi*phase)) / 2.0
}

// ── 迁移桥（内嵌口 + WS_ASSET_DIR 覆盖目录注入）──────────────────
// 内嵌口：//go:embed 指令不能向上跨目录引用 ⇒ 装配代码迁入 ui/assets 后，
// 内嵌改在贴图所在目录原地进行（ui/embed/embedfs.go，png 文件位置与字节逐字不动）。
// 内嵌键 = 裸文件名；调用点的 "embed/<name>" 路径字符串逐字保留，由下面
// embedKey 剥前缀后查新 FS。新旧语义精确对应：旧 FS 的键全部带 "embed/"
// 前缀 ⇒ 裸名（无前缀）在旧口一律查不到，适配口同样返回 fs.ErrNotExist。

func embedKey(p string) (string, bool) {
	if k, ok := strings.CutPrefix(p, "embed/"); ok {
		return k, true
	}
	return "", false
}

func embedRead(p string) ([]byte, error) {
	k, ok := embedKey(p)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return embedfs.FS.ReadFile(k)
}

func embedOpen(p string) (fs.File, error) {
	k, ok := embedKey(p)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return embedfs.FS.Open(k)
}

// PrefixedFS = ui 侧 embedFS 的垫片（assetdir.go 的
// embedFS.ReadFile("embed/"+name) 调用点逐字不动）。
type PrefixedFS struct{}

func (PrefixedFS) ReadFile(p string) ([]byte, error) { return embedRead(p) }

// OverrideLookup = WS_ASSET_DIR 覆盖目录查询的注入口。
// 实现体 = ui/assetdir.go 的 assetLookupPath，由 ui 包 init 注入
// （ui import 本包 ⇒ 本包不得 import ui；注入口先例 = ui/sky
// 贴图查询口，见 ui/sky/sky.go:24 注）。
// nil = 未注入（出厂路径：WS_ASSET_DIR 未设时 assetLookupPath 恒 false ⇒ 逐字等价）。
var OverrideLookup func(name string) (string, bool)

func overrideLookup(name string) (string, bool) {
	if OverrideLookup == nil {
		return "", false
	}
	return OverrideLookup(name)
}

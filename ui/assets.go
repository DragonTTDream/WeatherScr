package ui

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"

	"gioui.org/op/paint"
)

// 背景特效素材：Kenney《Background Elements》云 9 张 +《Particle Pack》
// 烟雾/光晕，全部 CC0（http://creativecommons.org/publicdomain/zero/1.0/）。
//
// 为什么用现成素材而不是继续程序生成：需求是从社区取现成的精美素材，
// 不用程序生成（需求 4）。程序生成的软边团块经视觉判读
// 判定为"像高斯模糊的白噪点、像均匀蒙版、有贴图感"——这是算法生成形状
// 的固有上限，调参调不出来。Kenney 的云是人工设计的扁平矢量插画，
// 边缘平滑、有前后层次，直接解决该问题。
//
// 素材来源与许可记录见 assets/LICENSE-kenney.txt。

//go:embed embed/*.png
var embedFS embed.FS

// loadEmbedded 把嵌入的 PNG 读成贴图集合。
//
// 云/雾精灵加载时做 alpha 羽化（featherAlpha）：低性能 x86 小主机反馈"云的锯齿状边缘
// 十分明显"，而 v0.10.3 的滤镜只有 Linear/Nearest（默认已是 Linear），
// ⇒ 锯齿来自精灵自身硬 alpha 边缘被放大 2.2 倍后的台阶，只能在加载时压成斜坡。
func loadEmbedded() (clouds []paint.ImageOp, cloudSz []image.Point, fogs []paint.ImageOp, fogSz []image.Point, glow paint.ImageOp, glowSz image.Point) {
	for i := 1; i <= 9; i++ {
		img, ok := readPNG("embed/cloud" + itoa(i) + ".png")
		if !ok {
			continue
		}
		n := upscaleCatmull(featherAlpha(toNRGBA(img), 1), spriteSupersample)
		clouds = append(clouds, paint.NewImageOp(n))
		cloudSz = append(cloudSz, n.Bounds().Size())
	}
	for i := 1; i <= 4; i++ {
		img, ok := readPNG("embed/fog" + itoa(i) + ".png")
		if !ok {
			continue
		}
		n := upscaleCatmull(featherAlpha(toNRGBA(img), 1), spriteSupersample)
		fogs = append(fogs, paint.NewImageOp(n))
		fogSz = append(fogSz, n.Bounds().Size())
	}
	if img, ok := readPNG("embed/glow.png"); ok {
		glow = paint.NewImageOp(toNRGBA(img))
		glowSz = img.Bounds().Size()
	}
	return
}

// toNRGBA 统一成 *image.NRGBA（featherAlpha 需要直接访问 Pix）。
func toNRGBA(img image.Image) *image.NRGBA {
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

// readPNG 是全部嵌入精灵的**唯一解码口**：解码后就地反预乘。
//
// 实测根因：ui/embed/glow.png、ui/embed/fog1.png 的 RGB/alpha 比值均值
// 1.31 / 1.35（最小 1.08，最大 1.72） ⇒ 这些 PNG 是**预乘编码**；
// Go 的 image/png 解成 NRGBA（非预乘口径） ⇒ 每个半透明像素被 α/255 压暗
// ⇒ 光晕外圈出现"低 alpha 的黑环"（实测反馈：
// "夜晚的外围的光晕出现明显的边界"。
//
// 样本（glow.png 水平中线）：
//
//	(81,81,81,50) (94,94,94,62) (112,112,112,77) (124,124,124,90) (136,136,136,101)
//
// RGB 与 alpha 同步衰减，反预乘后 RGB 应恒为 255。
//
// 只在**加载时**做一次（本函数是加载路径，不在每帧路径里），使全部精灵
// （cloud1..9 / fog1..4 / glow / star / flake / drop / mote / sun 以及本批新增的
// detail_* / cond_*统一受益。
func readPNG(name string) (image.Image, bool) {
	b, err := embedFS.ReadFile(name)
	if err != nil {
		return nil, false
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, false
	}
	return unpremultiply(toNRGBA(im)), true
}

// unpremultiply 把预乘编码的像素还原成非预乘（straight alpha）：
// a==0 时 RGB 置 0；a>0 时 R = clamp(R*255/a)，G/B 同理。
// 就地修改（调用方给的是刚解码出来的图，没有共享所有权）。
//
// 根因与样本数据见 readPNG 的注释。
func unpremultiply(n *image.NRGBA) *image.NRGBA {
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
			n.Pix[i+0] = mul255Over(n.Pix[i+0], a)
			n.Pix[i+1] = mul255Over(n.Pix[i+1], a)
			n.Pix[i+2] = mul255Over(n.Pix[i+2], a)
		}
	}
	return n
}

// mul255Over = v*255/a 收进 [0,255]（四舍五入）。
func mul255Over(v uint8, a int) uint8 {
	p := float64(v) * 255.0 / float64(a)
	if p >= 255 {
		return 255
	}
	if p <= 0 {
		return 0
	}
	return uint8(math.Round(p))
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return ""
}

// glowRaw 返回 Kenney light_01（glow.png）的原始像素，供 CPU 烘焙采样。
// 太阳用它而不是程序画的圆盘：美术做的径向衰减（中心 alpha 251 → r=127 归零）
// 比程序圆盘+羽化更不像"白色圆片叠模糊滤镜"（视觉判读 2026-10-16）。
func glowRaw() *image.NRGBA {
	img, ok := readPNG("embed/glow.png")
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

// featherAlpha 只对**边缘像素**做 (2r+1)^2 盒均值，只改 alpha 不动 RGB。
//
// 两轮低性能 x86 小主机反馈定出的口径：
//
//	反馈 A：云的锯齿状边缘十分明显 ⇒ 需要压掉放大 2.2 倍后的台阶；
//	反馈 B：很夸张、很糊，某些边缘是平的（像圆弧强行被拉平）
// ⇒ 第一版对**整张精灵**做 r=2 的盒均值，被放大 2.2 倍后边缘糊成一团，
//	  而盒滤波是低通：圆顶极值被邻域平均掉 ⇒ 圆弧顶部出现平台（"被拉平"）。
//	  这是本次改动引入的伪影，不是素材的问题。
//
// 现口径：**只处理边缘像素**（3×3 邻域内同时存在 alpha=0 与 alpha>0 的像素），
// 半径 1。内部实心区完全不动 ⇒ 形状与圆顶保形；台阶只发生在边缘 1 px 上，
// 也只在那里补。
func featherAlpha(src *image.NRGBA, r int) *image.NRGBA {
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

// spriteSupersample 精灵超采样倍率。
//
// 实测依据（TestEdgeProfileIsRamp）：Kenney 云精灵顶边 alpha 剖面 =
// [127, 255, 255, 255, …] —— **边缘只有 1 px 的半透明斜坡**，其余是硬实心。
// GPU 侧 v0.10.3 只有 FilterLinear，放大 2.2 倍后每个源像素变成 2.2 px 的软方块
// ⇒ 沿曲线边界就是肉眼可读的台阶（低性能 x86 小主机反馈"锯齿状边缘十分明显"）。
// 加载时先按三次核（Catmull-Rom）高质量放大 2 倍，绘制时把 scale 除以同一系数
// ⇒ GPU 近似 1:1 贴图，台阶在 CPU 的高质量重采样阶段就被消掉。
// 代价：精灵像素 ×4（9 张云 ≈ 3.5 MB），加载一次。
const spriteSupersample = 2

// upscaleCatmull 把图放大 f 倍（只支持整数倍），alpha 通道用 Catmull-Rom 核。
// RGB 直接取最近源像素（精灵是平涂色，插值 RGB 会引入暗边）。
func upscaleCatmull(src *image.NRGBA, f int) *image.NRGBA {
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
//
// 素材逐字节复制自上游 ui/assets/weather/source/presented/（含 detail/ 子目录），
// 许可与来源记录见 ui/embed/ATTRIBUTION.md。
// 上游的 detail/*.png 与 presented/*.png 全是 250×250（实测 2026-10-16）。

// 上游低性能 x86 小主机数字（低性能 x86 小主机 settings_v2.json：font_size 40、detail_icon_size 16、
// icon_size 192；实测：
//
//	detailFontSize = max(8, int(font_size*0.5)) = max(8, 20) = 20  （weather.py:850-851）
//	detailIconSize = max(30, detail_icon_size) = max(30, 16) = 30  （weather.py:866-867）
const (
	DetailFontPx = 30 // 需求：详情行文字要放大（上游公式值 20 太小）⇒ 取 30
	DetailIconPx = 40 // 同上：图标随字号同步放大（上游 max(30, detail_icon_size)=30 是上游口径，非本实现的上线口径）
	// detailIconSrcPx 上游素材的源边长（缩放系数 = 30.0/250.0、192.0/250.0）。
	detailIconSrcPx = 250
	// detailGapPx 上游 WeatherPresentation.qml:443 `anchors.leftMargin: 1.0`
	// 与 :419 `width: metricIcon.width + 1.0 + metricText.implicitWidth`。
	detailGapPx = 1
)

// detailRowHeight 上游 WeatherPresentation.qml:393-397 的行高公式：
// max(68.0, detailIconSize + 38.0, detailFontSize*3.2)
// = max(68, 30+38, 20*3.2=64) = 68 px。
func detailRowHeight() int {
	h := detailRowMinH
	if v := DetailIconPx + 38; v > h {
		h = v
	}
	if v := int(float64(DetailFontPx) * 3.2); v > h {
		h = v
	}
	return h
}

// detailRowMinH 上游行高公式的下限常量 68.0（QML:394）。
const detailRowMinH = 68

// windGlyphFrac = detail_wind.png 非透明像素包围盒占画布的比例（实测 2026-10-16，
// 方案 (a)。
// 实测包围盒 (34,62)-(228,203) ⇒ 宽 195/250 = 0.780、高 142/250 = 0.568，
// 取主导边（宽）0.780 作为 k。
// 上游 fillMode: PreserveAspectFit 把 250×250 画布整体缩进图标方框，画布里的
// 字形只占 0.78 ⇒ 40 px 图标下字形 ≈ 31 px，读起来像"-5"与数字黏在一起。
const windGlyphFrac = 0.780

// windIconDrawPx 风图标的绘制边长 = DetailIconPx / windGlyphFrac（上限 56 px），
// 使**字形**视觉尺寸与 DetailIconPx 一致（方案 (a)）。
// 40 / 0.780 = 51.28 → 51 px（未触 56 上限）。素材本身不动（不重绘、不裁剪）。
func windIconDrawPx() int {
	v := int(math.Round(float64(DetailIconPx) / windGlyphFrac))
	if v > 56 {
		v = 56
	}
	return v
}

type spriteEntry struct {
	op  paint.ImageOp
	sz  image.Point
	img *image.NRGBA // 反预乘后的原始像素，供 CPU 烘焙采样（bakeMoon）
	ok  bool
}

var spriteCache = struct {
	mu sync.Mutex
	m  map[string]*spriteEntry
}{m: map[string]*spriteEntry{}}

// loadSprite 加载并缓存一张嵌入精灵（走 readPNG 的反预乘口径）。
// 只在首次调用时解码 ⇒ 反预乘只发生在加载时，不进每帧路径（需求）。
// 缺失的素材也缓存（负缓存），逐帧调用不会反复读 embedFS。
func loadSprite(name string) (paint.ImageOp, image.Point, bool) {
	spriteCache.mu.Lock()
	defer spriteCache.mu.Unlock()
	if e, ok := spriteCache.m[name]; ok {
		return e.op, e.sz, e.ok
	}
	img, ok := readPNG("embed/" + name)
	if !ok {
		spriteCache.m[name] = &spriteEntry{}
		return paint.ImageOp{}, image.Point{}, false
	}
	n := toNRGBA(img)
	e := &spriteEntry{op: paint.NewImageOp(n), sz: n.Bounds().Size(), img: n, ok: true}
	spriteCache.m[name] = e
	return e.op, e.sz, true
}

// embedHas 判断嵌入素材是否存在（上游 _file_source 的"在不在"语义，
// rendering/quick/widgets/weather.py:144-147。
func embedHas(p string) bool {
	f, err := embedFS.Open(p)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// detailSprite 返回详情行某一项的图标贴图与源尺寸（rain/humidity/wind）。
// 素材名逐字照抄上游 _DETAIL_ICON_FILES（weather.py:70-74）的三项，
// 前缀改成本复刻的 detail_（需求 1 的命名）。
func detailSprite(kind string) (paint.ImageOp, image.Point, bool) {
	name, ok := detailIconFiles[kind]
	if !ok {
		return paint.ImageOp{}, image.Point{}, false
	}
	return loadSprite(name)
}

// detailIconFiles 上游 _DETAIL_ICON_FILES 的复刻（detail/ → detail_ 前缀）。
var detailIconFiles = map[string]string{
	"rain":     "detail_umbrella.png",
	"humidity": "detail_humidity.png",
	"wind":     "detail_wind.png",
}

// ── 月相──────────────────────────────
//
// 8 张月相素材（moon_*.png，250×250）已删除：月亮由独立层解析绘制
// （ui/render.go 的 bakeMoonAt / moonLayerGet），素材在运行时没有调用者。
// 相位到 8 等分序号的区间口径保留在下面 isMoonNew 里（新月不画月面的判据）。

// isMoonNew 相位是否落在 new 区间（新月无可见月面 ⇒ bakeMoon 不画月亮）。
//
// 8 等分每格宽 0.125，中心对齐 8 个标准相位 ⇒ 区间 = [中心−0.0625, 中心+0.0625：
//
//	[0.9375,1∪[0,0.0625)→new  [0.0625,0.1875)→waxing-crescent
//	[0.1875,0.3125→first-quarter  [0.3125,0.4375)→waxing-gibbous
//	[0.4375,0.5625→full  [0.5625,0.6875)→waning-gibbous
//	[0.6875,0.8125→waning-crescent  [0.8125,0.9375)→last-quarter
//
// 序号 = floor((p+0.0625)/0.125) mod 8（new 跨 0 界用 mod 收口），new = 序号 0。
func isMoonNew(p float64) bool {
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

// moonIllumination 照度 = (1 − cos(2π·phase)) / 2。
//
// 为什么自己算：实测 daily=moon_illumination / lunar_illumination / lunar_phase /
// illumination / moon_altitude / moon_azimuth **全部拒绝**，/v1/astronomy 端点
// 404 不存在 ⇒ 照度只能从 moon_phase 派生。
// 实测校验：phase=0.174 ⇒ illum = 0.270（27%，盈眉月）。
// 边界：illum(0)=0、illum(0.25)=0.5、illum(0.5)=1、illum(0.75)=0.5。
func moonIllumination(phase float64) float64 {
	return (1.0 - math.Cos(2.0*math.Pi*phase)) / 2.0
}

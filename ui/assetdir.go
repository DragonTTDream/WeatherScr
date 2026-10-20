package ui

// 素材覆盖目录（预览工装）。
//
// 目标：让同一套生产渲染路径能从**外部目录**读素材，出候选素材在不同天气下的效果图。出图工具 cmd/fxshot 用 FX_ASSET_DIR 透传，ui 包只认 WS_ASSET_DIR。
//
// 出厂中性（硬口径）：
//   - WS_ASSET_DIR 未设置 ⇒ 本文件所有查询都返回没有覆盖文件，
//     渲染路径走的还是改动前那一条（embedFS + 程序化烘焙），画面逐字节不变。
//   - 出厂二进制 cmd/scr 不读这个环境变量（闸门 grep 证明）。
//
// 语义（逐条对应素材预览工装取证 1.1 节 + 素材落地取证 1 节）：
//   - 目录非空且存在同名文件 ⇒ readPNG 改从该目录读；
//   - 目录里没有的名字 ⇒ 照旧走 embedFS；
//   - 反预乘处理保留（覆盖文件与内嵌文件走完全相同的
//     png.Decode → toNRGBA → unpremultiply 链路，见 readPNG / assetDecode）；
//   - 负缓存语义保留（查过的名字——含不存在——进缓存，逐帧路径不再重复 stat）；
//   - **内嵌素材存在即走贴图分支**（assetAvailable / assetImageAny）：
//     月亮、雨丝、云/雾的内嵌素材是默认路径，覆盖目录仍可覆盖它。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"gioui.org/op/paint"
)

// assetDirEnv 是本工装唯一的环境变量名，且只在 ui 包内读取。
const assetDirEnv = "WS_ASSET_DIR"

// assetOverrideNames 工装认识的覆盖文件名（出图工具启动行打印哪些生效用）。
// 顺序即四条分支的口径：天空三张、月亮、太阳、雨丝。
var assetOverrideNames = []string{
	"sky_day.png",
	"sky_night.png",
	"sky_sunset.png",
	"moon.png",
	"sun.png",
	"rain.png",
}

// assetDir 读覆盖目录；空串 = 未启用（出厂路径）。
func assetDir() string { return os.Getenv(assetDirEnv) }

type assetPathEntry struct {
	path string
	ok   bool
}

var assetPathCache = struct {
	mu sync.Mutex
	m  map[string]assetPathEntry
}{m: map[string]assetPathEntry{}}

// assetLookupPath 查 name 在覆盖目录里的同名普通文件。
//
// 找法：<dir>/<name>，找不到再试 <dir>/<basename(name)>
// （readPNG 传进来的名字带 embed/ 前缀，覆盖目录里按裸文件名放，两个写法都收）。
//
// 命中与未命中都进缓存 ⇒ 每帧路径里每个名字最多一次 stat（第一帧付，之后纯查表）。
// 未启用覆盖目录时连缓存都不进，直接返回 false。
func assetLookupPath(name string) (string, bool) {
	dir := assetDir()
	if dir == "" || name == "" {
		return "", false
	}
	key := dir + "\x00" + name
	assetPathCache.mu.Lock()
	defer assetPathCache.mu.Unlock()
	if e, hit := assetPathCache.m[key]; hit {
		return e.path, e.ok
	}
	cands := []string{filepath.Join(dir, name)}
	if base := filepath.Base(name); base != name {
		cands = append(cands, filepath.Join(dir, base))
	}
	res := assetPathEntry{}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			res = assetPathEntry{path: p, ok: true}
			break
		}
	}
	assetPathCache.m[key] = res
	return res.path, res.ok
}

type assetImageEntry struct {
	img *image.NRGBA
	op  paint.ImageOp
	sz  image.Point
	ok  bool
}

var assetImageCache = struct {
	mu sync.Mutex
	m  map[string]assetImageEntry
}{m: map[string]assetImageEntry{}}

// assetImage 读覆盖目录里的一张 PNG，返回反预乘后的像素 + ImageOp + 尺寸。
//
// 解码口径与 readPNG 逐字相同（png.Decode → toNRGBA → unpremultiply）。
// 未启用 / 文件不存在 / 解码失败 ⇒ ok=false（解码失败不回落 embedFS：
// 覆盖文件存在即取代该名字的嵌入素材，坏了就当这张素材没有）。
// 解码结果进缓存：天空/月亮/太阳的烘焙按缓存键重建时才付一次，
// 雨丝贴图逐帧只查表。
func assetImage(name string) (*image.NRGBA, paint.ImageOp, image.Point, bool) {
	if name == "" {
		return nil, paint.ImageOp{}, image.Point{}, false
	}
	dir := assetDir()
	if dir == "" {
		return nil, paint.ImageOp{}, image.Point{}, false
	}
	key := dir + "\x00" + name
	assetImageCache.mu.Lock()
	defer assetImageCache.mu.Unlock()
	if e, hit := assetImageCache.m[key]; hit {
		return e.img, e.op, e.sz, e.ok
	}
	res := assetImageEntry{}
	if p, ok := assetLookupPath(name); ok {
		f, err := os.Open(p)
		if err == nil {
			im, derr := png.Decode(f)
			f.Close()
			if derr == nil {
				n := unpremultiply(toNRGBA(im))
				res = assetImageEntry{
					img: n,
					op:  paint.NewImageOp(n),
					sz:  n.Bounds().Size(),
					ok:  true,
				}
			}
		}
	}
	assetImageCache.m[key] = res
	return res.img, res.op, res.sz, res.ok
}

// assetAvailable 素材在不在：**覆盖目录里的同名文件优先，其次内嵌素材**。
//
// B 版素材落地后，内嵌素材就是默认路径（月亮 / 雨丝 / 云 / 雾都在 ui/embed 里），
// 覆盖目录（WS_ASSET_DIR）仍可覆盖它 —— 工装语义不变：
// 目录里有同名文件 ⇒ 用目录里的；目录里没有 ⇒ 用内嵌的；内嵌也没有 ⇒ 程序化画法。
//
// 天空底图（sky_day/sky_night/sky_sunset）不在内嵌素材里（天空贴图落选，
// 画面保持自绘渐变），本函数对这三个名字返回的仍是"目录里有没有"。
func assetAvailable(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := assetLookupPath(name); ok {
		return true
	}
	return embedHas("embed/" + name)
}

// assetSourceTag 一张素材的**来源**标识，给烘焙缓存键用：
//
//	覆盖目录里的同名文件 → "<dir>|<name>"
//	内嵌素材 embed/<name> → "embed|<name>"
//	都没有               → ""
//
// 为什么缓存键必须带来源：月亮层/天空层的缓存键里那一项原本只记文件名，
// 内嵌素材存在即走贴图之后，**同一个文件名对应两个来源**
// （内嵌一张、覆盖目录一张），只记名字会让"启用覆盖目录后的第一次取层"
// 直接命中内嵌版那条缓存 ⇒ 覆盖不生效（自检 TestMoonBranchConditions 抓到：
// 贴图版与基线版逐像素相同）。
func assetSourceTag(name string) string {
	if name == "" {
		return ""
	}
	if _, ok := assetLookupPath(name); ok {
		return assetDir() + "|" + name
	}
	if embedHas("embed/" + name) {
		return "embed|" + name
	}
	return ""
}

// assetDecode 解码口径的唯一实现：png.Decode → toNRGBA → unpremultiply。
// 覆盖目录与内嵌素材两条来源共用，反预乘链路对两类文件同样生效。
func assetDecode(b []byte) *image.NRGBA {
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return unpremultiply(toNRGBA(im))
}

// assetImageAny 取一张贴图：覆盖目录优先，其次内嵌素材。
//
// 与 assetImage 的区别 = 少了"未启用覆盖目录就返回 false"这一条；
// 解码口径、缓存结构、"解码失败视为该素材不存在"的语义全部一致。
// 缓存键含目录（内嵌路径的键 = 空目录），切换覆盖目录后不会串味。
func assetImageAny(name string) (*image.NRGBA, paint.ImageOp, image.Point, bool) {
	if name == "" {
		return nil, paint.ImageOp{}, image.Point{}, false
	}
	key := assetDir() + "\x00" + name
	assetImageCache.mu.Lock()
	defer assetImageCache.mu.Unlock()
	if e, hit := assetImageCache.m[key]; hit {
		return e.img, e.op, e.sz, e.ok
	}
	var n *image.NRGBA
	if p, ok := assetLookupPath(name); ok {
		if f, err := os.Open(p); err == nil {
			b, rerr := io.ReadAll(f)
			f.Close()
			if rerr == nil {
				n = assetDecode(b)
			}
		}
	} else if b, err := embedFS.ReadFile("embed/" + name); err == nil {
		n = assetDecode(b)
	}
	res := assetImageEntry{}
	if n != nil {
		res = assetImageEntry{
			img: n,
			op:  paint.NewImageOp(n),
			sz:  n.Bounds().Size(),
			ok:  true,
		}
	}
	assetImageCache.m[key] = res
	return res.img, res.op, res.sz, res.ok
}

// AssetOverrideStatus 列出覆盖目录里存在、且工装认识的素材名。
// 出图工具启动行用它打印哪些覆盖文件生效。未启用覆盖目录时返回空。
func AssetOverrideStatus() []string {
	if assetDir() == "" {
		return nil
	}
	out := make([]string, 0, len(assetOverrideNames))
	for _, n := range assetOverrideNames {
		if _, ok := assetLookupPath(n); ok {
			out = append(out, n)
		}
	}
	return out
}

// assetOverrideEnabled 覆盖目录是否启用（未启用 = 出厂路径，逐字节不变）。
func assetOverrideEnabled() bool { return assetDir() != "" }

// assetResetForTest 清空覆盖目录相关缓存。
// 测试里切换 WS_ASSET_DIR 后必须调，否则上一个用例的不存在结果会被缓存带进下一个用例。
func assetResetForTest() {
	assetPathCache.mu.Lock()
	assetPathCache.m = map[string]assetPathEntry{}
	assetPathCache.mu.Unlock()
	assetImageCache.mu.Lock()
	assetImageCache.m = map[string]assetImageEntry{}
	assetImageCache.mu.Unlock()
	rainTexMemo.mu.Lock()
	rainTexMemo.dir = ""
	rainTexMemo.filled = false
	rainTexMemo.img = paint.ImageOp{}
	rainTexMemo.sz = image.Point{}
	rainTexMemo.ok = false
	rainTexMemo.mu.Unlock()
}

// ── 重采样（外部素材进烘焙层时用；出厂路径不会走到）──────────

// sampleBilinear 双线性采样，逐通道线性插值（非预乘口径）。
// 坐标 fx/fy 以像素中心计（fx=0 指向第 0 个像素中心），越界按边缘钳位。
func sampleBilinear(src *image.NRGBA, fx, fy float64) color.NRGBA {
	if src == nil {
		return color.NRGBA{}
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return color.NRGBA{}
	}
	x := fx - 0.5
	y := fy - 0.5
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	tx := x - float64(x0)
	ty := y - float64(y0)
	cx0 := x0
	if cx0 < 0 {
		cx0 = 0
	}
	if cx0 > w-1 {
		cx0 = w - 1
	}
	cx1 := x0 + 1
	if cx1 < 0 {
		cx1 = 0
	}
	if cx1 > w-1 {
		cx1 = w - 1
	}
	cy0 := y0
	if cy0 < 0 {
		cy0 = 0
	}
	if cy0 > h-1 {
		cy0 = h - 1
	}
	cy1 := y0 + 1
	if cy1 < 0 {
		cy1 = 0
	}
	if cy1 > h-1 {
		cy1 = h - 1
	}
	get := func(px, py int) [4]float64 {
		i := src.PixOffset(b.Min.X+px, b.Min.Y+py)
		return [4]float64{
			float64(src.Pix[i+0]), float64(src.Pix[i+1]),
			float64(src.Pix[i+2]), float64(src.Pix[i+3]),
		}
	}
	p00 := get(cx0, cy0)
	p10 := get(cx1, cy0)
	p01 := get(cx0, cy1)
	p11 := get(cx1, cy1)
	var v [4]float64
	for c := 0; c < 4; c++ {
		v[c] = p00[c]*(1-tx)*(1-ty) + p10[c]*tx*(1-ty) + p01[c]*(1-tx)*ty + p11[c]*tx*ty
	}
	return color.NRGBA{R: clamp8(v[0]), G: clamp8(v[1]), B: clamp8(v[2]), A: clamp8(v[3])}
}

// coverScale 等比缩放系数：把 src 铺满矩形 r 且不变形（cover，超出部分裁掉）。
func coverScale(sw, sh, rw, rh int) float64 {
	if sw <= 0 || sh <= 0 || rw <= 0 || rh <= 0 {
		return 0
	}
	sx := float64(rw) / float64(sw)
	sy := float64(rh) / float64(sh)
	if sx > sy {
		return sx
	}
	return sy
}

// blitCoverReplace 把 src 等比缩放（cover）后**整片写进** dst 全图。
// 用途 = 天空底图取代渐变色填充：dst 被逐像素覆盖，不叠加。
func blitCoverReplace(dst, src *image.NRGBA) {
	if dst == nil || src == nil {
		return
	}
	db := dst.Bounds()
	w, h := db.Dx(), db.Dy()
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	s := coverScale(sw, sh, w, h)
	if s <= 0 {
		return
	}
	// 居中：缩放后的源比目标大，两侧各裁掉一半
	ox := (s*float64(sw) - float64(w)) / 2
	oy := (s*float64(sh) - float64(h)) / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := sampleBilinear(src, (float64(x)+ox)/s, (float64(y)+oy)/s)
			i := dst.PixOffset(db.Min.X+x, db.Min.Y+y)
			dst.Pix[i+0] = c.R
			dst.Pix[i+1] = c.G
			dst.Pix[i+2] = c.B
			dst.Pix[i+3] = c.A
		}
	}
}

// blitCoverOver 把 src 等比缩放（cover）后按 src-over 合成进 dst 的矩形 r。
// 用途 = 太阳盘：贴图画在已有的 Lorentz 光斑之上。
func blitCoverOver(dst, src *image.NRGBA, r image.Rectangle) {
	if dst == nil || src == nil {
		return
	}
	db := dst.Bounds()
	r = r.Intersect(db)
	rw, rh := r.Dx(), r.Dy()
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	s := coverScale(sw, sh, rw, rh)
	if s <= 0 {
		return
	}
	ox := (s*float64(sw) - float64(rw)) / 2
	oy := (s*float64(sh) - float64(rh)) / 2
	for y := 0; y < rh; y++ {
		for x := 0; x < rw; x++ {
			c := sampleBilinear(src, (float64(x)+ox)/s, (float64(y)+oy)/s)
			a := float64(c.A) / 255.0
			if a <= 0 {
				continue
			}
			blendPix(dst, r.Min.X+x, r.Min.Y+y, c, a)
		}
	}
}

package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// TestUnpremultiplyDecodesPremultipliedPNG 对应要求：
// 构造一张 2×2 预乘图（如 (128,128,128,128)），断言反预乘后 RGB 为 255、alpha 不变；
// a==0 的像素 RGB 为 0。
// 构造口径：Go 的 image.RGBA 就是**预乘口径**（color.RGBA 的文档明说 premultiplied），
// png.Encode 把这些字节原样写进 PNG，png.Decode 又按**非预乘**口径解成 NRGBA
// —— 这正是 glow.png / fog1.png 在低性能 x86 实机上的遭遇（实测根因）。
func TestUnpremultiplyDecodesPremultipliedPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	// (0,0) 半透明灰：预乘值 (128,128,128,128) ⇒ 反预乘后 RGB 应为 255
	src.Set(0, 0, color.RGBA{R: 128, G: 128, B: 128, A: 128})
	// (1,0) 全不透明：alpha 255 ⇒ 反预乘是恒等
	src.Set(1, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	// (0,1) 全透明：a==0 ⇒ RGB 必须置 0
	src.Set(0, 1, color.RGBA{R: 77, G: 77, B: 77, A: 0})
	// (1,1) 低 alpha：64/255 ⇒ 64*255/64 = 255
	src.Set(1, 1, color.RGBA{R: 64, G: 64, B: 64, A: 64})

	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("编码预乘 PNG 失败: %v", err)
	}
	im, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	got := unpremultiply(toNRGBA(im))
	if got == nil {
		t.Fatalf("unpremultiply 返回 nil")
	}
	type px struct{ r, g, b, a uint8 }
	want := map[[2]int]px{
		{0, 0}: {255, 255, 255, 128}, // RGB 255、alpha 不变
		{1, 0}: {200, 100, 50, 255},  // 恒等
		{0, 1}: {0, 0, 0, 0},         // a==0 ⇒ RGB 置 0
		{1, 1}: {255, 255, 255, 64},  // 低 alpha 也拉回 255
	}
	for k, w := range want {
		i := got.PixOffset(k[0], k[1])
		g := px{got.Pix[i], got.Pix[i+1], got.Pix[i+2], got.Pix[i+3]}
		if g != w {
			t.Errorf("像素 (%d,%d) = %v，期望 %v", k[0], k[1], g, w)
		}
	}
}

// TestReadPNGUnpremultipliesGlow 用给的样本数据直接实测：
// glow.png 水平中线的 (81,81,81,50) (94,94,94,62) (112,112,112,77)
// (124,124,124,90) (136,136,136,101) —— 反预乘后 RGB 应恒为 255。
// 样本给的是 RGB 与 alpha **同步衰减**的五个点，没给 x 坐标，
// 故按 alpha 序列定位：中线上的半透明 alpha 序列必须出现 50/62/77/90/101
// （= 样本的 alpha），且这些像素反预乘后 RGB 全为 255。
func TestReadPNGUnpremultipliesGlow(t *testing.T) {
	im, ok := readPNG("embed/glow.png")
	if !ok {
		t.Skip("glow.png 未嵌入")
	}
	n, ok := im.(*image.NRGBA)
	if !ok {
		t.Fatalf("readPNG 应返回 *image.NRGBA，得到 %T", im)
	}
	b := n.Bounds()
	mid := b.Min.Y + b.Dy()/2
	want := []uint8{50, 62, 77, 90, 101}
	for _, a := range want {
		found := false
		for x := b.Min.X; x < b.Max.X; x++ {
			i := n.PixOffset(b.Min.X+x, mid)
			if n.Pix[i+3] != a {
				continue
			}
			found = true
			if n.Pix[i] != 255 || n.Pix[i+1] != 255 || n.Pix[i+2] != 255 {
				t.Errorf("中线 alpha=%d 的像素 (x=%d) 反预乘后 RGB = (%d,%d,%d)，期望 255",
					a, b.Min.X+x, n.Pix[i], n.Pix[i+1], n.Pix[i+2])
			}
			break
		}
		if !found {
			t.Errorf("中线上找不到 alpha=%d 的样本像素", a)
		}
	}
	// 全透明像素不得留下颜色（黑环的另一半来源）
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			i := n.PixOffset(b.Min.X+x, b.Min.Y+y)
			if n.Pix[i+3] == 0 && (n.Pix[i] != 0 || n.Pix[i+1] != 0 || n.Pix[i+2] != 0) {
				t.Fatalf("a==0 的像素 (%d,%d) 还留着 RGB (%d,%d,%d)",
					x, y, n.Pix[i], n.Pix[i+1], n.Pix[i+2])
			}
		}
	}
}

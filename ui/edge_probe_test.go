package ui

import (
	"image"
	"testing"
)

// TestEdgeProfileIsRamp 量化"圆弧被拉平"：沿云精灵顶边取一条竖直扫描线，
// 看 alpha 剖面是否为**单调斜坡**（无平台）。
// 盒滤波对整张做低通时，圆顶极值会被邻域平均 ⇒ 出现连续相同值（平台）。
func TestEdgeProfileIsRamp(t *testing.T) {
	raw := toNRGBA(mustPNG(t, "embed/cloud1.png"))
	soft := featherAlpha(raw, 1)
	up := upscaleCatmull(soft, spriteSupersample)
	for _, tc := range []struct {
		name string
		img  *image.NRGBA
	}{{"原图", raw}, {"羽化后", soft}, {"超采样2x", up}} {
		b := tc.img.Bounds()
		cx := b.Dx() / 2
		// 从上往下找第一个不透明像素，打印其后 8 个像素的 alpha
		var prof []int
		started := false
		for y := 0; y < b.Dy() && len(prof) < 8; y++ {
			a := int(tc.img.Pix[tc.img.PixOffset(cx, y)+3])
			if !started {
				if a > 0 {
					started = true
				} else {
					continue
				}
			}
			prof = append(prof, a)
		}
		t.Logf("%s 顶边 alpha 剖面 @x=%d: %v", tc.name, cx, prof)
		// 平台判据：连续 4 个完全相同的中间值（既非 0 也非 255）
		plateau := 0
		for i := 1; i < len(prof); i++ {
			if prof[i] == prof[i-1] && prof[i] > 0 && prof[i] < 255 {
				plateau++
			} else {
				plateau = 0
			}
			if plateau >= 3 {
				t.Errorf("%s 出现连续平台（圆弧被拉平）@i=%d v=%d prof=%v", tc.name, i, prof[i], prof)
				break
			}
		}
	}
}

func mustPNG(t *testing.T, name string) *image.NRGBA {
	t.Helper()
	im, ok := readPNG(name)
	if !ok {
		t.Fatalf("读不到 %s", name)
	}
	return toNRGBA(im)
}

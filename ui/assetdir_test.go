package ui

// 素材覆盖目录（预览工装）的自检。
//
// 覆盖四条分支的生效条件（天空 / 月亮 / 太阳 / 雨）与一条硬判据：
// WS_ASSET_DIR 未设置时，所有查询返回没有覆盖文件，渲染路径与改动前一致。
//
// 逐字节中性（未设置时画面不变）由 ui/pixeldump_test.go 的固定帧出图在改动前后
// 各出一次、逐字节比对来证明，不在本文件里（本文件只证明生效条件）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/DragonTTDream/WeatherScr/ui/sky"
	"github.com/DragonTTDream/WeatherScr/effect"
)

// writeTestPNG 往 dir 里写一张 PNG（Pix 原样写 = 可按预乘编码喂，用来验反预乘）。
func writeTestPNG(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
	if err := os.WriteFile(dir+"/"+name, b.Bytes(), 0o644); err != nil {
		t.Fatalf("写 %s 失败: %v", name, err)
	}
}

// newTestAssetDir 建一个临时覆盖目录。
func newTestAssetDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// useAssetDir 切换覆盖目录并清缓存；用例结束自动还原。
func useAssetDir(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		t.Setenv(assetDirEnv, "")
	} else {
		t.Setenv(assetDirEnv, dir)
	}
	assetResetForTest()
	t.Cleanup(func() { assetResetForTest() })
}

// 未设置 WS_ASSET_DIR 时：全部查询 false，readPNG 走 embedFS。
func TestAssetOverrideDisabledByDefault(t *testing.T) {
	useAssetDir(t, "")
	if assetOverrideEnabled() {
		t.Fatal("未设置环境变量时不应启用覆盖目录")
	}
	if p, ok := assetLookupPath("moon.png"); ok {
		t.Fatalf("未启用时不应命中覆盖文件，得到 %s", p)
	}
	if _, _, _, ok := assetImage("sky_day.png"); ok {
		t.Fatal("未启用时 assetImage 应返回 false")
	}
	if n := sky.SkyOverrideName(true); n != "" {
		t.Fatalf("未启用时天空底图应为空，得到 %s", n)
	}
	if n := sky.SkyOverrideName(false); n != "" {
		t.Fatalf("未启用时天空底图应为空，得到 %s", n)
	}
	if n := sky.SunOverrideName(); n != "" {
		t.Fatalf("未启用时太阳贴图应为空（内嵌素材里没有 sun.png），得到 %s", n)
	}
	// 月亮 / 雨丝：**内嵌素材存在即走**（素材落地取证 1）。
	// 未启用覆盖目录时这两项走的是内嵌素材，不再是"没有贴图"。
	if n := sky.MoonOverrideName(); n != "moon.png" {
		t.Fatalf("未启用覆盖目录时月亮应走内嵌素材，得到 %q", n)
	}
	if _, sz, ok := rainTexOverride(); !ok || sz.X <= 0 {
		t.Fatal("未启用覆盖目录时雨丝应走内嵌素材 embed/rain.png")
	}
	if tag := assetSourceTag("moon.png"); tag != "embed|moon.png" {
		t.Fatalf("内嵌素材的来源标识应为 embed|moon.png，得到 %q", tag)
	}
	if k := sky.SkyBakeTexKey(true); k != "" {
		t.Fatalf("未启用时天空键的贴图项应为空，得到 %q", k)
	}
	if st := AssetOverrideStatus(); len(st) != 0 {
		t.Fatalf("未启用时状态表应为空，得到 %v", st)
	}
	// 出厂路径：readPNG 仍从 embedFS 出图
	im, ok := readPNG("embed/glow.png")
	if !ok {
		t.Fatal("embedFS 路径应读到 glow.png")
	}
	if im.Bounds().Dx() <= 0 {
		t.Fatal("embedFS 读到的图尺寸异常")
	}
}

// 覆盖目录里的同名文件 ⇒ readPNG 改从目录读，且走同一套反预乘。
// 目录里没有的名字 ⇒ 照旧 embedFS。
func TestAssetOverrideReadsExternalAndUnpremultiplies(t *testing.T) {
	dir := newTestAssetDir(t)
	// 预乘编码：straight (200,100,50) α=100 ⇒ 文件里存 (78,39,19)
	prem := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			i := prem.PixOffset(x, y)
			prem.Pix[i+0] = 78
			prem.Pix[i+1] = 39
			prem.Pix[i+2] = 19
			prem.Pix[i+3] = 100
		}
	}
	writeTestPNG(t, dir, "glow.png", prem)
	useAssetDir(t, dir)

	im, ok := readPNG("embed/glow.png")
	if !ok {
		t.Fatal("覆盖目录里的 glow.png 应读到")
	}
	n, ok := im.(*image.NRGBA)
	if !ok {
		t.Fatalf("readPNG 应返回 *image.NRGBA，得到 %T", im)
	}
	c := n.NRGBAAt(0, 0)
	// 反预乘：78*255/100 = 198.9 → 199，39*255/100 = 99.45 → 99，19*255/100 = 48.45 → 48
	if c.A != 100 {
		t.Fatalf("alpha 应保持 100，得到 %d", c.A)
	}
	if diff := absInt(int(c.R) - 199); diff > 2 {
		t.Errorf("反预乘 R 应≈199，得到 %d", c.R)
	}
	if diff := absInt(int(c.G) - 99); diff > 2 {
		t.Errorf("反预乘 G 应≈99，得到 %d", c.G)
	}
	if diff := absInt(int(c.B) - 48); diff > 2 {
		t.Errorf("反预乘 B 应≈48，得到 %d", c.B)
	}

	// 目录里没有的名字 ⇒ embedFS（负缓存语义不变）
	im2, ok := readPNG("embed/cloud1.png")
	if !ok {
		t.Fatal("目录里没有 cloud1.png 时应回落 embedFS")
	}
	if im2.Bounds().Dx() <= 0 {
		t.Fatal("embedFS 回落读到的图尺寸异常")
	}
}

// 负缓存：查过的不存在结果不会因目录里后来出现同名文件而失效
// （与 loadSprite 的负缓存同一语义：一次加载周期内稳定）。
func TestAssetOverrideNegativeCache(t *testing.T) {
	dir := newTestAssetDir(t)
	useAssetDir(t, dir)
	if _, ok := assetLookupPath("sun.png"); ok {
		t.Fatal("目录为空时不应命中")
	}
	plain := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	writeTestPNG(t, dir, "sun.png", plain)
	if _, ok := assetLookupPath("sun.png"); ok {
		t.Fatal("已缓存的不存在结果不应被新文件翻案")
	}
	assetResetForTest()
	if _, ok := assetLookupPath("sun.png"); !ok {
		t.Fatal("清缓存后应命中")
	}
}

// 天空分支（规格 1.2）：
// is_day==1 → sky_day，is_day==0 → sky_night，日落窗口不启用（无日出日落数据源），
// 夜间星星照旧烘在贴图之上，缓存键含贴图名。
func TestSkyBranchConditions(t *testing.T) {
	dir := newTestAssetDir(t)
	mk := func(v color.NRGBA, w, h int) image.Image {
		im := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				im.SetNRGBA(x, y, v)
			}
		}
		return im
	}
	writeTestPNG(t, dir, "sky_day.png", mk(color.NRGBA{R: 200, G: 120, B: 40, A: 255}, 40, 80))
	writeTestPNG(t, dir, "sky_night.png", mk(color.NRGBA{R: 10, G: 20, B: 90, A: 255}, 40, 80))
	writeTestPNG(t, dir, "sky_sunset.png", mk(color.NRGBA{R: 250, G: 40, B: 40, A: 255}, 40, 80))
	useAssetDir(t, dir)

	if n := sky.SkyOverrideName(true); n != "sky_day.png" {
		t.Fatalf("白天应选 sky_day.png，得到 %s", n)
	}
	if n := sky.SkyOverrideName(false); n != "sky_night.png" {
		t.Fatalf("夜间应选 sky_night.png，得到 %s", n)
	}
	if sky.SkySunsetWindowActive() {
		t.Fatal("无日出日落数据源时日落窗口不应启用")
	}
	if n := sky.SkyOverrideName(true); n == "sky_sunset.png" {
		t.Fatal("日落窗口未启用时不应选 sky_sunset")
	}
	if sky.SkyBakeTexKey(true) == "" || sky.SkyBakeTexKey(false) == "" {
		t.Fatal("启用覆盖后天空缓存键的贴图项不应为空")
	}
	if sky.SkyBakeTexKey(true) == sky.SkyBakeTexKey(false) {
		t.Fatal("白天/夜间的天空缓存键应不同（避免昼夜切换后仍用旧图）")
	}

	// 贴图取代渐变色填充：烘焙结果应等于贴图缩放后的颜色（取中部一点判色）
	img := sky.RenderSkyCPU(40, 80, effect.Clear, true, nil, 0)
	c := img.NRGBAAt(20, 40)
	if c.R < 150 || c.G < 60 || c.B > 120 {
		t.Fatalf("天空底图未取自贴图，中部像素 = %v", c)
	}
	// 夜间：星星照旧画在贴图之上 ⇒ 烘焙结果不等于纯贴图
	nimg := sky.RenderSkyCPU(40, 80, effect.Clear, false, nil, 0)
	plain := image.NewNRGBA(image.Rect(0, 0, 40, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			plain.SetNRGBA(x, y, color.NRGBA{R: 10, G: 20, B: 90, A: 255})
		}
	}
	diff := 0
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			if nimg.NRGBAAt(x, y) != plain.NRGBAAt(x, y) {
				diff++
			}
		}
	}
	if diff == 0 {
		t.Fatal("夜间烘焙应包含画在贴图之上的星星/暗角，不应等于纯贴图")
	}
	t.Logf("夜间烘焙相对纯贴图的差异像素 = %d/3200", diff)

	// 缓存键含贴图名：昼夜两次 skyCached 应各建一次（不共用旧图）
	sky.SkyCache = sky.SkyCacheType{}
	size := image.Pt(80, 160)
	eDay := sky.SkyCached(size, effect.Clear, true, nil, 0)
	eNight := sky.SkyCached(size, effect.Clear, false, nil, 0)
	if eDay == eNight {
		t.Fatal("白天与夜间不应共用同一张烘焙纹理")
	}
	if sky.SkyCache.Key.Tex == "" {
		t.Fatal("天空缓存键的贴图项不应为空")
	}
	t.Logf("sky.SkyCache.Key.Tex = %s", sky.SkyCache.Key.Tex)
}

// 月亮分支（规格 1.3）：亮面用贴图表面细节、暗面保持透明、缓存键含贴图名。
func TestMoonBranchConditions(t *testing.T) {
	dir := newTestAssetDir(t)
	// 一张明显偏红的月亮贴图（程序化 core 是 246,244,230 偏白，容易区分）
	moon := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			moon.SetNRGBA(x, y, color.NRGBA{R: 250, G: 60, B: 60, A: 255})
		}
	}
	useAssetDir(t, "")
	w, h := 540, 960
	ph := f64p(0.5)
	off := sky.MoonLayerGet(w, h, ph)
	assetResetForTest()
	writeTestPNG(t, dir, "moon.png", moon)
	useAssetDir(t, dir)
	on := sky.MoonLayerGet(w, h, ph)
	if on == nil || off == nil {
		t.Fatal("月亮层不应为 nil")
	}
	if !strings.Contains(sky.MoonLayerCache.Key.Tex, "moon.png") {
		t.Fatalf("月亮层缓存键应含贴图名与来源，得到 %q", sky.MoonLayerCache.Key.Tex)
	}
	if !strings.Contains(sky.MoonLayerCache.Key.Tex, dir) {
		t.Fatalf("月亮层缓存键应含**来源**（覆盖目录路径），得到 %q", sky.MoonLayerCache.Key.Tex)
	}
	// 亮面颜色被贴图改变（满月 = 整盘亮面）
	cx, cy := int(0.72*float64(w)), int(0.095*float64(h))
	ox, oy := on.Off.X, on.Off.Y
	cOn := on.Img.NRGBAAt(cx-ox, cy-oy)
	cOff := off.Img.NRGBAAt(cx-ox, cy-oy)
	if !(cOn.R > 200 && cOn.G < cOff.G) {
		t.Fatalf("亮面未取贴图表面细节：贴图版=%v 程序化版=%v", cOn, cOff)
	}
	t.Logf("满月月心：贴图版=%v 程序化版=%v", cOn, cOff)
	// 暗面保持透明：眉月 0.125 的左半边（亮面在右）应全透明
	ph2 := f64p(0.125)
	layer := sky.MoonLayerGet(w, h, ph2)
	if layer == nil {
		t.Fatal("眉月层不应为 nil")
	}
	rc := int(sky.MoonRcFrac * float64(w))
	dark := 0
	for dy := -rc; dy <= rc; dy++ {
		for dx := -rc; dx <= 0; dx++ {
			x, y := cx+dx, cy+dy
			if x < ox || y < oy || x >= ox+layer.Size.X || y >= oy+layer.Size.Y {
				continue
			}
			if dx*dx+dy*dy > rc*rc {
				continue
			}
			if layer.Img.NRGBAAt(x-ox, y-oy).A == 0 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("眉月暗面（左半）应保持全透明")
	}
	t.Logf("眉月左半透明像素 = %d", dark)
}

// 太阳分支（规格 1.4）：有 sun.png 时盘用贴图，Lorentz 光斑（A=200、L=0.08w）保留。
func TestSunBranchConditions(t *testing.T) {
	dir := newTestAssetDir(t)
	sun := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			sun.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 255, A: 255})
		}
	}
	w, h := 540, 960
	useAssetDir(t, "")
	off := sky.RenderSkyCPU(w/2, h/2, effect.Clear, true, nil, 0)
	assetResetForTest()
	writeTestPNG(t, dir, "sun.png", sun)
	useAssetDir(t, dir)
	on := sky.RenderSkyCPU(w/2, h/2, effect.Clear, true, nil, 0)
	cx, cy := int(0.72*float64(w/2)), int(0.095*float64(h/2))
	cOn := on.NRGBAAt(cx, cy)
	cOff := off.NRGBAAt(cx, cy)
	if !(cOn.B > 150 && cOn.G < 100) {
		t.Fatalf("太阳盘未取贴图：贴图版=%v 程序化版=%v", cOn, cOff)
	}
	// Lorentz 光斑口径不变：贴图半径 0.11w 之外（0.25w 处）逐像素不变
	far := 0
	total := 0
	for dy := -20; dy <= 20; dy++ {
		x := cx + int(0.25*float64(w/2))
		y := cy + dy
		if x >= on.Bounds().Dx() || y >= on.Bounds().Dy() {
			continue
		}
		total++
		if on.NRGBAAt(x, y) == off.NRGBAAt(x, y) {
			far++
		}
	}
	if far != total {
		t.Fatalf("贴图外的 Lorentz 光斑应逐像素不变，命中 %d/%d", far, total)
	}
	t.Logf("贴图半径口径 0.11w = %.1f px，探针距离 %.1f px，%d/%d 像素逐字节相同",
		0.11*float64(w/2), float64(cx+int(0.25*float64(w/2))-cx), far, total)
}

// 雨分支（取证 1.5 + 素材落地取证 1）：
// 目录里没有 rain.png ⇒ 走**内嵌** embed/rain.png（内嵌素材存在即走）；
// 目录里有同名文件 ⇒ 覆盖它。
func TestRainBranchConditions(t *testing.T) {
	dir := newTestAssetDir(t)
	useAssetDir(t, dir)
	img, sz, ok := rainTexOverride()
	if !ok {
		t.Fatal("目录里没有 rain.png 时应走内嵌 embed/rain.png")
	}
	if base, bok := readPNG("embed/rain.png"); !bok {
		t.Fatal("内嵌 embed/rain.png 读不到")
	} else if sz != base.Bounds().Size() {
		t.Fatalf("雨丝贴图尺寸应为内嵌素材尺寸 %v，得到 %v", base.Bounds().Size(), sz)
	}
	if img.Size().X != sz.X {
		t.Fatal("ImageOp 尺寸应与贴图一致")
	}
	t.Logf("内嵌雨丝 = %dx%d（覆盖目录未启用时的默认路径）", sz.X, sz.Y)
	rain := image.NewNRGBA(image.Rect(0, 0, 8, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 8; x++ {
			rain.SetNRGBA(x, y, color.NRGBA{R: 225, G: 235, B: 245, A: 200})
		}
	}
	writeTestPNG(t, dir, "rain.png", rain)
	assetResetForTest()
	img, sz, ok = rainTexOverride()
	if !ok {
		t.Fatal("有 rain.png 时应启用贴图")
	}
	if sz.X != 8 || sz.Y != 40 {
		t.Fatalf("覆盖目录里的 rain.png 应取代内嵌素材，得到 %v", sz)
	}
	if img.Size().X != 8 {
		t.Fatal("ImageOp 尺寸应与贴图一致")
	}
	// 雪/雷共用这条分支的判据：类别判据在 Draw 里（pkDrop 全部、pkFlake 仅 Snow）
	f := NewField(effect.Snow, true, 20261015)
	if f == nil {
		t.Fatal("NewField 返回 nil")
	}
}

// 状态表：只列目录里存在且工装认识的名字。
func TestAssetOverrideStatusList(t *testing.T) {
	dir := newTestAssetDir(t)
	plain := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	writeTestPNG(t, dir, "moon.png", plain)
	writeTestPNG(t, dir, "notes.txt", plain) // 不是素材
	useAssetDir(t, dir)
	st := AssetOverrideStatus()
	if len(st) != 1 || st[0] != "moon.png" {
		t.Fatalf("状态表应只含 moon.png，得到 %v", st)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

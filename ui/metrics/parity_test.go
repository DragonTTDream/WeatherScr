package metrics

// TestMetricsParity 钉死 R5 迁移的对拍（plans/active.md:213 ui/metrics 卡判据 3）。
//
// 常量来源 = 迁移**前**的 ui 旧实现跑同一固定输入打印出来的数字（逐字抄录，不改位）：
//
//	取数件 = ui/zz_pre_metrics_test.go（临时，取数后删除），命令
//	ZZ_PRE_METRICS=1 go test -tags=novulkan,nox11 -count=1 -run TestZZPreMetricsNumbers ./ui -v
//	数字 = 该命令 stdout 的 PRE 行（%v 全精度打印），本文件逐字抄录
//
// 判据：迁移后的每个导出函数在固定输入下与旧实现的数字**逐位相等**（不是近似）。
// 浮点对拍用精确相等：公式逐字搬移 ⇒ 逐位相同；一旦有最后一位不同，就是公式被动过。

import (
	"image"
	"image/color"
	"os"
	"strconv"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

func TestMetricsParity(t *testing.T) {
	same := func(name string, got, want float64) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %.17g，对拍常量 %.17g（逐位相等判据）", name, got, want)
		}
	}
	sameInt := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %d，对拍常量 %d", name, got, want)
		}
	}

	// ---- 固定输入（与取数件逐字相同） ----
	pWidths := []float64{768, 973, 1178, 512, 921.6}
	pBands := []float64{973, 973, 973}
	pLayers := []Layer{
		{Start: 100, Width: 800, Pi: -1, Cy: 0.30},
		{Start: 600, Width: 700, Pi: 0, Cy: 0.31},
		{Start: 2500, Width: 640, Pi: 1, Cy: 0.31},
		{Start: 3100, Width: 1200, Pi: 2, Cy: 0.55},
		{Start: -50, Width: 300, Pi: 3, Cy: 0.12},
	}
	pFull := []Layer{
		{Start: 0, Width: 4000, Pi: -1, Cy: 0.30},
		{Start: 900, Width: 2100, Pi: 4, Cy: 0.30},
		{Start: 3000, Width: 500, Pi: 5, Cy: 0.31},
	}
	pPair := []Layer{
		{Start: 100, Width: 800, Pi: 0, Cy: 0.30},
		{Start: 700, Width: 800, Pi: 1, Cy: 0.31},
		{Start: 1500, Width: 900, Pi: 2, Cy: 0.90},
	}
	sizes := []image.Point{{512, 256}, {400, 200}}
	prof := []float64{0.25, 0.5, 0.75, 1, 0}

	// ---- BandWidthsNominal（netDiv = ui 侧 bandNetDiv 的取值 1.00） ----
	const netDiv = 1.00
	wantN1 := []float64{768}
	wantN3 := []float64{768, 972.79999999999995, 1177.5999999999999}
	wantN6 := []float64{768, 849.92000000000007, 931.84000000000003, 1013.76, 1095.6800000000001, 1177.5999999999999}
	for _, c := range []struct {
		n    int
		want []float64
	}{{1, wantN1}, {3, wantN3}, {6, wantN6}} {
		got := BandWidthsNominal(512, c.n, netDiv)
		if len(got) != len(c.want) {
			t.Fatalf("BandWidthsNominal(512, %d) 长度 = %d，期望 %d", c.n, len(got), len(c.want))
		}
		for i := range c.want {
			same("BandWidthsNominal(512,"+itoa(c.n)+")["+itoa(i)+"]", got[i], c.want[i])
		}
	}

	// ---- SpanOf / AvgLayersOf ----
	spanA := SpanOf(pWidths, 1080)
	spanB := SpanOf(pBands, 1080)
	same("SpanOf(pWidths,1080)", spanA, 2821.04)
	same("SpanOf(pBands,1080)", spanB, 3026)
	same("SpanOf(nil,1080)", SpanOf(nil, 1080), 1080)
	same("AvgLayersOf(pWidths,spanA)", AvgLayersOf(pWidths, spanA), 1.5429061622663984)
	same("AvgLayersOf(pBands,spanB)", AvgLayersOf(pBands, spanB), 0.96463978849966958)
	same("AvgLayersOf(pWidths,0)", AvgLayersOf(pWidths, 0), 0)

	// ---- DualPctOf ----
	same("DualPctOf(pWidths,spanA)", DualPctOf(pWidths, spanA), 54.290616226639834)
	same("DualPctOf(pBands,spanB)", DualPctOf(pBands, spanB), 0)
	same("DualPctOf(wide,3000)", DualPctOf([]float64{2000, 1800, 900}, 3000), 56.666666666666664)
	same("DualPctOf(one,3000)", DualPctOf([]float64{500}, 3000), 0)
	same("DualPctOf(wrap,3240)", DualPctOf([]float64{2600, 1200, 700}, 3240), 42.901234567901234)
	same("DualPctOf(full,3240)", DualPctOf([]float64{4000, 900, 900}, 3240), 55.555555555555557)

	// ---- WrapPeriod ----
	for _, c := range []struct {
		v, want float64
	}{{3300, 60}, {-50, 3190}, {0, 0}, {1620, 1620}, {3240, 0}, {6481.5, 1.5}} {
		same("WrapPeriod("+ftoa(c.v)+",3240)", WrapPeriod(c.v, 3240), c.want)
	}
	same("WrapPeriod(12,0)", WrapPeriod(12, 0), 0)

	// ---- DriftLayersStats ----
	lA, dA := DriftLayersStats(pLayers, 3240)
	same("DriftLayersStats(pLayers).layers", lA, 1.1234567901234569)
	same("DriftLayersStats(pLayers).dualPct", dA, 35.493827160493829)
	lB, dB := DriftLayersStats(pFull, 3240)
	same("DriftLayersStats(pFull).layers", lB, 2.0370370370370372)
	same("DriftLayersStats(pFull).dualPct", dB, 80.246913580246911)
	lC, dC := DriftLayersStats(pLayers, 0)
	same("DriftLayersStats(zeroSpan).layers", lC, 0)
	same("DriftLayersStats(zeroSpan).dualPct", dC, 0)

	// ---- DriftDualParticlePairs（bandN = ui 侧 airBandN 的取值 0.12） ----
	const bandN = 0.12
	same("DriftDualParticlePairs(pLayers)", DriftDualParticlePairs(pLayers, 3240, bandN), 0)
	same("DriftDualParticlePairs(pFull)", DriftDualParticlePairs(pFull, 3240, bandN), 0)
	same("DriftDualParticlePairs(pPair)", DriftDualParticlePairs(pPair, 3240, bandN), 6.1728395061728394)

	// ---- DriftArcOverlap ----
	same("DriftArcOverlap(pLayers[0],pLayers[1])", DriftArcOverlap(pLayers[0], pLayers[1], 3240), 300)
	same("DriftArcOverlap(pLayers[2],pLayers[3])", DriftArcOverlap(pLayers[2], pLayers[3], 3240), 40)
	same("DriftArcOverlap(pFull[0],pFull[2])", DriftArcOverlap(pFull[0], pFull[2], 3240), 500)

	// ---- SpriteSizeOr ----
	wantPts := []image.Point{{512, 256}, {400, 200}, {512, 256}, {512, 256}, {512, 256}}
	for i, spr := range []int{0, 1, 5, -1, -3} {
		got := SpriteSizeOr(sizes, spr)
		if got != wantPts[i] {
			t.Errorf("SpriteSizeOr(sizes,%d) = %v，对拍常量 %v", spr, got, wantPts[i])
		}
	}
	if got := SpriteSizeOr(nil, 2); got != (image.Point{}) {
		t.Errorf("SpriteSizeOr(nil,2) = %v，对拍常量 {0 0}", got)
	}

	// ---- MeanCoveragePct ----
	same("MeanCoveragePct(prof)", MeanCoveragePct(prof), 50)
	same("MeanCoveragePct(nil)", MeanCoveragePct(nil), 0)

	// ---- ProbeGTX ----
	var o op.Ops
	gtx := ProbeGTX(&o, 1080, 1920)
	if gtx.Ops != &o {
		t.Errorf("ProbeGTX 的 Ops = %v，期望就是传入的 &o", gtx.Ops)
	}
	sameInt("ProbeGTX Max.X", gtx.Constraints.Max.X, 1080)
	sameInt("ProbeGTX Max.Y", gtx.Constraints.Max.Y, 1920)
	sameInt("ProbeGTX Min.X", gtx.Constraints.Min.X, 1080)
	sameInt("ProbeGTX Min.Y", gtx.Constraints.Min.Y, 1920)

	// ---- OpsLen ----
	paint.FillShape(&o, color.NRGBA{A: 0xff, R: 0x10, G: 0x20, B: 0x30},
		clip.Rect{Max: image.Pt(1080, 1920)}.Op())
	d1, r1 := OpsLen(&o)
	sameInt("OpsLen(1 个 FillShape).data", d1, 26)
	sameInt("OpsLen(1 个 FillShape).refs", r1, 0)
	paint.FillShape(&o, color.NRGBA{A: 0xff, R: 0x40, G: 0x50, B: 0x60},
		clip.Rect{Max: image.Pt(512, 256)}.Op())
	d2, r2 := OpsLen(&o)
	sameInt("OpsLen(2 个 FillShape).data", d2, 52)
	sameInt("OpsLen(2 个 FillShape).refs", r2, 0)
	dn, rn := OpsLen(nil)
	sameInt("OpsLen(nil).data", dn, -1)
	sameInt("OpsLen(nil).refs", rn, -1)

	// ---- WSDbgFrames ----
	os.Setenv("WS_DBG_FRAMES", "1")
	if !WSDbgFrames() {
		t.Errorf("WS_DBG_FRAMES=1 时 WSDbgFrames() = false，对拍常量 true")
	}
	os.Unsetenv("WS_DBG_FRAMES")
	if WSDbgFrames() {
		t.Errorf("WS_DBG_FRAMES 未设时 WSDbgFrames() = true，对拍常量 false")
	}
	os.Setenv("WS_DBG_FRAMES", "0")
	if WSDbgFrames() {
		t.Errorf("WS_DBG_FRAMES=0 时 WSDbgFrames() = true，对拍常量 false")
	}
	os.Unsetenv("WS_DBG_FRAMES")
}

func itoa(v int) string { return strconv.Itoa(v) }

func ftoa(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

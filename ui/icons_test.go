package ui

import (
	"image"
	"math"
	"testing"

	"github.com/DragonTTDream/WeatherScr/weather"
)

func f64(v float64) *float64 { return &v }

// TestDetailRowGeometry 对应上游布局原文
// （WeatherPresentation.qml:393-397）：
// height = max(68.0, detailIconSize + 38.0, detailFontSize*3.2)
// = max(68, 30+38, 20*3.2=64) = 68 px。
func TestDetailRowGeometry(t *testing.T) {
	// 行高随上线口径的图标 40 / 字号 30 变化：max(68, 40+38, 30*3.2) = 96。
	// 公式本身逐字照上游 QML:393-397，变的是代入值（实测口径：放大）。
	if got := detailRowHeight(); got != 96 {
		t.Errorf("详情行高 = %d，期望 96（max(68, 40+38, 30*3.2)）", got)
	}
	// 上线口径 40：需求是详情行文字改成图标后不能变小。
	// 上游公式 max(30, detail_icon_size=16)=30 是上游口径，不是本实现的。
	if DetailIconPx != 40 {
		t.Errorf("详情图标 = %d，期望 40（实测口径：放大）", DetailIconPx)
	}
	// 上线口径 30：同上，需求是文字放大 ⇒ 从上游公式值 20 放大。
	if DetailFontPx != 30 {
		t.Errorf("详情字号 = %d，期望 30（实测口径：放大）", DetailFontPx)
	}
	if detailGapPx != 1 {
		t.Errorf("图标与文字间隙 = %d，期望 1（QML:419/443 的 1.0）", detailGapPx)
	}
}

// TestDetailItemsOrderAndFormat 对应需求 3：
// 顺序 rain → humidity → wind；湿度/降水取整加 %，风速一位小数加 " km/h"
// （上游 weather.py:725-727 的 f"{precip:.0f}%" / f"{wind:.1f} km/h"）。
// 某项为 nil 时该项文案为空 ⇒ 整格不画（几何 3 等分不变）。
func TestDetailItemsOrderAndFormat(t *testing.T) {
	w := &weather.Sample{
		PrecipitationProbability: f64(88),
		Humidity:                 f64(77),
		WindSpeed:                f64(5.6),
	}
	items := detailItems(w)
	if len(items) != 3 {
		t.Fatalf("三项，得到 %d 项", len(items))
	}
	want := []struct{ kind, text string }{
		{"rain", "88%"},
		{"humidity", "77%"},
		{"wind", "5.6 km/h"},
	}
	for i, x := range want {
		if items[i].kind != x.kind || items[i].text != x.text {
			t.Errorf("第 %d 项 = {%q,%q}，期望 {%q,%q}", i, items[i].kind, items[i].text, x.kind, x.text)
		}
	}

	// nil 项：整格不画（text 空），顺序与几何位不变
	partial := detailItems(&weather.Sample{Humidity: f64(77)})
	if partial[0].text != "" || partial[2].text != "" {
		t.Errorf("nil 项应为空文案，得到 %q / %q", partial[0].text, partial[2].text)
	}
	if partial[1].text != "77%" {
		t.Errorf("湿度项 = %q，期望 77%%", partial[1].text)
	}

	// 取整口径：88.4 → "88"，88.6 → "89"（上游 :.0f 是四舍五入）
	r := detailItems(&weather.Sample{PrecipitationProbability: f64(88.6)})[0].text
	if r != "89%" {
		t.Errorf("88.6 取整 = %q，期望 89%%", r)
	}

	// 详情行图标素材名逐字照抄上游 _DETAIL_ICON_FILES（weather.py:70-74）
	for kind, file := range map[string]string{
		"rain":     "detail_umbrella.png",
		"humidity": "detail_humidity.png",
		"wind":     "detail_wind.png",
	} {
		if got := detailIconFiles[kind]; got != file {
			t.Errorf("%s 素材 = %q，期望 %q", kind, got, file)
		}
		if _, _, ok := detailSprite(kind); !ok {
			t.Errorf("%s 素材 %q 加载不到（应已嵌入 ui/embed/）", kind, file)
		}
	}
}

// TestDetailRainUsesCurrent 对应需求 5（2026-10-16
// 逐字决定"伞图标口径：保持当前时刻"：详情行的降水概率项取**当前时刻**。
//
//	实测数据 "Current.PrecipitationProbability=42 且 Hourly[0]=16 ⇒ 42%" 与
//	"Current 为 nil 且 Hourly[0]=16 ⇒ 回落 16%" 是 provider 层的映射
//	（在 weather 包 TestFetchUmbrellaUsesCurrent / TestFetchUmbrellaFallsBackToHourly0 实测）；
//	本测试实测映射结果进详情行的文本口径。
//
// 取代"取当天最大值"版：
// "伞 0% vs 预警 88%"自相矛盾；本次预警改成未来 N 小时滚动窗口后，
// 当天最大值不再是屏幕上的预警量 ⇒ 伞项回到当前时刻。
func TestDetailRainUsesCurrent(t *testing.T) {
	// 1 当前时刻 42% ⇒ 伞项 42%（当天最大值 88 不得抢位）
	w := &weather.Sample{
		PrecipitationProbability: f64(42),
		TodayPrecipProbMax:       intPtr(88),
	}
	if got := detailItems(w)[0].text; got != "42%" {
		t.Errorf("current=42/daily_max=88 的 rain 项 = %q，期望 42%%（取当前时刻）", got)
	}
	// 2 当前时刻为 nil ⇒ 空文案（整格不画）；provider 层的回落见 weather 包测试
	if got := detailItems(&weather.Sample{TodayPrecipProbMax: intPtr(88)})[0].text; got != "" {
		t.Errorf("current=nil 的 rain 项 = %q，期望空串（不再取当天最大值）", got)
	}
	// 3 两项都 nil ⇒ 空文案
	if got := detailItems(&weather.Sample{})[0].text; got != "" {
		t.Errorf("两项都 nil 的 rain 项 = %q，期望空串", got)
	}
}

func intPtr(v int) *int { return &v }

// TestCondIconNameMap 对应需求 2：
// 码集合逐字照抄上游 _WEATHER_CODE_ICON_MAP（weather.py:45-54），
// 夜间按 isDay 选 -day/-night，上游没有 night 变体的项夜间沿用同一张。
func TestCondIconNameMap(t *testing.T) {
	day := map[int]string{
		0:  "cond_clear-day.png",
		1:  "cond_partly-cloudy-day.png",
		2:  "cond_partly-cloudy-day.png",
		3:  "cond_overcast-day.png",
		45: "cond_fog-day.png",
		48: "cond_fog-day.png",
		51: "cond_drizzle.png",
		53: "cond_drizzle.png",
		55: "cond_drizzle.png",
		56: "cond_drizzle.png",
		57: "cond_drizzle.png",
		61: "cond_rain.png",
		63: "cond_rain.png",
		65: "cond_rain.png",
		80: "cond_rain.png",
		81: "cond_rain.png",
		82: "cond_rain.png",
		66: "cond_hail.png",
		67: "cond_hail.png",
		71: "cond_snow.png",
		73: "cond_snow.png",
		75: "cond_snow.png",
		77: "cond_snow.png",
		85: "cond_snow.png",
		86: "cond_snow.png",
		95: "cond_thunderstorms-day.png",
		96: "cond_thunderstorms-day.png",
		99: "cond_thunderstorms-day.png",
	}
	for code, want := range day {
		if got := condIconName(code, true); got != want {
			t.Errorf("码 %d 白天 = %q，期望 %q", code, got, want)
		}
	}
	// 夜间：有 -night 变体的换过去，没有的沿用 day 版
	night := map[int]string{
		0:  "cond_clear-night.png",
		2:  "cond_partly-cloudy-night.png",
		3:  "cond_overcast-night.png",
		45: "cond_fog-night.png",
		51: "cond_drizzle.png",
		61: "cond_rain.png",
		66: "cond_hail.png",
		71: "cond_snow.png",
		95: "cond_thunderstorms-night.png",
	}
	for code, want := range night {
		if got := condIconName(code, false); got != want {
			t.Errorf("码 %d 夜间 = %q，期望 %q", code, got, want)
		}
	}
	// 映射不到 → 空串（调用方退化为纯文字列，需求 4）
	for _, code := range []int{-1, 4, 100, 200} {
		if got := condIconName(code, true); got != "" {
			t.Errorf("码 %d 应映射不到，得到 %q", code, got)
		}
	}
	// 低性能 x86 小主机配置 icon_size = 192
	if WeatherIconSize != 192 {
		t.Errorf("WeatherIconSize = %d，期望 192", WeatherIconSize)
	}
	// 上线态钉 false：天气卡片布局失衡 ⇒ 撤 192px 状况图标。
	// 上游低性能 x86 小主机 show_condition_icon = True 是**上游**的事实，不是本实现的上线态。
	if WeatherShowIcon {
		t.Errorf("WeatherShowIcon 应为 false（实测判布局失衡，已撤状况图标）")
	}
}

// TestCondSpriteLoads 实测图标真的进来了：每个映射到的码都能取到贴图与源尺寸。
func TestCondSpriteLoads(t *testing.T) {
	for _, code := range []int{0, 1, 3, 45, 51, 61, 66, 71, 95} {
		for _, isDay := range []bool{true, false} {
			iop, sz, ok := condSprite(code, isDay)
			if !ok {
				t.Errorf("码 %d isDay=%v 取不到贴图", code, isDay)
				continue
			}
			if sz.X != 250 || sz.Y != 250 {
				t.Errorf("码 %d isDay=%v 源尺寸 = %v，期望 250×250", code, isDay, sz)
			}
			if iop.Size().X == 0 {
				t.Errorf("码 %d isDay=%v 的 ImageOp 尺寸为 0", code, isDay)
			}
		}
	}
}

// TestDrawSpriteScaleFactor 实测缩放系数就是指定的 30.0/250.0 与 192.0/250.0
// （drawSprite 内部就是 dst/src 的直接除法，这里把口径钉住防漂移）。
func TestDrawSpriteScaleFactor(t *testing.T) {
	src := image.Pt(250, 250)
	if got, want := float32(DetailIconPx)/float32(src.X), 40.0/250.0; got != float32(want) {
		t.Errorf("详情图标缩放 = %v，期望 30.0/250.0", got)
	}
	if got, want := float32(WeatherIconSize)/float32(src.X), 192.0/250.0; got != float32(want) {
		t.Errorf("状况图标缩放 = %v，期望 192.0/250.0", got)
	}
}

// TestWindIconReadability 对应方案 (a)：
// 风图标按字形占画布的比例放大绘制边长 = DetailIconPx / k（上限 56 px），
// 使字形视觉尺寸与 DetailIconPx 一致。
// 实测 k = detail_wind.png 非透明像素包围盒主导边 / 画布 = 195/250 = 0.780
// （bbox (34,62)-(228,203)，宽 195、高 142，取主导边宽）。
// 40 / 0.780 = 51.28 → 51 px（未触 56 上限）。素材本身不动。
func TestWindIconReadability(t *testing.T) {
	if windGlyphFrac != 0.780 {
		t.Errorf("windGlyphFrac = %v，期望 0.780（实测 bbox 主导边 195/250）", windGlyphFrac)
	}
	if got := windIconDrawPx(); got != 51 {
		t.Errorf("windIconDrawPx = %d，期望 51（40/0.780=51.28 取整，未触 56 上限）", got)
	}
	// 放大后字形视觉尺寸 ≈ DetailIconPx（0.780 × 51 ≈ 40）
	if g := windGlyphFrac * float64(windIconDrawPx()); math.Abs(g-40) > 1 {
		t.Errorf("放大后字形视觉尺寸 = %.1f，期望 ≈40", g)
	}
	// 上限 56：windIconDrawPx 的收口口径（实测值 51 未触上限，断言不越界）
	if got := windIconDrawPx(); got > 56 {
		t.Errorf("windIconDrawPx = %d，超过 56 上限", got)
	}
}

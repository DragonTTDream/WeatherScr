// fxshot：离线渲染特效帧并量化可见性。
//
// 用途：不在低性能 x86 小主机上跑，用 gioui 的 headless GPU 在本机把
// ui.Field 渲染成 PNG，直接数"非背景像素占比"。这样调特效参数
// 不用来回折腾低性能 x86 小主机（一次往返 3~6 分钟）。
//
// GOOS=linux 专用（headless 走 EGL/OSMesa）。
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui"
	"github.com/DragonTTDream/WeatherScr/weather"
)

func main() {
	// 素材预览工装（规格 1.6）：FX_ASSET_DIR 存在 ⇒ 透传给 ui 包的 WS_ASSET_DIR，
	// 让渲染路径从外部目录读素材。必须在任何 ui 调用之前设（贴图集在首次用到时加载）。
	// 出厂二进制 cmd/scr 不读这个变量，也不读 WS_ASSET_DIR。
	if v := os.Getenv("FX_ASSET_DIR"); v != "" {
		os.Setenv("WS_ASSET_DIR", v)
		fmt.Printf("ASSET_DIR=%s\n", v)
		files := ui.AssetOverrideStatus()
		if len(files) == 0 {
			fmt.Printf("ASSET_FILES=none\n")
		} else {
			fmt.Printf("ASSET_FILES=%s\n", strings.Join(files, ","))
		}
	}
	clsName := "rain"
	if len(os.Args) > 1 {
		clsName = os.Args[1]
	}
	cls := parseClass(clsName)
	isDay := os.Getenv("FX_DAY") == "1"
	frames := 40
	if v := os.Getenv("FX_FRAMES"); v != "" {
		frames, _ = strconv.Atoi(v)
	}
	w, h := 1080, 1920
	if v := os.Getenv("FX_W"); v != "" {
		w, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("FX_H"); v != "" {
		h, _ = strconv.Atoi(v)
	}

	win, err := headless.NewWindow(w, h)
	if err != nil {
		fmt.Println("HEADLESS_FAIL=" + err.Error())
		return
	}
	defer win.Release()

	field := ui.NewField(cls, isDay, 20261015)
	var ops op.Ops
	dt := 66 * time.Millisecond
	// FX_DT（秒）：默认 66 ms = 真实帧间隔口径。此前把 "FX_FRAMES=260" 标成
	// "第 260 秒"是错的——260 步 × 66 ms 只有 17 秒模拟时间。
	// FX_DT=1 让每步 = 1 秒模拟时间，步数即秒数，可与粒子场的时序测试对齐。
	if v := os.Getenv("FX_DT"); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
			dt = time.Duration(s * float64(time.Second))
		}
	}
	stepTo := 0
	stepUp := func(to int) {
		for ; stepTo < to; stepTo++ {
			field.Step(dt, w, h)
		}
	}
	stepUp(frames)
	// 闪现间隔按码分级（D4）：把 FX_CODE 传进粒子场。
	// 非雷暴码 ⇒ 不闪（出厂口径），要看闪光用 FX_FLASH 强制一次局域闪光。
	field.SetWeatherCode(fxCode())
	if v := os.Getenv("FX_FLASH"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		field.SetFlash(f)
	}

	// 降雨提醒：FX_ALERT 是**存在开关** —— 非空 = 出预警（出厂 panelrow 版式），
	// 不再接受版式名。FX_ALERT_PCT=概率，FX_ALERT_MM=毫米，
	// FX_ALERT_PULSE=相位 0..1，FX_ALERT_STATIC=1 关掉脉动。
	var alert *ui.Alert
	if os.Getenv("FX_ALERT") != "" {
		pct := 78
		if v := os.Getenv("FX_ALERT_PCT"); v != "" {
			pct, _ = strconv.Atoi(v)
		}
		mm := 1.2
		if v := os.Getenv("FX_ALERT_MM"); v != "" {
			mm, _ = strconv.ParseFloat(v, 64)
		}
		ph := 0.0
		if v := os.Getenv("FX_ALERT_PULSE"); v != "" {
			ph, _ = strconv.ParseFloat(v, 64)
		}
		yoff := 0
		if v := os.Getenv("FX_ALERT_YOFF"); v != "" {
			yoff, _ = strconv.Atoi(v)
		}
		// FX_ALERT_LANG：预警文案语言。
		// 出图工具默认 "zh" = 预览图与现状逐字一致；ui 包的口径是
		// "只有恰好 zh 走中文，其余一律英文"，故这里必须显式给默认值。
		lang := os.Getenv("FX_ALERT_LANG")
		if lang == "" {
			lang = "zh"
		}
		alert = &ui.Alert{Pct: pct, MM: mm, Pulse: ph, YOff: yoff, Lang: lang,
			PulseOn: os.Getenv("FX_ALERT_STATIC") != "1"}
		fmt.Printf("ALERT pct=%d tier=%s(%d) pulse=%.2f lang=%s\n",
			pct, alert.TierName(lang), alert.Tier(), ph, lang)
	}

	if v := os.Getenv("FX_BANDSPREAD"); v != "" {
		ui.CloudBandYSpread, _ = strconv.ParseFloat(v, 64)
	}
	if v := os.Getenv("FX_BANDTOP"); v != "" {
		ui.CloudBandYTop, _ = strconv.ParseFloat(v, 64)
	}
	fmt.Printf("BAND top=%.2f spread=%.2f\n", ui.CloudBandYTop, ui.CloudBandYSpread)

	clockOff, weatherOff := 0, 0
	if v := os.Getenv("FX_CLOCKY"); v != "" {
		clockOff, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("FX_WEATHERY"); v != "" {
		weatherOff, _ = strconv.Atoi(v)
	}
	fmt.Printf("OFFS clock=%d weather=%d\n", clockOff, weatherOff)

	// FX_LAYERS：图层掩码（0 = 全开）。用于"开/关某层出两张图再相减"，
	// 把某一层的贡献逐像素分离出来（LayerSky=1 LayerDepth=2 LayerField=4
	// LayerClock=8 LayerWeath=16 LayerShadow=32 LayerAlert=64；全开=127，
	// 只关粒子层 = 127^4 = 123。
	var layers ui.LayerMask
	if v := os.Getenv("FX_LAYERS"); v != "" {
		n, _ := strconv.Atoi(v)
		layers = ui.LayerMask(n)
		fmt.Printf("LAYERS=%d\n", n)
	}

	// 走生产渲染路径（ui.Screen.Layout），保证离线图与低性能 x86 小主机一致
	screen := ui.New()
	th := time.Now()
	renderTo := func(name string) bool {
		for i := 0; i < 3; i++ {
			ops.Reset()
			gtx := layout.Context{
				Ops:         &ops,
				Constraints: layout.Exact(image.Point{X: w, Y: h}),
				Now:         th,
			}
			screen.Layout(gtx, ui.Frame{
				Now:         th,
				Clock:       ui.DefaultClockSpec(),
				Weather:     sample(),
				Class:       cls,
				IsDay:       isDay,
				Field:       field,
				Location:    "Hangzhou, China",
				Alert:       alert,
				ClockYOff:   clockOff,
				WeatherYOff: weatherOff,
				MoonPhase:   fxMoonPhase(),
				Layers:      layers,
			})
			if err := win.Frame(&ops); err != nil {
				fmt.Println("FRAME_FAIL=" + err.Error())
				return false
			}
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if err := win.Screenshot(img); err != nil {
			fmt.Println("SHOT_FAIL=" + err.Error())
			return false
		}
		f, err := os.Create(name)
		if err != nil {
			fmt.Println("WRITE_FAIL=" + err.Error())
			return false
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			fmt.Println("PNG_FAIL=" + err.Error())
			return false
		}
		fmt.Printf("SHOT_OK=%s %dx%d steps=%d t=%.1fs\n", name, w, h, stepTo, float64(stepTo)*dt.Seconds())
		fmt.Printf("量化=%s\n", quantify(img))
		return true
	}

	// FX_SHOTS="a,b,c"：**定点连拍**。同一条模拟里走到指定步数各出一张图，
	// 每张出图前先打印当时"云对日/月盘的覆盖率"——
	// 这样看的每一帧都自带"事件确实在这一帧里"证据，
	// 不再重犯"拿没拍到事件的帧下结论"这类判读错误。
	if v := os.Getenv("FX_SHOTS"); v != "" {
		base := "fxshot_" + clsName
		if f := os.Getenv("FX_OUT"); f != "" {
			base = f
		}
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			n, err := strconv.Atoi(tok)
			if err != nil {
				continue
			}
			stepUp(n)
			c, cov := field.CelestialCover(w, h)
			fmt.Printf("SHOT step=%d t=%.1fs 云压在盘上=%d 朵 盘面被覆盖=%.1f%%\n",
				n, float64(n)*dt.Seconds(), c, cov)
			// 云实例清单（dev 取证用，FX_INV=1）：屏上云的数量与逐朵尺寸。
			// 判据：观感口径"一小坨一小坨" = 屏上小朵数量与 dw 分布，
			// 必须能在出图同一时刻量化，不能靠看图猜。
			printInv(field, n)
			if !renderTo(fmt.Sprintf("%s.s%05d.png", base, n)) {
				return
			}
		}
		if os.Getenv("FX_DEBUG") == "1" {
			fmt.Print(field.DebugStats(field.TotalCount()))
		}
		return
	}

	name := "fxshot_" + clsName + ".png"
	if f := os.Getenv("FX_OUT"); f != "" {
		name = f
	}
	if !renderTo(name) {
		return
	}
	printInv(field, frames)
	if os.Getenv("FX_DEBUG") == "1" {
		fmt.Print(field.DebugStats(field.TotalCount()))
	}
}

func quantify(img *image.RGBA) string {
	bd := img.Bounds()
	type key struct{ r, g, b uint8 }
	cnt := map[key]int{}
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			p := img.Pix[(y*bd.Dx()+x)*4:]
			cnt[key{p[0], p[1], p[2]}]++
		}
	}
	var bg key
	best := -1
	for k, n := range cnt {
		if n > best {
			best, bg = n, k
		}
	}
	tot, diff := 0, 0
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			p := img.Pix[(y*bd.Dx()+x)*4:]
			tot++
			d := int(p[0]) - int(bg.r)
			if d < 0 {
				d = -d
			}
			e := int(p[1]) - int(bg.g)
			if e < 0 {
				e = -e
			}
			f := int(p[2]) - int(bg.b)
			if f < 0 {
				f = -f
			}
			if d+e+f > 40 {
				diff++
			}
		}
	}
	return fmt.Sprintf("背景=(%d,%d,%d) 非背景=%.2f%%", bg.r, bg.g, bg.b, 100.0*float64(diff)/float64(tot))
}

// parseClass 把类名转成 effect.Class。
func parseClass(name string) effect.Class {
	switch strings.ToLower(name) {
	case "clear":
		return effect.Clear
	case "cloudy":
		return effect.Cloudy
	case "partly-cloudy", "partly_cloudy", "partlycloudy":
		return effect.PartlyCloudy
	case "overcast":
		return effect.Overcast
	case "fog":
		return effect.Fog
	case "rain":
		return effect.Rain
	case "snow":
		return effect.Snow
	case "thunder":
		return effect.Thunder
	case "dust":
		return effect.Dust
	}
	// 未知类名回落 Clear 是**静默**的：出图时看着像"这个类长这样"，
	// 实际画的是晴档（盘 + 光斑 + 晴天空）。出图判读前必须确认类名被认下。
	fmt.Printf("CLASS_UNRECOGNIZED=%s (回落 clear)\n", name)
	return effect.Clear
}

// sample 造一份固定天气样本，让离线图带上文字层（与低性能 x86 小主机同构）。
func sample() *weather.Sample {
	t := 22.4
	hu := 84.0
	pp := 0.0
	ws := 4.6
	cond := "Mainly clear"
	// 用低性能 x86 小主机实时数据出图（FX_T/FX_HU/FX_PP/FX_WS/FX_COND/FX_TMAX/FX_TMIN）：
	// 离线图与低性能 x86 小主机逐字一致，判读才有意义。
	if v := os.Getenv("FX_T"); v != "" {
		t, _ = strconv.ParseFloat(v, 64)
	}
	if v := os.Getenv("FX_HU"); v != "" {
		hu, _ = strconv.ParseFloat(v, 64)
	}
	if v := os.Getenv("FX_PP"); v != "" {
		pp, _ = strconv.ParseFloat(v, 64)
	}
	if v := os.Getenv("FX_WS"); v != "" {
		ws, _ = strconv.ParseFloat(v, 64)
	}
	if v := os.Getenv("FX_COND"); v != "" {
		cond = v
	}
	s := &weather.Sample{
		Condition:                cond,
		Temperature:              &t,
		Humidity:                 &hu,
		PrecipitationProbability: &pp,
		WindSpeed:                &ws,
	}
	// FX_CODE：WMO 状况码（状况图标要靠它选图）。
	// 默认 1 = "Mainly clear"，与上面的 cond 一致。
	code := fxCode()
	s.WeatherCode = code
	// FX_CC：当前云量（%）。云量 ≥30 时 ClassifyByCloudCover 把 Clear 变 Cloudy
	// fxshot 原先不设云量 ⇒ 永远出不了多云类。
	// 同一个值也喂给 CloudCoverPct（太阳可见度的输入，见 ui.frameCloudPct）。
	if v := os.Getenv("FX_CC"); v != "" {
		n, _ := strconv.Atoi(v)
		s.CloudCover = &n
		s.CloudCoverPct = n
	}
	if v := os.Getenv("FX_TMAX"); v != "" {
		mx, _ := strconv.ParseFloat(v, 64)
		s.TodayPrecipMM = nil
		_ = mx
	}
	if v := os.Getenv("FX_MM"); v != "" {
		mm, _ := strconv.ParseFloat(v, 64)
		s.TodayPrecipMM = &mm
	}
	return s
}

// fxCode 出图开关 FX_CODE：WMO 状况码。空 = 1（"Mainly clear"）。
// 同一份取值喂两处：Sample.WeatherCode（图标与文字）与粒子场的闪现档位
// （ui.Field.SetWeatherCode，闪现间隔按码分级）。
func fxCode() int {
	v := os.Getenv("FX_CODE")
	if v == "" {
		return 1
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 1
	}
	return n
}

// fxMoonPhase 出图开关 FX_MOON：月相分数（0..1）。空 = nil（走 bakeMoonCrisp 回落画法）。
// 用途：离线验月相素材是否真被烘进天空。
func fxMoonPhase() *float64 {
	v := os.Getenv("FX_MOON")
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// pf 解析浮点（FX_INV 取证用）。
func pf(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(strings.Split(s, " ")[0]), 64)
	return v
}

// printInv 云实例清单（dev 取证，FX_INV=1）。
// 观感口径"一小坨一小坨" = 屏上小朵云的数量与逐朵宽度，必须量化，不能靠看图猜。
func printInv(field *ui.Field, step int) {
	if os.Getenv("FX_INV") != "1" {
		return
	}
	onScreen, narrow, wide := 0, 0, 0
	for _, ln := range strings.Split(field.DebugStats(field.TotalCount()), "\n") {
		if !strings.HasPrefix(ln, "p") || !strings.Contains(ln, "kind=4 ") {
			continue
		}
		vals := map[string]float64{}
		for _, tok := range strings.Fields(ln) {
			if i := strings.Index(tok, "="); i > 0 {
				if v, err := strconv.ParseFloat(tok[i+1:], 64); err == nil {
					vals[tok[:i]] = v
				}
			}
		}
		x, y, dw, alpha, spr := vals["x"], vals["y"], vals["dw"], vals["alpha"], vals["spr"]
		if x > -0.15 && x < 1.15 && y > -0.15 && y < 1.15 {
			onScreen++
			if dw < 400 {
				narrow++
			} else {
				wide++
			}
			fmt.Printf("INV i=%s x=%.3f y=%.3f dw=%.0f alpha=%.2f spr=%d\n", strings.TrimPrefix(strings.Fields(ln)[0], "p"), x, y, dw, alpha, int(spr))
		}
	}
	fmt.Printf("INV step=%d 屏上云=%d 朵（<400px=%d，>=400px=%d）\n", step, onScreen, narrow, wide)
}

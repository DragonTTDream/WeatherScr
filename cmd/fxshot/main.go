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
	if v := os.Getenv("FX_FLASH"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		field.SetFlash(f)
	}

	// 降雨提醒：FX_ALERT=版式，FX_ALERT_PCT=概率，FX_ALERT_MM=毫米，
	// FX_ALERT_PULSE=相位 0..1，FX_ALERT_STATIC=1 关掉脉动。
	var alert *ui.Alert
	if st := os.Getenv("FX_ALERT"); st != "" {
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
		alert = &ui.Alert{Pct: pct, MM: mm, Style: st, Pulse: ph, YOff: yoff, Lang: lang,
			PulseOn: os.Getenv("FX_ALERT_STATIC") != "1"}
		fmt.Printf("ALERT style=%s pct=%d tier=%s(%d) pulse=%.2f lang=%s\n",
			st, pct, alert.TierName(lang), alert.Tier(), ph, lang)
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
	code := 1
	if v := os.Getenv("FX_CODE"); v != "" {
		code, _ = strconv.Atoi(v)
	}
	s.WeatherCode = code
	// FX_CC：当前云量（%）。云量 ≥30 时 ClassifyByCloudCover 把 Clear 变 Cloudy
	// fxshot 原先不设云量 ⇒ 永远出不了多云类。
	if v := os.Getenv("FX_CC"); v != "" {
		n, _ := strconv.Atoi(v)
		s.CloudCover = &n
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

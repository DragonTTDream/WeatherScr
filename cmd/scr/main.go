// WeatherScr.scr 主程序 —— Windows 屏保协议。
//
//	/s            全屏运行（主路径）
//	/c            配置对话框，父窗口 = GetForegroundWindow()
//	/c <hwnd>     配置对话框，父窗口 = 该 HWND
//	无参数        配置对话框，父窗口 = **NULL**（旧实现打 usage，必须改）
//	/p|/l <hwnd>  预览模式：在给定 HWND 下建子窗口离屏渲染
//	/a <hwnd>     现代 Windows 由系统管理密码 → 直接退出，不做任何事
//	--help / -h / ?   打印用法并退出 0（usage 只留给这三个）
//	无法识别      立即静默退出，不弹窗不打印
//
// 解析规则（大小写不敏感、前缀 `/` 或 `-`、字母后空格或冒号皆可、`/p`≡`/l`）
// 在 cmd/scr/args.go，表驱动单测在 cmd/scr/args_test.go。
//
// 调试开关：环境变量 SCR_DEBUG=1 ⇒ 1/4 尺寸居中、不置顶、
// 不因输入退出、不隐藏任务栏、打 DBG= 帧耗时与样式位。
package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/geo"
	"github.com/DragonTTDream/WeatherScr/scrargs"
	"github.com/DragonTTDream/WeatherScr/ui"
	"github.com/DragonTTDream/WeatherScr/weather"
)

// 退出阈值
const (
	// mouseExitThresholdPx 逐字照抄上游 rendering/runtime_input.py:111
	// `MOUSE_EXIT_THRESHOLD = 10`，且判据是**相对初始位置的位移**
	// （:466 `math.hypot(delta.x(), delta.y()) > MOUSE_EXIT_THRESHOLD`，
	// delta = current_pos - self._initial_mouse_pos，不是累积移动量。
	mouseExitThresholdPx = 10
	maxRuntime           = 30 * time.Minute

	// windowClass 固定 gioui 的窗口类名。gioui 用 app.ID 作
	// RegisterClassEx 的类名（os_windows.go:163-174），app.ID 默认是
	// os.Args[0] 的基名（app.go:179-180）—— 那会随 exe 改名而变，
	// 定成固定串后 FindWindowW 才可靠。
	windowClass = "WeatherScrSaver"
)

// scrDebug 是调试开关：SCR_DEBUG=1。
// 默认（无该变量）行为必须与现状完全一致。
// layerMask 读图层开关（性能定位用）。
// 语法：逗号分隔的图层名；`none`/空/`all` = 全开（0 在 ui 侧按全开处理）。
// 例：只画背景不画文字 = `sky,depth,field`。
//
// 读**exe 同目录的 `scr.layers` 文件**而不是环境变量：屏保进程的环境块
// 继承自登录时的 Explorer（同一条实测结论），`setx` 不重启
// Explorer 不生效 ⇒ 环境变量这条路对屏保进程不通。
func layerMask() ui.LayerMask {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WS_LAYERS")))
	if v == "" {
		exe, err := os.Executable()
		if err != nil {
			return 0
		}
		b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "scr.layers"))
		if err != nil {
			return 0
		}
		v = strings.ToLower(strings.TrimSpace(string(b)))
	}
	if v == "" || v == "none" || v == "all" {
		return 0 // 0 = 全开
	}
	var m ui.LayerMask
	for _, name := range strings.Split(v, ",") {
		switch strings.TrimSpace(name) {
		case "sky":
			m |= ui.LayerSky
		case "depth":
			m |= ui.LayerDepth
		case "field":
			m |= ui.LayerField
		case "clock":
			m |= ui.LayerClock
		case "weather":
			m |= ui.LayerWeath
		case "shadow":
			m |= ui.LayerShadow
		}
	}
	return m
}

// fpsFileBudget 读 exe 同目录 `scr.fps`（一行整数 = 目标帧率），换算成帧预算。
// 缺省/非法 = 66 ms（15 fps）。上限 30 fps，下限 2 fps。
// 用途：CPU 与帧率近似线性，预算冲突时由帧率这一维解决。
func fpsFileBudget() time.Duration {
	v := strings.TrimSpace(os.Getenv("WS_FPS"))
	if v == "" {
		exe, err := os.Executable()
		if err != nil {
			return 66 * time.Millisecond
		}
		b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "scr.fps"))
		if err != nil {
			return 66 * time.Millisecond
		}
		v = strings.TrimSpace(string(b))
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 2 || n > 30 {
		return 66 * time.Millisecond
	}
	return time.Duration(1000/n) * time.Millisecond
}

func scrDebug() bool {
	if os.Getenv("SCR_DEBUG") == "1" {
		return true
	}
	// 标记文件这条路必需：屏保进程的环境变量继承自登录时的 Explorer
	// 环境块，`setx` 后不重启 Explorer 不生效 ⇒ 只能靠 exe 同目录标记。
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "scr.debug")); err == nil {
		return true
	}
	return false
}

// scrLogOnly 只要日志、不改几何与输入行为：exe 同目录 `scr.log` 标记文件。
// 必需拆开——scrDebug 会同时把窗口缩成 1/4 居中并禁用输入退出，
// 实测因此量到 `pointerEvents=0`（spin 的圆在 (500,500)，
// 而 1/4 窗口在 (405,720)-(675,1200)，光标根本没进窗口。
// 生产态几何 + 生产态退出判定，要配生产态日志，只能靠这个独立开关。
func scrLogOnly() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(exe), "scr.log"))
	return err == nil
}

func main() {
	app.ID = windowClass
	args := os.Args[1:]
	sa := scrargs.Parse(args)
	switch sa.Action {
	case scrargs.ActionRun:
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s", len(args), first(args), sa.Action)
		run()
		return

	case scrargs.ActionConfigure:
		// 父窗口：/c <hwnd> → 该 HWND；/c 无数字 → GetForegroundWindow()；
		// 无参数 → NULL（协议表）。
		parent := sa.HWND
		if !sa.HasHWND {
			if len(args) == 0 {
				parent = 0
			} else {
				parent = foregroundWindow()
			}
		}
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s PARENT=%x", len(args), first(args), sa.Action, parent)
		configure(parent)
		return

	case scrargs.ActionPreview:
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s HWND=%x", len(args), first(args), sa.Action, sa.HWND)
		preview(sa.HWND)
		return

	case scrargs.ActionPassword:
		// /a：现代 Windows 由系统管理密码 → 直接退出，不做任何事
		os.Exit(0)
		return

	case scrargs.ActionHelp:
		usage()
		os.Exit(0)
		return

	case scrargs.ActionUnknown:
		// 规范：无法识别 → 立即退出，不弹窗不打印（协议表）
		os.Exit(0)
		return
	}
}

func first(a []string) string {
	if len(a) == 0 {
		return ""
	}
	return a[0]
}

// logf 带时间戳的一行日志。
//
// 理由：排查中"日志停在 219 字节"无法与"程序真的没打印"区分开
// （coverWatcher 的 COVER= 行没进日志）。每行前缀
// HH:MM:SS.mmm 后可以直接看出程序在哪一刻停止输出。
// 逐行直写：fmt.Printf 每次调用是一个 write(2)，行内原子，天然逐行 flush
// （stdout 无缓冲场景逐行立即可见）。
func logf(format string, args ...any) {
	fmt.Printf("%s ", time.Now().Format("15:04:05.000"))
	fmt.Printf(format, args...)
	fmt.Printf("\n")
	// 真实屏保路径下 stdout 无处可去：屏保由 Winlogon/Explorer 在屏保
	// desktop 上启动，没有控制台、也没有人接管道 ⇒ 此前在真实路径上
	// **一条日志证据都拿不到**（s12b/s12c 的 SCR_EXIT 全部丢失）。
	// ⇒ 调试态同时落文件：exe 同目录 `scr.debug` 标记文件存在，
	// 或环境变量 SCR_DEBUG=1 时，逐行追加写 `scr_run.log`。
	// 标记文件这条路是必需的：屏保进程的环境变量继承自登录时的
	// Explorer 环境块，`setx` 之后不重启 Explorer 不生效，不可靠。
	dbgLogLine(fmt.Sprintf("%s %s", time.Now().Format("15:04:05.000"),
		fmt.Sprintf(format, args...)))
}

// dbgLog 调试态日志文件（nil = 未启用）。
var dbgLog *os.File
var dbgOnce sync.Once

// dbgLogLine 启用时把一行追加写进 exe 同目录的 scr_run.log 并立刻 Sync。
func dbgLogLine(line string) {
	dbgOnce.Do(func() {
		if !scrDebug() && !scrLogOnly() {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(filepath.Dir(exe), "scr_run.log"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o666)
		if err != nil {
			return
		}
		dbgLog = f
		// panic / runtime 的致命输出走 **stderr**，而真实屏保路径上
		// stderr 无处可去 ⇒ 进程"无声无息地死"（实测：scr_run.log 停在
		// SCR_FIRSTFRAME 之后，没有任何退出行，6 次轮询全空。
		// 把 stderr 一并接到同一个文件，崩溃栈才留得下来。
		os.Stderr = f
	})
	if dbgLog == nil {
		return
	}
	fmt.Fprintln(dbgLog, line)
	_ = dbgLog.Sync()
}

func usage() {
	fmt.Println(`WeatherScr - 时间 + 天气 + 动态天气背景屏保
用法:
   WeatherScr.scr /s            全屏运行
   WeatherScr.scr /c            配置对话框（父窗口 = 当前前台窗口）
   WeatherScr.scr /c <hwnd>     配置对话框（父窗口 = 该 HWND）
   （无参数）                    配置对话框（父窗口 = NULL）
   WeatherScr.scr /p <hwnd>     预览模式（/l 等价；在给定 HWND 下建子窗口）
   WeatherScr.scr /a <hwnd>     直接退出（现代 Windows 由系统管理密码）
   --help / -h / /?             打印本用法
前缀 / 与 - 等价，字母大小写不敏感，字母后空格或冒号皆可（/p 123 与 -P:123 同义）。
调试: 环境变量 SCR_DEBUG=1 ⇒ 1/4 尺寸居中、不置顶、输入不退出、不隐任务栏。
配置: ` + config.Path())
}

// /c：打印配置路径与当前配置（保留原行为），打印完继续开窗口渲染设置面板
// 退出码必须 0。
// parent 是配置对话框的父窗口（协议表）：NULL = 无父。
func configure(parent uintptr) {
	c := config.Load()
	fmt.Println("CONFIG_PATH=" + c.Path())
	b, err := c.MarshalString()
	if err != nil {
		fmt.Println("CONFIG_ERROR=" + err.Error())
	} else {
		fmt.Println("CONFIG=")
		fmt.Println(b)
	}
	runSettings(c, parent)
}

// toModel / saveModel：config.Config ↔ 面板模型。
// ui 包不 import config（config 已 import ui，会成环），转换放在这里。
func toModel(c config.Config) ui.PanelModel {
	return ui.PanelModel{
		Location:            c.Location,
		FontSize:            c.FontSize,
		IconSize:            c.IconSize,
		Margin:              c.Margin,
		Units:               c.Units,
		RefreshMinutes:      c.RefreshMinutes,
		ShowBackground:      c.ShowBackground,
		BgOpacity:           c.BgOpacity,
		ClockBgOpacity:      c.ClockBgOpacity,
		ShowConditionIcon:   c.ShowConditionIcon,
		ShowDetailsRow:      c.ShowDetailsRow,
		ShowForecast:        c.ShowForecast,
		ShowFiveDayForecast: c.ShowFiveDayForecast,
		EffectsOn:           len(c.EffectEnabled) > 0,
		WallpaperDir:        c.WallpaperDir,
		WallpaperIntervalS:  c.WallpaperIntS,
		ClockYOffset:        c.ClockYOffset,
		WeatherYOffset:      c.WeatherYOffset,
		InteractionMode:     c.InteractionMode,
		AlertEnabled:        c.AlertEnabled == nil || *c.AlertEnabled,
		AlertThresholdPct:   alertThreshold(c),
		AlertYOffset:        c.AlertYOffset,
		AlertPulse:          c.AlertPulse == nil || *c.AlertPulse,
		AlertWindowHours:    alertWindowHours(c),
		LocationLat:         c.LocationLat,
		LocationLon:         c.LocationLon,
		LocationNameZh:      c.LocationNameZh,
		LocationNameEn:      c.LocationNameEn,
		DisplayLanguage:     displayLang(c),
	}
}

// displayLang 显示语言，未设时按默认 "en"（= 当前样式，不改）。
func displayLang(c config.Config) string {
	if c.DisplayLanguage == "zh" {
		return "zh"
	}
	return "en"
}

// alertThreshold 取配置阈值，未设时回落到包默认（30%）。
func alertThreshold(c config.Config) int {
	if c.AlertThresholdPct >= 1 {
		return c.AlertThresholdPct
	}
	return ui.AlertThresholdPct
}

// alertWindowHours 取预警滚动窗口长度：
// 合法值只允许 3 或 6，非法值（含未设的 0）回落默认 3。
// config.Load 读盘时已做过同样的收口并写了日志；这里再收口一次，
// 让内存里现值（含 /c 面板写回的值）也绝不会带非法值进渲染。
func alertWindowHours(c config.Config) int {
	v, _ := config.NormalizeAlertWindowHours(c.AlertWindowHours)
	return v
}

// saveModel 写回 config 并复用 config 现有保存函数 Config.Save。
func saveModel(c config.Config, m ui.PanelModel) error {
	c.Location = m.Location
	c.FontSize = m.FontSize
	c.IconSize = m.IconSize
	c.Margin = m.Margin
	c.Units = m.Units
	c.RefreshMinutes = m.RefreshMinutes
	c.AlertEnabled = &m.AlertEnabled
	c.AlertThresholdPct = m.AlertThresholdPct
	c.AlertYOffset = m.AlertYOffset
	c.AlertPulse = &m.AlertPulse
	// 预警滚动窗口长度：面板只给 3/6，写回前再收口一次
	c.AlertWindowHours = alertWindowHours(c)
	if h := m.AlertWindowHours; h == 3 || h == 6 {
		c.AlertWindowHours = h
	}
	c.LocationLat = m.LocationLat
	c.LocationLon = m.LocationLon
	c.LocationNameZh = m.LocationNameZh
	c.LocationNameEn = m.LocationNameEn
	c.DisplayLanguage = m.DisplayLanguage
	c.ShowBackground = m.ShowBackground
	c.BgOpacity = m.BgOpacity
	c.ClockBgOpacity = m.ClockBgOpacity
	c.ShowConditionIcon = m.ShowConditionIcon
	c.ShowDetailsRow = m.ShowDetailsRow
	c.ShowForecast = m.ShowForecast
	c.ShowFiveDayForecast = m.ShowFiveDayForecast
	// 特效开关 = effect.enabled 列表：开 = 全 7 类（同 Defaults），关 = 空
	if m.EffectsOn {
		c.EffectEnabled = []string{"clear", "cloudy", "fog", "rain", "snow", "thunder", "dust"}
	} else {
		c.EffectEnabled = []string{}
	}
	c.WallpaperDir = m.WallpaperDir
	c.WallpaperIntS = m.WallpaperIntervalS
	c.ClockYOffset = m.ClockYOffset
	c.WeatherYOffset = m.WeatherYOffset
	c.InteractionMode = m.InteractionMode
	return c.Save()
}

// runSettings 打开 gioui 窗口渲染设置面板。
// 热键：Enter 保存 / Esc 取消 / Ctrl+Z 撤销 / L 锁定
// （语义抄上游 rendering/quick/input_controller.py:205-232）。
// 改完即写盘：面板每有改动立刻 Config.Save()。
//
// parent 是配置对话框要求的父窗口；gioui 建的是顶层窗口
// （app/os_windows.go:190），不能改父成 WS_CHILD，故经 w.Run 在窗口线程上
// 设 GWL_HWNDPARENT（顶层窗口的 owner）作为等价实现。
func runSettings(cfg config.Config, parent uintptr) {
	panel := ui.NewSettingsPanel(toModel(cfg))
	// 地名搜索结果的回传通道（容量 1：同一时刻只允许一次在飞的搜索）
	type locResult struct {
		places []geo.Place
		err    error
	}
	locResults := make(chan locResult, 1)
	w := new(app.Window)
	w.Option(
		app.Title("WeatherScr 设置"),
		// 竖屏 1080×1920 上完整显示：窗口按 dp 给，
		// 内容用可滚动的 material.List，控件全部可达。
		app.Size(640, 1100),
		app.MinSize(320, 480),
	)
	th := ui.New()
	var ops op.Ops
	ownerDone := false

	save := func() {
		if err := saveModel(cfg, panel.Model()); err != nil {
			fmt.Println("SCR_CFG_SAVE_FAIL=" + err.Error())
			panel.SetSaveError(err)
			return
		}
		panel.SetSaveError(nil)
	}

	go func() {
		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				fmt.Println("SCR_CFG_EXIT=destroy")
				quit(0)

			case app.FrameEvent:
				// 父窗口挂接必须在窗口线程上做（同一条正道）。
				if parent != 0 && !ownerDone {
					ownerDone = true
					w.Run(func() {
						applyOwner(findOwnWindow(windowClass), parent)
					})
				}
				gtx := app.NewContext(&ops, e)
				act := panel.HandleKeys(gtx)
				panel.Layout(gtx, th.Theme)
				if panel.SaveClicked(gtx) {
					act = "save"
				}
				if panel.CancelClicked(gtx) {
					act = "cancel"
				}
				if panel.UndoClicked(gtx) {
					act = "undo"
				}
				if panel.LockClicked(gtx) {
					act = "lock"
				}
				// 地名搜索：点"搜索"→ 后台 goroutine 发请求 →
				// 结果经通道送回 UI 线程（panel 只在窗口线程上被碰）。
				if panel.LocSearchClicked(gtx) {
					q := panel.SearchQuery()
					panel.SetSearching(q)
					go func(q string) {
						se := &geo.Searcher{BaseURL: cfg.WeatherBaseURL}
						ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						places, err := se.Search(ctx, q)
						locResults <- locResult{places: places, err: err}
						w.Invalidate() // 唤醒下一帧把候选画出来
					}(q)
				}
				select {
				case r := <-locResults:
					if r.err != nil {
						panel.SetLocationResults(nil, "搜索失败："+r.err.Error())
					} else {
						panel.SetLocationResults(r.places,
							fmt.Sprintf("找到 %d 个候选，点\"选它\"写入坐标：", len(r.places)))
					}
				default:
				}
				if i, ok := panel.PickLocation(gtx); ok {
					if pl, ok2 := panel.Candidate(i); ok2 {
						panel.SetPickedLocation(pl)
					}
				}
				switch act {
				case "undo":
					panel.Undo()
				case "lock":
					panel.ToggleLock()
				case "save":
					save()
					fmt.Println("SCR_CFG_EXIT=save")
					quit(0)
				case "cancel":
					fmt.Println("SCR_CFG_EXIT=cancel 不写盘")
					quit(0)
				}
				if panel.Changed() {
					save() // 改完即写盘
				}
				e.Frame(gtx.Ops)
			}
		}
	}()

	fmt.Println("SCR_CFG_WINDOW=已建设置窗口（/c）")
	app.Main()
	fmt.Println("SCR_CFG_MAIN_RETURNED=窗口已全部关闭")
	os.Exit(0)
}

// /p|/l <hwnd>：预览模式。
// 子窗口 + gioui headless 离屏渲染 + SetDIBitsToDevice 贴图的实现在
// preview_windows.go（构建标签 windows && !novulkan）；
// novulkan 构建下 preview_novulkan_windows.go 打 PREVIEW_UNSUPPORTED。
// 预览不置顶、不隐藏任务栏、不因输入退出。
func preview(hwnd uintptr) {
	drawPreview(hwnd)
	os.Exit(0) // 预览失败也不得非零退出
}

// ---- /s 全屏运行 ----

// 事件注册用的 tag（gioui 官方示例一律用指针作 tag）
type pointerTag struct{}
type keyTag struct{}

var pt pointerTag
var kt keyTag

func run() {
	logf("SCR_START=run 已进入")
	cfg := config.Load()
	logf("SCR_CONFIG=%s", cfg.Path())

	// 实测：启动不得被网络卡住。先用上次成功的缓存立刻显示，
	// 网络刷新放后台 goroutine。2026-10-15 实测：同步 fetch 在低性能 x86 小主机上
	// 因无 DNS 卡满超时，屏保停在 SCR_START 不动。
	st := &weatherState{}
	if s := lastGood(weather.NormalizeLocationKey(cfg.Location)); s != nil {
		st.set(s)
		logf("SCR_CACHED=class=%s", st.snapshot().Class)
	} else {
		logf("SCR_CACHED=无缓存，先显示空天气")
	}
	go refresher(cfg, st)

	// gioui v0.10.3 把 ViewEvent 存进 coalesced.view 后从不投递
	// （window.go:671 赋值，全文件无读取），w.Event() 收不到
	// Win32ViewEvent，拿不到 HWND。改走窗口类名：gioui 用 app.ID 作
	// RegisterClassEx 的类名（os_windows.go:163-174），app.ID 已定成
	// 固定串，用 EnumWindows + 类名 + PID 找自己的窗口。
	//
	// 实测：coverLoop 在这里**只负责找句柄并
	// 记 ownHwnd**，一律不调 ShowWindow / coverFullMonitor / hideTaskbar ——
	// 跨线程 ShowWindow 会永久阻塞（实测）。
	// 显示与铺满一律经下面的 `w.Run(func(){...})` 在窗口线程上执行。
	go coverLoop()

	dbg := scrDebug() // 实测：SCR_DEBUG=1
	scrDbgFlag = dbg  // coverLoop 独立线程共用同一判定
	if dbg {
		logf("DBG=SCR_DEBUG=1 生效：1/4 尺寸居中、不置顶、不隐任务栏、输入不退出")
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("WeatherScr"),
			app.Fullscreen.Option(),
			app.Decorated(false),
		)

		// 帧预算：默认 66 ms = 15 fps。
		// 实测这台低性能 x86 小主机上"背景整屏贴图 + 文字"地板就是 9% CPU @15 fps，
		// 预算 ≤5% 与 15–20 fps 不能同时成立 ⇒ 帧率做成可调，量曲线后再定。
		// 口径与 scr.layers 一致：读 exe 同目录文件 `scr.fps`（屏保进程拿不到环境变量）。
		// 声明必须在建场之前：位移速度系数由它导出（实测口径）。
		frameBudget := fpsFileBudget()
		screen := ui.New()
		snap := st.snapshot()
		field := ui.NewField(snap.Class, snap.IsDay, time.Now().UnixNano())
		field.SetSpeedScale(speedFromBudget(frameBudget))
		// 类别切换交叉淡入淡出状态（需求：类别切换不得整片突现，云要缓慢进场）：
		// 旧场淡出 + 新场淡入同时做，新场的云从屏外左侧错峰进场。
		// 代价如实记录：淡出期间**两个场同时绘制**，粒子层填充翻倍，
		// 持续 fadeDur 秒；CPU 复测见实测数据。
		var prevField *ui.Field
		var fadeT time.Duration
		const fadeDur = 9 * time.Second
		var ops op.Ops
		last := time.Now()
		frames, nPointer, nKey := 0, 0, 0
		// 退出判定状态：初始鼠标位置 + 相对位移
		in := &inputState{interaction: cfg.InteractionMode, debug: dbg}
		next := time.Now()
		// 帧率日志按**时间**打：帧率低于 1 时按帧数打日志
		// 等于不打（实测 40 秒不足 60 帧 ⇒ 一条 SCR_TICK 都没有）。
		start := time.Now()
		lastTick := time.Now()
		firstFrame := true
		lastActive := field.ActiveCount()

		// **看门狗已彻底移除。**
		// 原实现：time.Sleep(maxRuntime=30min) 后 quit(0)。低性能 x86 小主机证据：
		// 22:30:51 启动 → 22:56:59 SCR_EXIT=watchdog，存活 26 分 08 秒，
		// 低性能 x86 小主机上两次出现"屏保自己关了"。屏保的生命周期归 Windows（有输入即退出），
		// 不该由本实现自身定时自杀。退出路径仍保留：app.DestroyEvent 与输入判定。
		_ = maxRuntime // 常量保留作记录，不再消费

		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				logf("SCR_EXIT=destroy frames=%d pointerEvents=%d keyEvents=%d",
					frames, nPointer, nKey)
				quit(0)

			case app.FrameEvent:
				frameStart := time.Now()
				gtx := app.NewContext(&ops, e)
				size := gtx.Constraints.Max

				// 注册输入。键盘必须显式抢焦点（key.FocusCmd），
				// 否则按键永远不会路由到本 tag —— 2026-10-15 实测：
				// 不抢焦点时注入 VK_ESCAPE 屏保不退出。
				event.Op(gtx.Ops, &pt)
				event.Op(gtx.Ops, &kt)
				gtx.Execute(key.FocusCmd{Tag: &kt})
				// 指针注册**不能**包在 pointer.PassOp 里：PassOp 的语义是
				// "本区域对指针事件透明、让给更早注册的 handler"，
				// 而 pointer.Filter 就注册在这段区间内 ⇒ 永远收不到。
				// 实测（全屏 1080x1920、SetCursorPos 20 次全部成功、
				// 光标确在窗口内：`SCR_TICK ... pointerEvents=0 dist=0.0`
				// 连续多条 ⇒ 鼠标退出判定从未被喂过事件。
				//
				// v0.10.3 没有 pointer.InputOp/Zone（实测 `undefined`），
				// handler 的区域取自**发射 filter 时的裁剪区**
				// （io/input/router.go:835-838 `pc.currentAreaBounds()`）
				// ⇒ 显式压一个整窗 clip 再发射 filter。
				area := clip.Rect(image.Rect(0, 0, size.X, size.Y)).Push(gtx.Ops)
				drained := drainInput(gtx, in, &nPointer, &nKey)
				area.Pop()

				if drained {
					logf("SCR_EXIT=input pointerEvents=%d keyEvents=%d dist=%.1f interaction=%v",
						nPointer, nKey, in.dist, in.interaction)
					quit(0)
				}
				frames++
				if firstFrame {
					firstFrame = false
					logf("SCR_FIRSTFRAME=elapsed=%s size=%dx%d",
						time.Since(start).Round(time.Millisecond), size.X, size.Y)
				}
				// 显示 + 铺满 + 样式重申**不在帧循环里做，也不经 w.Run**。
				// 实测在 w.Run 回调里调 SetWindowPos/ShowWindow 会重入
				// gioui 窗口过程并 panic（keyQueue.BoundsFor 越界，
				// 退出码 2 ⇒ 全部移到 coverLoop 的独立 LockOSThread 线程。
				// 详见 foreground_windows.go:coverLoop 的注释。
				// SCR_TICK：每 2 秒一次，按时间不按帧数，
				// 并额外打印 SCR_FPS frames=%d elapsed=%s。
				if time.Since(lastTick) >= 2*time.Second {
					bf, bt := field.MoonBoostStats()
					logf("SCR_MOONBOOST frame=%d total=%d active=%d", bf, bt, field.ActiveCount())
					logf("SCR_TICK frames=%d pointerEvents=%d keyEvents=%d dist=%.1f",
						frames, nPointer, nKey, in.dist)
					logf("SCR_FPS frames=%d elapsed=%s",
						frames, time.Since(start).Round(time.Millisecond))
					// 烘焙缓存命中率：Builds 持续增长 = 每帧重建，
					// 那意味着每帧一次纹理上传，烘焙就是净增成本而不是优化。
					scs := ui.SkyCacheGet()
					logf("SCR_SKYCACHE hits=%d builds=%d last=%s bake=%.1fms",
						scs.Hits, scs.Builds, scs.LastKey, scs.LastMs)
					lastTick = time.Now()
				}

				dt := time.Since(last)
				last = time.Now()
				field.Step(dt, size.X, size.Y)

				// fps 自适应生效时打一行（削减/恢复在低性能 x86 小主机上
				// 必须可见，与"日志可判停"同口径
				// 交叉淡入淡出推进：旧场淡出、新场淡入，走完就丢掉旧场。
				if prevField != nil {
					fadeT += dt
					k := float64(fadeT) / float64(fadeDur)
					if k >= 1 {
						prevField = nil
						field.SetFade(1)
						field.SetEntering(false) // 进场完成，之后按常规口径
					} else {
						prevField.SetFade(1 - k)
						field.SetFade(k)
						prevField.Step(dt, size.X, size.Y)
					}
				}
				if ac := field.ActiveCount(); ac != lastActive {
					logf("SCR_ADAPT active=%d total=%d fps=%.1f", ac, field.TotalCount(), field.FPS())
					lastActive = ac
				}

				// 后台刷新换到了新天气 → 重建粒子场
				cur := st.snapshot()
				if cur.Class != snap.Class || cur.IsDay != snap.IsDay {
					prevField = field // 旧场进入淡出，不是一帧消失
					snap = cur
					field = ui.NewFieldEntering(snap.Class, snap.IsDay, time.Now().UnixNano())
					field.SetSpeedScale(speedFromBudget(frameBudget))
					field.SetFade(0)
					fadeT = 0
					lastActive = field.ActiveCount()
					logf("SCR_WEATHER=class=%s isDay=%v fade=%ds", snap.Class, snap.IsDay, int(fadeDur/time.Second))
				}
				snap = cur

				screen.Layout(gtx, ui.Frame{
					Now:       time.Now(),
					Clock:     ui.DefaultClockSpec(),
					Weather:   snap.Sample,
					Class:     snap.Class,
					IsDay:     snap.IsDay,
					Field:     field,
					PrevField: prevField,
					Location:  cfg.Location,
					// 面板底与主字色、布局偏移
					Panel:       panelStyle(cfg),
					ClockYOff:   cfg.ClockYOffset,
					WeatherYOff: cfg.WeatherYOffset,
					Alert:       buildAlert(cfg, snap.Sample, time.Now()),
					// 今日月相（需求 5）：把 Sample.MoonPhase
					// 传进 Frame，烘进天空纹理。nil-safe（Sample 可能为 nil）。
					MoonPhase: moonPhaseOf(snap.Sample),
					Layers:    layerMask(),
				})

				// 隐藏光标
				pointer.CursorNone.Add(gtx.Ops)

				gtx.Execute(op.InvalidateCmd{})
				// 逐帧计时（2026-10-16 排查）：低性能 x86 小主机上首帧 61 ms 之后
				// 一条 SCR_TICK 都没有 ⇒ 首帧之后每帧 >2 秒。
				// 必须把"布局耗时"与"e.Frame（提交+GPU 渲染）耗时"分开量，
				// 才能知道是 CPU 侧绘制指令贵，还是 GPU 侧贴图贵。
				fLayout := time.Since(frameStart)
				e.Frame(gtx.Ops)
				if frames <= 30 {
					logf("SCR_FRAME n=%d layout=%s frame=%s size=%dx%d active=%d",
						frames, fLayout.Round(time.Millisecond),
						time.Since(frameStart).Round(time.Millisecond),
						size.X, size.Y, field.ActiveCount())
				}
				if dbg {
					// 实测：SCR_DEBUG=1 时打 DBG= 前缀的每帧耗时
					logf("DBG=frame n=%d layout=%s frame=%s size=%dx%d active=%d",
						frames, fLayout.Round(time.Millisecond),
						time.Since(frameStart).Round(time.Millisecond),
						size.X, size.Y, field.ActiveCount())
				}

				if d := time.Until(next); d > 0 {
					time.Sleep(d)
				}
				next = next.Add(frameBudget)
				if time.Now().After(next) {
					next = time.Now()
				}
			}
		}
	}()

	logf("SCR_MAIN=即将进入 app.Main")
	app.Main()
	logf("SCR_MAIN=app.Main 已返回（窗口未建立）")
}

// inputState 是退出判定的状态。
type inputState struct {
	// interaction = config 的 interaction_mode（上游 default_settings.py:27
	// 'input'.'interaction_mode': False。True = 交互模式：鼠标位移与左键
	// 不退出（上游 rendering/runtime_input.py:425-426, 454-458）。
	interaction bool
	// debug = SCR_DEBUG=1：
	// 鼠标移动/按键一律不退出，退出只保留 Esc（与 Ctrl+C 进程级）。
	debug bool
	// 初始鼠标位置：上游 :462-464 `if self._initial_mouse_pos is None:
	// self._initial_mouse_pos = current_pos`。
	haveInitial bool
	initialX    float32
	initialY    float32
	// dist = hypot(当前 - 初始)：**相对初始位置的位移，不是累积移动量**
	// （上游 :465-466 delta = current_pos - self._initial_mouse_pos）。
	dist float64
}

// drainInput 收干本帧的输入事件。返回 true = 该退出屏保。
//
// 判据逐条对应上游 rendering/runtime_input.py：
// - 鼠标位移：:462-467 `hypot(current - initial) > MOUSE_EXIT_THRESHOLD(10)`
// - 左键按下：:424-429 左键 → _request_exit（交互模式/Ctrl 下 :425-426 不退出）
// - 滚轮：:254-268 handle_wheel 不请求退出 → 本复刻也不退出
// - 键盘：:371-378 Esc/Q 退出；热键层被消费不退出；其余任意键退出
func drainInput(gtx layout.Context, st *inputState, nPointer, nKey *int) bool {
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &pt,
			Kinds:  pointer.Move | pointer.Press | pointer.Scroll | pointer.Enter | pointer.Leave,
		})
		if !ok {
			break
		}
		pe, isP := ev.(pointer.Event)
		if !isP {
			continue
		}
		*nPointer++
		fmt.Printf("SCR_PTR kind=%s pos=%.0f,%.0f\n", pe.Kind, pe.Position.X, pe.Position.Y)
		switch pe.Kind {
		case pointer.Press:
			if st.pressExit(pe.Buttons) {
				return true
			}
		case pointer.Scroll:
			// 上游 handle_wheel（:254-268）不请求退出 → 不退出
		case pointer.Move:
			if st.moveExit(pe.Position.X, pe.Position.Y) {
				return true
			}
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &kt})
		if !ok {
			break
		}
		*nKey++
		ke, isK := ev.(key.Event)
		if !isK || ke.State != key.Press {
			continue
		}
		// Esc / Q → 退出（上游 :371-373）
		if ke.Name == key.NameEscape || ke.Name == "Q" {
			// 实测：DEBUG 下退出只保留 Esc（Ctrl+C 是进程级，天然可用）
			if ke.Name == key.NameEscape || !st.debug {
				fmt.Printf("SCR_KEY=%s 退出\n", ke.Name)
				return true
			}
			fmt.Printf("SCR_KEY=%s DEBUG 下不退出\n", ke.Name)
			continue
		}
		// 热键层（指定 Z X C Space ↑ ↓ PageUp PageDown Home End；
		// 上游键策略还含 ←/→ 换轨道，偏差已记入工程记录。
		// 本复刻没有幻灯片 → 无对应功能：打印一行后**不退出**。
		if isHotkey(ke.Name) {
			fmt.Printf("SCR_KEY=%s 无对应功能\n", ke.Name)
			continue
		}
		// 其余任意键一律退出（上游 :378-379）；DEBUG 下不退出
		if st.debug {
			fmt.Printf("SCR_KEY=%s DEBUG 下不退出(其余键)\n", ke.Name)
			continue
		}
		fmt.Printf("SCR_KEY=%s 退出(其余键)\n", ke.Name)
		return true
	}
	// 焦点诊断：窗口没有 OS 焦点时按键不会路由过来
	for {
		ev, ok := gtx.Event(key.FocusFilter{Target: &kt})
		if !ok {
			break
		}
		if fe, isF := ev.(key.FocusEvent); isF {
			fmt.Printf("SCR_FOCUS=%v\n", fe.Focus)
		}
	}
	return false
}

// moveExit 处理一次鼠标移动：首帧记录**初始位置**（上游 :462-464），
// 之后每帧算 hypot(当前 - 初始)，> MOUSE_EXIT_THRESHOLD(10) 即退出
// （上游 :465-466）。**不是累积位移**。交互模式下不退出（上游 :454-458）。
// DEBUG 模式下不退出。
func (st *inputState) moveExit(x, y float32) bool {
	if !st.haveInitial {
		st.haveInitial = true
		st.initialX, st.initialY = x, y
		return false
	}
	dx := float64(x - st.initialX)
	dy := float64(y - st.initialY)
	st.dist = math.Hypot(dx, dy)
	return !st.interaction && !st.debug && st.dist > mouseExitThresholdPx
}

// pressExit 左键点击 → 退出（上游 :424-429）；交互模式不退出（上游 :425-426）；
// DEBUG 模式不退出。
func (st *inputState) pressExit(buttons pointer.Buttons) bool {
	return buttons&pointer.ButtonPrimary != 0 && !st.interaction && !st.debug
}

// isHotkey 上游键策略里被消费、不触发退出的热键层
// （rendering/runtime_input.py:325-370：Z/X/C/←/→/Space/↑/↓/PageUp/PageDown/Home/End）。
func isHotkey(n key.Name) bool {
	switch n {
	case "Z", "X", "C",
		key.NameSpace,
		key.NameUpArrow, key.NameDownArrow,
		key.NameLeftArrow, key.NameRightArrow,
		key.NamePageUp, key.NamePageDown,
		key.NameHome, key.NameEnd:
		return true
	}
	return false
}

// panelStyle 把 config 的面板键映射成 ui.PanelStyle
// （键名与取值口径 = 上游 default_settings.py 'weather' 块 :1523-1540）。
func panelStyle(c config.Config) ui.PanelStyle {
	st := ui.DefaultPanelStyle()
	st.Set = true
	st.Show = c.ShowBackground
	st.BG = nrgba(c.BgColor)
	st.BGOpacity = c.BgOpacity
	st.ClockBGOpacity = c.ClockBgOpacity // 时钟块遮罩
	st.Border = nrgba(c.BorderColor)
	st.BorderOpacity = c.BorderOpacity
	if c.Margin >= 0 {
		st.Margin = c.Margin
	}
	st.TextColor = nrgba(c.TextColor)
	return st
}

func nrgba(a [4]int) color.NRGBA {
	return color.NRGBA{R: u8(a[0]), G: u8(a[1]), B: u8(a[2]), A: u8(a[3])}
}

func u8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// weatherState 是帧环与后台刷新共享的天气状态。
type weatherState struct {
	mu    sync.Mutex
	s     *weather.Sample
	cls   effect.Class
	isDay bool
}

type snapshot struct {
	Sample *weather.Sample
	Class  effect.Class
	IsDay  bool
}

// moonPhaseOf 从 Sample 取今日月相，nil-safe（Sample 为 nil 时返回 nil ⇒ 
// 月亮回落到 bakeMoonCrisp 画法。需求 5。
// lastMoonPhase 记住**上一次取到的有效月相**。
// 缺陷：取不到月相时返回 nil，而月亮层对 nil 的画法是 p=0.5 = **满月**
// ⇒ 老机器上一次抓取打嗝（网络延迟/超时）就让残月当场变满月，抓到了又变回来
// ⇒ 读图发现残月和满月竟然在眼前切换了。
// 口径：月相一天只走约 1/29.5 ≈ 0.034，**没有理由在两次抓取之间跳变**
// ⇒ 取不到就沿用上一次的，绝不回落成满月。
var lastMoonPhase float64

func moonPhaseOf(s *weather.Sample) *float64 {
	if s != nil && s.MoonPhase != nil {
		lastMoonPhase = *s.MoonPhase
		v := lastMoonPhase
		return &v
	}
	if lastMoonPhase > 0 {
		v := lastMoonPhase
		return &v
	}
	return nil // 开机以来一次都没取到 ⇒ 交月亮层按满月画
}

func (st *weatherState) set(s *weather.Sample) {
	// 分档口径 = 云量优先（定案）；云量缺失时退回 WMO 码。
	cls, _ := effect.ClassifyByCloudCover(s.WeatherCode, s.CloudCover)
	// 天气类覆盖：实测云/雨/雾这些类时，低性能 x86 小主机当时的真实天气
	// 往往就是晴天（实测杭州"Mainly clear"），
	// 看不到云就无从判断。exe 旁放 scr.cls 写类名即强制该类。
	// 只影响动画类，不改温度/湿度/风力等真实读数与文字。
	if ov := classOverride(); ov != nil {
		cls = *ov
	}
	st.mu.Lock()
	st.s = s
	st.cls = cls
	st.isDay = s.IsDay == 1
	st.mu.Unlock()
}

// classOverride 读 exe 旁的 scr.cls（clear/cloudy/rain/thunder/fog/snow）。
// 与 scr.fps 同一套机制：文件在进程启动时读，改了要重启进程才生效。
func classOverride() *effect.Class {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	// 与 scr.fps 同一口径：exe 旁的同名文件（实测日志/开关文件都在 exe 旁）
	p := filepath.Join(filepath.Dir(exe), "scr.cls")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	name := strings.ToLower(strings.TrimSpace(string(b)))
	var c effect.Class
	var ok bool
	switch name {
	case "clear", "clear-day", "clear_day":
		c, ok = effect.Clear, true
	case "cloudy", "cloud":
		c, ok = effect.Cloudy, true
	case "rain":
		c, ok = effect.Rain, true
	case "thunder", "thunderstorm":
		c, ok = effect.Thunder, true
	case "fog":
		c, ok = effect.Fog, true
	case "snow":
		c, ok = effect.Snow, true
	}
	if !ok {
		logf("SCR_CLS_OVERRIDE 无法识别的类名 %q\n", name)
		return nil
	}
	logf("SCR_CLS_OVERRIDE 强制天气类=%s（文件 %s）\n", name, p)
	return &c
}

func (st *weatherState) snapshot() snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	return snapshot{Sample: st.s, Class: st.cls, IsDay: st.isDay}
}

// lastGood 读上次成功的天气。无缓存返回 nil。
//
// **必须按位置键取，取不到再按 CachedAt 取最新的一条**
// "早上8点看到背景的天气从白天突然变成晚上，过了一会了又变回了白天"。
// 原实现 `for _, v := range cache { return &vv }` 取的是 **Go map 迭代的第一条**，
// 而 Go 的 map 顺序是随机的。低性能 x86 小主机缓存里存着 3 条记录：
//
//	"Hangzhou, China" → is_day=0（旧地名、旧时刻留下的）
//	"Hangzhou"     → is_day=1
//	"杭州市"       → is_day=1
//
// 每次 API 503 失败（低性能 x86 小主机日志里 503 频繁）就走这条回落路径，
// 于是有 1/3 概率捞到那条 is_day=0 的旧记录 ⇒ 整个场景翻成黑夜，
// 下次刷新成功又翻回来。日志逐字对应：每个 SCR_FETCH_FAIL 后必跟 isDay=false，
// 每个 SCR_FETCH_OK 后必跟 isDay=true。
func lastGood(key string) *weather.Sample {
	cache := weather.LoadCache(cachePath())
	if len(cache) == 0 {
		return nil
	}
	if v, ok := cache[key]; ok {
		vv := v
		return &vv
	}
	var best *weather.Sample
	for _, v := range cache {
		vv := v
		if best == nil || vv.CachedAt > best.CachedAt {
			b := vv
			best = &b
		}
	}
	return best
}

// refresher 后台刷新天气：启动即取一次，之后按 refresh_minutes 循环。
// 失败保留上一次成功值，绝不清空。
func refresher(cfg config.Config, st *weatherState) {
	timeout := time.Duration(cfg.FetchTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	interval := time.Duration(cfg.RefreshMinutes) * time.Minute
	// 下限 1 分钟：设置面板是自由文本输入，填 0 或负数会把 API 打成轮询。
	if interval < time.Minute {
		interval = time.Minute
	}
	for {
		c := &weather.Client{
			HTTP:    &http.Client{Timeout: timeout},
			BaseURL: cfg.WeatherBaseURL,
		}
		// 设置里"搜索选点"存过坐标 ⇒ 运行时零地理编码请求。
		// 判据用 (0,0) 之外的坐标：赤道/本初子午线交点在海里，不是合法城镇。
		if cfg.LocationLat != 0 || cfg.LocationLon != 0 {
			c.HasCoords, c.Lat, c.Lon = true, cfg.LocationLat, cfg.LocationLon
		}
		s, err := c.Fetch(displayLocation(cfg))
		if err != nil {
			fmt.Printf("SCR_FETCH_FAIL=%v base=%q\n", err, cfg.WeatherBaseURL)
			if g := lastGood(weather.NormalizeLocationKey(cfg.Location)); g != nil {
				st.set(g)
			}
		} else {
			key := weather.NormalizeLocationKey(cfg.Location)
			cache := weather.LoadCache(cachePath())
			cache[key] = *s
			if err := weather.SaveCache(cachePath(), cache); err != nil {
				fmt.Printf("SCR_CACHE_SAVE_FAIL=%v\n", err)
			}
			st.set(s)
			fmt.Printf("SCR_FETCH_OK=%s %.1f°C code=%d\n", s.Condition, deref(s.Temperature), s.WeatherCode)
		}
		time.Sleep(interval)
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func cachePath() string {
	return config.CachePath()
}

// quit 是唯一的退出出口：先还原任务栏再退出，
// 绝不把登录账户的任务栏留在隐藏态。
func quit(code int) {
	restoreTaskbar()
	os.Exit(code)
}

// speedFromBudget 由帧预算算位移速度系数（实测口径）：
// speedScale = 目标帧率 / 15 ⇒ 每帧位移恒定 = vx/15，与 15 fps 逐帧观感一致。
func speedFromBudget(budget time.Duration) float64 {
	if budget <= 0 {
		return 1
	}
	fps := float64(time.Second) / float64(budget)
	return fps / 15.0
}

// buildAlert 按配置与天气样本构造降雨提醒。
//
// 数据源：**未来 N 小时滚动窗口** = weather.WindowProb(s, 当地时间, N)
// - Pct = 窗口内逐小时 precipitation_probability 的最大值
// - MM  = 窗口内逐小时 precipitation 的求和
//
// 逐字定为：
//
//	"预警应该是如果接下来要出门有雨的概率，比如早上出门那显示全天没问题，
//	  比如晚上出门那就应该是晚上是否会下雨了，**它应该是动态的，而不是一天的最大值**"
//	"**默认3小时，设置项可以改成6小时**"
//
// ⇒ 不再用 daily.precipitation_probability_max（一天的最大值）。
// 时基：响应顶层 utc_offset_seconds 已存进 Sample.UTCOffsetSeconds，
// "现在"按当地时间算：local := now.UTC().Add(utc_offset_seconds 秒)。
// 窗口长度 N = cfg.AlertWindowHours（默认 3，可设 6）。
// 触发线：cfg.AlertThresholdPct（默认 30 = 官方"有机会出现降水"下限）**不变**。
// 版式：panelrow = 天气面板下沿内的居中胶囊（定案"贴紧-60"）。
// 脉动相位：1.2 秒一个周期，按墙钟时间取相位 ⇒ 与帧率无关（时间积分，口径）。
func buildAlert(cfg config.Config, s *weather.Sample, now time.Time) *ui.Alert {
	if s == nil || cfg.AlertEnabled != nil && !*cfg.AlertEnabled {
		return nil
	}
	hours := alertWindowHours(cfg)
	// 时基：逐字口径
	local := now.UTC().Add(time.Duration(s.UTCOffsetSeconds) * time.Second)
	pct, mm, ok := weather.WindowProb(s, local, hours)
	if !ok {
		return nil // 无可用条目 ⇒ 预警不显示
	}
	th := cfg.AlertThresholdPct
	if th < 1 {
		th = ui.AlertThresholdPct
	}
	if pct < th {
		return nil
	}
	pulseOn := true
	if cfg.AlertPulse != nil {
		pulseOn = *cfg.AlertPulse
	}
	ph := math.Mod(float64(now.UnixNano())/1e9/1.2, 1.0)
	return &ui.Alert{
		Pct:         pct,
		MM:          mm,
		WindowHours: hours, // 逐字进文案
		Style:       "panelrow",
		YOff:        cfg.AlertYOffset,
		Pulse:       ph,
		PulseOn:     pulseOn,
		// 预警文案跟随 display_language（需求 5）：
		// 口径与地名显示同一个 displayLang（本文件 :338-344）—— 只有恰好
		// "zh" 走中文，其余一律英文。
		Lang: displayLang(cfg),
	}
}

// displayLocation 返回显示用的地名。
//
// 铁律：**display_language 默认 "en" = 当前样式，不改**；
// 设置里选了中文地名 **不等于** 显示语言变中文 —— 两者必须互相独立。
// ⇒ en 取存下的英文名 location_name_en，zh 取 location_name_zh，
//
//	对应语言没存过才回落到 location 原值（旧配置兼容）。
func displayLocation(cfg config.Config) string {
	if cfg.DisplayLanguage == "zh" {
		if cfg.LocationNameZh != "" {
			return cfg.LocationNameZh
		}
		return cfg.Location
	}
	if cfg.LocationNameEn != "" {
		return cfg.LocationNameEn
	}
	return cfg.Location
}

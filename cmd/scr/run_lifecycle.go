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

// 职责（卡9-app/scr）：屏保生命周期：设置窗口/全屏窗创建、窗口尺寸二值与 normalizeDialog 调用点、runSettings 主循环
// 搬移来源：cmd/scr/main.go:53-67,167-190,470-633,645-955（逐字，含注释）
// 规格：plans/active.md:214 —— app/scr = 屏保生命周期；本卡零行为变化。
// 包文档注释（旧 main.go:1-16）逐字随本文件走（Go 包文档注释只认紧贴 package 的那一份）。
// 跨文件共享声明逐字集中到本文件顶部：退出阈值 const 块（旧 main.go:53-67 = mouseExitThresholdPx/maxRuntime/windowClass）、进程内存口径 const 块（旧 main.go:167-190，main() 消费）、事件注册 tag 类型与 pt/kt（旧 main.go:645-652）。

import (
	"context"
	"fmt"
	"image"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/geo"
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

// ── 内存硬闸与 GC 触发阈值 ─────────────────────────
//
// 依据（分层阶梯实测，180 s 窗口、单层掩码，取证记录见 out/notes-textcache.md）：
//
//	单层 RSS 底数：sky/depth 52–57 MB
//	field +6~9 MB、weather +6 MB
//	全开（新构建）52 s = 单核 28.9% / 整机 7.2%，RSS 96.0 → 102.2 MB
//
// 常驻内存预算 120 MB ⇒ 硬闸取 **110 MB**，留 10 MB 余量给峰值
// （类别切换时新旧两个粒子场并存、纹理上传、GC 还没收走的垃圾）。
//
// 两者的分工：**SetMemoryLimit 是硬闸，SetGCPercent 是主导。**
// GCPercent 决定"堆涨多少触发一次 GC"（默认 100 ⇒ 堆翻倍触发一次），
// 提到 200 ⇒ 触发次数减少 ⇒ 每帧花在 GC 上的 CPU 下降，代价是堆峰值上升；
// 堆峰值涨到多少由 GCPercent 说话，limit 只兜底 —— 逼近硬闸时 runtime
// 会无视 GCPercent 强行回收（CPU 又花回 GC 上）。
// 所以"提高 GCPercent"只有在 limit 已经压住上限的前提下才安全。
// 实测峰值 102.2 MB 离 110 MB 还有 7.8 MB ⇒ 余量够，不会长期顶着硬闸跑。
const (
	// memLimitBytes 进程内存硬闸 = 120 MB 预算 − 10 MB 余量。
	memLimitBytes = 110 << 20
	// gcPercent GC 触发阈值：堆涨 200% 触发一次（默认 100）。
	gcPercent = 200
)

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
	// 窗口尺寸（b11-setrect §1.1「设置窗强制横向矩形，与显示器朝向无关」）：
	//   横屏主机 = 请求 1920×1080、下限 900×540（现有行为零回归）
	//   竖屏主机 = 请求 1100×640、下限 720×420，按工作区钳制保宽>高
	//            （sizeH=min(sizeH,sizeW-60) ⇒ 竖长条形态被需求消灭）
	// 尺寸来源 = 纯函数 settingsSizeFor（cmd/scr/setsize.go；钉死表 setsize_test.go）。
	//
	// 朝向来源 = 主显示器工作区（SPI_GETWORKAREA，foreground_windows.go 已有的
	// 同款查询，导出为 workAreaIsLandscape；非 Windows 桩返回 false ⇒ 恒竖屏）。
	// 判定本体是 ui.PanelIsLandscape(宽,高) = 宽>高 且 宽≥900（b11 阈值），与面板
	// 内部双栏判定同一条 ⇒ 窗口几何与面板版式不会各说各话。
	land := workAreaIsLandscape()
	// 工作区尺寸 = workAreaIsLandscape 的同源查询（workAreaSizePx，同源 SPI_GETWORKAREA；
	// 非 Windows 桩给 1080×1920 竖屏样例）。查询失败 ⇒ 不钳制（按 1100×640 请求值
	// 出，窗口侧 normalizeDialog 的工作区夹取自会兜底）。
	workW, workH := 1100, 640
	if ww, wh, ok := workAreaSizePx(); ok {
		workW, workH = ww, wh
	}
	// app.Size / app.MinSize 收的是 dp（gioui.org/unit）；100% 缩放下 1 dp = 1 px。
	szW, szH, szMinW, szMinH := settingsSizeFor(land, workW, workH)
	sizeW, sizeH := unit.Dp(szW), unit.Dp(szH)
	minW, minH := unit.Dp(szMinW), unit.Dp(szMinH)
	w := new(app.Window)
	w.Option(
		app.Title("WeatherScr 设置"),
		// 竖屏 1080×1920 上完整显示：窗口按 dp 给，
		// 内容用可滚动的 material.List，控件全部可达。
		// 横屏走同一套 dp 口径（开放项处置：DPI 按 100% 主机实测 ⇒ 1 dp = 1 px）。
		app.Size(sizeW, sizeH),
		app.MinSize(minW, minH),
	)
	// 面板主题（B2）：配色跟随 Windows 深浅色 + 面板局部字号 panelTextSp。
	// 与屏保侧的 ui.New() 是两份互不相干的 Theme ⇒ 屏保画面零影响
	// （Screen.Theme 的消费点只有这里与 cmd/setshot，spec §0）。
	// 深浅色在构建面板时读一次注册表，面板开着期间不跟系统变
	// （取舍见 ui/theme.go 顶部注释）。
	// B5：字号按朝向选档（横屏 19/21/13sp，竖屏 15/17/12sp 逐字不变）。
	th := ui.NewPanelTheme(land)
	var ops op.Ops
	ownerDone := false
	// 风格夺回的一次性守卫（与 ownerDone 同构）：只在第一帧执行一次。
	styleDone := false

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
				// 设置窗口的风格与尺寸夺回：`.scr` 启动路径下 STARTUPINFO 带全屏
				// 尺寸/位置，gioui 建窗的 CW_USEDEFAULT（app/os_windows.go:195-196）
				// 被它接管，`app.Size` 的 SetWindowPos 只改尺寸不碰样式位 ⇒
				// 窗口落成满屏无边框。这里显式把风格、扩展样式与尺寸定位夺回。
				//
				// 线程规则：SetWindowLong/SetWindowPos/ShowWindow 必须跑在创建该
				// 窗口的线程上（app/window.go:321-326），所以放 `w.Run` 回调里；
				// 从本 goroutine 直接调用会永久阻塞。第一帧只做一次。
				if !styleDone {
					styleDone = true
					w.Run(func() {
						// 目标尺寸 = settingsSizeFor 按朝向 + 工作区钳制出来的请求值（b11-setrect：
						// 横屏 1920×1080 / 竖屏 1100×640 钳制口径，宽>高恒成立）。
						normalizeDialog(findOwnWindow(windowClass), int(sizeW), int(sizeH), land)
					})
				}
				// 父窗口挂接必须在窗口线程上做（同一条正道）。
				if parent != 0 && !ownerDone {
					ownerDone = true
					w.Run(func() {
						applyOwner(findOwnWindow(windowClass), parent)
					})
				}
				gtx := app.NewContext(&ops, e)
				act := panel.HandleKeys(gtx)
				panel.Layout(gtx, th)
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
	// 批次 b-alert-v2 第 6 步：官方信号**独立拉取**（10 min 节奏），
	// 与 1 min 的自算刷新分开跑（spec/alert-latency-and-logic.md §11 末行逐字：
	// 「拉取周期：官方信号 10 min（一次省级请求）；自算随 refresh_minutes=1」）。
	go officialRefresher(cfg)

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
	// P5b 测量件开关（spec/p5b-dbg.md）：WS_DBG_FRAMES=1 才启用全帧帧时长 +
	// 每帧绘制次数日志。**未设时 dbgFrames=false ⇒ 下面每一处使用都短路**，
	// 帧循环行为与本批次之前逐字一致（判据 4）。
	dbgFrames := ui.WSDbgFrames()
	if dbgFrames {
		// 启用确认行只在开关内打（未启用时一行都不产生）。
		logf("SCR_WSDBGFRAMES=1 生效：每帧一行 SCR_FRAME_MS，绘制计数开启")
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title(os.Getenv("WS_TITLE")), // P7 取证用：环境变量决定窗口标题（空 = 与改前一致，不改默认行为）
			// 不申请 gioui 的 Fullscreen：它在 Windows 上 = 最大化显示态
			// （app/os_windows.go:921-923 用 SW_SHOWMAXIMIZED），Win32 把最大化
			// 窗口钳到工作区 ⇒ 底部任务栏那条 40 px 露出（实机取证：截图
			// y=1880–1919 纯白、帧日志 size=1080x1880 共 400 帧）。
			// 铺满由 applySaverStyle 独占：WS_POPUP|WS_VISIBLE + SW_SHOWNORMAL +
			// SetWindowPos(HWND_TOPMOST, 整块 rcMonitor)。见 foreground_windows.go 头部。
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
		// 闪现间隔按码分级（D4）：建场就把码传进去。
		if snap.Sample != nil {
			field.SetWeatherCode(snap.Sample.WeatherCode)
		}
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

				// 闪现间隔按码分级（D4）：每帧把码传进粒子场（值变了才生效，零成本）。
				if snap.Sample != nil {
					field.SetWeatherCode(snap.Sample.WeatherCode)
					if prevField != nil {
						prevField.SetWeatherCode(snap.Sample.WeatherCode)
					}
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

				// P3 判据 D 第 3 条：云/雾拥挤度**每 200 帧一条**（不得逐帧打，
				// GUI 子系统下 stdout 为空 ⇒ 走 logf 文件日志，与 SCR_FETCH_* 同口径）。
				// P6 设计第 6 条：行内**同时给两个口径** —— dualBandOnly（带-only 真落位，
				// DriftOverlapBandOnly）与 dualIncl（含粒子云/雾团，DriftOverlapIncl），
				// 实机复采看 dualBandOnly ≤5% / dualIncl ≤10% 两条线。
				if frames%200 == 0 {
					crowdLayers, crowdDualIncl := field.DriftOverlapIncl()
					_, crowdDualBandOnly := field.DriftOverlapBandOnly()
					logf("SCR_CROWD layers=%.2f dualBandOnly=%.1f%% dualIncl=%.1f%% active=%d",
						crowdLayers, crowdDualBandOnly, crowdDualIncl, field.ActiveCount())
				}

				// 后台刷新换到了新天气 → 重建粒子场
				cur := st.snapshot()
				if cur.Class != snap.Class || cur.IsDay != snap.IsDay {
					prevField = field // 旧场进入淡出，不是一帧消失
					snap = cur
					field = ui.NewFieldEntering(snap.Class, snap.IsDay, time.Now().UnixNano())
					field.SetSpeedScale(speedFromBudget(frameBudget))
					field.SetFade(0)
					if snap.Sample != nil {
						field.SetWeatherCode(snap.Sample.WeatherCode)
					}
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
				// P5b 测量件（spec/p5b-dbg.md 第 2 条）：`frames <= 30` 这个采样上限
				// **只在 WS_DBG_FRAMES=1 时放开** ⇒ 追加的是 `|| dbgFrames`，
				// 未启用时 dbgFrames=false，`frames <= 30 || false` 与改前逐字等价，
				// 默认路径语义一字未改（判据 4）。
				if frames <= 30 || dbgFrames {
					logf("SCR_FRAME n=%d layout=%s frame=%s size=%dx%d active=%d",
						frames, fLayout.Round(time.Millisecond),
						time.Since(frameStart).Round(time.Millisecond),
						size.X, size.Y, field.ActiveCount())
				}
				if dbgFrames {
					// 每帧一行，走现成文件日志 logf（GUI 子系统下 stdout 为空，
					// 与 SCR_CROWD / SCR_FETCH_* 同口径）。帧耗时口径 = 主循环现成的
					// frameStart 计时（与上一行 SCR_FRAME 的 frame= 同一个量），
					// **没有新增计时器**。draws 取本帧累计值，打印后清零。
					draws := field.DrawCount()
					field.ResetDrawCount()
					logf("SCR_FRAME_MS n=%d ms=%d draws=%d active=%d w=%d h=%d",
						frames, time.Since(frameStart).Milliseconds(),
						draws, field.ActiveCount(), size.X, size.Y)
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

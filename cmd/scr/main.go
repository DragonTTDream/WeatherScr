package main

// 职责（app/scr）：main+args 派发（/s /c /p|/l /a --help/-h/? 与无法识别静默退出）
// 搬移来源：cmd/scr/main.go:192-252,254-259,261-281,341-370,635-643,1476-1541（逐字，含注释）
// 口径：app/scr = 屏保生命周期；本次搬移零行为变化。
// 闸门逐字 grep cmd/scr/main.go 的取数日志锚点（WXURL / SCR_FETCH_OK / SCR_FETCH_FAIL / ALERTDIAG_SLOTS / OnRequest）⇒ 锚点所在函数 refresher 必须留在 main.go。

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"gioui.org/app"

	"github.com/DragonTTDream/WeatherScr/config"
	"github.com/DragonTTDream/WeatherScr/scrargs"
	"github.com/DragonTTDream/WeatherScr/weather"
)

func main() {
	// 进程启动早期就定死内存口径：硬闸 110 MB + 触发阈值 200%。
	// 不读环境变量、不加配置键（与 config 的 34 个键无关）。
	debug.SetMemoryLimit(memLimitBytes)
	debug.SetGCPercent(gcPercent)

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
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s", len(args), first(args), sa.Action)
		os.Exit(0)
		return

	case scrargs.ActionHelp:
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s", len(args), first(args), sa.Action)
		usage()
		os.Exit(0)
		return

	case scrargs.ActionUnknown:
		// 规范：无法识别 → 立即退出，不弹窗不打印（协议表）。
		// 带外取证例外（实机取证）：这条路径此前是**完全无声**的，
		// 于是"进程不见了 + 零日志"与"启动失败 / argv 畸形 / 秒退"三种情况
		// 在现场完全分不开（实测：tasklist 20 次全空、三处日志全零行）。
		// 这里只往**标记文件开启的日志**里写一行：不弹窗、不打印 stdout、
		// 不改协议行为 —— 生产态（无 scr.log / scr.debug / 临时目录标记）
		// 下 dbgLogLine 直接 return，一个字节都不落。
		logf("SCR_ARGC=%d ARG0=%q ACTION=%s", len(args), first(args), sa.Action)
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
// 理由：排查中"日志停在 219 字节"无法与"程序真的没打印"区分开
// （coverWatcher 的 COVER= 行没进日志）。每行前缀
// HH:MM:SS.mmm 后可以直接看出程序在哪一刻停止输出。
// 逐行直写：fmt.Printf 每次调用是一个 write(2)，行内原子，天然逐行 flush
// （stdout 无缓冲场景逐行立即可见）。
func logf(format string, args ...any) {
	fmt.Printf("%s ", time.Now().Format("01-02 15:04:05.000"))
	fmt.Printf(format, args...)
	fmt.Printf("\n")
	// 真实屏保路径下 stdout 无处可去：屏保由 Winlogon/Explorer 在屏保
	// desktop 上启动，没有控制台、也没有人接管道 ⇒ 此前在真实路径上
	// **一条日志证据都拿不到**（s12b/s12c 的 SCR_EXIT 全部丢失）。
	// ⇒ 调试态同时落文件：exe 同目录 `scr.debug` 标记文件存在，
	// 或环境变量 SCR_DEBUG=1 时，逐行追加写 `scr_run.log`。
	// 标记文件这条路是必需的：屏保进程的环境变量继承自登录时的
	// Explorer 环境块，`setx` 之后不重启 Explorer 不生效，不可靠。
	dbgLogLine(fmt.Sprintf("%s %s", time.Now().Format("01-02 15:04:05.000"),
		fmt.Sprintf(format, args...)))
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

// /p|/l <hwnd>：预览模式。
// 子窗口 + gioui headless 离屏渲染 + SetDIBitsToDevice 贴图的实现在
// preview_windows.go（构建标签 windows && !novulkan）；
// novulkan 构建下 preview_novulkan_windows.go 打 PREVIEW_UNSUPPORTED。
// 预览不置顶、不隐藏任务栏、不因输入退出。
func preview(hwnd uintptr) {
	drawPreview(hwnd)
	os.Exit(0) // 预览失败也不得非零退出
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
	// 连续失败计数（成功即复位）。**不在本周期内重试**
	// （口径：请求超时就下一周期再请求，本周期内不重试），只把下一周期拉长：
	// 阶梯 = weather.NextRefreshInterval，上游给了 Retry-After 时取两者较大值。
	failCount := 0
	for {
		c := &weather.Client{
			HTTP:    &http.Client{Timeout: timeout},
			BaseURL: cfg.WeatherBaseURL,
			// 带外可观测：每次取数把完整 URL 打进日志（实机取证"走没走代理"）。
			OnRequest: func(u string) { logf("WXURL=%s", u) },
		}
		// 设置里"搜索选点"存过坐标 ⇒ 运行时零地理编码请求。
		// 判据用 (0,0) 之外的坐标：赤道/本初子午线交点在海里，不是合法城镇。
		if cfg.LocationLat != 0 || cfg.LocationLon != 0 {
			c.HasCoords, c.Lat, c.Lon = true, cfg.LocationLat, cfg.LocationLon
		}
		s, err := c.Fetch(displayLocation(cfg))
		wait := interval
		if err != nil {
			failCount++
			wait = weather.NextRefreshInterval(failCount, interval)
			var pe *weather.ProviderError
			if errors.As(err, &pe) && pe.RetryAfter > wait {
				wait = pe.RetryAfter
			}
			// 走文件日志：GUI 子系统下 fmt.Printf 的 stdout 是空的
			// （； W14 实机就因此看不见失败）。
			logf("SCR_FETCH_FAIL=%v base=%q", err, cfg.WeatherBaseURL)
			if g := lastGood(weather.NormalizeLocationKey(cfg.Location)); g != nil {
				st.set(g)
			}
			// 退避行同样走 logf（理由与 SCR_FETCH_FAIL 一致：GUI 下 stdout 为空）。
			logf("SCR_FETCH_BACKOFF=%s FAILS=%d", wait, failCount)
		} else {
			failCount = 0
			key := weather.NormalizeLocationKey(cfg.Location)
			cache := weather.LoadCache(cachePath())
			cache[key] = *s
			if err := weather.SaveCache(cachePath(), cache); err != nil {
				logf("SCR_CACHE_SAVE_FAIL=%v", err)
			}
			st.set(s)
			logf("SCR_FETCH_OK=%s %.1f°C code=%d", s.Condition, deref(s.Temperature), s.WeatherCode)
			// 数据闸门带外日志（逐字格式，走 logf 才进文件）。
			// PROB_N < 12（灾害层官方配对里最长的窗口，weather.officialMaxWindowHours）
			// ⇒ 判据置'数据不可用'态（见 weather.Evaluate）。
			logf("ALERTDIAG_SLOTS HOURLY=%d PROB_N=%d MM_N=%d MIN15_N=%d",
				len(s.HourlyTime), s.ProbN, s.MMN, s.Min15N)
			// 判据 + 滞回：一个采样周期 = 这一次取数。
			advanceAlertEval(cfg, s, time.Now())
		}
		time.Sleep(wait)
	}
}

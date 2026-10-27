package main

// 职责（app/scr）：调试开关读取：SCR_DEBUG / scr.log / scr.layers / scr.fps / scr.cls 与带外日志落点
// 搬移来源：cmd/scr/main.go:69-165,283-339,1190-1226（逐字，含注释）
// 规格：—— app/scr = 屏保生命周期；本卡零行为变化。
// WS_TITLE 读取点（旧 main.go:704 app.Title(os.Getenv("WS_TITLE"))）在 run() 的 w.Option 里，函数体逐字未动 ⇒ 留在 run_lifecycle.go。

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui"
)

// scrDebug 是调试开关：SCR_DEBUG=1。
// 默认（无该变量）行为必须与现状完全一致。
// layerMask 读图层开关（性能定位用）。
// 语法：逗号分隔的图层名；`none`/空/`all` = 全开（0 在 ui 侧按全开处理）。
// 例：只画背景不画文字 = `sky,depth,field`。
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

// dbgLog 调试态日志文件（nil = 未启用）。
var dbgLog *os.File
var dbgOnce sync.Once

// tempLogMarkerName = 临时目录标记文件名（第三个开关）。
// 为什么要有它（实机取证）：exe 同目录的两个标记（scr.debug / scr.log）
// 对装在 `C:\Program Files\WeatherScr` 与 `C:\Windows\System32` 的产物，
// 都要跨过 ACL/完整性级别这道墙才能落 `scr_run.log`；取证路径一旦依赖系统目录写入，
// "拿不到日志"与"写不进去"就分不开。临时目录标记把取证与安装位置解耦。
const tempLogMarkerName = "WeatherScr-scr.log"

// tempLogEnabled 临时目录里有没有标记文件。
func tempLogEnabled() bool {
	_, err := os.Stat(filepath.Join(os.TempDir(), tempLogMarkerName))
	return err == nil
}

// dbgLogPath 决定日志落点（纯函数，钉死单测可测）：
// - 临时目录标记在 ⇒ 落临时目录（与安装位置、ACL 无关）；
// - 否则 exe 同目录（scr.debug / scr.log 的老口径）。
func dbgLogPath(exeDir, tempDir string, useTemp bool) string {
	if useTemp {
		return filepath.Join(tempDir, "WeatherScr-scr_run.log")
	}
	return filepath.Join(exeDir, "scr_run.log")
}

// dbgLogLine 启用时把一行追加写进日志文件并立刻 Sync。
func dbgLogLine(line string) {
	dbgOnce.Do(func() {
		useTemp := tempLogEnabled()
		if !useTemp && !scrDebug() && !scrLogOnly() {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.OpenFile(dbgLogPath(filepath.Dir(exe), os.TempDir(), useTemp),
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

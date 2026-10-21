// Package config 是 WeatherScr 的唯一配置。
package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/DragonTTDream/WeatherScr/ui"
)

// Config 键与一致。
type Config struct {
	Location string `json:"location"`
	// ---- 位置：存坐标 + 搜索选点 ----
	// 设置里搜索 → 选一个候选 → 存坐标与中英文显示名；运行时零地理编码请求。
	// 旧配置没有这两个键 ⇒ HasLocation 为 false，回落到按名地理编码（兼容）。
	LocationLat float64 `json:"location_lat"`
	LocationLon float64 `json:"location_lon"`
	// LocationNameZh/En = 同一地点的中英文显示名（搜索时一并存下）。
	LocationNameZh string `json:"location_name_zh"`
	LocationNameEn string `json:"location_name_en"`
	// DisplayLanguage = 地名显示语言："en"（默认 = 当前样式，不改）/ "zh"。
	DisplayLanguage string `json:"display_language"`
	WallpaperDir    string `json:"wallpaper_dir"`
	WallpaperIntS   int    `json:"wallpaper_interval_s"`
	Units           string `json:"units"`
	RefreshMinutes  int    `json:"refresh_minutes"`

	// WeatherBaseURL 非空 = 天气请求走自建代理（低性能 x86 小主机无 DNS/无外网）。
	// 空 = 直连 Open-Meteo。
	WeatherBaseURL string `json:"weather_base_url"`

	// FetchTimeoutSeconds 单次天气请求超时。上游 timeout=10；
	// 屏保启动不得被网络卡住，取小值并配合后台刷新。
	FetchTimeoutSeconds int `json:"fetch_timeout_seconds"`

	EffectEnabled []string `json:"effect.enabled"`
	FontFamily    string   `json:"font_family"`

	// ---- 面板底与呈现（键名逐字照抄上游 core/settings/default_settings.py
	// 'weather' 块 :1523-1540；'clock' 块 :503-528 带同名键；
	// 'input' 块 :27 的 interaction_mode。色值形状同上游 = [R,G,B,A]。
	// 证据：----

	BgColor   [4]int  `json:"bg_color"`   // 上游 [35,35,35,255]
	BgOpacity float64 `json:"bg_opacity"` // 上游 0.3；本复刻默认 0.22
	// ClockBgOpacity 时钟块面板底不透明度，默认 0.26。
	//
	// **偏离上游**：上游 'clock' 块
	// （core/settings/default_settings.py:503-528）是 bg_opacity 0.3 +
	// **show_background: False** —— 上游时钟默认没有面板底（它的背景是 shader，
	// 不忙。本复刻背景是忙碌的 Kenney 云/雾，实测云压在时间数字上
	// （视觉判读），时钟区需要遮罩且强于天气块。
	// 此前把 0.45 → 0.26、天气块 0.3 → 0.22。
	// 客观指标 bin/fxmetrics.py 实测
	// panel_step 亮度落差 **25.7–47.4 / 255**、大跳变 89–134 处 ⇒ 面板造成的
	// 断层是量化出来的最大败笔。时钟块遮罩归零，回到上游
	// 'clock' 块 `show_background: False`（default_settings.py:503-528）
	// 的原品语义 —— 时钟只有阴影字、无面板底。键名与键语义不变。
	ClockBgOpacity      float64 `json:"clock_bg_opacity"`       // 本复刻 0.0 = 对齐上游 show_background False
	BorderColor         [4]int  `json:"border_color"`           // 上游 [255,255,255,255]
	BorderOpacity       float64 `json:"border_opacity"`         // 上游 1.0；本复刻默认 0.0 = 不描边
	ShowBackground      bool    `json:"show_background"`        // 上游 True
	TextColor           [4]int  `json:"color"`                  // 上游 [255,255,255,230]
	FontSize            int     `json:"font_size"`              // 上游 25
	IconSize            int     `json:"icon_size"`              // 上游 96
	Margin              int     `json:"margin"`                 // 上游 30
	ShowConditionIcon   bool    `json:"show_condition_icon"`    // 上游 True
	ShowDetailsRow      bool    `json:"show_details_row"`       // 上游 True
	ShowForecast        bool    `json:"show_forecast"`          // 上游 True
	ShowFiveDayForecast bool    `json:"show_five_day_forecast"` // 上游 False

	// InteractionMode 上游 'input' 块 :27 'interaction_mode': False。
	// False = 传统"一动就退出"；True = 交互模式（不因鼠标输入退出）。
	InteractionMode bool `json:"interaction_mode"`

	// 布局偏移（时钟/天气块 y 偏移，像素）。上游对应 custom_layout 的 rect，
	// 无同名标量键；本复刻按需求提供像素偏移。
	// TODO(未取证)：上游 custom_layout 的偏移单位与口径未取证。
	ClockYOffset   int `json:"clock_y_offset"`
	WeatherYOffset int `json:"weather_y_offset"`

	// ---- 降雨提醒----
	// 上游无任何天气预警功能 ⇒ 本条是需求驱动的新增。
	// 阈值按中国标准：30% = 中国降水概率分级里"有机会出现降水"下限。
	// AlertYOffset 提醒条垂直偏移（像素），与 clock_y_offset / weather_y_offset 同族可调；
	// 默认 -60 = 贴紧天气文字（需求：贴紧 -60）。
	AlertEnabled      *bool `json:"alert_enabled"`
	AlertThresholdPct int   `json:"alert_threshold_pct"`
	AlertYOffset      int   `json:"alert_y_offset"`
	AlertPulse        *bool `json:"alert_pulse"`
	// 预警视野旋钮已在批次 b-alert-v1b 第 2 步拆掉：窗口由官方四级**配对**决定
	// （红/橙 3 h、黄 6 h、蓝 12 h），日常层视野 = 常量 2 h（weather.umbrellaViewHours）。
	// 旧键 alert_window_hours 与旧的视野小时键读到时只写一行日志，不映射成任何视野值。
	// AlertMinMM = 日常层的淋湿门槛（mm / 15 分钟格），默认 0.05（≈0.2 mm/h）。
	// 合法范围 0.02–0.5，越界回落默认并写日志（第 8 步）。
	AlertMinMM float64 `json:"alert_min_mm"`

	// 路径为本进程实际使用的配置文件（不写入 JSON）
	path string
}

// 上游实况缺省值（实测自 settings_v2.json）。
func Defaults() Config {
	// 指针型开关的默认值载体
	tTrue := true
	return Config{
		Location:        "Hangzhou, China",
		DisplayLanguage: "en",
		WallpaperDir:    `C:\Users\Public\Pictures`,
		WallpaperIntS:   40,
		Units:           "c",
		// 刷新间隔 30 → 1 分钟。需求：获取天气不占资源时按 1 分钟刷新。
		// 实测成立（低性能 x86 小主机，真实接口地址）：
		// 地理编码已缓存 ⇒ 一次刷新 = 1 个请求、平均 476 ms 网络等待、响应体 1090 B；
		// 1 分钟刷新 = 60 次/小时 ≈ 60 KB/小时，等待在独立 goroutine 里，CPU 可忽略。
		RefreshMinutes:      1,
		FetchTimeoutSeconds: 5,
		EffectEnabled: []string{
			"clear", "cloudy", "fog", "rain", "snow", "thunder", "dust",
		},
		FontFamily: ui.FontFamily,

		// 逐字照抄上游 'weather' 块（default_settings.py:1523-1540）与
		// 'input' 块 :27。
		//
		// 按视觉读图整改三个默认值：
		// 面板底 = **全透明**
		//
		// 需求：面板底取全透明（两个可选项：与上游一致 / 全透明，取后者）。
		// 实现走上游自己的开关语义：'weather' 块 `show_background`
		// 是可调项（上游 :521 'clock' 就是 False） ⇒ 置 False = 无底无边框，
		// 不新增任何上游没有的键。
		// 上游原值留档在此，供设置面板/文档对照：
		// bg_color [35,35,35,255] / bg_opacity 0.3 /
		// border_color [255,255,255,255] / border_opacity 1.0
		BgColor:             [4]int{35, 35, 35, 255},
		BgOpacity:           0.0, // 全透明
		ClockBgOpacity:      0.0, // 全透明
		BorderColor:         [4]int{255, 255, 255, 255},
		BorderOpacity:       0.0,   // 无边框
		ShowBackground:      false, // 全透明：不画面板底也不画边框（上游同名字段的 False 取值）
		TextColor:           [4]int{255, 255, 255, 230},
		FontSize:            25,
		IconSize:            96,
		Margin:              30,
		ShowConditionIcon:   true,
		ShowDetailsRow:      true,
		ShowForecast:        true,
		ShowFiveDayForecast: false,
		InteractionMode:     false,
		ClockYOffset:        0,
		WeatherYOffset:      0,
		AlertEnabled:        &tTrue,
		AlertThresholdPct:   30,
		AlertYOffset:        -60,
		AlertPulse:          &tTrue,
		// 淋湿门槛默认 0.05 mm/15min（视野已进常量，配置里没有旋钮）。
		AlertMinMM: defaultAlertMinMM,
	}
}

// defaultAlertMinMM = 淋湿门槛默认值（第 8 步：0.05 mm/15min ≈ 0.2 mm/h）。
// 与 weather.defaultMinMMPer15min 同值（两处常量，谁都不引用谁，避免跨包耦合）。
const defaultAlertMinMM = 0.05

// NormalizeAlertMinMM 把越界的淋湿门槛收口成默认 0.05，并报告是否发生回落。
// 合法范围 = [0.02, 0.5]（第 8 步）。
func NormalizeAlertMinMM(v float64) (float64, bool) {
	if v >= 0.02 && v <= 0.5 {
		return v, false
	}
	return defaultAlertMinMM, true
}

// MigrateAlertWindowHours 旧键处置（批次 b-alert-v1b 第 2 步）：**只作废、不映射**。
//
// 视野旋钮已拆（spec/b-alert-v1b.md「标定实测」：视野 12 h ⇒ 亮时占比 36%、
// 提前量中位 13.75 h，同时违反两条目标；视野 2 h + 门槛 0.05 三条目标同时达标）。
// 预警窗口由官方四级**配对**决定（红/橙 3 h、黄 6 h、蓝 12 h，见 weather/window.go），
// 日常层视野 = 常量 2 h ⇒ 旧键 alert_window_hours 读到时**不再映射成任何视野值**，
// 只由调用方写一行日志（不静默）。返回"是否读到了旧键"（口径照 NormalizeAlertMinMM：
// 收口逻辑在本函数，写日志在调用方）。值 0 = 旧配置里等于没设 ⇒ 报告"无需作废"。
func MigrateAlertWindowHours(v int) bool { return v != 0 }

// 安装位置。
const InstallDir = `C:\Program Files\WeatherScr`
const ConfigFile = "config.json"
const CacheFile = "weather-cache.json"

// Path 返回配置文件路径。
func Path() string { return filepath.Join(InstallDir, ConfigFile) }

// CachePath 返回天气缓存路径（last-good evidence）。
// 安装目录不可写时退到登录账户目录，保证断网后仍有上次天气可显示。
func CachePath() string {
	if _, err := os.Stat(InstallDir); err == nil {
		p := filepath.Join(InstallDir, CacheFile)
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			f.Close()
			return p
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		_ = os.MkdirAll(filepath.Join(d, "WeatherScr"), 0o755)
		return filepath.Join(d, "WeatherScr", CacheFile)
	}
	return filepath.Join(os.TempDir(), CacheFile)
}

// Load 读配置；文件缺失/损坏 → 返回缺省值而非报错
// （屏保约束：配置读不到也要能跑，不得空白或崩）。
func Load() Config { return LoadFrom(Path()) }

// LoadFrom 从指定路径读配置（Load 用 Path()；测试用临时文件，
// 因为 Path() 是 Windows 安装目录，非 Windows 的自检只能走显式路径。
//
// 读完后收口（批次 b-alert-v1 第 8 步 + b-alert-v1b 第 2 步）：
//
//	旧键 alert_window_hours 读到时写一行作废日志（窗口由官方量级配对决定，
//	  本键作废且不映射成任何视野值）；
//	alert_min_mm 合法范围 0.02–0.5，越界回落 0.05 并写日志。
func LoadFrom(path string) Config {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Defaults()
	}
	// 旧键：Config 里已无该字段 ⇒ 从原始 JSON 再解一次该键，只写日志不改行为。
	var raw struct {
		AlertWindowHours *int `json:"alert_window_hours"`
	}
	if err := json.Unmarshal(b, &raw); err == nil && raw.AlertWindowHours != nil &&
		MigrateAlertWindowHours(*raw.AlertWindowHours) {
		log.Printf("config: 旧键 alert_window_hours=%d 已作废 —— 预警窗口由官方量级配对决定"+
			"（红/橙 3 h、黄 6 h、蓝 12 h），日常层视野固定 2 h，本键不映射成任何视野值",
			*raw.AlertWindowHours)
	}
	if got, fell := NormalizeAlertMinMM(c.AlertMinMM); fell {
		log.Printf("config: alert_min_mm=%v 越界（合法 0.02–0.5），回落默认值 %v", c.AlertMinMM, got)
		c.AlertMinMM = got
	}
	c.path = path
	return c
}

// Save 写配置（缩进 4 空格，与上游实况文件风格一致）。
func (c Config) Save() error {
	if err := os.MkdirAll(InstallDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), b, 0o644)
}

// Path 返回该配置来源路径。
func (c Config) Path() string {
	if c.path != "" {
		return c.path
	}
	return Path()
}

// MarshalString 把配置序列化成缩进 JSON 串（/c 用）。
func (c Config) MarshalString() (string, error) {
	b, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

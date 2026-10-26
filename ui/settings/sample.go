package settings

// R5 卡 8（ui/settings 拆包）：夹具 sampleModel 随判据一起迁进本包，
// 从"判据文件内的函数"改成**非测试文件里的函数**（本体 = 旧 ui/settings_ui_test.go:12-49，
// 字段值与注释逐字未动）——因为闸门判据本体在 check_landscape.go / check_b2.go
// 这类非测试文件里（ui 侧闸门薄壳要能调用，见 shells_test.go 头注），夹具必须同样在非测试侧。

func sampleModel() Model {
	return Model{
		Location:            "Hangzhou, China",
		FontSize:            25,
		IconSize:            96,
		Margin:              30,
		Units:               "c",
		RefreshMinutes:      30,
		ShowBackground:      true,
		BgOpacity:           0.3,
		ClockBgOpacity:      0.45,
		ShowConditionIcon:   true,
		ShowDetailsRow:      true,
		ShowForecast:        true,
		ShowFiveDayForecast: false,
		EffectsOn:           true,
		WallpaperDir:        `C:\Users\Public\Pictures`,
		WallpaperIntervalS:  40,
		ClockYOffset:        0,
		WeatherYOffset:      0,
		InteractionMode:     false,
		// 提醒与位置。**必须写规范值**：面板 setModel 会把
		// display_language 规范成 "en"（非 zh 一律回落 en），夹具写空串会让
		// model() 读出 "en" ≠ 夹具的 "" ⇒ 无事件帧也被判成"有改动"（实测两条测试就是这么红的）。
		AlertEnabled:      false,
		AlertThresholdPct: 0,
		AlertYOffset:      0,
		AlertPulse:        false,
		AlertMinMM:        0.05,
		LocationLat:       0,
		LocationLon:       0,
		LocationNameZh:    "",
		LocationNameEn:    "",
		DisplayLanguage:   "en",
	}
}

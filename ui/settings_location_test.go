package ui

// （ui/settings 拆包）·闸门薄壳（79 的源码锚点 =
// `grep -q "TestWithCountry" ui/settings_location_test.go` + 闸门链里的同名 -run）。
// 判据本体随 withCountry 迁进 ui/settings（check_location.go:22 的 CheckWithCountry，
// 六条口径逐字未动），这里留一行薄壳 ⇒ 闸门文件零改动。

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/ui/settings"
)

func TestWithCountry(t *testing.T) { settings.CheckWithCountry(t) }

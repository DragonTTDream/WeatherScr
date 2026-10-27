package settings

// （ui/settings 拆包）·判据名薄壳：判据本体见 check_location.go:22（逐字迁移）。
// ui 侧同名薄壳在 ui/settings_location_test.go（闸门 的源码锚点路径）。

import "testing"

func TestWithCountry(t *testing.T) { CheckWithCountry(t) }

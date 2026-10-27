package config

import (
	"testing"
)

// S4 钉死单测：默认单次天气请求超时 = 10 秒（对齐上游 timeout=10）。
// 键名与语义逐字不动，只有默认值口径变化（docs/CONFIG.md 第 27 行同步）。
func TestDefaultFetchTimeoutSecondsIs10(t *testing.T) {
	if got := Defaults().FetchTimeoutSeconds; got != 10 {
		t.Errorf("Defaults().FetchTimeoutSeconds = %d，期望 10", got)
	}
}

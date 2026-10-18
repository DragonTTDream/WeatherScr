package main

import (
	"testing"

	"github.com/DragonTTDream/WeatherScr/config"
)

// TestDisplayLanguageIndependent 锁死 2026-10-16 两次指定的铁律：
// **选了中文地名 ≠ 显示语言变中文**。display_language 默认 "en" = 当前样式不改，
// 显示语言与"选了哪个地名"必须互相独立。
//
// 犯过的形：部署时把 display_language 写成 zh，理由是"选中文区名就该显示中文"
// —— 需求：显示语言默认英文，结果串按 display_language 输出，不随所选地名变。
func TestDisplayLanguageIndependent(t *testing.T) {
	// 搜索选点存下的中英文对
	base := config.Config{
		LocationLat:    30.2495,
		LocationLon:    120.1583,
		LocationNameZh: "杭州市",
		LocationNameEn: "Hangzhou",
	}

	t.Run("默认(未设)必须走英文", func(t *testing.T) {
		c := base
		c.Location = "Hangzhou"
		if got := displayLocation(c); got != "Hangzhou" {
			t.Fatalf("display_language 未设时应显示英文，得到 %q", got)
		}
	})

	t.Run("en 必须显示英文，与存了中文名无关", func(t *testing.T) {
		c := base
		c.DisplayLanguage = "en"
		c.Location = "Hangzhou"
		if got := displayLocation(c); got != "Hangzhou" {
			t.Fatalf("en 档显示中文了：%q", got)
		}
	})

	t.Run("zh 才显示中文", func(t *testing.T) {
		c := base
		c.DisplayLanguage = "zh"
		c.Location = "杭州市"
		if got := displayLocation(c); got != "杭州市" {
			t.Fatalf("zh 档应显示中文，得到 %q", got)
		}
	})

	t.Run("旧配置(没存过英文名)回落 location", func(t *testing.T) {
		c := config.Config{Location: "Hangzhou, China", DisplayLanguage: "en"}
		if got := displayLocation(c); got != "Hangzhou, China" {
			t.Fatalf("旧配置应回落 location，得到 %q", got)
		}
	})

	t.Run("Defaults 的显示语言必须是 en", func(t *testing.T) {
		if got := config.Defaults().DisplayLanguage; got != "en" {
			t.Fatalf("Defaults().DisplayLanguage = %q，必须是 en（当前样式不改）", got)
		}
	})
}

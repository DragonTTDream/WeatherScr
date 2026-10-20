package ui

import (
	"os"
	"strconv"
)

// 预览工装（dev-only，出厂二进制不读这些变量；闸门逐字证明）。
// 用途：出太阳铺光候选效果图的对比图交肉眼定夺，不改出厂口径。
//
//	WS_GLOWP=0      关掉 pkGlow 光斑粒子（Kenney light_01 的小圆圈）
//	WS_SUN_L        旧近场铺光的 L（屏宽分数），出厂 0.18；单一连续剖面起烘焙不再读它
//	WS_SUN_SKY      全天空那一组（主圈 + 全天空常数）的幅度系数，出厂 1.0（0 = 整组关掉）
//	WS_SUN_LIFT_R   盘周提亮的平台半径（× 盘半径），出厂 3.0
//	WS_SUN_LIFT_LV  盘周提亮峰值亮度，出厂 0（提亮圈已归零，见 render.go 的 sunLiftLevel）
//	WS_SUN_LIFT_FADE 提亮平台到归零的半径倍数（越大越没有圈边），出厂 1.2667
var (
	glowParticlesOn = envBool("WS_GLOWP", true)
	sunNearL        = envFloat("WS_SUN_L", sunNearLFrac)
	sunLiftRFrac    = envFloat("WS_SUN_LIFT_R", 3.0)
	sunLiftLv       = envFloat("WS_SUN_LIFT_LV", 0.0)
	sunLiftFadeFrac = envFloat("WS_SUN_LIFT_FADE", 1.2667)
	sunSkyScale     = envFloat("WS_SUN_SKY", 1.0)
)

func envBool(name string, def bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envFloat(name string, def float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

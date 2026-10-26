package ui

// R5 卡 6：ui/assets 模块迁移桥（纯别名，零行为）。
//
// ui/assets.go 全量迁入 ui/assets 包（package assets，导出名），effects.go 的
// 云贴图档表 + R3 白名单迁入 ui/assets/cloudtier.go。本文件只做两件事：
//  1. 旧标识符 → 新包导出符号的别名（ui 包内全部调用点逐字不动，
//     含 assetdir.go 与全部 _test.go）；
//  2. WS_ASSET_DIR 覆盖目录查询注入口注入（实现在 assetdir.go，
//     注入口先例 = ui/sky 的贴图查询口，见 ui/sky/sky.go:24 注）。
// 别名 = 同一函数体/同一数据，编译期钉死，不存在第二实现。

import (
	"github.com/DragonTTDream/WeatherScr/ui/assets"
)

// 旧 const 组（assets.go 374-381、491-499、516、524）。
const (
	spriteSupersample = assets.SpriteSupersample
	spriteNetDiv      = assets.SpriteNetDiv
	cloudNetScale     = assets.CloudNetScale
	fogNetScale       = assets.FogNetScale
	bandNetDiv        = assets.BandNetDiv
	detailIconSrcPx   = assets.DetailIconSrcPx
	detailGapPx       = assets.DetailGapPx
	detailRowMinH     = assets.DetailRowMinH
	windGlyphFrac     = assets.WindGlyphFrac
	DetailFontPx      = assets.DetailFontPx
	DetailIconPx      = assets.DetailIconPx
)

// 旧 var/func（函数别名指向 assets 包同一实现）。
var (
	loadEmbedded     = assets.LoadEmbedded
	toNRGBA          = assets.ToNRGBA
	readPNG          = assets.ReadPNG
	unpremultiply    = assets.Unpremultiply
	mul255Over       = assets.Mul255Over
	itoa             = assets.Itoa
	glowRaw          = assets.GlowRaw
	featherAlpha     = assets.FeatherAlpha
	upscaleCatmull   = assets.UpscaleCatmull
	detailRowHeight  = assets.DetailRowHeight
	windIconDrawPx   = assets.WindIconDrawPx
	loadSprite       = assets.LoadSprite
	loadSpriteSet    = assets.LoadSpriteSet
	spriteImg        = assets.SpriteImg
	embedHas         = assets.EmbedHas
	detailSprite     = assets.DetailSprite
	detailIconFiles  = assets.DetailIconFiles
	isMoonNew        = assets.IsMoonNew
	moonIllumination = assets.MoonIllumination
	// cloudIdxAllowed = R3 白名单本体（同一底层数组，非副本）。
	cloudIdxAllowed = assets.CloudIdxAllowed
	// embedFS = 内嵌口垫片：assetdir.go 的 embedFS.ReadFile("embed/"+name)
	// 调用点逐字不动（"embed/" 前缀剥离在 assets 侧完成）。
	embedFS = assets.PrefixedFS{}
)

// cloudForm = 云形态口径表类型（档表数据在 assets，宽度由 cloudFormOf 现算后填入）。
type cloudForm = assets.CloudForm

// 覆盖目录查询注入（出厂路径 = WS_ASSET_DIR 未设 ⇒ assetLookupPath 恒 false ⇒ 逐字等价）。
func init() {
	assets.OverrideLookup = assetLookupPath
}

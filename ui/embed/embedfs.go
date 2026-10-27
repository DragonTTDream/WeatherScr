// Package embed 内嵌 ui/embed 目录的全部贴图 PNG。
// （贴图装配迁入 ui/assets）：//go:embed 不能跨目录向上引用，
// 装配代码搬到 ui/assets 后需要新的内嵌口 ⇒ 在贴图所在目录原地加本桥文件。
// 纪律：ui/embed/*.png 文件位置与字节逐字不动（37 个文件原样），
// 本文件只新增内嵌口，不改任何找图逻辑；ui/assets 侧调用点的
// "embed/<name>" 路径字符串逐字保留，由 ui/assets 的适配口剥前缀后查本 FS。
package embed

import "embed"

// FS = ui/embed 目录的内嵌文件系统，键 = 裸文件名（"cloud1.png"）。
// 旧 ui 包内嵌口（embedFS，键 = "embed/cloud1.png"）的等价性由
// ui/assets 的 embedKey/ReadPNG/EmbedHas/PrefixedFS 适配口逐字保持。

//go:embed *.png
var FS embed.FS

// Package overlay 是叠加在天空与粒子之上的三层东西：雾团（含雪天的均匀灰幕）、雾霭梯度、
// 雷暴的局域闪光与闪电本体；消费者 = ui（ui/effects.go 在生成与绘制处调用本包）。
// 本包是 拆分 从 ui/effects.go **逐字迁出**的雾团/灰幕/闪光叠加函数群
// 公式、常量、随机数取用顺序、绘制顺序、透明度值一行没改。
// 对 ui 私有状态的依赖一律改成参数/回调注入（粒子字段值、贴图表、落位口、贴图查询口、探针计数指针），
// 判据 = ui/overlay/parity_test.go 的 TestOverlayParity（固定输入对拍，数字 = 迁移前旧实现打印的校验数）。
package overlay

// 边界：闪电的**时间线**（stepBolt / boltEnvelope / boltGapForCode
// 与 boltDur*/boltGap*/boltX*/boltY* 常量）、本次闪现用第几张贴图的选图（boltSpriteIndex）、
// 程序折线退路 genBolt、离线路径 SetFlash 的锚点补齐 = **留在 ui 的 Field**
// （它们 = 闪现状态机与贴图装配，不是叠加层本身）。
// TODO(未取证)：这些是否也归 ui/overlay，规格里没有取证条目，等 REA 补证据后再定。

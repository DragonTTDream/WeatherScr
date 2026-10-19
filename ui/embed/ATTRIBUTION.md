# 素材来源

`detail_umbrella.png` / `detail_humidity.png` / `detail_wind.png`
素材来自 SRPSS（MIT License, Copyright (c) 2026 Jayde Ver Elst / Basjohn），
逐字节复制自上游 `ui/assets/weather/source/presented/`（含 `detail/` 子目录）。

其余（`cloud1..9.png`、`fog1..4.png`、`glow.png`）是 Kenney 素材，CC0，
许可记录见 `../assets/LICENSE-kenney.txt`（`ui/assets.go` 顶部注释）。

上游的状况图标集（`cond_*.png`，含 `-night` 变体与 `haze`/`smoke`/`*-sleet` 备用项）
与 8 张月相图（`moon_new.png` 等，250×250）在素材期被参考过，**未采用**：
状况图标按布局判读撤出画面，月亮改由解析绘制，两组文件已不在本仓库，
运行时也没有任何代码路径加载它们。

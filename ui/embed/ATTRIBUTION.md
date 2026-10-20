# 素材来源（逐文件）

| 文件 | 来源 | 许可 | 本仓库的改动 |
| --- | --- | --- | --- |
| `cloud1..9.png` | OpenGameArt "Clouds with Transparency", WickedInsignia — https://opengameart.org/content/clouds-with-transparency | CC0 1.0 | 512 宽 → 448 宽，高等比（LANCZOS） |
| `fog1..4.png` | OpenGameArt "Thick Fog", LFA — https://opengameart.org/content/thick-fog | CC0 1.0 | 1024×512 → 896×448（LANCZOS），alpha 通道 ×0.60 |
| `moon.png` | NASA SVS "CGI Moon Kit"（LRO/LROC 数据，Ernie Wright USRA 制可视化）— https://svs.gsfc.nasa.gov/4720 | 公有领域 | 512×512 RGB → 384×384，转 RGBA、alpha 全 255 |
| `rain.png` | 本项目自绘 | MIT（随代码） | 256×256 → 192×192 |
| `glow.png` | Kenney "Particle Pack"（light_01）— https://kenney.nl/assets/particle-pack | CC0 1.0 | 无 |
| `snow1..3.png` | Kenney "Particle Pack"（circle_05）— https://kenney.nl/assets/particle-pack ，镜像 `shorepine/kenney` 分支 `main` 路径 `2d/Particle Pack/PNG (Transparent)/` | CC0 1.0 | 转 RGBA → 裁 alpha 包围盒 → LANCZOS 缩到 64×64 / 48×48 / 96×96（`bin/prepare_particles.py`） |
| `rain1..7.png` | Kenney "Particle Pack"（trace_01..07）— 同上 | CC0 1.0 | 转 RGBA → 裁 alpha 包围盒 → LANCZOS 等比缩到宽 48（高 110–176，按包围盒） |
| `sunbloom.png` | Kenney "Particle Pack"（light_01）— 同上 | CC0 1.0 | 转 RGBA → 裁 alpha 包围盒 → LANCZOS 缩到 256×256 |
| `bolt1..3.png` | Kenney "Particle Pack"（spark_01..03）— 同上 | CC0 1.0 | 转 RGBA → 裁 alpha 包围盒 → LANCZOS 缩到最长边 320 |
| `sunrays.png` | Kenney "Smoke Particles"（flash05）— https://kenney.nl/assets/smoke-particles | CC0 1.0 | 转 RGBA → LANCZOS 等比缩到宽 512（525×534 → 512×521，alpha 保留） |
| `sunglow.png` | Kenney "Light Masks"（circle_a）— https://kenney.nl/assets/light-masks | CC0 1.0 | 转 RGBA → LANCZOS 等比缩到宽 1024（512×512 → 1024×1024，alpha 保留） |
| `flashglow.png` | Kenney "Light Masks"（circle_a）— https://kenney.nl/assets/light-masks | CC0 1.0 | 转 RGBA → 512×512（源图原尺寸，LANCZOS 重采样一遍统一口径） |
| `detail_umbrella.png` `detail_humidity.png` `detail_wind.png` | 上游 SRPSS（MIT, Copyright (c) 2026 Jayde Ver Elst / Basjohn），逐字节复制自上游 `ui/assets/weather/source/presented/`（含 `detail/` 子目录） | MIT | 无 |

许可记录全文见 `../assets/LICENSE-ogazoo.txt`（OpenGameArt 两项 CC0 + NASA 公有领域）
与 `../assets/LICENSE-kenney.txt`（Kenney，含"云/雾已被替换"的变更记录）。

历史来源变更：`cloud1..9.png` 与 `fog1..4.png` 在早期版本里是 Kenney 素材
（Background Elements 的云、Particle Pack 的 smoke_01/02/04/05），
现已被上面两栏的 OpenGameArt 素材替换；`glow.png` 仍是 Kenney light_01。

上游的状况图标集（`cond_*.png`，含 `-night` 变体与 `haze`/`smoke`/`*-sleet` 备用项）
与 8 张月相图（`moon_new.png` 等，250×250）在素材期被参考过，**未采用**：
状况图标按布局判读撤出画面，月亮改由解析绘制 + 上面的 NASA 月面贴图，
两组文件已不在本仓库，运行时也没有任何代码路径加载它们。

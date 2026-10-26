package sky

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// 天空底色 + 星月 + 日照渐变的烘焙层（逐字迁自 ui/render.go:1004-2834）。
// 迁移纪律：公式、常量、颜色值、绘制顺序一字未改；唯一的文本变化 =
//   ① 顶层符号导出（首字母大写，供 ui 跨包调用）与缓存结构体字段导出；
//   ② 对 ui 私有贴图函数的依赖改为 assets 查询口调用（见 sky.go）。

// ── 静态背景烘焙缓存（性能优化）──────────────────────────
//
// 实测：每帧发 19 个 paint op（天空 1 + 地平线辉光 2 + 暗角 4 边×4 段 16），
// 15 fps ⇒ 285 次 draw call/s。`DBG=frame layout=0–1ms frame=8–12ms`
// ⇒ 贵的是提交+GPU（填充与 draw call），不是 op 构建。
//
// 天空与暗角只随 (天气类, 昼夜, 尺寸) 变，逐帧完全重复 ⇒ 烘焙成一张
// 纹理，每帧 1 次贴图。烘焙在 1/2 分辨率做（生成成本 1/4），
// 贴图时放大 2×（GPU 双线性采样，顺带把渐变的台阶抹平）。
//
// 逐像素复现 GPU 语义：`gradAlpha` = 从 (c,a1) 到 (c,a2) 的线性渐变，
// src-over 合成 ⇒ CPU 侧按同一插值与同一合成公式逐像素算。

const SkyCacheScale = 2 // 烘焙分辨率 = 屏幕 / 2（稳态口径，批次 A2 不许动）

// skyCacheBootScale 尺寸变化首帧的烘焙尺度（C3 第 2 条）：比稳态**粗一档**，
// 首帧只烘 1/16 面积（稳态 1/4），将"尺寸切换当帧全量烘焙"的那一笔重排拆开
// （实测同刻 SCR_SKYCACHE bake=99.8ms 落在 layout>1 s 的那一帧上）。
// 第二帧起回到 skyCacheScale 稳态尺度（见 skyCached 的升级重烘）。
const SkyCacheBootScale = SkyCacheScale * 2

// BakeScale 本次烘焙应使用的尺度（纯函数、数值确定，C3 钉死对象）：
//
//	尺寸变化后的首帧 ⇒ skyCacheBootScale（4，粗一档）
//	其余帧（含稳态、含启动期之后）⇒ skyCacheScale（2，稳态口径不变）
//
// 尺度 = 屏幕 ÷ 该值 = 烘焙边长分母；越大越粗、越便宜。
func BakeScale(firstFrameAfterSizeChange bool) int {
	if firstFrameAfterSizeChange {
		return SkyCacheBootScale
	}
	return SkyCacheScale
}

// ── 覆盖目录里的天体底图（预览工装，未启用时全部返回空 = 出厂路径不变）──
//
// 依据：素材预览工装规格 1.2（天空底图）、1.3（月亮）、1.4（太阳）。
// 生效条件逐字照规格：覆盖目录里有同名文件才走贴图，没有就走原有程序化画法。

// skySunsetWindowActive 当前时刻是否落在 [sunrise-30min, sunrise+30min] ∪
// [sunset-30min, sunset+30min]（规格 1.2 的日落优先窗口）。
//
// 日出/日落要取自现有 weather.Sample 字段：实测 weather/schema.go 的 Sample
// 全部字段（temperature/condition/location/humidity/precipitation_probability/
// windspeed/forecast/is_day/weather_code/cloud_cover/today_precip_prob_max/
// today_precip_mm/moon_phase/hourly_*/utc_offset_seconds/_cached_at）
// **没有任何日出日落字段** ⇒ 取不到 ⇒ 按规格：取不到就不启用 sunset 分支，
// 这里恒 false，sky_sunset.png 永远不参与选择。
//
// TODO(未取证)：日出日落的数据源（哪个字段、什么格式、时基怎么换算）未取证，
// 不自行发明取法。取证后本函数改为读 Frame.Now + weather.Sample。
func SkySunsetWindowActive() bool { return false }

// skyOverrideName 天空底图用哪张贴图（"" = 不用贴图，走渐变色填充）。
// 选择规则（取证 1.2）：is_day==1 → sky_day.png；is_day==0 → sky_night.png；
// 日落窗口内优先 sky_sunset.png（窗口判据见 skySunsetWindowActive）。
// 覆盖目录里没有、且没有内嵌同名素材 ⇒ ""（照旧渐变填充）。
// 现状：三张天空贴图都不在内嵌素材里（天空贴图落选，画面保持自绘）
// ⇒ 本函数的取值与"只看覆盖目录"时逐字一致。
func SkyOverrideName(isDay bool) string {
	if SkySunsetWindowActive() {
		if assets.Available("sky_sunset.png") {
			return "sky_sunset.png"
		}
	}
	n := "sky_night.png"
	if isDay {
		n = "sky_day.png"
	}
	if !assets.Available(n) {
		return ""
	}
	return n
}

// sunOverrideName 太阳盘用哪张贴图（"" = 走 bakeSunAnalytic 程序化画法）。
// 覆盖目录优先，其次内嵌 embed/sun.png；两者都没有 ⇒ ""。
// 现状：内嵌素材里没有 sun.png（方形太阳盘落选，太阳盘走程序化径向衰减画法）
// ⇒ 出厂路径恒 ""，太阳盘由 bakeSunAnalytic 画。
func SunOverrideName() string {
	if !assets.Available("sun.png") {
		return ""
	}
	return "sun.png"
}

// moonOverrideName 月亮亮面用哪张贴图（"" = 走 bakeMoonAt 程序化着色）。
// **内嵌 embed/moon.png 存在即走**（NASA 月面已落地）⇒ 出厂路径就是贴图；
// 覆盖目录里有同名文件时覆盖它（工装语义不变）。
func MoonOverrideName() string {
	if !assets.Available("moon.png") {
		return ""
	}
	return "moon.png"
}

// skyBakeTexKey 天空烘焙缓存键里用了哪张贴图这一项（取证 1.2：
// 键必须包含用了哪张贴图，避免昼夜/黄昏切换后仍用旧图）。
// 取值 = 贴图名 + **来源**（覆盖目录 / 内嵌），见 assetSourceTag。
// 没有任何贴图 = 空串。
func SkyBakeTexKey(isDay bool) string {
	s := assets.SourceTag(SkyOverrideName(isDay))
	if isDay {
		if n := SunOverrideName(); n != "" {
			s += "|sun=" + assets.SourceTag(n)
		}
	}
	return s
}

type SkyCacheKey struct {
	Cls   effect.Class
	IsDay bool
	W, H  int
	Moon  int    // 量化后的月相（moonPhaseKey），需求 4
	Tex   string // 覆盖目录里用了哪张贴图（天空底图 + 太阳盘）；空 = 出厂程序化
	// cloud = 量化后的云量（5 个百分点一档）。太阳可见度按云量连续走，
	// 键里没有它的话，云量变了还沿用旧图 ⇒ 太阳亮度停在旧云量上。
	// 量化到 5% 的理由：云量只在抓取（30 分钟 TTL）之间变，一档 5% 的
	// 亮度差 = 0.04 的 dim 差，肉眼读不出；不量化会让每次抓取都重烘一张。
	Cloud int
}

type SkyCacheEntry struct {
	Img *image.NRGBA
	Ops paint.ImageOp // 按 image 指针缓存纹理（gioui 以 image 指针为纹理键）
}

type SkyCacheType struct {
	Key SkyCacheKey
	Ent *SkyCacheEntry
	// coarse = 当前缓存条目是"尺寸变化首帧"的粗尺度烘焙 ⇒
	// 下一次同键命中时按稳态尺度升级重烘一次（C3 第 2 条：第二帧起回稳态）。
	Coarse bool
}

var SkyCache SkyCacheType

// SkyCacheStats 烘焙缓存命中统计（性能定位用）。
type SkyCacheStats struct {
	Hits, Builds int
	LastKey      string
	LastMs       float64
}

var SkyStats SkyCacheStats

// SkyCacheGet 读出统计快照。
func SkyCacheGet() SkyCacheStats { return SkyStats }

// moonPhaseKey 把月相量化进烘焙缓存键（需求 4）：
// 键里放**量化后的相位** int(phase*800)（1/800 相位精度），避免每帧因浮点抖动重烘。
// nil（取不到月相）→ -1，与"相位 0 = 新月"区分开（缓存键的口径不动：
// 两者画面现在一致 = 都不画月亮，键里仍要分出"没有数据"与"数据说它是新月"）。
func MoonPhaseKey(phase *float64) int {
	if phase == nil {
		return -1
	}
	p := *phase
	p = p - math.Floor(p)
	if p < 0 {
		p += 1
	}
	return int(p * 800)
}

// skyCached 返回烘焙好的背景纹理与它的 ImageOp；尺寸/类别/月相/云量变化时重建。
func SkyCached(size image.Point, cls effect.Class, isDay bool, phase *float64, cloudPct int) *SkyCacheEntry {
	k := SkyCacheKey{
		Cls: cls, IsDay: isDay, W: size.X, H: size.Y,
		Moon: MoonPhaseKey(phase), Tex: SkyBakeTexKey(isDay),
		Cloud: cloudPct / 5,
	}
	if SkyCache.Key == k && SkyCache.Ent != nil && !SkyCache.Coarse {
		SkyStats.Hits++
		return SkyCache.Ent
	}
	// 尺寸变化判据 = 键里的 w/h 与上一张不同（且上一张存在）：
	// 只有这种情况的首帧用粗一档尺度，第二帧起（同键命中）升级回稳态尺度。
	sizeChanged := SkyCache.Ent != nil && (SkyCache.Key.W != size.X || SkyCache.Key.H != size.Y)
	scale := BakeScale(sizeChanged)
	t0 := time.Now()
	img := RenderSkyCPU(size.X/scale, size.Y/scale, cls, isDay, phase, cloudPct)
	ent := &SkyCacheEntry{Img: img, Ops: paint.NewImageOp(img)}
	SkyCache.Key = k
	SkyCache.Ent = ent
	SkyCache.Coarse = scale != SkyCacheScale
	SkyStats.Builds++
	SkyStats.LastKey = fmt.Sprintf("%s/%v/%dx%d/moon=%d/tex=%s/cloud=%d",
		cls, isDay, size.X, size.Y, k.Moon, k.Tex, k.Cloud)
	SkyStats.LastMs = float64(time.Since(t0).Microseconds()) / 1000.0
	return ent
}

// renderSkyCPU 在 CPU 上逐像素画出与 skyGradient + skyDepth 相同的画面。
// cloudPct = 云量（0–100），决定白天太阳的可见度（见 bakeCelestial）。
func RenderSkyCPU(w, h int, cls effect.Class, isDay bool, phase *float64, cloudPct int) *image.NRGBA {
	dst := SkyBaseCPU(w, h, cls, isDay)

	// 4 月亮 / 太阳：静止天体，烘进纹理 ⇒ 每帧零成本。
	//
	// 实测依据：`drawMoon` 每帧画 64 个圆盘，最外环半径 = rc×(1+3.4) = 4.4rc，
	// 单环 (2·4.4·48.6)² ≈ 183 Kpx ⇒ 64 环 ≈ 11.7 Mpx/帧 = **5.7 个整屏当量**
	// （实测口径 ≈ 25% CPU）。64 环叠加实现的就是
	// `A(frac) = A0·exp(−decay·frac)` 这条指数曲线本身，
	// 故 CPU 侧直接按该解析式逐像素积分，与 GPU 环叠加等价（同一 A0/decay/spread）。
	// 唯一取舍：月亮 ±4% 的"呼吸"脉动不再逐帧动（4% 幅度肉眼不可读）。
	// **所有类别都烘天体与星星**
	// "星星只是在云层后面不是多云就没星星了，月亮也一样，多云只是云飘在月亮上面"。
	// 原实现 `if cls == effect.Clear` ⇒ 云量一过 30% 类别从 Clear 变 Cloudy，
	// 星星与月亮整片消失，屏幕从"深夜蓝+星+月"突变成"灰云盖屏"
	// （低性能 x86 小主机实测：类别变化前后天空小窗 14.1 → 46~58）。
	// 烘进纹理天然在云的下层：云在粒子层后画 ⇒ 云飘在月亮上面。
	BakeCelestial(dst, w, h, cls, isDay, phase, cloudPct)
	if !isDay {
		BakeStars(dst, w, h)
	}
	return dst
}

// skyBaseCPU 天空底图 = 上面 1/2/3 步（贴图或渐变 + 地平线辉光带 + 四角暗角），
// **不含天体与星星**。
//
// 拆出来的原因：太阳盘的判据要量"同位置的天空基色"（盘内像素 = 比同位置基色
// 亮 ≥24 级的连续核），探针必须拿到**没有烘天体的那一张**，
// 而不是自己复制一遍渐变/暗角公式（复制出来的就不是屏幕上真实存在的东西）。
func SkyBaseCPU(w, h int, cls effect.Class, isDay bool) *image.NRGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	base := SkyColor(cls, isDay)
	top := color.NRGBA{
		R: uint8(float64(base.R) * 0.45),
		G: uint8(float64(base.G) * 0.45),
		B: uint8(float64(base.B) * 0.55),
		A: 0xff,
	}
	horizon := color.NRGBA{
		R: clamp8(float64(base.R)*1.35 + 18),
		G: clamp8(float64(base.G)*1.35 + 18),
		B: clamp8(float64(base.B)*1.25 + 14),
		A: 0xff,
	}
	glow := color.NRGBA{
		R: clamp8(float64(base.R)*1.25 + 14),
		G: clamp8(float64(base.G)*1.25 + 14),
		B: clamp8(float64(base.B)*1.25 + 14),
		A: 0xff,
	}
	fw, fh := float64(w), float64(h)

	// 1 天空：top → horizon 竖向线性
	//
	// 覆盖目录里有同名底图时，这一整步由贴图取代（规格 1.2：贴图**取代**渐变色填充）。
	// 等比缩放（cover，不变形）到现有天空缓存尺寸，skyCacheScale 口径不变；
	// 第 2/3/4 步（地平线辉光带、四角暗角、天体与星星）照旧画在贴图之上，
	// 这些是已定板口径，工装不动它们。
	// 未启用覆盖目录时 skyOverrideName 返回空串 ⇒ 与改动前逐字节一致。
	skyTex := ""
	if n := SkyOverrideName(isDay); n != "" {
		if img, _, _, ok := assets.ImageAny(n); ok {
			skyTex = n
			assets.BlitCoverReplace(dst, img)
		}
	}
	if skyTex == "" {
		for y := 0; y < h; y++ {
			t := float64(y) / math.Max(1, fh-1)
			r := LerpF(float64(top.R), float64(horizon.R), t)
			g := LerpF(float64(top.G), float64(horizon.G), t)
			b := LerpF(float64(top.B), float64(horizon.B), t)
			for x := 0; x < w; x++ {
				dst.SetNRGBA(x, y, color.NRGBA{R: clamp8(r), G: clamp8(g), B: clamp8(b), A: 0xff})
			}
		}
	}

	// 2 地平线辉光带：底部 22%，alpha 0 → 30 → 0
	y0, ym, y1 := int(fh*0.78), int(fh*0.89), h
	BlendAlphaV(dst, image.Rect(0, y0, w, ym), glow, 0, 30)
	BlendAlphaV(dst, image.Rect(0, ym, w, y1), glow, 30, 0)

	// 3 四角暗角：几何衰减分段（与 GPU 版同一张表）
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}
	vignetteAlpha := []uint8{38, 20, 9, 3, 0}
	vignetteFrac := []float32{0, 0.35, 0.65, 0.85, 1.0}
	hv, hw := fh*0.16, fw*0.12
	for i := 0; i+1 < len(vignetteAlpha); i++ {
		f0, f1 := float64(vignetteFrac[i]), float64(vignetteFrac[i+1])
		a0, a1 := vignetteAlpha[i], vignetteAlpha[i+1]
		BlendAlphaV(dst, image.Rect(0, int(hv*f0), w, int(hv*f1)), black, a0, a1)
		BlendAlphaV(dst, image.Rect(0, h-int(hv*f1), w, h-int(hv*f0)), black, a1, a0)
		BlendAlphaH(dst, image.Rect(int(hw*f0), 0, int(hw*f1), h), black, a0, a1)
		BlendAlphaH(dst, image.Rect(w-int(hw*f1), 0, w-int(hw*f0), h), black, a1, a0)
	}
	return dst
}

// bakeCelestial 把天体画进烘焙图：夜 = 月亮（有月相走 bakeMoonAt 的月相画法，
// 无月相**不画月亮**，规格 2.3；昼 = 单一连续剖面的铺光，见 bakeSunAnalytic）。
//
// 白天铺光 = **云量连续变暗**（sunDim），盘的可见性 = **天气类 + 云量分级**
// （sunDiscVisibility，盘可见度的唯一决策点）：
//
//	白天（isDay）任何档都画铺光：全天空抬升项与宽光贴图照旧按 dim 变暗
//	盘本体 / 小亮核 / 近场项这一组乘盘可见度 ⇒ 可见度 0 时盘整组没了
//	clear / partly-cloudy / cloudy：云量 ≤50 满可见、50→75 线性淡出、>75 为 0
//	overcast / fog / dust：恒 0（全阴与雾的直射≈0，日照不计）
//	rain / snow / thunder：云量 ≤50 = 太阳雨 ⇒ 1.0；>50 ⇒ 0
//	云量 100%  → dim 落在下限 0.20 ⇒ 灰蒙宽光仍在，不许全黑
//	夜间       → 不画太阳（月亮走独立层）
//
// 依据（日照与天空状况的公开口径）：直接太阳辐照 ≥120 W/m² 才计为日照
// （ECMWF Parameter Database 189、WMO Codes Registry 270、Prescott/Solar Energy 对比文）
// ⇒ 直射被云挡完时盘不可见；3–5 oktas = partly cloudy = partly sunny（晴多云时盘可见，
// NWS/oktas，UW-Madison The Weather Guys 2024-01-08）；太阳雨 = 有降水而总云量低时太阳可见
// （Wikipedia Sunshower）。
//
// 云量是连续量（实测码 1 ≈ 27–41%、码 2 ≈ 52–79%、码 3 = 100%）⇒ 亮度按云量连续走；
// "盘在不在"是分级量 ⇒ 按天气类 + 云量分级。两件事分开，不互相稀释。
//
// 云盖日月的绘制顺序不动：天体烘在天空纹理里，云带与粒子云都在天空之后画
// （render.go 的 Layout：skyBlit → moonBlit → Field.Draw → 面板 → 文字）。
func BakeCelestial(dst *image.NRGBA, w, h int, cls effect.Class, isDay bool, phase *float64, cloudPct int) {
	if !isDay {
		// 夜间月亮**不再烘进天空纹理**：天空是半分辨率烘焙（skyCacheScale=2），
		// 月亮在里面只有 32 px 半径，放大 2 倍后外缘出现整像素台阶
		// （离屏图 shot66 量到相邻行最大跳变 7 px），且云（α 0.28–0.45）
		// 压不住 248 级月面 ⇒ 月亮读成在云层上面。
		// 月亮改由独立层 moonBlit 以全分辨率画在云之前（见该函数注释）。
		return
	}
	// 白天：**任何档都画铺光**（亮度由云量给的 dim 连续决定），
	// 盘的可见度由 sunDiscVisibility 一处决定（盘本体/小亮核/近场项都乘它）。
	// 可见度 0 = 不画盘（走 disc=false 分支：只画宽光与全天空抬升）。
	// 白天也不画月亮：月亮走独立层（moonBlit 在 isDay 时直接 return），
	// 旧口径靠 `if isDay { …; return }` 挡掉 bakeMoon，这一条必须保住。
	dim := SunDimOf(cloudPct)
	vis := SunDiscVisibility(cls, cloudPct)
	BakeSunAnalytic(dst, w, h, vis > 0, dim, vis)
	// 白天不画月亮（月亮走独立层，见上面 isDay==false 分支的说明）。
	// 旧写法在这里跟了一条 bakeMoon 调用：语法上可达、实际永不执行，
	// 改成显式"画完就返回"后 vet 的 unreachable 检查才不红；
	// bakeMoon（整屏口径的月亮烘焙）保留给离屏判读路径。
	return
}

// bakeMoon 月亮烘焙：**本实现自身的解析画法**
// "去掉虚线只画亮面，需要光晕，原来那个就挺好的，让参考其显示方式让其不糊不粗糙，
//
//	不是让直接换成上游的了！"。
//
// 四条口径：
//  1. **只画亮面**，暗面不留任何轮廓 ⇒ 上游素材那圈 8 段虚线描边去掉
// （量 moon-waxing-crescent.png：半径带 1775 px 其中半透明 269 px = 那圈虚线）。
// 2. **边缘锐利用上游的 limb 口径**（neon_celestial.py:309-311
// `limb = smoothstep(0.015, 0.10, facing)`，`facing = √(1−(r/R)²)`
// ⇒ 过渡带只占半径 0.49%（R=65 px 时 0.32 px），不是原先 10% 半径的糊边。
// 借的是**显示方式**，不是换成上游素材。
// 3. **保留光晕**（需求：要光晕，原来那个就挺好）：指数衰减 + cos² 窗归一到 0，
// 消除外边界硬台阶（实测无窗时 alpha 6→0 的台阶就是可见边界）。
// 4. **放大**：rc 0.045w → 0.060w（需求：月亮要更大；
// 上游素材口径下有效月面只有画布的 56%，所以素材版看着小。
//
// 月相几何（θ=2πφ，a=|cosθ| 归一化半轴，E=(X/a)²+Y²<1，X/Y 为对圆心归一化坐标）：
//
//	盈（φ<0.5，亮面在右）：cosθ>0 ⇒ 亮 = 右半 ∧ ¬E（眉月）
//	                        cosθ<0 ⇒ 亮 = 右半 ∨ E（凸月）
//	                        cosθ=0 ⇒ 亮 = 右半（上弦）
//	亏（φ>0.5，亮面在左）：X 取反，同一套。
//
// 自检：φ=0 全暗 · 0.125 右眉 · 0.25 右半 · 0.375 右凸 · 0.5 全亮 · 0.75 左半 · 0.875 左眉。
// 照度 k=(1−cosθ)/2；k<0.02 视为新月，不画月也不画光晕。
// 光晕强度随 k 走（0.35+0.65k）：眉月的光晕比满月弱。
// ---- 月亮独立层（全分辨率，画在云之前）----

type MoonLayerKey struct {
	W, H int
	Moon int
	Tex  string // 覆盖目录里用了哪张月亮贴图；空 = 出厂程序化着色
}

type MoonLayerEntry struct {
	Img  *image.NRGBA
	Ops  paint.ImageOp
	Off  image.Point // 子矩形在整屏里的左上角
	Size image.Point
}

var (
	MoonLayerCache struct {
		Key MoonLayerKey
		Ent *MoonLayerEntry
	}
	MoonLayerStats struct{ Builds, Hits int }
)

// moonLayerGet 以**整屏分辨率**把月亮烘进它的外接矩形（含光晕），缓存一份。
// 尺寸 = 2×maxR = 2×rc×(1+haloSpread) = 0.384w（1080 屏 ≈ 415 px）
// ⇒ 内存 ≈ 415×415×4 = 0.7 MB，远小于把整屏烘成全分辨率的 8.3 MB。
func MoonLayerGet(w, h int, phase *float64) *MoonLayerEntry {
	k := MoonLayerKey{W: w, H: h, Moon: MoonPhaseKey(phase), Tex: assets.SourceTag(MoonOverrideName())}
	if e := MoonLayerCache.Ent; e != nil && MoonLayerCache.Key == k {
		MoonLayerStats.Hits++
		return e
	}
	rc := MoonRcFrac * float64(w)
	maxR := int(math.Ceil(rc*(1.0+MoonHaloSpread))) + 1
	cx, cy := int(0.72*float64(w)), int(0.095*float64(h))
	ox, oy := cx-maxR, cy-maxR
	bw, bh := 2*maxR, 2*maxR
	// 夹进屏内
	if ox < 0 {
		bw += ox
		ox = 0
	}
	if oy < 0 {
		bh += oy
		oy = 0
	}
	if bw <= 0 || bh <= 0 {
		return nil
	}
	if ox+bw > w {
		bw = w - ox
	}
	if oy+bh > h {
		bh = h - oy
	}
	if bw <= 0 || bh <= 0 {
		return nil
	}
	dst := image.NewNRGBA(image.Rect(0, 0, bw, bh))
	BakeMoonAt(dst, bw, bh, ox, oy, w, h, phase)
	e := &MoonLayerEntry{Img: dst, Ops: paint.NewImageOp(dst), Off: image.Point{X: ox, Y: oy}, Size: image.Point{X: bw, Y: bh}}
	MoonLayerCache.Key = k
	MoonLayerCache.Ent = e
	MoonLayerStats.Builds++
	return e
}

// moonBlit 把月亮层贴到绝对屏幕位置：绘制顺序 天空 → **月亮** → 云/粒子 → 面板 → 文字。
// 云在粒子层后画 ⇒ 云真正飘在月亮上面（多云时云飘在月亮上面）。
// 填充率成本 = 415×415 / (1080×1920) = 0.166 整屏 ≈ 0.7% CPU @15 fps。
func MoonBlit(gtx layout.Context, size image.Point, isDay bool, phase *float64) {
	if isDay || size.X <= 0 || size.Y <= 0 {
		return
	}
	e := MoonLayerGet(size.X, size.Y, phase)
	if e == nil || e.Size.X <= 0 || e.Size.Y <= 0 {
		return
	}
	cl := clip.Rect(image.Rect(e.Off.X, e.Off.Y, e.Off.X+e.Size.X, e.Off.Y+e.Size.Y)).Push(gtx.Ops)
	// **变换必须带平移**：图像画在"当前变换原点"上，clip.Rect 只限制绘制区域、
	// 不移动图像。第一版用恒等变换 ⇒ 月亮被贴到窗口左上角，与裁剪框 (568,40)
	// 不相交 ⇒ 整层一个像素都没画出来（离屏探针 TestMoonBlitProbe 实测月核区亮像素 = 0）。
	// 1:1 不缩放 ⇒ 只平移 (off.X, off.Y)。skyBlit 用恒等+缩放是因为它铺满整屏、原点就是 (0,0)。
	tr := op.Affine(f32.NewAffine2D(1, 0, float32(e.Off.X), 0, 1, float32(e.Off.Y))).Push(gtx.Ops)
	e.Ops.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
}

// 月亮几何（bakeMoonAt 与 moonLayerGet 共用，两处必须一致）
const (
	MoonRcFrac     = 0.060 // 月核半径 = 0.060 × 屏宽（需求：月亮更大 → 0.045→0.060）
	MoonHaloSpread = 2.2   // 光晕外沿 = rc×(1+2.2) = 3.2rc
)

// glowTab 指数衰减查表：dd ∈ [0, haloSpread（归一化半径）→ exp(−haloDecay·dd)。
// 辉光叠加的内层循环用它，避免每源一次 math.Exp（12 万像素 × 200 源）。
var GlowTab = func() [GlowTabN]float64 {
	var t [GlowTabN]float64
	for i := 0; i < GlowTabN; i++ {
		dd := float64(i) * 2.2 / GlowTabN // haloSpread
		t[i] = math.Exp(-2.6 * dd)        // haloDecay
	}
	return t
}()

const GlowTabN = 256

// bakeMoon 整屏口径的月亮烘焙（dst 覆盖整屏时使用）。
func BakeMoon(dst *image.NRGBA, w, h int, phase *float64) {
	BakeMoonAt(dst, w, h, 0, 0, w, h, phase)
}

// bakeMoonAt 把月亮画进 dst 的一块**子矩形**：dst 尺寸 boxW×boxH，
// 其在整屏里的左上角 = (ox, oy)，月心/半径按**整屏** w×h 计算。
// 用途 = 月亮独立层（全分辨率，画在云之前）：
// 天空纹理是半分辨率烘焙（skyCacheScale=2），月亮烘在里面只有 32 px 半径，
// 放大 2 倍后外缘出现整像素台阶（低性能 x86 小主机离屏图 shot66 量到相邻行最大跳变 7 px）；
// 且云盖在半分辨率月亮上只有 0.28–0.45 不透明度，压不住 248 级的月面
// ⇒ 月亮读成在云层上面。
// 拆成独立层后：全分辨率 ⇒ 边缘锐利；绘制顺序 天空→月亮→云 ⇒ 云飘在月亮上面。
func BakeMoonAt(dst *image.NRGBA, boxW, boxH, ox, oy int, w, h int, phase *float64) {
	// 取不到月相 ⇒ **不画月亮**（规格 D5/D7 第 2.3 条）。
	// 旧口径是 `p := 0.5`（取不到就当满月）⇒ 相位缺失时画出一轮满月，
	// 与"相位 0 = 新月不画"是两条路。现与新月一致：整层不落一笔。
	// 月相几何、光晕算法、盘面尺寸口径全部不动，只改这一条兜底。
	if phase == nil {
		return
	}
	p := *phase - math.Floor(*phase)
	if p < 0 {
		p += 1
	}
	theta := 2.0 * math.Pi * p
	cosT := math.Cos(theta)
	k := (1.0 - cosT) / 2.0
	if k < 0.02 {
		return // 新月
	}
	// 月心按整屏算，再减去子矩形左上角 ⇒ 子矩形内的坐标
	cx, cy := 0.72*float64(w)-float64(ox), 0.095*float64(h)-float64(oy)
	rc := MoonRcFrac * float64(w)
	a := math.Abs(cosT)
	waxing := p < 0.5

	core := color.NRGBA{R: 246, G: 244, B: 230, A: 0xff}
	halo := color.NRGBA{R: 214, G: 226, B: 238, A: 0xff}
	// 亮面贴图（预览工装，规格 1.3）：覆盖目录里有 moon.png 时，**亮面**的
	// 表面细节取自贴图；暗面照旧透明（ac 恒 0 的像素只走光晕，与改动前同）。
	// 不许动的口径全部保留：月相几何 θ=2πφ、half、inE、limb、
	// 光晕 = 亮面泛光（bloom of lit region，glowMul = 0.35+0.65k）、
	// 盘面尺寸 moonRcFrac = 0.060 屏宽、不画虚线。
	// 映射：盘面归一化坐标 (X,Y)∈[-1,1] → 贴图源矩形（盘面外接方框对齐贴图外接方框）。
	// 贴图自身 alpha=0 的像素回落到程序化 core 色（贴图在该处没信息，不发明新色）。
	// 未启用覆盖目录 ⇒ moonTex = nil ⇒ 逐字节走改动前的程序化着色。
	var moonTex *image.NRGBA
	var mtw, mth float64
	if name := MoonOverrideName(); name != "" {
		if img, _, sz, ok := assets.ImageAny(name); ok && sz.X > 0 && sz.Y > 0 {
			moonTex = img
			mtw = float64(sz.X)
			mth = float64(sz.Y)
		}
	}
	const (
		haloA0      = 95.0 // 光晕峰值 alpha（原 120 偏脏，按 95 收一档）
		haloDecay   = 2.6
		haloSpread  = MoonHaloSpread // 光晕外沿 = rc×(1+2.2) = 3.2rc
		haloWin     = 0.7            // cos² 窗起点（先前口径：边界值 0、边界斜率≈0）
		halfFeather = 0.006          // 半侧边界羽化（归一化）：rc=65 px 时 0.4 px，亚像素防锯齿
		termFeather = 0.012          // 晨昏线（椭圆）羽化
	)
	glowMul := 0.35 + 0.65*k

	// litAt 归一化坐标 (X,Y) 处的亮面占比（月相几何，见函数头注释）。
	litAt := func(X, Y float64) float64 {
		sX := X
		if !waxing {
			sX = -X
		}
		half := smoothstep(-halfFeather, halfFeather, sX)
		if a < 1e-6 {
			return half
		}
		e := (sX*sX)/(a*a) + Y*Y
		// 注意：ui/effects.go:352 的 smoothstep 在 b<=a 时是**硬台阶**（x<a 返 0 否则 1），
		// 不是递减坡。第一版这里写的是 smoothstep(1+ε, 1−ε, e) ⇒ inE 恒 0
		// ⇒ 所有相位退化成"半月亮"（满月 0.5 也只剩右半，低性能 x86 小主机出图抓到）。
		// 递减必须写成"递增坡取反"。
		inE := 1.0 - smoothstep(1.0-termFeather, 1.0+termFeather, e)
		if cosT > 0 {
			return half * (1.0 - inE)
		}
		return half + (1.0-half)*inE
	}

	// 亮面点源网格（归一化坐标，16×16，步长 = 1/8 半径）：辉光的来源 = **当前可见的亮面**。
	type gsrc struct{ x, y, w float64 }
	const gN = 16
	srcs := make([]gsrc, 0, gN*gN)
	full := make([]gsrc, 0, gN*gN)
	for i := 0; i < gN; i++ {
		for j := 0; j < gN; j++ {
			Xc := -1.0 + 2.0*(float64(i)+0.5)/gN
			Yc := -1.0 + 2.0*(float64(j)+0.5)/gN
			rr := Xc*Xc + Yc*Yc
			if rr > 1.0 {
				continue
			}
			full = append(full, gsrc{Xc, Yc, 1.0})
			l := litAt(Xc, Yc)
			if l <= 0.01 {
				continue
			}
			facing := math.Sqrt(math.Max(0, 1.0-rr))
			srcs = append(srcs, gsrc{Xc, Yc, l * smoothstep(0.015, 0.10, facing) * (1.0 - 0.12*rr)})
		}
	}
	// 归一化：满月（整盘全亮）在月缘处的辉光和 = 1 ⇒ 各相位相对满月按比例走
	peak := 0.0
	for a := 0; a < 8; a++ {
		th := math.Pi * float64(a) / 4.0
		ux, uy := math.Cos(th)*1.05, math.Sin(th)*1.05
		sum := 0.0
		for _, s := range full {
			dx, dy := ux-s.x, uy-s.y
			dd := math.Sqrt(dx*dx + dy*dy)
			if dd >= haloSpread {
				continue
			}
			sum += GlowTab[int(dd*GlowTabN/haloSpread)]
		}
		if sum > peak {
			peak = sum
		}
	}
	glowNorm := 1.0
	if peak > 0 {
		glowNorm = 1.0 / peak
	}

	maxR := rc * (1.0 + haloSpread)
	// **必须钳到 0**：外扩 -1 在月亮贴顶/贴左时把起点变成 -1，
	// `dst.PixOffset(x, -1)` 直接负索引 panic
	// （有一版一启动就崩：窗口 810x1389、月亮在 0.095h、
	// oy 钳位后 cy-maxR ≤ 0 ⇒ `index out of range [-704]`，
	// 栈 bakeMoonAt render.go:1399 ← moonLayerGet ← moonBlit。
	x0 := int(math.Max(0, cx-maxR)) - 1
	y0 := int(math.Max(0, cy-maxR)) - 1
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	x1 := int(math.Min(float64(boxW), cx+maxR)) + 1
	y1 := int(math.Min(float64(boxH), cy+maxR)) + 1
	if x1 > boxW {
		x1 = boxW
	}
	if y1 > boxH {
		y1 = boxH
	}

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			X := (float64(x) - cx) / rc
			Y := (float64(y) - cy) / rc
			d := math.Sqrt(X*X + Y*Y)
			// 辉光场：对 spread 内的每个亮面源求 Σ w·exp(−decay·dist/rc)。
			// **盘内也铺**
			// 需求：用光晕把暗面补上 ⇒ 暗面被辉光填上，
			// 不再是被光晕围出来的发光的坑。亮面画在辉光之上。
			acc := 0.0
			for _, s := range srcs {
				dx, dy := X-s.x, Y-s.y
				dd := math.Sqrt(dx*dx + dy*dy)
				if dd >= haloSpread {
					continue
				}
				acc += s.w * GlowTab[int(dd*GlowTabN/haloSpread)]
			}
			g := haloA0 * acc * glowNorm * glowMul
			if d > 1.0 {
				// 外沿 cos² 窗归一到 0，消除硬边界
				frac := (d - 1.0) / haloSpread
				if frac >= 1.0 {
					continue
				}
				if frac > haloWin {
					u := (frac - haloWin) / (1.0 - haloWin)
					g *= math.Cos(u*math.Pi/2) * math.Cos(u*math.Pi/2)
				}
			}
			ag := 0.0
			if g > 0.4 {
				ag = g / 255.0
				if ag > 1 {
					ag = 1
				}
			}
			ac := 0.0
			if d <= 1.0 {
				// 月面：只画亮面，画在辉光之上
				facing := math.Sqrt(math.Max(0, 1.0-d*d))
				limb := smoothstep(0.015, 0.10, facing) // 上游口径：锐边
				shade := 1.0 - 0.12*(d*d)               // 临边昏暗 12%（实测上游素材降幅）
				ac = limb * litAt(X, Y) * shade
				if ac > 1 {
					ac = 1
				}
				if ac < 0.004 {
					ac = 0
				}
			}
			// **必须直接写非预乘 NRGBA**：月亮层是透明底，blendPix 走预乘混合后
			// 把预乘 RGB 当非预乘存进 NRGBA ⇒ α 小的像素颜色被存成近黑
			// ⇒ 整盘显成黑椭圆（离屏放大图实测）。烘进不透明天空时该错被底遮住，
			// 拆成独立透明层后暴露。
			a := ag + ac*(1.0-ag)
			if a <= 0.002 {
				continue
			}
			wg, wc := ag, ac*(1.0-ag)
			// 亮面色：无贴图 = 程序化 core（改动前口径）；有贴图 = core 与贴图像素
			// 按贴图自身 alpha 混合（贴图缺信息处自动回落 core）。
			cr, cg, cb := float64(core.R), float64(core.G), float64(core.B)
			if moonTex != nil && ac > 0 {
				c := assets.SampleBilinear(moonTex, (X+1.0)*0.5*mtw, (Y+1.0)*0.5*mth)
				ta := float64(c.A) / 255.0
				cr += (float64(c.R) - cr) * ta
				cg += (float64(c.G) - cg) * ta
				cb += (float64(c.B) - cb) * ta
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = clamp8((wg*float64(halo.R) + wc*cr) / a)
			dst.Pix[i+1] = clamp8((wg*float64(halo.G) + wc*cg) / a)
			dst.Pix[i+2] = clamp8((wg*float64(halo.B) + wc*cb) / a)
			dst.Pix[i+3] = uint8(a * 255.0)
		}
	}
}

// bakeStars 把星星烘进天空纹理
// "星星只是在云层后面不是多云就没星星了，月亮也一样，多云只是云飘在月亮上面"。
//
// 烘进纹理 ⇒ ① 每帧零成本（星星原本是 Clear 夜间的 110 个粒子，别的类别一个没有）
// ② 天然在云的**下层**（云在粒子层绘制，后画在上）。
// 确定性种子：同一尺寸/类别烘出的星位逐帧一致，不闪跳；闪烁仍由 Clear 的粒子星承担。
func BakeStars(dst *image.NRGBA, w, h int) {
	const n = 150
	rng := rand.New(rand.NewSource(20261016))
	for i := 0; i < n; i++ {
		x := int(rng.Float64() * float64(w))
		y := int(rng.Float64() * float64(h) * 0.92)
		b := 0.18 + 0.62*rng.Float64()*rng.Float64()
		sz := 1
		if rng.Float64() < 0.12 {
			sz = 2
		}
		c := color.NRGBA{R: 244, G: 246, B: 252, A: 0xff}
		for dy := 0; dy < sz; dy++ {
			for dx := 0; dx < sz; dx++ {
				px, py := x+dx, y+dy
				if px >= w || py >= h {
					continue
				}
				BlendPix(dst, px, py, c, b)
			}
		}
	}
}

// bakeMoonCrisp 月亮：硬边圆盘 + 临边昏暗，**无外发光、无月海纹理**。
//
// 参照 = 上游 GL 着色器 upstream/rendering/gl_programs/neon_celestial.py:309-311：
//
//	float facing = max(dot(normal, -ray), 0.0);
//	float limb   = smoothstep(0.015, 0.10, facing);
//	...
//	return vec4(moon, clamp(uMoonColor.a,0,1) * limb);   // 球外直接 vec4(0.0)
//
// 两条实测结论（2026-10-16）：
// 1. **边缘**：球体投影 facing = √(1−(r/R)²)，代入上游 smoothstep 参数 ⇒
// facing=0.10 → r/R=0.99499，facing=0.015 → r/R=0.99989
// ⇒ 过渡带只占**半径的 0.49%**（R=48.6 px 时 0.24 px，即亚像素抗锯齿）。
// 直接量上游素材 ui/assets/weather/source/moon-full.png 也印证：
// 沿水平剖面 r=68 处 α=255、r=72 处 α=0，**0..255 之间的过渡带 = 0 px**。
// 原先的 `feather = 0.10`（半径的 10%）**比上游宽 20 倍** ⇒ 就是"糊"。
// 2. **无外发光**：上游月亮的 alpha 只由 limb 决定，球外返回 vec4(0.0)，
// 素材里也没有任何光晕像素。原先挂着 haloA0=120 / spread=3.4 的光晕，
// 铺到 4.4 倍核半径 ⇒ 第二处"糊"。
//
// 临边昏暗照素材实测：填充色中心 (130,193,218) → 外缘 (114,185,213)，
// 即中心亮、外缘暗，降幅 12%（着色器里对应 earthshine/basinMoonSurfaceBrightness
// 那组项的净效果，这里按实测值做线性近似，不复制着色器的月海反照率。
//
// **本批起不再是 phase==nil 的回落画法**：nil 现在与新月一致 = 不画月亮（规格 2.3），
// 本函数没有调用者，保留不删（登记见 out/notes-sunmoon.md）。
func BakeMoonCrisp(dst *image.NRGBA, w, h int) {
	cx, cy := 0.72*float64(w), 0.095*float64(h)
	rc := 0.045 * float64(w)
	col := color.NRGBA{R: 236, G: 236, B: 224, A: 0xff}
	core := 248.0 // 上游素材 α=255 全不透明；留 7 级余量避免与天空硬切

	maxR := rc * 1.02
	x0 := int(math.Max(0, cx-maxR))
	x1 := int(math.Min(float64(w), cx+maxR))
	y0 := int(math.Max(0, cy-maxR))
	y1 := int(math.Min(float64(h), cy+maxR))

	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			d := math.Hypot(dx, dy)
			rr := d / rc
			if rr > 1.02 {
				continue
			}
			facing := 0.0
			if rr < 1.0 {
				facing = math.Sqrt(1.0 - rr*rr)
			}
			// 上游逐字参数
			limb := smoothstep(0.015, 0.10, facing)
			if limb <= 0 {
				continue
			}
			// 临边昏暗：中心亮、外缘暗，降 12%（照素材实测）
			shade := 1.0 - 0.12*rr*rr
			c := color.NRGBA{
				R: clamp8(float64(col.R) * shade),
				G: clamp8(float64(col.G) * shade),
				B: clamp8(float64(col.B) * shade),
				A: 0xff,
			}
			BlendPix(dst, x, y, c, core*limb/255.0)
		}
	}
}

// blendAlphaV/H：把 (c,a1)→(c,a2) 的线性渐变按 src-over 合成进 dst。
// 与 paint.LinearGradientOp + PaintOp 的 src-over 语义逐像素等价。
func BlendAlphaV(dst *image.NRGBA, r image.Rectangle, c color.NRGBA, a1, a2 uint8) {
	n := r.Max.Y - r.Min.Y
	if n <= 0 || r.Max.X <= r.Min.X {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		t := 0.0
		if n > 1 {
			t = float64(y-r.Min.Y) / float64(n-1)
		}
		a := LerpF(float64(a1), float64(a2), t) / 255.0
		for x := r.Min.X; x < r.Max.X; x++ {
			BlendPix(dst, x, y, c, a)
		}
	}
}

func BlendAlphaH(dst *image.NRGBA, r image.Rectangle, c color.NRGBA, a1, a2 uint8) {
	n := r.Max.X - r.Min.X
	if n <= 0 || r.Max.Y <= r.Min.Y {
		return
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		t := 0.0
		if n > 1 {
			t = float64(x-r.Min.X) / float64(n-1)
		}
		a := LerpF(float64(a1), float64(a2), t) / 255.0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			BlendPix(dst, x, y, c, a)
		}
	}
}

func BlendPix(dst *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := dst.PixOffset(x, y)
	ia := 1.0 - a
	dst.Pix[i+0] = clamp8(float64(dst.Pix[i+0])*ia + float64(c.R)*a)
	dst.Pix[i+1] = clamp8(float64(dst.Pix[i+1])*ia + float64(c.G)*a)
	dst.Pix[i+2] = clamp8(float64(dst.Pix[i+2])*ia + float64(c.B)*a)
	// 背景恒不透明，alpha 通道保持 0xff
	dst.Pix[i+3] = 0xff
}

func LerpF(a, b, t float64) float64 { return a + (b-a)*t }

// skyBlit 把烘焙好的背景按整窗贴一次：1 个 clip + 1 个 ImageOp + 1 个 PaintOp。
// 取代 skyGradient(1) + skyDepth(18) = 19 个 op。
func SkyBlit(gtx layout.Context, size image.Point, cls effect.Class, isDay bool, phase *float64, cloudPct int) {
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	ent := SkyCached(size, cls, isDay, phase, cloudPct)
	if ent == nil {
		SkyGradient(gtx, size, cls, isDay)
		SkyDepth(gtx, size, cls, isDay)
		return
	}
	cl := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	sz := ent.Ops.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		cl.Pop()
		SkyGradient(gtx, size, cls, isDay)
		SkyDepth(gtx, size, cls, isDay)
		return
	}
	sx := float32(size.X) / float32(sz.X)
	sy := float32(size.Y) / float32(sz.Y)
	// op.Affine 是累加语义（已验证），必须 Push/Pop 成对。
	//
	// 变换写法实测（headless 逐像素探针 probe_blit）：
	// `f32.NewAffine2D(sx,0,0,0,sy,0)` 直接矩阵 ⇒ 整屏覆盖，(1070,1900) 有像素；
	// `f32.NewAffine2D(1,0,0,0,1,0).Scale(Point{sx,sy}, Point{})` ⇒ **全透明，
	// 什么都没画**。第一版用的就是后者，后果是"背景不画 ⇒ CPU 假降 11 个点"。
	// 故必须用直接矩阵，禁止 Scale(s, p) 形式。
	tr := op.Affine(f32.NewAffine2D(sx, 0, 0, 0, sy, 0)).Push(gtx.Ops)
	ent.Ops.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	cl.Pop()
}

// ---- C4 太阳盘 + 泛光（亮核 + 泛光，射线已删）+ 云量调可见度 ----
//
// 口径（逐条）：
//
//	1 实心盘 + 边缘羽化：盘半径 rc = 0.045w（既有口径），盘内近均匀亮
//	  （盘体叠加量 sunDiscLevel=120，晴档平顶那轮由 170 降下，见常量处注释），
//	  羽化带宽 = 半径的 12%，cos² ⇒ 两端导数为 0。
//	2 盘外近场铺光：A=200、L=0.18w（近场 Lorentz，长尾并入天空底色，不做归零截断）。
//	  这一项就是中间亮核的尺寸口径：0.18w 的 Lorentz 在 0.45w 处只剩 200/(1+6.25)=28 级，
//	  中间读起来是一个小而实的亮核；远处范围由全天空抬升项（L=0.9w）与宽光贴图（1.6w）撑开
//	  ⇒ 中间小、整体铺光范围不缩（半径 0.55w/幅度 150 的写法把中间撑成一大片糊开的亮区）。
//	3 12 条放射光芒已删除（条数、峰值、半宽、长度与绘制分支全部移除）。
//	4 盘周天空提亮 = **归零**（sunLiftLevel=0）：提亮是"盘周一圈亮边"的成因，
//	  出厂值改 0；函数 sunSkyLift 与 dev 变量 WS_SUN_LIFT_LV 保留为工装（见 ui/sunpreview.go）。
//	5 泛光 = embed/sunbloom.png（Kenney light_01，CC0），中心与月亮同一口径
//	  (0.72w, 0.095h)，半径 6×rc，峰值叠加量 80。
//	6 **云量调可见度**：亮度系数 sunDim = 1 − 0.8·(云量/100)，夹在 [0.20, 1.00]。
//	  盘体级、光斑 A、泛光峰值都乘 dim；盘半径 = sunDiscRcFrac×(0.75+0.25·dim)。
//	  云量 ≥85 的灰蒙档不画硬圆盘（走 disc=false 分支，只留宽光）；
//	  云量 100 时 dim 落在下限 0.20 ⇒ 灰蒙宽光仍在，不许全黑。
//
// 门控：bakeCelestial 在**任何白天档**都画铺光，夜间不画（月亮走独立层）。
// **盘的可见度**由单一函数 sunDiscVisibility(天气类, 云量) 决定（见该函数的逐格口径）：
//
//	日照定义 = 直接太阳辐照 ≥120 W/m² 才计为日照（1981 年由 210 改为 120，
//	ECMWF Parameter Database 189、WMO Codes Registry 270、Prescott/Solar Energy 对比文）
//	⇒ 日照就是直射光束基本未被遮挡；阴天直射≈0 ⇒ 盘不可见。
//	okta 口径（NWS/oktas，UW-Madison The Weather Guys 2024-01-08）：1–3 oktas = mostly clear，
//	3–5 oktas = partly cloudy = partly sunny（晴多云时太阳可见），5–7 = mostly cloudy，8 = 全阴，
//	雾 = 9 oktas（天空被遮蔽）⇒ 5 oktas = 62.5% 云量以上盘就该淡出。
//	太阳雨（Wikipedia Sunshower）：头顶一朵阵雨云、太阳角度不被遮挡 ⇒ 有降水而总云量低时盘可见。
//
// 盘本体、小亮核、近场项这一组乘可见度；**全天空抬升项与宽光贴图不乘**
// （云对光的衰减是连续量，漫射光仍在太阳方向 ⇒ 阴天/雨天照旧有灰蒙宽光，按 sunDim 变暗）。
// 天体烘在天空纹理里 ⇒ 泛光零逐帧成本；云带与粒子云都在天空之后画（图层律不动，云盖在太阳之上）。
const (
	SunDiscRcFrac  = 0.045 // 盘半径 = 0.045 × 烘焙宽（既有口径，dim=1 时）
	SunFeatherFrac = 0.12  // 羽化带宽 = 半径 12%（规格上限）
	// 盘体叠加量（dim=1 时的峰值）170 → 120（规格 2.2 幅度重调）。
	// 为什么降：整帧实测（1080×1920 晴档）亮度 ≥250 的像素 = 18047 个、
	// bbox (702,107)-(853,258) = 152×152 px 的平顶，中心就是盘心 (777,182)，
	// 盘心 RGB = (255,252,238) ⇒ 盘体峰值 170 把盘心连同盘外一圈一起顶进 255 钳平区。
	// 留头合成（见 bakeSunAnalytic 末尾）把"加满即平"换成"越近白越慢"，
	// 峰值 120 时盘心实测落在 240 上下（判据 T2 ≤245），盘区仍是全场最亮的一截。
	SunDiscLevel = 120.0

	SunLiftLevel     = 0.0 // 盘周提亮峰值 = 归零（提亮圈的成因，出厂值 0）
	SunLiftR0Frac    = 1.0 // 提亮起点 = 1×半径（盘内已由盘体填满，不重复抬）
	SunLiftR1Frac    = 3.0 // 提亮平台终点 = 3×半径
	SunLiftR2Frac    = 3.8 // 提亮归零半径（3×rc 之外用 cos² 收口，避免 3×rc 处出现台阶）
	SunBloomAsset    = "sunbloom.png"
	SunBloomLevel    = 80.0 // 泛光峰值叠加量（实测：80 时盘缘外相邻差 3.5 级 ≤6，环带峰 210 < 盘体 248）
	SunBloomRcFrac   = 6.0  // 泛光半径 = 6×盘半径（= 0.27 倍烘焙宽）
	SunBloomAngSteps = 72   // 角向平均的取样方向数（见 sunBloomRadial）

	// 云量调可见度的系数口径（D2 第 2 条）：
	// sunDim = 1 − sunDimSlope·(云量/100)，夹在 [sunDimFloor, 1.00]。
	SunDimSlope   = 0.8
	SunDimFloor   = 0.20 // 云量 100 时的下限 ⇒ 灰蒙宽光，不许全黑
	SunNoDiscPct  = 85   // 云量 ≥85 的灰蒙档：不画硬圆盘，只画宽光
	SunRcDimMin   = 0.75 // 盘半径随 dim 缩放下限：rc×(0.75+0.25·dim)
	SunRcDimSlope = 0.25
)

// ---- 近场铺光（中间亮核那一档）----
//
// 近场铺光的两项之一（另一项 = 下面 skyLiftLFrac 的全天空抬升）：
// 两项都是 A/(1+(r/L)²) 的长尾，加到天空底色上，任何一处都不做归零截断。
//
// 近场项 = 中间亮核那一档：L = 0.18 倍烘焙宽、A = 200 ⇒ 中间是一个小而实的亮核
// （0.45w 处只剩 200/(1+6.25) = 28 级）。远处范围由 L=0.9w 的抬升项与
// 1.6 倍烘焙宽的宽光贴图撑开 ⇒ 中间小、整体铺光范围不缩。
// 近场项乘盘可见度 sunDiscVisibility（盘淡出时中间亮核一起淡出）；
// 全天空抬升项与宽光贴图不乘（见 bakeSunAnalytic 的 vis 口径）。
//
// 本批起：近场 Lorentz 项并入上面的 exp 形式（单一连续剖面），这两个常量不再进画面。
const (
	SunNearAmp   = 200.0 // 近场铺光峰值 A_n（dim=1、盘可见度=1 时）
	SunNearLFrac = 0.18  // L_n = 0.18 倍烘焙宽
)

// ---- 全天空抬升（去圈）----
//
// 需求："太阳永远只在那一片永远是缩在那里，光线永远只有那一圈"。
// 实测根因：近场光斑 A=200、L=0.18w 在 d=0.5w 处只剩 200/(1+(0.5/0.18)²)=23 级
// ⇒ 可见亮区被压在太阳附近一圈。
//
// 口径：以太阳为原点的极缓衰减 lift(d) = A_sky/(1+(d/L_sky)²)，
//
//	L_sky = skyLiftLFrac × 烘焙宽 = 0.9 倍烘焙宽
//	A_sky = sunSkyLiftAmp × dim  = 90 × dim
//
// 与近场铺光**叠加**（近场保留太阳附近的亮核），两条都乘 dim。
// 两条同形（A/(1+(d/L)²)）、同乘 dim、都不做归零截断 ⇒ 盘外只有一条连续衰减，
// 加性铺光不再经过任何贴图掩膜（掩膜的归零点就是可见边界，见 sunGlowAt 的口径说明）。
// 抬升项**不进** 1.5w 处的 cos² 窗：那条窗的作用是把近场项在绘制截止处压平，
// 抬升项铺满整张画布、画布内没有截止，加窗只会把四角重新压回暗角
// （实测 1.5w 窗在四角处 u=0.93 ⇒ 窗值 0.013，抬升 18.6 级被吃掉只剩 0.2 级）。
// 抬升项自身的衰减就是单调光滑的 Lorentz，画布边缘处没有台阶。
// 本批（单一连续剖面）起：这两项 Lorentz 常量**不再进画面**，
// 口径由下面的 sunMainAmp/sunMainLFrac/sunSkyUniform 接管（登记见 out/notes-sunmoon.md）。
const (
	SkyLiftLFrac  = 0.9  // L_sky = 0.9 倍烘焙宽
	SunSkyLiftAmp = 90.0 // A_sky（dim=1 时盘外任意点的抬升上限）
)

// ---- 铺光 = 单一连续剖面（规格 D5 第 2.1 条）----
//
// 加性光只保留两项解析项 + 一个全天空常数：
//
//	lift(r) = A·exp(−r/L) + Acore·exp(−r/Lc) + U
//	A     = sunMainAmp × dim          = 150·dim        （主圈）
//	L     = sunMainLFrac × 烘焙宽      = 0.90 倍烘焙宽   （主圈半径 = 0.9 倍屏宽）
//	Acore = sunCoreExpAmp × dim × vis = 70·dim·vis     （小亮核，只在盘可见时，规格 2.2 由 120 降下）
//	Lc    = sunCoreExpLFrac × 烘焙宽   = 0.06 倍烘焙宽
//	U     = sunSkyUniform × dim       = 12·dim         （全天空常数抬升）
//
// 换掉 Lorentz 的实测理由（规格 1.4 条）：旧加性光由多个**带边界**的项拼成
// —— 近场 Lorentz（A=200、L=0.18w）+ 小核 Lorentz（A=46、L=0.09w）+
// 全天空 Lorentz（L=0.9w）+ 宽光贴图（掩膜在 rim 归零）。
// 晴档沿水平射线逐 40 px 的实测剖面斜率在 r≈190/320/660 三处突变
// （−1.6→−19.9→−28.3→−9.8→−4.3→0），且光在 r≈0.7w 就落到天空底色
// ⇒ 肉眼读到"分界线"、"光只在那几个圈"。
// Lorentz 尾巴 ∝1/r² 与天空底色相交处就是可见台阶；exp 尾巴与底色不相交。
//
// 乘子口径不动：主圈项乘 dim、小亮核乘 dim×盘可见度、全天空常数只乘 dim
// （云把直射连续削暗、漫射光仍在太阳方向 ⇒ 灰蒙档照旧有铺光，按 dim 变暗）。
// 盘本体（sunDiscRcFrac=0.045、半径 ×(0.75+0.25·dim)）与盘羽化口径不动。
const (
	SunMainAmp   = 150.0 // A = 150·dim
	SunMainLFrac = 0.90  // L = 0.90 倍烘焙宽（主圈半径放大到 0.9 倍屏宽）
	// 小亮核峰值 Acore = 120·dim·vis → 70·dim·vis（规格 2.2 幅度重调）。
	// 为什么降：小亮核是平顶的第二条来源 —— 它的半径 Lc = 0.06 倍烘焙宽 = 65 px，
	// 峰值 120 与主圈 150、盘体 170 在盘心叠加到 430 级，
	// 旧口径 a = added/255 钳到 1 ⇒ 盘心 (255,252,238)、向左逐 20px 连平 10 点
	// （251,250,251,250,250,249,249,249,249,247）⇒ 182×182 的平顶。
	// 留头合成后 70 级的小亮核把盘心推到 240 上下就停住（判据 T1 = 0 个 ≥250 像素）。
	SunCoreExpAmp   = 70.0
	SunCoreExpLFrac = 0.06 // Lc = 0.06 倍烘焙宽
	SunSkyUniform   = 12.0 // U = 12·dim（全天空常数抬升）
	// 留头合成的连做层数（见 screenKeep）：一个因子分这么多层灌，每层一半量。
	// 为什么是 2：连做一次时盘心 246 级（判据 T2 要 ≤245，超 1 级），
	// 分 2 层后盘心 240 级；四角净抬升几乎不动（角上只有主圈 5.9 + 常数 3.4 级，
	// 层数对这么小的量没有影响，实测 4.36 → 4.4 级）。
	SunScreenLayers = 2.0
)

// ---- 档间太阳形态（贴图）----
//
// 晴 / 多云见日（画盘的档）：星芒贴图 sunrays.png 加性绘制 + 小亮核；
// 阴 / 雨 / 雪 / 雾 / 尘（云量 ≥85 的灰蒙档，不画盘）：宽光罩 sunglow.png 只作
// **软形状底**（给宽光一点不规则的形状，不再当加性光的掩膜），
// 屏内的加性光量由解析两项给出（sunGlowAt）。
// 两张都来自 Kenney CC0（见 assets/LICENSE-kenney.txt 的包清单），
// 走现有覆盖目录工装口径（WS_ASSET_DIR 同名覆盖优先，见 assetImageAny）。
const (
	SunRaysAsset = "sunrays.png" // 星芒（Kenney Smoke Particles flash05，512 宽）
	SunGlowAsset = "sunglow.png" // 宽光罩（Kenney Light Masks circle_a，1024 宽）
	// 两张贴图的绘制宽度都把 rim 推到屏外：
	// 星芒 1.4 倍烘焙宽 ⇒ 半宽 0.7w，中心在 0.72w ⇒ 左右 rim 都在屏外（0.02w 与 1.42w）；
	// 宽光 1.6 倍烘焙宽 ⇒ 半宽 0.8w ⇒ 左 rim 在 -0.08w、右 rim 在 1.52w，都在屏外。
	// 贴图掩膜在 rim 处归零这件事本身没问题，问题是归零点落在屏内就是一条可见边界；
	// 把 rim 推到屏外后，屏内可见的只有解析长衰减（见 sunGlowAt）。
	SunRaysWidthFrac = 1.4  // 星芒宽 = 1.4 倍烘焙宽（边缘出屏）
	SunGlowWidthFrac = 1.6  // 宽光宽 = 1.6 倍烘焙宽（rim 出屏，屏内贡献由解析项接管）
	SunRaysAmp       = 22.0 // 星芒加性峰值（级）
	SunCoreAmp       = 46.0 // 星芒中心小亮核的加性峰值（级）
	SunCoreLFrac     = 0.09 // 小亮核的 Lorentz 半径（倍烘焙宽）
	SunGlowAmp       = 90.0 // 宽光软形状底的加性峰值（级）
	// 本批起：小亮核的 Lorentz 口径并入上面的 exp 形式（sunCoreExpAmp/sunCoreExpLFrac），
	// 宽光贴图在屏内的加性贡献整项停用（屏内像素一律不得由它产生加性光）
	// ⇒ sunCoreAmp/sunCoreLFrac/sunGlowAmp/sunGlowWidthFrac 不再进画面。
)

// sunDimOf 云量 → 太阳亮度系数：1 − 0.8·(pct/100)，夹在 [0.20, 1.00]。
//
// 云量是 0–100 的连续量（实测码只是它的离散化：码 1 ≈ 27–41%、码 2 ≈ 52–79%、
// 码 3 = 100%），所以可见度按云量走、不按天气类走。
// 云量越出 [0,100] 先钳位再算（上游给脏值时不得把 dim 推出区间）。
func SunDimOf(pct int) float64 {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	d := 1.0 - SunDimSlope*float64(pct)/100.0
	if d < SunDimFloor {
		return SunDimFloor
	}
	if d > 1.0 {
		return 1.0
	}
	return d
}

// sunNoDisc 云量是否进到灰蒙档（≥85 ⇒ 不画硬圆盘，只画宽光）。
func SunNoDisc(pct int) bool { return pct >= SunNoDiscPct }

// ---- 太阳盘可见性分级（盘可见度的唯一决策点）----
//
// 需求："除了太阳雨这种天气，其他时候太阳应该几乎不可见或被云遮挡"。
// 盘的存在与否按**天气类 + 云量**分级；盘照不照得亮（连续变暗）仍走 sunDim。
// 两条是分开的事：云把直射光连续削暗（sunDim），云把盘遮住（可见度分级）。
//
// 逐格口径（本函数是盘可见度的唯一出处，别处不许再乘系数）：
//
//	clear / partly-cloudy / cloudy（非降水类）
//		云量 ≤50        → 1.0（1–3 oktas = mostly clear/sunny）
//		云量 50→75      → 线性 1.0→0.0（3–5 oktas = partly sunny 还见日，5–7 = mostly cloudy）
//		云量 >75        → 0.0
//	overcast          → 0.0（8 oktas = 全阴，直射≈0 ⇒ 日照不计）
//	rain / snow / thunder
//		云量 ≤50        → 1.0（太阳雨：头顶一朵阵雨云、太阳角度不被遮挡）
//		云量 >50        → 0.0
//	fog / dust        → 0.0（雾 = 9 oktas，天空被遮蔽，观测不到）
//
// cloudy 是历史档（逐小时云量 ≥30 时由 ClassifyByCloudCover 给出，见 effect 包），
// 表里没有单列：它是"非降水、云在天上"的那一族，与 clear/partly-cloudy 同族 ⇒ 走同一行。
// 夜间不画太阳由 bakeCelestial 的 isDay 分支挡（既有口径不动）。
func SunDiscVisibility(cls effect.Class, cloudPct int) float64 {
	p := cloudPct
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	switch cls {
	case effect.Clear, effect.PartlyCloudy, effect.Cloudy:
		switch {
		case p <= 50:
			return 1.0
		case p >= 75:
			return 0.0
		default:
			// 50→75 线性淡出：50 处 1.0、75 处 0.0（60 处 0.6）
			return float64(75-p) / 25.0
		}
	case effect.Rain, effect.Snow, effect.Thunder:
		// 太阳雨：有降水而总云量低时太阳可见；云量一过 50 盘就没
		if p <= 50 {
			return 1.0
		}
		return 0.0
	default:
		// overcast / fog / dust
		return 0.0
	}
}

// sunDiscProfile 盘体剖面：r ≤ rc·(1−12%) 内取 1（盘内近均匀），
// 羽化带 [0.88rc, rc] 内 cos² 落到 0，盘外 0。
func SunDiscProfile(r, rc float64) float64 {
	r1 := rc * (1.0 - SunFeatherFrac)
	if r <= r1 {
		return 1.0
	}
	if r >= rc {
		return 0.0
	}
	c := math.Cos((r - r1) / ((rc - r1) * 2.0) * math.Pi)
	return c * c
}

// sunSkyLift 盘周天空提亮量。**出厂值归零**（sunLiftLevel=0 ⇒ 整条曲线恒 0）：
// 提亮是"盘周一圈亮边"的成因，出厂口径是关掉的。
//
// 函数与 dev 变量（WS_SUN_LIFT_R / WS_SUN_LIFT_LV / WS_SUN_LIFT_FADE）保留 = 出图工装：
// 把 WS_SUN_LIFT_LV 设成非 0 就能出"带提亮圈"的对比图交肉眼挑。
// 曲线形状不动：r ≤ 平台半径取满，平台半径到归零半径之间 cos² 收到 0
// （cos² 两端导数为 0 ⇒ 平台末端不出现台阶）。
func SunSkyLift(r, rc float64) float64 {
	r1 := SunLiftRFrac * rc
	r2 := SunLiftRFrac * SunLiftFadeFrac * rc // 归零半径 = 平台半径 × fade（出厂 1.2667 = 3.0/3.8 同比例）
	if r <= r1 {
		return SunLiftLv
	}
	if r >= r2 {
		return 0.0
	}
	c := math.Cos((r - r1) / ((r2 - r1) * 2.0) * math.Pi)
	return SunLiftLevel * c * c
}

// ---- 太阳泛光（规格 1.4：sunbloom.png 烘进天空纹理）----
//
// 取样口径 = **按半径取角向平均**（72 个方向求均值），并强制剖面单调不升。
// 两条理由，都是实测：
//
//	(a) 源贴图 light_01 不是径向对称：实测 r=80 处 16 个方向的 alpha
//	    最小 40、最大 135（比 3.4），且 r 约 88 处有一圈亮环（剖面 75 到 109 的回升）。
//	    直接按角度采样 = 在盘外重新造出方向性结构，与"删掉 12 条射线"的目的相反
//	    （自检 TestNoSunRays 的判据：绕盘心 16 个角度采样，最大/最小比 ≤1.25）。
//	(b) 剖面的回升段会在中心行上留下"变亮"的像素，违反
//	    TestSunProfileNoHardEdge 的"盘缘之外不升、相邻差 ≤6 级"。
//
// 叠加口径 = 只取贴图的 **alpha 当强度掩膜**，颜色走太阳自己的色带（blendPix）。
// 贴图的 RGB 一个字节都不写进天空纹理，月亮层踩过的"把预乘 RGB 当非预乘写进
// NRGBA"这个坑在本路径上不存在（readPNG 已把贴图反预乘成 straight alpha，
// 这里连用都不用它 RGB）。

var (
	SunBloomProfMu   sync.Mutex
	SunBloomProf     []float64
	SunBloomProfSize int
)

// sunBloomRadial 返回泛光贴图的角向平均 alpha 剖面（索引 = 贴图像素半径，
// 值 0..255，单调不升）。按贴图半径缓存，烘焙路径每键一次，逐帧零成本。
// 贴图缺失（素材没落地）=> 返回 nil，太阳退回纯解析画法，不空不崩。
func SunBloomRadial() []float64 {
	SunBloomProfMu.Lock()
	defer SunBloomProfMu.Unlock()
	img := assets.Sprite(SunBloomAsset)
	if img == nil {
		return nil
	}
	b := img.Bounds()
	n := b.Dx() / 2
	if SunBloomProf != nil && SunBloomProfSize == n {
		return SunBloomProf
	}
	at := func(x, y int) int {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= b.Dx() {
			x = b.Dx() - 1
		}
		if y >= b.Dy() {
			y = b.Dy() - 1
		}
		return int(img.Pix[img.PixOffset(b.Min.X+x, b.Min.Y+y)+3])
	}
	c := float64(b.Dx()-1) / 2.0
	prof := make([]float64, n+1)
	for r := 0; r <= n; r++ {
		var sum float64
		for k := 0; k < SunBloomAngSteps; k++ {
			th := float64(k) * 2.0 * math.Pi / float64(SunBloomAngSteps)
			sum += float64(at(int(math.Round(c+float64(r)*math.Cos(th))), int(math.Round(c+float64(r)*math.Sin(th)))))
		}
		prof[r] = sum / float64(SunBloomAngSteps)
	}
	// 强制单调不升：外圈比内圈亮的段（light_01 实测的那圈亮环）压平到内侧值
	for i := 1; i <= n; i++ {
		if prof[i] > prof[i-1] {
			prof[i] = prof[i-1]
		}
	}
	SunBloomProf = prof
	SunBloomProfSize = n
	return prof
}

// sunBloomAt 泛光在偏移 (dx,dy)（对盘心）处的叠加量。
// 半径映射：屏幕半径 r 占泛光半径 Rb 的比例 => 贴图像素半径（贴图半径 = 半宽）。
func SunBloomAt(dx, dy, rc float64) float64 {
	prof := SunBloomRadial()
	if len(prof) == 0 {
		return 0
	}
	Rb := SunBloomRcFrac * rc
	r := math.Hypot(dx, dy)
	if r >= Rb {
		return 0
	}
	n := float64(len(prof) - 1)
	sr := r / Rb * n
	i := int(sr)
	if i >= len(prof)-1 {
		return 0
	}
	t := sr - float64(i)
	v := prof[i]*(1.0-t) + prof[i+1]*t
	return SunBloomLevel * v / 255.0
}

// clamp01 把浮点数夹进 [0,1]（留头乘子的取值域，见 screenKeep）。
func Clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// wl2 盘这一组（小亮核/盘体/提亮/泛光）的 cos² 窗值 = cos²(π·d/2R)，d ≥ R 取 0。
//
// 就是旧口径里 near 乘的那个 wl*wl，抽成函数是为了让"进窗/不进窗"在留头合成的
// 因子表里写清楚（主圈与全天空常数不进窗，盘这一组进窗）。
// 窗在 r=R 处函数值与导数同时为 0 ⇒ 绘制截止处既无台阶也无折角（口径不动）。
func Wl2(d, R float64) float64 {
	if d >= R {
		return 0.0
	}
	wl := math.Cos(d / R * math.Pi * 0.5)
	return wl * wl
}

// screenKeep 留头合成（规格 2.1 第 1 条）的逐通道"留头乘子"累乘。
//
// 一个加性因子 add（单位：亮度级）按光色 col 折算成该通道的加光量 addC = add·colC/255，
// 该通道的留头合成就是规格写的那一条：
//
//	out = base + addC·(1 − base/255)
//	    = 255 − (255 − base)·(1 − addC/255)
//
// 本函数只把乘子连乘到既有乘子上，写入时一次成型：
// 连做 N 层 = 255 − (255 − base)·Π(1 − addC_i/255)（screen 满足交换律与结合律，
// 因子顺序不影响结果）。
//
// 一个因子分 sunScreenLayers 层灌（每层 add/sunScreenLayers 级）：同一因子的量不变，
// 只是把它拆成几层连做。为什么拆：连做一次的乘子 Π(1 − addC_i/255) 在盘心是 0.0426
// ⇒ 盘心 246 级，超出判据 T2 的 245；拆成 2 层后乘子 0.0733 ⇒ 盘心 240 级。
// 拆层不改变剖面（各因子求和不动），只改变"越接近白抬得越慢"这件事的强度：
// 层数越多，同一级光在已亮处抬得越少（层数 →∞ 时乘子 → exp(−ΣaddC_i/255)）。
//
// 乘子夹在 [0,1]：单层加光量 addC/层数 ≥ 255 时 (1 − addC/层数/255) ≤ 0 ⇒ 夹到 0。
// 夹子只作用在**单层**上，不作用在总和上 —— 旧口径的平顶就来自把总和的
// a = added/255 钳到 1（加满即 255，整帧实测 182×182 平顶）；这里没有任何一处
// 对总和设上限，越接近白 (1 − base/255) 越小 ⇒ 同一级光抬得越少 ⇒ 平顶不存在。
//
// 与旧口径的关系：单层加光量 ≤ 255 时两者逐字相等（旧口径 blendPix 是
// dst + a·(col − dst)，a = added/255，R 通道 col.R=255 ⇒ dst + added·(1 − dst/255)），
// 差别只在加光量 > 255 的那一截：旧口径钳平，新口径接着抬、越抬越慢。
func ScreenKeep(kR, kG, kB, add float64, col color.NRGBA) (float64, float64, float64) {
	if add <= 0 {
		return kR, kG, kB
	}
	s := add / (SunScreenLayers * 255.0 * 255.0)
	mR := math.Pow(Clamp01(1.0-s*float64(col.R)), SunScreenLayers)
	mG := math.Pow(Clamp01(1.0-s*float64(col.G)), SunScreenLayers)
	mB := math.Pow(Clamp01(1.0-s*float64(col.B)), SunScreenLayers)
	return Clamp01(kR * mR), Clamp01(kG * mG), Clamp01(kB * mB)
}

// bakeSunAnalytic 把太阳画进烘焙图：心 (0.72w, 0.095h)。
//
// 加性光 = **单一连续剖面**（规格 D5 第 2.1 条）：
//
//	lift(r) = A·exp(−r/L) + Acore·exp(−r/Lc) + U
//
// disc=true  = 主圈 + 小亮核 + 盘体 + 盘周提亮（出厂归零）+ 泛光；
// disc=false = 主圈 + 全天空常数（云量 ≥85 的灰蒙档走这一支，盘这一组全 0）。
// dim        = 云量给的亮度系数（sunDimOf，夹在 [0.20,1.00]），缩放四样：
//
//	主圈      A = 150·dim
//	小亮核    Acore = 70·dim·vis   （规格 2.2 幅度重调：120 → 70）
//	全天空常数 U = 12·dim
//	盘体级    sunDiscLevel(120)×dim×vis   （规格 2.2 幅度重调：170 → 120）
//	盘半径    sunDiscRcFrac×(0.75+0.25·dim)
//
// 泛光峰值也按 dim×vis 缩放（口径外的第 4 项，理由：泛光是压住盘缘剖面的那一项，
// 不跟着缩就会在低 dim 下相对盘体重新抬头，把"盘"读回来）。
//
// vis        = 盘可见度（sunDiscVisibility 一处给出，夹在 [0,1]），只缩放**盘这一组**：
//
//	盘体级、小亮核、星芒、泛光 都乘 vis
//
// **主圈项与全天空常数不乘 vis**：云把直射挡掉时漫射光仍在太阳方向，
// 阴天/雨天的天空照旧按 dim 变暗 ⇒ 无盘 + 灰蒙铺光。
// 返回 true = 画成功（dst 已被叠加）。
//
// 曲线口径：
//
//	盘内 r ≤ 0.88rc：铺光 + 盘体在盘心合计 582 级
//	       （主圈 150 + 常数 12 + 主圈·窗 150 + 小亮核 70 + 盘体 120 + 泛光 80，
//	        最大的单项是主圈 150），逐通道留头合成把盘心推到 255 之下就越来越慢
//	       ⇒ 盘内近均匀亮、但没有"加满钳平"那一段（规格 2.1 第 1 条要消掉的东西）
//	羽化带 0.88rc..rc：宽 = 半径 12%，cos² 从盘体落到 0（两端导数 0 ⇒ 清晰但不割裂）
//	盘外：单一连续剖面 A·exp(−r/L) + Acore·exp(−r/Lc) + U，再加提亮与泛光，
//	       全部单调光滑、不做任何归零截断，长尾直接并进天空底色
//	       （可见边界的来源就是"截断"本身：Lorentz 尾巴 ∝1/r² 与天空底色有交点，
//	        exp 尾巴没有交点）
//	半径 1.5 屏宽处乘 cos² 窗 ⇒ 绘制截止处既无台阶也无折角（既有口径不动：
//	窗在 u=1 处函数值与导数同时为 0）；全天空常数不进窗（窗会把四角压回暗角）
//
// 宽光贴图（sunglow.png）在屏内的加性贡献整项停用：它的掩膜在 rim 归零，
// 归零边就是"分界线"的一个来源 ⇒ 屏内像素一律不得由它产生加性光，
// 盘与光全用解析项（登记见 out/notes-sunmoon.md）。
func BakeSunAnalytic(dst *image.NRGBA, w, h int, disc bool, dim, vis float64) bool {
	// 单一烘焙循环：剖面 / 盘 / 提亮 / 泛光 的加性光逐因子走留头合成，
	// 乘子连乘后一次写入（不逐层 src-over：逐层 src-over 会在层界处留下可见的斜率拐点，
	// 正是"像个圆盘"的旧病根；留头合成的连乘是逐通道乘法，层界处没有插值上限的突变）。
	cx, cy := 0.72*float64(w), 0.095*float64(h)
	// 单一连续剖面的三条量（规格 2.1 逐字）
	A := SunMainAmp * dim              // 主圈 A = 150·dim
	L := SunMainLFrac * float64(w)     // L = 0.90 倍烘焙宽（主圈半径 = 0.9 倍屏宽）
	Acore := SunCoreExpAmp * dim * vis // 小亮核 Acore = 70·dim·vis（只在盘可见时，规格 2.2 由 120 降下）
	Lc := SunCoreExpLFrac * float64(w) // Lc = 0.06 倍烘焙宽
	// 全天空常数 U = 12·dim
	U := SunSkyUniform * dim
	// 预览工装系数 SunSkyScale（出厂 1.0）整项缩放**全天空那一组**（主圈 + 常数）：
	// 判据用它把全天空组关掉、只量中间亮核（小亮核 + 盘体 + 泛光），出厂路径不动画面。
	A *= SunSkyScale
	U *= SunSkyScale
	rc := SunDiscRcFrac * (SunRcDimMin + SunRcDimSlope*dim) * float64(w)
	extraR := SunLiftR2Frac * rc  // 盘/提亮的最大作用半径（3.8×rc）
	bloomR := SunBloomRcFrac * rc // 泛光半径（6×rc = 0.27 倍烘焙宽）

	// 盘这一组的绘制半径 = 1.5 倍屏宽 + cos² 窗（既有口径不动：窗在 r=R 处函数值与导数同时为 0）。
	// 循环范围 = 整张画布：全天空常数要铺到四角（四角对盘心的距离 = 1.76 倍烘焙宽）。
	rad := 1.5 * float64(w)
	x0, x1 := 0, w
	y0, y1 := 0, h

	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			d := math.Hypot(dx, dy)
			ul := d / L
			// 主圈项 + 全天空常数：这两项不进 cos² 窗（铺满整张画布、画布内没有截止）
			main := A * math.Exp(-ul)
			// ---- 留头合成（规格 2.1）：剖面不动，写入改成逐通道 screen ----
			// 旧口径：added 求和 → a = added/255 钳到 1 → src-over ⇒ 加满即 255 平顶
			// （整帧实测：晴档 ≥250 的像素 18047 个、bbox 152×152 平顶、盘心 (255,252,238)；
			//  规格 §1 在 fxshot 整帧上量到 19611 个、182×182）。
			// 新口径：每个因子单独走 out = base + add·(1 − base/255)，逐层连做
			// （screen 可交换可结合 ⇒ 连做 N 层 = 255 − (255−base)·Π(1 − add_i/255)）。
			// 为什么逐因子而不是一次性把总和灌进去：盘心总和 582 级 > 255，
			// 一次性灌时 (1 − 582/255) 是负数 ⇒ 夹到 0 ⇒ 盘心 255，平顶原样落地；
			// 逐因子时最大的单项是主圈 150 级（乘子 0.412），没有任何一处对总和设上限
			// ⇒ 盘心落在 255 之下，且越近白 (1 − base/255) 越小、同一级光抬得越少。
			// 因子表 = 剖面的既有各项，一项不增不减（单一循环、单一连续剖面不动）。
			// 旧口径的求和顺序（本批前一版的代码，逐字对得上）：
			//
			//	added = main + U + near·wl²，near = main + 小亮核 + 盘体 + 提亮 + 泛光
			//
			// ⇒ 因子 = 主圈(不进窗) + 全天空常数(不进窗) + 主圈·窗 + 小亮核·窗 +
			//   盘体·窗 + 提亮·窗 + 泛光·窗，七项，求和与旧口径逐项相等。
			// 为什么一项一个因子、不把 near 合成一个因子灌进去：near 在盘心是
			// 70+120+80 = 270 级 > 255 ⇒ (1 − 270/255) 是负数 ⇒ 该通道被夹到 255，
			// 平顶原样落地（判据 T1 要 0 个 ≥250 像素）。拆开后最大的因子是主圈 150 级，
			// 乘子 1 − 150/255 = 0.412 > 0 ⇒ 盘心落在 255 之下、且离 255 越近抬得越慢。
			kR, kG, kB := 1.0, 1.0, 1.0
			uc := d / Lc
			core, discLv, liftLv, bloomLv := 0.0, 0.0, 0.0, 0.0
			if d <= rad {
				// 小亮核（盘这一组）：峰值乘盘可见度 ⇒ 盘淡出时中间亮核一起淡出
				if disc {
					core = Acore * math.Exp(-uc)
				}
				// 盘/提亮两条量的作用半径上限 = 3.8×rc（提亮的归零半径）。
				// 逐像素先比半径再调用：整张烘焙 518 Kpx 里落在该半径内的只有 34 Kpx（6.6%），
				// 逐像素无条件调用时实测 540×960 烘焙从 17 ms 涨到 39 ms。
				if disc && d <= extraR {
					// 盘体级乘盘可见度（盘淡出时中间亮核一起淡出）
					discLv = SunDiscLevel * vis * SunDiscProfile(d, rc)
					liftLv = SunSkyLift(d, rc)
				}
				// 泛光：半径 6×rc，逐像素一次剖面采样（贴图 alpha 的角向平均）。
				// 峰值随 dim 缩放（与盘体、光斑同向，见函数头注释的缩放清单）。
				if disc && d <= bloomR {
					bloomLv = SunBloomAt(dx, dy, rc) * dim * vis
				}
			}
			w2 := Wl2(d, rad)
			// 光色（口径不动：中心近白 → 外缘暖黄，盘内取 k=1）
			k := math.Exp(-ul)
			if disc && d <= rc {
				k = 1.0
			}
			col := color.NRGBA{
				R: clamp8(255.0),
				G: clamp8(232.0 + 20.0*k),
				B: clamp8(176.0 + 62.0*k),
				A: 0xff,
			}
			// 逐因子连乘（顺序无关，screen 满足交换律/结合律）
			kR, kG, kB = ScreenKeep(kR, kG, kB, main, col)
			kR, kG, kB = ScreenKeep(kR, kG, kB, U, col)
			if d <= rad {
				kR, kG, kB = ScreenKeep(kR, kG, kB, main*w2, col)
				kR, kG, kB = ScreenKeep(kR, kG, kB, core*w2, col)
				kR, kG, kB = ScreenKeep(kR, kG, kB, discLv*w2, col)
				kR, kG, kB = ScreenKeep(kR, kG, kB, liftLv*w2, col)
				kR, kG, kB = ScreenKeep(kR, kG, kB, bloomLv*w2, col)
			}
			if kR >= 1.0 && kG >= 1.0 && kB >= 1.0 {
				continue // 没有任何光要写（旧口径的 a < 0.004 跳过，同一性能口径）
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = clamp8(255.0 - (255.0-float64(dst.Pix[i+0]))*kR)
			dst.Pix[i+1] = clamp8(255.0 - (255.0-float64(dst.Pix[i+1]))*kG)
			dst.Pix[i+2] = clamp8(255.0 - (255.0-float64(dst.Pix[i+2]))*kB)
			dst.Pix[i+3] = 0xff
		}
	}

	// 档间太阳形态：画盘的档 = 星芒 + 小亮核，灰蒙档 = 宽光罩（都不画盘）。
	// 加性叠加（不是 src-over）：贴图 alpha 当强度掩膜，颜色走太阳自己的色带，
	// 与泛光同一口径（贴图 RGB 一个字节都不写进天空纹理）。
	// 星芒贴图（sunrays.png）在屏内的加性贡献整项停用：规格 2.1 把屏内加性光
	// 收敛为"两项解析项 + 一个全天空常数"。星芒是带方向性的贴图掩膜，
	// 它在剖面里留下的正是"斜率突变"（实测沿水平向左射线在 r≈120–160 px 处
	// 斜率变化 5.3 级/20px）。bakeSunRays 与它的掩膜函数保留不删
	// （登记见 out/notes-sunmoon.md）。
	//
	// 灰蒙档（disc=false）同样不再画宽光贴图：屏内的加性光一律由上面的单一连续剖面给出。
	// 该贴图（sunglow.png，宽 1.6 倍烘焙宽）的掩膜在 rim 归零，归零边落在屏内就是
	// 那条看得见的分界线 ⇒ 规格 2.1 要求"屏内像素一律不得由它产生加性光"，
	// 实现上取"直接停用"这一条（bakeSunGlow 保留不删，登记见 out/notes-sunmoon.md）。

	// 有 sun.png（覆盖目录优先，其次内嵌）⇒ 太阳盘改由贴图来画（取证 1.4）。
	// 口径不变的三样：中心 (0.72w, 0.095h)、上面那条 Lorentz 光斑（A=200、L=0.18w）、
	// 以及整条烘焙链（本函数仍只烘进天空纹理）。
	// 贴图叠在光斑之上（src-over），光斑不被擦掉。
	// 盘半径口径 = 0.11w：本文件 bakeSunFromSprite 的历史注释里写明的太阳尺寸口径
	// （半径 0.11w，比月核 0.045w 大一倍多，做出光源分量）—— 树里没有别的太阳尺寸口径。
	// 现状：内嵌素材里没有 sun.png（方形太阳盘落选）⇒ 出厂路径走不到这一支，
	// 盘由上面的程序化画法画。
	// 盘这一组（贴图盘同样在可见度 0 时不画）。
	if disc {
		name := SunOverrideName()
		if name != "" {
			if img, _, _, ok := assets.ImageAny(name); ok {
				sr := 0.11 * float64(w)
				assets.BlitCoverOver(dst, img, image.Rect(
					int(cx-sr), int(cy-sr), int(cx+sr)+1, int(cy+sr)+1))
			}
		}
	}
	return true
}

// addPix 加性叠加：dst += c×k（逐通道钳到 255），alpha 通道保持 0xff。
//
// 与 blendPix 的差别：blendPix 是 src-over（将 dst 向纯色插值，上限就是该色本身），
// 星芒/宽光要的是"在既有天空上再加一档亮度"，src-over 在盘内那种已经 246 级的地方
// 加不出东西（插值上限 246）。加性叠加在饱和处钳到 255，在暗处按掩膜线性抬。
func AddPix(dst *image.NRGBA, x, y int, c color.NRGBA, k float64) {
	if k <= 0 {
		return
	}
	i := dst.PixOffset(x, y)
	dst.Pix[i+0] = clamp8(float64(dst.Pix[i+0]) + float64(c.R)*k)
	dst.Pix[i+1] = clamp8(float64(dst.Pix[i+1]) + float64(c.G)*k)
	dst.Pix[i+2] = clamp8(float64(dst.Pix[i+2]) + float64(c.B)*k)
	dst.Pix[i+3] = 0xff
}

var (
	SunMaskMu   sync.Mutex
	SunMaskProf = map[string][]float64{}
)

// sunRadialMask 返回一张贴图的 **角向平均 alpha 剖面**（索引 = 贴图像素半径，值 0..1，
// 强制单调不升），按贴图名缓存。口径与 sunBloomRadial 逐字同源（同一套理由）：
//
//	实测 lm_circle_a（sunglow/flashglow 的源图）的 alpha 剖面不是单调的：
//	r=0.2..0.3 段 0.81→0.72（内凹），r=0.5..0.7 段回升到 0.90（一圈亮环），
//	r=0.7..0.8 段 0.48→0.01。直接按原图采样 = 在太阳外圈重新造出"一圈"，
//	正是要去掉的那个形状（需求："光线永远只有那一圈"）。
//
// 剖面缺失（素材没落地）⇒ 返回 nil，调用方退回既有的解析画法，不空不崩。
func SunRadialMask(name string) []float64 {
	SunMaskMu.Lock()
	defer SunMaskMu.Unlock()
	if p, ok := SunMaskProf[name]; ok {
		return p
	}
	img := assets.Sprite(name)
	if img == nil {
		SunMaskProf[name] = nil
		return nil
	}
	b := img.Bounds()
	n := b.Dx() / 2
	if n <= 0 {
		SunMaskProf[name] = nil
		return nil
	}
	at := func(x, y int) int {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= b.Dx() {
			x = b.Dx() - 1
		}
		if y >= b.Dy() {
			y = b.Dy() - 1
		}
		return int(img.Pix[img.PixOffset(b.Min.X+x, b.Min.Y+y)+3])
	}
	c := float64(b.Dx()-1) / 2.0
	prof := make([]float64, n+1)
	for r := 0; r <= n; r++ {
		var sum float64
		for k := 0; k < SunBloomAngSteps; k++ {
			th := float64(k) * 2.0 * math.Pi / float64(SunBloomAngSteps)
			sum += float64(at(int(math.Round(c+float64(r)*math.Cos(th))), int(math.Round(c+float64(r)*math.Sin(th)))))
		}
		prof[r] = sum / float64(SunBloomAngSteps) / 255.0
	}
	for i := 1; i <= n; i++ {
		if prof[i] > prof[i-1] {
			prof[i] = prof[i-1]
		}
	}
	SunMaskProf[name] = prof
	return prof
}

// sunMaskAt 掩膜剖面在偏移 (dx,dy) 处的取值：半径按 widthFrac×烘焙宽 的那张罩归一。
func SunMaskAt(prof []float64, dx, dy, widthFrac float64, w int) float64 {
	if len(prof) == 0 {
		return 0
	}
	R := widthFrac * float64(w) * 0.5
	r := math.Hypot(dx, dy)
	if r >= R {
		return 0
	}
	n := float64(len(prof) - 1)
	sr := r / R * n
	i := int(sr)
	if i >= len(prof)-1 {
		return 0
	}
	t := sr - float64(i)
	return prof[i]*(1.0-t) + prof[i+1]*t
}

// sunRaysAt 星芒 + 小亮核在偏移 (dx,dy)（对太阳中心）处的加性级数。
//
// 星芒掩膜 = sunrays.png 的 alpha 本体（星芒是有方向性的形状，不做角向平均），
// 小亮核 = Lorentz（半径 sunCoreLFrac×w、峰值 sunCoreAmp×dim）。整条随 dim 衰减。
// 烘焙与判据共用这一个函数：探针要预测"没有星芒时该有多亮"时必须能算出同一条量，
// 抄一份公式就会漂移（TestNoSunLiftRing 的预测器靠它对齐烘焙）。
// 贴图缺失 ⇒ 0（画面退回纯解析画法）。
func SunRaysAt(dx, dy float64, w int, dim float64) float64 {
	return SunRaysMaskAt(dx, dy, w)*SunRaysAmp*dim + SunCoreAt(dx, dy, w, dim)
}

// sunCoreAt 星芒中心的小亮核 = Lorentz（半径 sunCoreLFrac×w、峰值 sunCoreAmp×dim）。
//
// 它走**主烘焙循环**（进 added、吃 alpha 钳位、铺满整张画布），不走星芒贴图那条矩形：
// 实测把小亮核挂在贴图矩形里，矩形外缘（r = dh/2 = 156.6 px）处 Lorentz 尾巴还有
// 4.0 级，矩形一裁就在相邻两行留下 4.00 级的台阶（剖面判据 ≤3 级）。
// 挂在主循环里就没有这条矩形边界。
func SunCoreAt(dx, dy float64, w int, dim float64) float64 {
	coreL := SunCoreLFrac * float64(w)
	d := math.Hypot(dx, dy)
	return SunCoreAmp * dim / (1.0 + (d/coreL)*(d/coreL))
}

// sunRaysMaskAt 星芒贴图在偏移 (dx,dy) 处的 alpha（0..1）；矩形外 ⇒ 0。
//
// 取样 = **双线性**（不是最近邻）：贴图 512 宽被铺到 0.57×烘焙宽（540 屏上 307 px，
// 缩放 0.60），最近邻会每 1.67 个源像素取一个 ⇒ 源图边缘那条陡坡在屏幕上变成
// 逐像素 0.21 的跳变（实测星芒外缘相邻差 4.67 级 > 3 级判据）。
// GPU 侧的精灵走 Linear 过滤，CPU 烘焙取同一种过滤口径。
func SunRaysMaskAt(dx, dy float64, w int) float64 {
	img := assets.Sprite(SunRaysAsset)
	if img == nil {
		return 0
	}
	b := img.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	dw := SunRaysWidthFrac * float64(w)
	dh := dw * sh / sw
	if math.Abs(dx) > dw/2 || math.Abs(dy) > dh/2 {
		return 0
	}
	fx := (dx/dw + 0.5) * sw
	fy := (dy/dh + 0.5) * sh
	// 矩形边缘收口：星芒的竖直长芒一直铺到贴图下缘（实测下缘 alpha 非 0），
	// 直接按矩形裁 = 在 r=dh/2 处留下 4.67 级的硬台阶（判据 ≤3 级）。
	// 外 30% 用 cos² 收到 0：u=1 处函数值与导数同时为 0 ⇒ 没有台阶也没有折角。
	u := math.Abs(dx) / (dw / 2)
	if v := math.Abs(dy) / (dh / 2); v > u {
		u = v
	}
	if u >= 1.0 {
		return 0
	}
	taper := 1.0
	if u > 0.7 {
		c := math.Cos((u - 0.7) / 0.3 * math.Pi * 0.5)
		taper = c * c
	}
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx, ty := fx-float64(x0), fy-float64(y0)
	at := func(x, y int) float64 {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x > int(sw)-1 {
			x = int(sw) - 1
		}
		if y > int(sh)-1 {
			y = int(sh) - 1
		}
		return float64(img.Pix[img.PixOffset(b.Min.X+x, b.Min.Y+y)+3]) / 255.0
	}
	a00, a10 := at(x0, y0), at(x0+1, y0)
	a01, a11 := at(x0, y0+1), at(x0+1, y0+1)
	return ((a00*(1-tx)+a10*tx)*(1-ty) + (a01*(1-tx)+a11*tx)*ty) * taper
}

// bakeSunRays 晴 / 多云见日档的星芒：sunrays.png 在太阳位置加性绘制，
// 宽 0.57 倍烘焙宽，中心叠一个小亮核。逐像素量走 sunRaysAt（判据同源）。
// 贴图缺失 ⇒ 直接返回（画面退回纯解析画法）。
func BakeSunRays(dst *image.NRGBA, w, h int, cx, cy, dim float64) {
	img := assets.Sprite(SunRaysAsset)
	if img == nil {
		return
	}
	b := img.Bounds()
	dw := SunRaysWidthFrac * float64(w)
	dh := dw * float64(b.Dy()) / float64(b.Dx())
	x0 := int(math.Max(0, math.Floor(cx-dw/2)))
	x1 := int(math.Min(float64(w), math.Ceil(cx+dw/2)+1))
	y0 := int(math.Max(0, math.Floor(cy-dh/2)))
	y1 := int(math.Min(float64(h), math.Ceil(cy+dh/2)+1))
	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			m := SunRaysMaskAt(dx, dy, w)
			k := m * SunRaysAmp * dim / 255.0
			if k <= 0.002 {
				continue
			}
			col := color.NRGBA{R: 255, G: clamp8(236.0 + 16.0*m), B: clamp8(196.0 + 52.0*m), A: 0xff}
			AddPix(dst, x, y, col, k)
		}
	}
}

// sunAdditiveAt 盘外加性铺光在距盘心 d 处的级数 = 单一连续剖面（规格 2.1）。
//
//	lift(d) = A·exp(−d/L) + Acore·exp(−d/Lc) + U
//	A     = 150·dim,  L  = 0.90 × 烘焙宽
//	Acore = 120·dim·vis, Lc = 0.06 × 烘焙宽
//	U     =  12·dim（全天空常数）
//
// 三项都铺到整张画布、都不做归零截断：铺光里任何一处"归零"都会在归零点两侧留下
// 一条可见边界（需求：阳光总是存在边界，不是逐渐降低到与天空同色），
// 所以加性光的掩膜只能是这条解析曲线本身。
// dim 夹在 [sunDimFloor,1] ⇒ 云量 100 的灰蒙档仍有铺光。
// 烘焙与判据共用这一个函数（抄一份公式就会漂移）。
func SunAdditiveAt(d float64, w int, dim float64, vis float64) float64 {
	if dim < SunDimFloor {
		dim = SunDimFloor
	}
	L := SunMainLFrac * float64(w)
	Lc := SunCoreExpLFrac * float64(w)
	return SunMainAmp*dim*math.Exp(-d/L) +
		SunCoreExpAmp*dim*vis*math.Exp(-d/Lc) +
		SunSkyUniform*dim*SunSkyScale
}

// sunGlowAt 灰蒙档铺光在偏移 (dx,dy) 处的加性级数。
//
// 口径变更（写明理由）：旧口径 = sunglow.png 的角向平均掩膜 × sunGlowAmp × dim，
// 该掩膜在贴图 rim 处**归零**，归零点落在屏内 ⇒ 光强一步落到天空底色 = 肉眼看到的圆边界。
// 现口径 = 单一连续剖面（见 sunAdditiveAt），屏内没有任何归零截断，
// 灰蒙档不画盘 ⇒ vis=0 ⇒ 小亮核那一项为 0。
func SunGlowAt(dx, dy float64, w int, dim float64) float64 {
	return SunAdditiveAt(math.Hypot(dx, dy), w, dim, 0)
}

// bakeSunGlow 灰蒙档（云量 ≥85：阴 / 雨 / 雪 / 雾 / 尘）的宽光软形状底：
// sunglow.png 加性绘制，宽 1.6 倍烘焙宽（rim = 0.8 倍烘焙宽，落在屏外），中心 = 太阳位置。
//
// **本批起停用**（规格 2.1：删掉宽光贴图在屏内的加性贡献，屏内像素一律不得由它产生
// 加性光）：bakeSunAnalytic 不再调用本函数，盘与光全用解析项。
// 函数与它用的掩膜（sunRadialMask/sunMaskAt）保留不删，登记见 out/notes-sunmoon.md。
//
// 两条分工（停用前的口径，写明理由）：
//
//	屏内的加性光量 = 主烘焙循环里的解析剖面（sunGlowAt 给的就是这条量）；
//	本函数只往上叠"形状"（贴图掩膜给的软起伏），幅度 sunGlowAmp×dim，
//	它的 rim 在屏外，屏内看不到归零截断。
//
// 旧口径把这张掩膜当加性光的掩膜（宽 0.93 倍烘焙宽，rim = 0.465w 落在屏内），
// 于是 0.465w 处光强一步落到天空底色 = 需求里那条"阳光的边界"。
// 贴图缺失 ⇒ 直接返回（画面退回纯解析宽光，屏内光量不受影响）。
func BakeSunGlow(dst *image.NRGBA, w, h int, cx, cy, dim float64) {
	prof := SunRadialMask(SunGlowAsset)
	if len(prof) == 0 {
		return
	}
	half := SunGlowWidthFrac * float64(w) / 2
	x0 := int(math.Max(0, math.Floor(cx-half)))
	x1 := int(math.Min(float64(w), math.Ceil(cx+half)+1))
	y0 := int(math.Max(0, math.Floor(cy-half)))
	y1 := int(math.Min(float64(h), math.Ceil(cy+half)+1))
	for y := y0; y < y1; y++ {
		dy := float64(y) - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) - cx
			m := SunMaskAt(prof, dx, dy, SunGlowWidthFrac, w)
			k := m * SunGlowAmp * dim / 255.0
			if k <= 0.002 {
				continue
			}
			col := color.NRGBA{R: 255, G: clamp8(234.0 + 18.0*m), B: clamp8(198.0 + 50.0*m), A: 0xff}
			AddPix(dst, x, y, col, k)
		}
	}
}

// RenderSkyForProbe 导出天空烘焙层（含烘焙天体），供离线判读用。
// 与低性能 x86 小主机同源：走同一个 skyCacheScale 口径，保证判读对象就是上线画面。
// 月相传 nil ⇒ 不画月亮（与新月一致，规格 2.3；判读口径与无月相数据时一致）。
// cloudPct = 云量（0–100），决定白天太阳的可见度。
func RenderSkyForProbe(w, h int, cls effect.Class, isDay bool, cloudPct int) *image.NRGBA {
	return RenderSkyCPU(w/SkyCacheScale, h/SkyCacheScale, cls, isDay, nil, cloudPct)
}

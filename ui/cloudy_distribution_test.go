package ui

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
	"github.com/DragonTTDream/WeatherScr/ui/sky"
)

// TestCloudYDistribution 打印云的真实落位与"云盘真正压到月盘"时长占比。
// 覆盖测试原先 padX 取的是光晕半径 0.192（月盘只有 0.06） ⇒ 矩形虚大、占比虚高。
func TestCloudYDistribution(t *testing.T) {
	f := NewField(effect.Cloudy, false, 20261015)
	t.Logf("Step 之前：")
	for i := range f.parts {
		if f.parts[i].kind == pkCloud {
			t.Logf("  pre  x=%.3f y=%.3f vy=%.4f", f.parts[i].x, f.parts[i].y, f.parts[i].vy)
		}
	}
	f.Step(time.Second, PortraitW, PortraitH)
	f.adaptFps(15)
	t.Logf("Step 之后：")
	for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
		p := &f.parts[i]
		if p.kind == pkCloud {
			t.Logf("  x=%.3f y=%.3f sc=%.2f alpha=%.2f", p.x, p.y, p.sc, p.alpha)
		}
	}
	// 月盘（不含光晕）：半径 0.06w，归一化 y 半径 = 0.06*W/H
	dR := sky.MoonRcFrac
	dRy := sky.MoonRcFrac * float64(PortraitW) / float64(PortraitH)
	// 云盘半高（归一化）：贴图 127 px × sc × (0.55+0.45·depth)
	hit := 0
	frames := 0
	for sec := 1; sec <= 900; sec++ {
		f.Step(time.Second, PortraitW, PortraitH)
		f.adaptFps(15)
		frames++
		for i := 0; i < f.ActiveCount() && i < len(f.parts); i++ {
			p := &f.parts[i]
			if p.kind != pkCloud {
				continue
			}
			// **真实尺寸口径**：直接取绘制同口径的半宽/半高（airHalfN），
			// 与 p.sc 无关（实测 p.sc=12 零变化：云走 spriteScale 的净倍率常量）。
			hw, hh := f.airHalfN(p)
			if p.x+hw > 0.72-dR && p.x-hw < 0.72+dR && p.y+hh > 0.095-dRy && p.y-hh < 0.095+dRy {
				hit++
				break
			}
		}
	}
	t.Logf("云盘真正压到月盘（含云身半高）：%d/%d 帧 = %.1f%%", hit, frames, 100*float64(hit)/float64(frames))
}

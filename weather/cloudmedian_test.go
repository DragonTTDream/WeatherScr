package weather

import (
	"testing"
	"time"

	"github.com/DragonTTDream/WeatherScr/effect"
)

// iv 便捷构造 *int。
func iv(v int) *int { return &v }

// TestCloudMedianClass 三模型云量仲裁的档位判据。
// 逐条打印实测的中位数与档位，判据不放宽：
//
//	(100, 62, 56) ⇒ 中位数 62 ⇒ PartlyCloudy（默认单源 best_match 云量 100 会判成阴）
//	(100,100, 94) ⇒ 中位数 100 ⇒ Overcast
//	( 20, 18, 10) ⇒ 中位数 18 ⇒ Clear
//	缺一个模型 ⇒ 用剩下两家（中位数随样本数变，不是拿两家最大值顶）
//	降水类 ⇒ 云量不得覆盖（61 号码 100% 云量仍在下雨）
//	三家全缺 ⇒ 退回天气码判定
func TestCloudMedianClass(t *testing.T) {
	cases := []struct {
		name       string
		code       int
		gfs        *int
		icon       *int
		ecmwf      *int
		wantMedian *int
		want       effect.Class
	}{
		{"实测那一点 12:45", 3, iv(100), iv(62), iv(56), iv(62), effect.PartlyCloudy},
		{"三家全阴", 3, iv(100), iv(100), iv(94), iv(100), effect.Overcast},
		{"三家全晴", 1, iv(20), iv(18), iv(10), iv(18), effect.Clear},
		{"缺 ecmwf ⇒ 用 gfs/icon 两家", 3, iv(100), iv(62), nil, iv(81), effect.Overcast},
		{"缺 icon ⇒ 用 gfs/ecmwf 两家", 3, iv(100), nil, iv(56), iv(78), effect.Overcast},
		{"缺 gfs ⇒ 用 icon/ecmwf 两家", 3, nil, iv(62), iv(56), iv(59), effect.PartlyCloudy},
		{"两家 30 边界", 0, iv(30), iv(30), nil, iv(30), effect.PartlyCloudy},
		{"两家 70 边界", 0, iv(70), iv(70), nil, iv(70), effect.PartlyCloudy},
		{"71 ⇒ 阴", 0, iv(71), iv(71), iv(71), iv(71), effect.Overcast},
		{"29 ⇒ 晴", 0, iv(29), iv(29), iv(29), iv(29), effect.Clear},
		{"降水类不被云量覆盖（雨）", 61, iv(100), iv(100), iv(100), iv(100), effect.Rain},
		{"降水类不被云量覆盖（雷）", 95, iv(12), iv(10), iv(8), iv(10), effect.Thunder},
		{"降水类不被云量覆盖（雪）", 71, iv(100), iv(100), iv(100), iv(100), effect.Snow},
		{"雾类不被云量覆盖", 45, iv(100), iv(100), iv(100), iv(100), effect.Fog},
		{"三家全缺 ⇒ 退回天气码（3=阴）", 3, nil, nil, nil, nil, effect.Overcast},
		{"三家全缺 ⇒ 退回天气码（1=晴）", 1, nil, nil, nil, nil, effect.Clear},
	}
	for _, c := range cases {
		got := MedianCloud(c.gfs, c.icon, c.ecmwf)
		if (got == nil) != (c.wantMedian == nil) {
			t.Errorf("%s：中位数 = %v，期望 %v", c.name, derefI(got), derefI(c.wantMedian))
		} else if got != nil && *got != *c.wantMedian {
			t.Errorf("%s：中位数 = %d，期望 %d", c.name, *got, *c.wantMedian)
		}
		cls, _ := ClassFromCloud(c.code, got)
		t.Logf("%-30s 码=%d 三家=%v/%v/%v ⇒ 中位数=%v ⇒ %-13s（期望 %s）",
			c.name, c.code, derefI(c.gfs), derefI(c.icon), derefI(c.ecmwf),
			derefI(got), cls, c.want)
		if cls != c.want {
			t.Errorf("%s：档位 = %s，期望 %s", c.name, cls, c.want)
		}
	}
}

func derefI(p *int) any {
	if p == nil {
		return "nil"
	}
	return *p
}

// TestMedianFromModelsIndex 逐模型数组按下标取值（含下标越界与长度不齐的缺失口径）。
func TestMedianFromModelsIndex(t *testing.T) {
	gfs := []int{100, 90, 80}
	icon := []int{62, 60, 58}
	ecmwf := []int{56, 54}
	if m := MedianFromModels(gfs, icon, ecmwf, 0); m == nil || *m != 62 {
		t.Errorf("下标 0：中位数 = %v，期望 62（100/62/56 三家取中）", derefI(m))
	}
	if m := MedianFromModels(gfs, icon, ecmwf, 1); m == nil || *m != 60 {
		t.Errorf("下标 1：中位数 = %v，期望 60（90/60/54 三家取中）", derefI(m))
	}
	if m := MedianFromModels(gfs, icon, ecmwf, 2); m == nil || *m != 69 {
		t.Errorf("下标 2（ecmwf 越界 ⇒ 按 80/58 两家算）：中位数 = %v，期望 69", derefI(m))
	}
	if m := MedianFromModels(gfs, icon, ecmwf, 7); m != nil {
		t.Errorf("下标越界应全缺 ⇒ 实得 %v", derefI(m))
	}
	if m := MedianFromModels(nil, nil, nil, 0); m != nil {
		t.Errorf("三条数组都缺 ⇒ 应 nil，实得 %v", derefI(m))
	}
}

// TestCurrentHourIndex 当前小时在 hourly.time 里的下标（当地墙钟 + utc_offset_seconds）。
func TestCurrentHourIndex(t *testing.T) {
	times := []string{"2024-05-20T10:00", "2024-05-20T11:00", "2024-05-20T12:00"}
	now := time.Date(2024, 5, 20, 4, 30, 0, 0, time.UTC) // UTC 04:30 + 8h = 当地 12:30
	if got := CurrentHourIndex(times, 8*3600, now); got != 2 {
		t.Errorf("下标 = %d，期望 2（当地 12:00 那一条）", got)
	}
	if got := CurrentHourIndex(times, 0, now); got != -1 {
		t.Errorf("偏移 0（当地 04:00 不在表里）⇒ 应 -1，实得 %d", got)
	}
	if got := CurrentHourIndex(nil, 8*3600, now); got != -1 {
		t.Errorf("空数组 ⇒ 应 -1，实得 %d", got)
	}
}

package weather

import (
	"math"
	"testing"
)

// TestMoonPhaseFetchProbe 直查月相解析链：用出厂参数集打真实 API，
// 打印 Sample.MoonPhase，判明缓存里 moon_phase=null 出在哪一段。
func TestMoonPhaseFetchProbe(t *testing.T) {
	c := &Client{}
	c.HasCoords = true
	c.Lat, c.Lon = 30.2937, 120.1602
	s, err := c.Fetch("Hangzhou, China")
	if err != nil {
		t.Fatalf("FETCH_FAIL %v", err)
	}
	if s.MoonPhase == nil {
		t.Logf("MOONPHASE=nil  <== 解析链没填上")
	} else {
		t.Logf("MOONPHASE=%.4f illum=%.3f", *s.MoonPhase, (1-math.Cos(2*math.Pi*(*s.MoonPhase)))/2)
	}
	t.Logf("SAMPLE code=%d cloudpct=%d cloudmedian=%d isday=%v", s.WeatherCode, s.CloudCoverPct, s.CloudMedian, s.IsDay)
}

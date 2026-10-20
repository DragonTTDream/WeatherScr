package weather

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestMoonRawProbe 打一次出厂参数集，直接看响应里 daily 有哪些键。
func TestMoonRawProbe(t *testing.T) {
	v := RequestParams(30.2937, 120.1602)
	u := "https://api.open-meteo.com/v1/forecast?" + v.Encode()
	t.Logf("URL=%s", u)
	cl := &http.Client{Timeout: 15 * time.Second}
	r, err := cl.Get(u)
	if err != nil {
		t.Fatalf("GET_FAIL %v", err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	t.Logf("STATUS=%d BYTES=%d", r.StatusCode, len(b))
	var j map[string]any
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatalf("JSON_FAIL %v raw=%.300s", err, b)
	}
	d, _ := j["daily"].(map[string]any)
	t.Logf("DAILY_KEYS=%v", probeMapKeys(d))
	t.Logf("DAILY_MOON=%v", d["moon_phase"])
	h, _ := j["hourly"].(map[string]any)
	t.Logf("HOURLY_KEYS=%v", probeMapKeys(h))
}

func probeMapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

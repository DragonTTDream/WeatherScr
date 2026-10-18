package effect

import "testing"

// TestClassifyByCloudCover 云量分档口径（定案）。
func TestClassifyByCloudCover(t *testing.T) {
	iv := func(v int) *int { return &v }
	cases := []struct {
		code int
		cc   *int
		want Class
		why  string
	}{
		{1, iv(26), Clear, "杭州实测点：码1 云量26% ⇒ 晴"},
		{1, iv(30), Cloudy, "30% 起给云粒子（分档：30–60 少云）"},
		{0, iv(55), Cloudy, "码0 但云量 55% ⇒ 多云"},
		{0, iv(92), Cloudy, "码0 但云量 92% ⇒ 阴"},
		{0, iv(12), Clear, "码0 云量 12% ⇒ 晴"},
		{1, nil, Clear, "云量缺失 ⇒ 退回 WMO 码，不猜"},
		{61, iv(95), Rain, "降水类由码决定，云量不改它（95% 云量不改变'在下雨'）"},
		{95, iv(95), Thunder, "雷暴由码决定"},
		{71, iv(95), Snow, "雪由码决定"},
		{45, iv(95), Fog, "雾由码决定"},
	}
	for _, c := range cases {
		got, _ := ClassifyByCloudCover(c.code, c.cc)
		if got != c.want {
			t.Errorf("码=%d 云量=%v 期望 %s 实得 %s（%s）", c.code, c.cc, c.want, got, c.why)
		}
	}
}

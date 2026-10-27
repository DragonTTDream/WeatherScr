package main

import "testing"

// 钉死单测（'钉死单测'1..4 条，逐条对应下面的测试函数）。
// 数值全部逐字抄自 foreground_windows.go 现有定义与实机稳态样本，不许换算。
// 口径说明（写在这里以免读者按括注反推）：spec 第 24 行的钉死值是
// `ExStyleDrift(0x00000088, 0x00000088)==false` / `ExStyleDrift(0x00000008, 0x00000088)==true`。
// 后一条 cur=0x00000008 已含 WS_EX_TOPMOST，缺的是 WS_EX_TOOLWINDOW 位；
// 判定函数按 spec 要求 2 的口径实现 = "期望位（want 掩码）不齐即为漂移"，
// 故两条钉死值同时成立。函数签名与两条期望返回值都逐字照抄，未放宽。

// wantStyle / wantEx = saverStyle(dbg=false) 的正常态期望位
// （foreground_windows.go:193-199：style = wsPopup|wsVisible，
// 非 dbg 时 exstyle = wsExToolwindow|wsExTopmost）。
func a3WantNormal() (uintptr, uintptr) {
	return uintptr(wsPopup | wsVisible), uintptr(coverExToolwindow | coverExTopmost)
}

// 钉死 1：实机稳态 exstyle 不漂移；缺期望位的 exstyle 漂移。
// 0x00000088 = 的 `COVER=…EXSTYLE=00000088`。
func TestA3ExStyleDriftPinned(t *testing.T) {
	if ExStyleDrift(0x00000088, 0x00000088) {
		t.Errorf("ExStyleDrift(0x00000088, 0x00000088) = true，期望 false（实机稳态位必须判稳态）")
	}
	if !ExStyleDrift(0x00000008, 0x00000088) {
		t.Errorf("ExStyleDrift(0x00000008, 0x00000088) = false，期望 true（期望位不齐 ⇒ 漂移）")
	}
	// topmost 分支实际传的掩码就是 coverExTopmost：缺 topmost 位必须判漂移。
	if !ExStyleDrift(uintptr(coverExToolwindow), uintptr(coverExTopmost)) {
		t.Errorf("缺 WS_EX_TOPMOST 位必须判漂移（cur=0x%08x want=0x%08x）",
			coverExToolwindow, coverExTopmost)
	}
	if ExStyleDrift(uintptr(coverExTopmost|coverExToolwindow), uintptr(coverExTopmost)) {
		t.Errorf("topmost 位在册时不得判漂移（稳态不得调 SetWindowPos）")
	}
}

// 钉死 2：style 含 WS_VISIBLE 且 exstyle 含期望位 ⇒ false；缺 WS_VISIBLE ⇒ true。
func TestA3StyleDriftPinned(t *testing.T) {
	wantStyle, wantEx := a3WantNormal()
	if StyleDrift(wantStyle, wantStyle, wantEx, wantEx) {
		t.Errorf("稳态样式 style=0x%08x exstyle=0x%08x 判成漂移，期望 false", wantStyle, wantEx)
	}
	if wantStyle&wsVisible == 0 {
		t.Fatalf("期望位里必须含 WS_VISIBLE（wsVisible=0x%08x，抄自 covermath.go:15）", wsVisible)
	}
	if !StyleDrift(wantStyle&^wsVisible, wantStyle, wantEx, wantEx) {
		t.Errorf("缺 WS_VISIBLE 的 style=0x%08x 未判漂移，期望 true", wantStyle&^wsVisible)
	}
	// exstyle 侧同理：缺 topmost ⇒ 漂移（gioui 抹掉 topmost 的实测形态）。
	if !StyleDrift(wantStyle, wantStyle, wantEx&^coverExTopmost, wantEx) {
		t.Errorf("缺 WS_EX_TOPMOST 的 exstyle=0x%08x 未判漂移，期望 true", wantEx&^coverExTopmost)
	}
}

// 钉死 3：稳态 100 次判定（同一对 cur/want）⇒ 漂移次数 = 0。
// 这是"稳态零 API 调用"的可测代理判据：漂移次数 0 ⇔ coverLoop 每轮都跳过
// applySaverStyle 与 SetWindowPos 两条分支。
func TestA3SteadyState100RoundsZeroDrift(t *testing.T) {
	wantStyle, wantEx := a3WantNormal()
	curStyle, curEx := wantStyle, wantEx // 稳态：实机 exstyle 即 0x00000088
	if curEx != 0x00000088 {
		t.Fatalf("稳态期望 exstyle = 0x%08x，必须与实机样本 0x00000088 逐字一致", curEx)
	}
	drifts := 0
	for i := 0; i < 100; i++ {
		if StyleDrift(curStyle, wantStyle, curEx, wantEx) {
			drifts++
		}
		if ExStyleDrift(curEx, uintptr(coverExTopmost)) {
			drifts++
		}
	}
	if drifts != 0 {
		t.Errorf("稳态 100 次判定漂移次数 = %d，期望 0（等价稳态零 API 调用）", drifts)
	}
}

// 钉死 4：期望位常量逐字钉死（值来自 foreground_windows.go 现有定义，逐字抄）。
func TestA3ExpectedBitsVerbatim(t *testing.T) {
	pins := []struct {
		name string
		got  uint64
		want uint64
		src  string
	}{
		{"coverExTopmost", uint64(coverExTopmost), 0x00000008, "foreground_windows.go:100 wsExTopmost"},
		{"coverExToolwindow", uint64(coverExToolwindow), 0x00000080, "foreground_windows.go:101 wsExToolwindow"},
		{"coverExAppwindowForbidden", uint64(coverExAppwindowForbidden), 0x00040000, "foreground_windows.go:102 wsExAppwindow"},
		{"coverStyleOverlappedForbidden", uint64(coverStyleOverlappedForbidden), 0x00CF0000, "foreground_windows.go:122 wsOverlappedwindow"},
		{"wsPopup", uint64(wsPopup), 0x80000000, "covermath.go:14 wsPopup"},
		{"wsVisible", uint64(wsVisible), 0x10000000, "covermath.go:15 wsVisible"},
	}
	for _, p := range pins {
		if p.got != p.want {
			t.Errorf("%s = 0x%08X，期望逐字值 0x%08X（出处 %s）", p.name, p.got, p.want, p.src)
		}
	}
	// 实机稳态 exstyle 逐字对齐：WS_EX_TOPMOST|WS_EX_TOOLWINDOW == 0x00000088。
	if got := uint64(coverExTopmost | coverExToolwindow); got != 0x00000088 {
		t.Errorf("稳态期望 exstyle = 0x%08X，期望逐字 0x00000088（实机 COVER=…EXSTYLE=00000088）", got)
	}
	// 禁止位口径钉死：铺满目标样式里不得含 WS_OVERLAPPEDWINDOW 位。
	if coverStyleTarget()&coverStyleOverlappedForbidden != 0 {
		t.Errorf("coverStyleTarget = 0x%08X 含 WS_OVERLAPPEDWINDOW 位", coverStyleTarget())
	}
}

// 附加（不放宽上面四条）：漂移判定的触发面 —— gioui 实测抹样式后的形态必须判漂移。
func TestA3DriftDetectsGiouiTamper(t *testing.T) {
	wantStyle, wantEx := a3WantNormal()
	// coverLoop:471-473 的实测样本：STYLE=17000000 EXSTYLE=00040000（无 topmost/toolwindow，含 APPWINDOW）
	if !StyleDrift(0x17000000, wantStyle, 0x00040000, wantEx) {
		t.Errorf("gioui 实测抹样式形态 (STYLE=17000000, EXSTYLE=00040000) 未判漂移")
	}
	if !ExStyleDrift(0x00040000, uintptr(coverExTopmost)) {
		t.Errorf("EXSTYLE=00040000 缺 topmost，ExStyleDrift 必须判漂移")
	}
	// 只补回 APPWINDOW（topmost/toolwindow 在册）也判漂移：禁止位口径。
	if !StyleDrift(wantStyle, wantStyle, wantEx|coverExAppwindowForbidden, wantEx) {
		t.Errorf("exstyle 带 WS_EX_APPWINDOW 时未判漂移（项目口径：该位必须清掉）")
	}
	if !StyleDrift(wantStyle|coverStyleOverlappedForbidden, wantStyle, wantEx, wantEx) {
		t.Errorf("style 带 WS_OVERLAPPEDWINDOW 位时未判漂移（闸门口径：该组位必须为 0）")
	}
}

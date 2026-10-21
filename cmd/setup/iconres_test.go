package main

import "testing"

// TestIconSizes 表驱动：GetSystemMetrics(SM_CXSMICON) 的各种返回值 ⇒ 两次 LoadImageW
// 要的边长（判据来自 spec/publish-window-icons-brief.md §1「hIcon 32 px、hIconSm SM_CXSMICON px」）。
func TestIconSizes(t *testing.T) {
	tests := []struct {
		name      string
		cxSmIcon  int32
		wantBig   int32
		wantSmall int32
	}{
		{"标准 16", 16, 32, 16},
		{"高 DPI 24", 24, 32, 24},
		{"高 DPI 32", 32, 32, 32},
		{"度量大于 32 时夹到 32", 48, 32, 32},
		{"度量 0 回落 16", 0, 32, 16},
		{"度量负数回落 16", -1, 32, 16},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			big, small := iconSizes(tc.cxSmIcon)
			if big != tc.wantBig || small != tc.wantSmall {
				t.Errorf("iconSizes(%d) = (%d, %d)，期望 (%d, %d)", tc.cxSmIcon, big, small, tc.wantBig, tc.wantSmall)
			}
		})
	}
}

// TestPickIconResourceID 表驱动：图标组 ID 候选的择一（判据 = brief §2 的 ID 1 +
// 实测 cmd/setup 的 syso 里 RT_GROUP_ICON 落在 ID 2，见 out/notes-window-icons.md）。
func TestPickIconResourceID(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int
		handles  []uintptr
		wantID   int
		wantHand uintptr
		wantOK   bool
	}{
		{"ID 1 命中", []int{1, 2}, []uintptr{0xABCD, 0}, 1, 0xABCD, true},
		{"ID 1 取不到 ⇒ 用 ID 2", []int{1, 2}, []uintptr{0, 0xABCD}, 2, 0xABCD, true},
		{"两路都取不到 ⇒ 无命中", []int{1, 2}, []uintptr{0, 0}, 0, 0, false},
		{"句柄少于候选 ⇒ 不越界", []int{1, 2}, []uintptr{0}, 0, 0, false},
		{"空候选 ⇒ 无命中", nil, nil, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, hand, ok := pickIconResourceID(tc.ids, tc.handles)
			if id != tc.wantID || hand != tc.wantHand || ok != tc.wantOK {
				t.Errorf("pickIconResourceID(%v, %#x) = (%d, %#x, %v)，期望 (%d, %#x, %v)",
					tc.ids, tc.handles, id, hand, ok, tc.wantID, tc.wantHand, tc.wantOK)
			}
		})
	}
}

// TestIconResourceIDs 锁住候选序列本身：第 1 候选必须是 brief §2 的 1。
func TestIconResourceIDs(t *testing.T) {
	if len(iconResourceIDs) == 0 || iconResourceIDs[0] != iconResourceID {
		t.Errorf("iconResourceIDs = %v，第 1 候选必须是 brief §2 的 %d", iconResourceIDs, iconResourceID)
	}
}

// TestIconHandles 表驱动：兜底选择（判据来自 brief §2「LoadImageW 返回 0 时不得 panic，
// 走 LoadIconW(0, IDI_APPLICATION) 兜底」）。
func TestIconHandles(t *testing.T) {
	const fb = uintptr(idiApplication) // 兜底句柄哨兵：LoadIconW(0, IDI_APPLICATION) 成功时返回的非 0 值
	tests := []struct {
		name             string
		big, small       uintptr
		fallback         uintptr
		wantIcon         uintptr
		wantIconSm       uintptr
		wantUsedFallback bool
	}{
		{"两路都取到 ⇒ 原样用", 0x1001, 0x2002, fb, 0x1001, 0x2002, false},
		{"大图标失败 ⇒ 兜底，小图标照用", 0, 0x2002, fb, fb, 0x2002, true},
		{"小图标失败 ⇒ 退用大图标句柄", 0x1001, 0, fb, 0x1001, 0x1001, true},
		{"两路都失败 ⇒ 双双兜底", 0, 0, fb, fb, fb, true},
		{"连兜底也失败 ⇒ 填 0，不 panic", 0, 0, 0, 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			icon, iconSm, used := iconHandles(tc.big, tc.small, tc.fallback)
			if icon != tc.wantIcon || iconSm != tc.wantIconSm {
				t.Errorf("iconHandles(%#x, %#x, %#x) = (%#x, %#x)，期望 (%#x, %#x)",
					tc.big, tc.small, tc.fallback, icon, iconSm, tc.wantIcon, tc.wantIconSm)
			}
			if used != tc.wantUsedFallback {
				t.Errorf("iconHandles(%#x, %#x, %#x) 的 usedFallback = %v，期望 %v",
					tc.big, tc.small, tc.fallback, used, tc.wantUsedFallback)
			}
		})
	}
}

// TestIconConstants 锁住 brief §2 逐字给出的常量，防止改名/改值时悄悄跑偏。
func TestIconConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"RT_GROUP_ICON 资源 ID（rsrc -ico 嵌的就是它）", iconResourceID, 1},
		{"IMAGE_ICON", imageIcon, 3},
		{"LR_SHARED", lrShared, 0x00008000},
		{"SM_CXICON", smCxIcon, 0},
		{"SM_CXSMICON", smCxSmIcon, 19},
		{"IDI_APPLICATION", idiApplication, 0x7F00},
		{"hIcon 边长（brief §1）", wizardIconPx, 32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %#x，期望 %#x", tc.name, tc.got, tc.want)
			}
		})
	}
}

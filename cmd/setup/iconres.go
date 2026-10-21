package main

// 标题栏图标的**纯逻辑**部分：资源 ID 候选、尺寸换算、兜底选择。
//
// 本文件**不带 build tag** ⇒ Linux 侧 `go test ./cmd/setup` 会编译并跑到它（main.go 是
// //go:build windows，Linux 下不参与编译）；配套表驱动单测见 iconres_test.go。
// 真正的 Win32 调用在 main.go 的 wizardIcons()（windows 侧）。
//
// 证据：spec/publish-window-icons.md §0 —— cmd/setup/main.go 注册窗口类处从未给
// wndClassExW.hIcon / hIconSm 赋值（全文件原本无 LoadIconW/LoadImageW）⇒ 标题栏与
// 任务栏显示系统通用图标。常量与签名逐字取自 spec/publish-window-icons-brief.md §2。

const (
	// brief §2：`rsrc -ico` 嵌进 .syso 的是 RT_GROUP_ICON，ID = 1。
	// 实测（out/notes-window-icons.md 的 PE 资源目录解析）：
	//   cmd/scr/rsrc_windows_amd64.syso（只嵌图标）⇒ RT_GROUP_ICON ID = 1，与 brief 一致；
	//   cmd/setup/rsrc_windows_amd64.syso（图标 + manifest）⇒ RT_GROUP_ICON ID = 2，
	//   因为 rsrc 把 manifest 排成 ID 1、图标组顺延（rsrc@v0.10.2 rsrc/rsrc.go:31-56 的 newid 顺序）。
	iconResourceID = 1

	// LoadImageW 的 uType = IMAGE_ICON（brief §2 参数序第 3 项）。
	imageIcon = 3
	// LR_SHARED：句柄归系统共享，不得 DestroyIcon（brief §2 参数序第 6 项）。
	lrShared = 0x00008000

	// GetSystemMetrics 索引，brief §2 逐字：SM_CXICON=0、SM_CXSMICON=19。
	smCxIcon   = 0
	smCxSmIcon = 19

	// LoadIconW(0, IDI_APPLICATION) 兜底（brief §2：LoadImageW 返回 0 时不得 panic）。
	idiApplication = 0x7F00

	// brief §1：wc.hIcon 固定按 32 px 取。
	wizardIconPx = 32
	// SM_CXSMICON 返回 0 时的小图标边长。
	// TODO(未取证)：低 DPI / 主题缩放下 SM_CXSMICON 返回 0 的实际场景未取证，
	// 这里按 Windows 传统小图标默认边长 16 兜底（spec 未覆盖该分支）。
	defaultSmallIconPx = 16
)

// iconResourceIDs = 取图标时按顺序尝试的 RT_GROUP_ICON 资源 ID。
// 第 1 候选 = brief §2 点名的 1（实测 cmd/scr 的 syso 就是它）；
// 第 2 候选 = 2，实测 cmd/setup 的 syso 里图标组落在 2（manifest 占了 1）。
// 两条都实测过，不是猜的；解析输出见 out/notes-window-icons.md。
var iconResourceIDs = []int{iconResourceID, iconResourceID + 1}

// pickIconResourceID 从「候选 ID + 各自 LoadImageW 返回的句柄」里挑第一个取到的。
// 纯函数：只吃已取到的句柄，不碰 Win32 ⇒ 可表驱动单测（TestPickIconResourceID）。
// 全 0 时返回 ok=false（调用方继续走 LoadIconW 兜底，绝不 panic）。
func pickIconResourceID(ids []int, handles []uintptr) (int, uintptr, bool) {
	n := len(ids)
	if len(handles) < n {
		n = len(handles)
	}
	for i := 0; i < n; i++ {
		if handles[i] != 0 {
			return ids[i], handles[i], true
		}
	}
	return 0, 0, false
}

// iconSizes 把 GetSystemMetrics(SM_CXSMICON) 的返回值换算成两次 LoadImageW 要的像素边长。
// 纯函数：只吃 int32、不碰 Win32 ⇒ 可表驱动单测（TestIconSizes）。
// 大图标固定 32 px（brief §1）；小图标用系统度量，度量取不到（0/负）时回落 16 px，
// 并且不超过大图标边长。
func iconSizes(cxSmIcon int32) (int32, int32) {
	big := int32(wizardIconPx)
	small := cxSmIcon
	if small <= 0 {
		small = int32(defaultSmallIconPx)
	}
	if small > big {
		small = big
	}
	return big, small
}

// iconHandles 决定最终写进 wc.hIcon / wc.hIconSm 的句柄。入参是**已经取到**的三个句柄
// （大图标、小图标、LoadIconW 兜底），0 表示那一路没取到。纯函数 ⇒ 表驱动单测 TestIconHandles。
//
// 规则（brief §2「返回 0 时不得 panic，走 LoadIconW(0, IDI_APPLICATION) 兜底」）：
//   - 大图标取到 → 用它；取不到 → 用兜底句柄；
//   - 小图标取到 → 用它；取不到 → 退用大图标那一路的句柄（含兜底），Windows 自行缩放；
//   - 连兜底也是 0 → 两个都填 0：窗口类照常注册，标题栏回落系统图标，**绝不 panic**。
//
// TODO(未取证)：小图标取不到时「退用大图标句柄」的口径未取证（spec/brief 只写了不得 panic）。
func iconHandles(big, small, fallback uintptr) (icon, iconSm uintptr, usedFallback bool) {
	icon = big
	if icon == 0 {
		icon = fallback
	}
	iconSm = small
	if iconSm == 0 {
		iconSm = icon
	}
	return icon, iconSm, big == 0 || small == 0
}

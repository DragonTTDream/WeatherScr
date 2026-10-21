package main

// B16 闸门：MoveFileExW 的 flags 常量值。
//
// 2026-10-21 实机踩过：`movefileReplaceExisting` 写成 **0x10**（= MOVEFILE_CREATE_HARDLINK），
// 于是「重命名让开」实际建了一个硬链接：调用返回 TRUE（判成功、日志写"已让开/已删除"），
// 原文件分毫未动，随后删掉的是刚建的硬链接。表现 = 卸载日志「已删除 <path>」，核验却
// 「文件仍在」；`--delete-only` 报 DEL_OK 而文件依旧（out/setup-uninst.log 18:36:59）。
//
// Win32（winbase.h）逐字口径：
//   0x00000001 MOVEFILE_REPLACE_EXISTING
//   0x00000002 MOVEFILE_COPY_ALLOWED
//   0x00000004 MOVEFILE_DELAY_UNTIL_REBOOT
//   0x00000008 MOVEFILE_WRITE_THROUGH
//   0x00000010 MOVEFILE_CREATE_HARDLINK
//
// 这条只能做在单测层：编译和 vet 都不会发现 0x10 与 0x1 的差别。

import "testing"

func TestMoveFileExReplaceFlagValue(t *testing.T) {
	const (
		wantReplaceExisting = 0x00000001
		movefileCopyAllowed = 0x00000002
		movefileDelayThrow  = 0x00000004
		movefileWriteThru   = 0x00000008
		movefileHardlink    = 0x00000010
	)
	if movefileReplaceExisting != wantReplaceExisting {
		t.Fatalf("movefileReplaceExisting = 0x%08X，必须是 MOVEFILE_REPLACE_EXISTING = 0x%08X",
			movefileReplaceExisting, wantReplaceExisting)
	}
	for _, bad := range []uint32{movefileCopyAllowed, movefileDelayThrow, movefileWriteThru, movefileHardlink} {
		if movefileReplaceExisting == bad {
			t.Fatalf("movefileReplaceExisting = 0x%08X 是别的 MOVEFILE_* 标志（建硬链接/延迟重启/只写穿透），不是 REPLACE_EXISTING",
				bad)
		}
	}
}

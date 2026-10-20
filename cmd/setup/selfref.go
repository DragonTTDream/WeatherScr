package main

import "strings"

// 卸载回滚值的自指判定（纯函数，无 build tag ⇒ 可在非 windows 平台跑测试）。
//
// 证据 E5（spec §0.5）：实机卸载后 SCRNSAVE.EXE = C:\Windows\System32\WeatherScr.scr，
// 而该文件已被本次卸载删除（SYS32 MISSING）⇒ 当"安装前记录的值"就是本程序自己的安装路径时，
// 恢复它等于把屏保设置指向一个不存在的文件。此时按"安装前没有屏保"处理（走 deleteScrSave()）。
//
// 比较口径（spec §1.5）：**大小写不敏感**、**去引号**后比较。

// normalizeScrPath 规范化屏保路径：去首尾空白、去包裹的双引号、正斜杠归一成反斜杠、转小写。
// 注册表里的值可能带引号（例如从 .scr 关联命令行抄来的 "C:\...\x.scr"），大小写也可能不同。
func normalizeScrPath(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, `"`) {
		s = strings.TrimPrefix(s, `"`)
	}
	for strings.HasSuffix(s, `"`) {
		s = strings.TrimSuffix(s, `"`)
	}
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", `\`)
	return strings.ToLower(s)
}

// isSelfScrPath 判断回滚值 prev 是否就是本程序的安装路径 self（大小写不敏感、去引号后比较）。
// prev 为空（安装前没有屏保）⇒ 不算自指。
func isSelfScrPath(prev, self string) bool {
	p := normalizeScrPath(prev)
	if p == "" {
		return false
	}
	return p == normalizeScrPath(self)
}

// uninstallScrAction 卸载时对 SCRNSAVE.EXE 值的动作：
// 空值 / 自指值 ⇒ "delete"（删除该值，等于回到"没有屏保"）；其余 ⇒ "restore"（写回原值）。
const (
	scrActionDelete  = "delete"
	scrActionRestore = "restore"
)

func uninstallScrAction(prev, self string) string {
	if prev == "" || isSelfScrPath(prev, self) {
		return scrActionDelete
	}
	return scrActionRestore
}

package main

import "fmt"

// 完成页文案（批次 D5，spec §1.3）。纯函数、无 build tag ⇒ 非 windows 平台可测（分层同 winsize.go）。
//
// 根因（spec §0，已取证）：
//   - main.go runInstall 的**全部** return（含 6 处失败分支）都发 wmAppDone（main.go:603）
//   - main.go:316-319 `case wmAppDone:` 无条件 showPage(pageFinish)
//   - main.go:550 完成页标题写死 "安装完成"
//   ⇒ 安装失败（已回滚）时完成页仍显示「安装完成」。
//
// 文案逐字取自 spec §1.3，窗口代码只调用本文件的 finishTitle / finishBody。

const (
	finishTitleOK   = "安装完成"
	finishTitleFail = "安装失败（已回滚）"

	// 成功正文 = D5 之前的现文案，逐字保留（spec §1.3「正文不变（现文案）」）。
	finishBodyOK = "屏保已安装：C:\\Windows\\System32\\WeatherScr.scr\r\n\r\n" +
		"· 桌面快捷方式 \"天气屏保 · 设置\" 打开设置窗口\r\n" +
		"· 桌面快捷方式 \"天气屏保 · 启动\" 立即预览\r\n" +
		"· 卸载：控制面板 › 程序和功能，或运行本安装器 --uninstall"

	// 失败正文：spec §1.3 逐字，%s = rollback(why) 记录的 lastFail。
	finishBodyFailFmt = "安装未完成，注册表与快捷方式已回滚。\r\n\r\n原因：%s\r\n\r\n" +
		"· 常见原因：屏保正在运行占用文件，或 UAC 被取消\r\n" +
		"· 关闭屏保后重新运行本安装器"

	// lastFail 为空时的回落词（spec §1.3「空则 未记录」）。
	finishReasonFallback = "未记录"
)

// finishTitle 完成页标题：成功/失败两态（spec §1.3）。
func finishTitle(ok bool) string {
	if ok {
		return finishTitleOK
	}
	return finishTitleFail
}

// finishBody 完成页正文：成功 = 现文案；失败 = 回滚说明 + 原因（why 空 ⇒ 未记录）。
func finishBody(ok bool, why string) string {
	if ok {
		return finishBodyOK
	}
	if why == "" {
		why = finishReasonFallback
	}
	return fmt.Sprintf(finishBodyFailFmt, why)
}

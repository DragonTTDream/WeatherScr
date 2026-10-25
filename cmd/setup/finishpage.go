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

// R13（spec §1，证据 evidence/p8-split-wizard-settings-20261023.md）：卸载跑完也要落完成页，
// 文案与安装态分开 ⇒ 卸载两态常量逐字取自 spec §1（测试 TestFinishUninstallTexts 钉死）。
const (
	finishTitleUninstallOK   = "卸载完成"
	finishTitleUninstallFail = "卸载失败（文件仍在，未删除）"

	// 卸载成功正文（spec §1 逐字）。
	finishBodyUninstallOK = "屏保已从 C:\\Windows\\System32 删除。\r\n\r\n" +
		"· 桌面快捷方式已删除\r\n" +
		"· 屏保设置已恢复为安装前的值\r\n" +
		"· 如需再次使用：运行本安装器重新安装"

	// 卸载失败正文：%s = runUninstall 核验失败时记录的 lastFail（spec §1 逐字）。
	finishBodyUninstallFailFmt = "卸载未完成：%s\r\n\r\n" +
		"· 屏保文件 C:\\Windows\\System32\\WeatherScr.scr 未被删除\r\n" +
		"· 常见原因：屏保正在运行占用文件，或 UAC 被取消\r\n" +
		"· 关闭屏保后重新运行本安装器 --uninstall"
)

// finishOp 完成页的两种操作态（spec §1）：安装跑完 / 卸载跑完。
type finishOp string

const (
	opInstall   finishOp = "install"
	opUninstall finishOp = "uninstall"
)

// finishTitleFor 完成页标题：按操作态 + 成功/失败两态取文案（spec §1）。
func finishTitleFor(op finishOp, ok bool) string {
	if op == opUninstall {
		if ok {
			return finishTitleUninstallOK
		}
		return finishTitleUninstallFail
	}
	if ok {
		return finishTitleOK
	}
	return finishTitleFail
}

// finishBodyFor 完成页正文：按操作态 + 成功/失败两态取文案，why 空 ⇒ 回落「未记录」（spec §1）。
func finishBodyFor(op finishOp, ok bool, why string) string {
	if ok {
		if op == opUninstall {
			return finishBodyUninstallOK
		}
		return finishBodyOK
	}
	if why == "" {
		why = finishReasonFallback
	}
	if op == opUninstall {
		return fmt.Sprintf(finishBodyUninstallFailFmt, why)
	}
	return fmt.Sprintf(finishBodyFailFmt, why)
}

// finishTitle 完成页标题（安装态薄壳，签名与返回逐字不变，spec §2）：转调 finishTitleFor(opInstall, ok)。
func finishTitle(ok bool) string {
	return finishTitleFor(opInstall, ok)
}

// finishBody 完成页正文（安装态薄壳，签名与返回逐字不变，spec §2）：转调 finishBodyFor(opInstall, ok, why)。
func finishBody(ok bool, why string) string {
	return finishBodyFor(opInstall, ok, why)
}

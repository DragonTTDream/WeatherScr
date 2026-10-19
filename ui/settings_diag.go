package ui

// 面板自检导出面：只读计数，供离屏量化工具（cmd/setshot）打印。
//
// 这两个方法不参与绘制：Layout 走的仍是 p.rows(th) + material.List，
// 计数与 Layout 送进列表的行数同一来源（同一个 p.rows），
// 所以不存在"为了计数改绘制路径"。

// RowCount 返回面板行数（p.rows(nil) 的口径）。
//
// rows() 里每个行构造函数只返回闭包，th 在闭包内部才被解引用，
// 因此 th = nil 时计数安全。
func (p *SettingsPanel) RowCount() int {
	return len(p.rows(nil))
}

// ListOffset 返回列表主轴的滚动偏移（px）。
//
// 面板打开时该值应为 0（列表停在内容起始处）；非 0 表示一打开
// 就停在内容中段或末尾，首屏看不到行。
func (p *SettingsPanel) ListOffset() int {
	return p.list.Position.Offset
}

// ListVisibleCount 返回上一帧落在视口内的行数（layout.Position.Count）。
//
// 只读上一帧 Layout 记下的可见行数，供离屏量化打印"首屏可见 N/26 行"。
func (p *SettingsPanel) ListVisibleCount() int {
	return p.list.Position.Count
}

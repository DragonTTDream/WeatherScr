package ui

import (
	"fmt"
	"image"
	"strconv"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
)

// TestUndoStackParity 钉死 Ctrl+Z 撤销栈在 64 上限处的溢出行为（R12 判据 3）。
//
// 压栈走**生产入栈点** SettingsPanel.Layout（ui/settings_ui.go:429-437：
// 本帧 model 与上一帧不同 ⇒ 压入 before，容量 64，溢出丢最旧）；
// 弹栈走生产 SettingsPanel.Undo（ui/settings_ui.go:295-306）；
// 状态文案直接读 p.status。
//
// 基准：迁移前旧实现（history []PanelModel + append + 「len>64 ⇒ history = history[1:]」）
// 于 2026-10-26 实测打印的 82 项序列（80 次压栈越 64 上限，随后 82 次 Undo），
// 逐字写死在下面；换成 github.com/zyedidia/generic/stack 之后复跑必须逐字一致。
//
// 口径：每帧只改 edMargin（第 i 帧写 100+i），Layout 末尾压入"上一帧的模型"
// ⇒ 压栈序列的 Margin 依次 30、101、102、…、179；越上限丢最旧后栈里剩
// 116…179（顶 = 179）⇒ 前 64 项弹栈 179→116 全「已撤销」，后 18 项空栈
// 「无可撤销」且模型停在 116。
func TestUndoStackParity(t *testing.T) {
	const (
		steps = 80 // 压栈次数（越 64 上限）
		pops  = 82 // 64 次命中 + 18 次空栈
	)

	th := New().Theme
	p := NewSettingsPanel(sampleModel())
	var ops op.Ops
	for i := 1; i <= steps; i++ {
		ops.Reset() // gioui 复用 op.Ops 前必须 Reset
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Point{X: 900, Y: 1600}),
		}
		p.edMargin.SetText(strconv.Itoa(100 + i))
		p.Layout(gtx, th) // 生产入栈点在这个函数内部
		if !p.Changed() {
			t.Fatalf("第 %d 帧改了 Margin 却未报改动", i)
		}
	}

	got := make([]string, 0, pops)
	for i := 0; i < pops; i++ {
		ok := p.Undo()
		got = append(got, fmt.Sprintf("%d:%s:%v", p.Model().Margin, p.status, ok))
	}

	if len(got) != len(goldenUndoPopSequence) {
		t.Fatalf("弹栈项数 = %d，期望 %d", len(got), len(goldenUndoPopSequence))
	}
	for i, want := range goldenUndoPopSequence {
		if got[i] != want {
			t.Fatalf("第 %d 项弹栈对拍不一致\ngot  %s\nwant %s", i, got[i], want)
		}
	}
	t.Logf("撤销栈 82 项弹栈序列逐字一致：前 64 项 179→116「已撤销」，后 18 项 116「无可撤销」")
}

// goldenUndoPopSequence 是旧实现实测序列，格式 "Margin:状态文案:Undo 返回值"。
var goldenUndoPopSequence = []string{
	"179:已撤销:true", "178:已撤销:true", "177:已撤销:true", "176:已撤销:true", "175:已撤销:true", "174:已撤销:true",
	"173:已撤销:true", "172:已撤销:true", "171:已撤销:true", "170:已撤销:true", "169:已撤销:true", "168:已撤销:true",
	"167:已撤销:true", "166:已撤销:true", "165:已撤销:true", "164:已撤销:true", "163:已撤销:true", "162:已撤销:true",
	"161:已撤销:true", "160:已撤销:true", "159:已撤销:true", "158:已撤销:true", "157:已撤销:true", "156:已撤销:true",
	"155:已撤销:true", "154:已撤销:true", "153:已撤销:true", "152:已撤销:true", "151:已撤销:true", "150:已撤销:true",
	"149:已撤销:true", "148:已撤销:true", "147:已撤销:true", "146:已撤销:true", "145:已撤销:true", "144:已撤销:true",
	"143:已撤销:true", "142:已撤销:true", "141:已撤销:true", "140:已撤销:true", "139:已撤销:true", "138:已撤销:true",
	"137:已撤销:true", "136:已撤销:true", "135:已撤销:true", "134:已撤销:true", "133:已撤销:true", "132:已撤销:true",
	"131:已撤销:true", "130:已撤销:true", "129:已撤销:true", "128:已撤销:true", "127:已撤销:true", "126:已撤销:true",
	"125:已撤销:true", "124:已撤销:true", "123:已撤销:true", "122:已撤销:true", "121:已撤销:true", "120:已撤销:true",
	"119:已撤销:true", "118:已撤销:true", "117:已撤销:true", "116:已撤销:true", "116:无可撤销:false", "116:无可撤销:false",
	"116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false",
	"116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false",
	"116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false", "116:无可撤销:false",
}

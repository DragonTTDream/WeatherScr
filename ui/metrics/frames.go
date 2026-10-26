package metrics

// R5 迁移（逐字搬移，plans/active.md:213 ui/metrics 卡）——来源：
//
//	WSDbgFrames ← ui/overlap_probe.go:57-60（WS_DBG_FRAMES 开关，P5b 测量件）
//	ProbeGTX    ← ui/overlap_probe.go:73-80（probeGTX，探针用的空 layout.Context）
//	OpsLen      ← ui/overlap_probe.go:359-381（gioui v0.10.3 op.Ops 内部切片长度，判据 5）
//
// 帧耗时本体（SCR_FRAME / SCR_FRAME_MS 的计时行）在 cmd/scr 主循环里，本卡不动。

import (
	"image"
	"os"
	"reflect"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
)

// WSDbgFrames 环境变量 WS_DBG_FRAMES=1 ⇒ 开启全帧测量（P5b 测量件的唯一开关）。
// 每次调用现读环境：建场是低频事件 ⇒ 开销可忽略，且测试里 setenv/unsetenv
// 立刻生效，没有缓存陈旧窗口。
func WSDbgFrames() bool { return os.Getenv("WS_DBG_FRAMES") == "1" }

// ProbeGTX 探针用的空 layout.Context（只给 Ops，离屏计数不真渲染）。
func ProbeGTX(o *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         o,
		Constraints: layout.Exact(image.Pt(w, h)),
		Now:         time.Now(),
	}
}

// OpsLen 返回 gtx.Ops 的序列化缓冲长度与引用数。
// gioui v0.10.3 的 op.Ops 不导出长度，这里通过导出字段 Ops.Internal
// 反射读内部 data / refs 切片的长度（只读，不改 op.Ops 内容）。
func OpsLen(o *op.Ops) (data, refs int) {
	if o == nil {
		return -1, -1
	}
	v := reflect.ValueOf(&o.Internal).Elem()
	data, refs = -1, -1
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		switch f.Name {
		case "data":
			data = v.Field(i).Len()
		case "refs":
			refs = v.Field(i).Len()
		}
	}
	return data, refs
}

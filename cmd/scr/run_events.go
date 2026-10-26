package main

// 职责（卡9-app/scr）：事件分发：本帧输入事件收干与退出判定、热键层转接、点击回填（左键/位移/滚轮/键盘）
// 搬移来源：cmd/scr/main.go:957-1094（逐字，含注释）
// 规格：plans/active.md:214 —— app/scr = 屏保生命周期；本卡零行为变化。
// 设置面板侧的点击回填（SaveClicked/CancelClicked/UndoClicked/LockClicked/LocSearchClicked）在 run_lifecycle.go:runSettings 体内，函数体逐字未动 ⇒ 未拆。

import (
	"math"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
)

// inputState 是退出判定的状态。
type inputState struct {
	// interaction = config 的 interaction_mode（上游 default_settings.py:27
	// 'input'.'interaction_mode': False。True = 交互模式：鼠标位移与左键
	// 不退出（上游 rendering/runtime_input.py:425-426, 454-458）。
	interaction bool
	// debug = SCR_DEBUG=1：
	// 鼠标移动/按键一律不退出，退出只保留 Esc（与 Ctrl+C 进程级）。
	debug bool
	// 初始鼠标位置：上游 :462-464 `if self._initial_mouse_pos is None:
	// self._initial_mouse_pos = current_pos`。
	haveInitial bool
	initialX    float32
	initialY    float32
	// dist = hypot(当前 - 初始)：**相对初始位置的位移，不是累积移动量**
	// （上游 :465-466 delta = current_pos - self._initial_mouse_pos）。
	dist float64
}

// drainInput 收干本帧的输入事件。返回 true = 该退出屏保。
//
// 判据逐条对应上游 rendering/runtime_input.py：
// - 鼠标位移：:462-467 `hypot(current - initial) > MOUSE_EXIT_THRESHOLD(10)`
// - 左键按下：:424-429 左键 → _request_exit（交互模式/Ctrl 下 :425-426 不退出）
// - 滚轮：:254-268 handle_wheel 不请求退出 → 本复刻也不退出
// - 键盘：:371-378 Esc/Q 退出；热键层被消费不退出；其余任意键退出
func drainInput(gtx layout.Context, st *inputState, nPointer, nKey *int) bool {
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &pt,
			Kinds:  pointer.Move | pointer.Press | pointer.Scroll | pointer.Enter | pointer.Leave,
		})
		if !ok {
			break
		}
		pe, isP := ev.(pointer.Event)
		if !isP {
			continue
		}
		*nPointer++
		logf("SCR_PTR kind=%s pos=%.0f,%.0f\n", pe.Kind, pe.Position.X, pe.Position.Y)
		switch pe.Kind {
		case pointer.Press:
			if st.pressExit(pe.Buttons) {
				return true
			}
		case pointer.Scroll:
			// 上游 handle_wheel（:254-268）不请求退出 → 不退出
		case pointer.Move:
			if st.moveExit(pe.Position.X, pe.Position.Y) {
				return true
			}
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &kt})
		if !ok {
			break
		}
		*nKey++
		ke, isK := ev.(key.Event)
		if !isK || ke.State != key.Press {
			continue
		}
		// Esc / Q → 退出（上游 :371-373）
		if ke.Name == key.NameEscape || ke.Name == "Q" {
			// 实测：DEBUG 下退出只保留 Esc（Ctrl+C 是进程级，天然可用）
			if ke.Name == key.NameEscape || !st.debug {
				logf("SCR_KEY=%s 退出\n", ke.Name)
				return true
			}
			logf("SCR_KEY=%s DEBUG 下不退出\n", ke.Name)
			continue
		}
		// 热键层（指定 Z X C Space ↑ ↓ PageUp PageDown Home End；
		// 上游键策略还含 ←/→ 换轨道，偏差已记入工程记录。
		// 本复刻没有幻灯片 → 无对应功能：打印一行后**不退出**。
		if isHotkey(ke.Name) {
			logf("SCR_KEY=%s 无对应功能\n", ke.Name)
			continue
		}
		// 其余任意键一律退出（上游 :378-379）；DEBUG 下不退出
		if st.debug {
			logf("SCR_KEY=%s DEBUG 下不退出(其余键)\n", ke.Name)
			continue
		}
		logf("SCR_KEY=%s 退出(其余键)\n", ke.Name)
		return true
	}
	// 焦点诊断：窗口没有 OS 焦点时按键不会路由过来
	for {
		ev, ok := gtx.Event(key.FocusFilter{Target: &kt})
		if !ok {
			break
		}
		if fe, isF := ev.(key.FocusEvent); isF {
			logf("SCR_FOCUS=%v\n", fe.Focus)
		}
	}
	return false
}

// moveExit 处理一次鼠标移动：首帧记录**初始位置**（上游 :462-464），
// 之后每帧算 hypot(当前 - 初始)，> MOUSE_EXIT_THRESHOLD(10) 即退出
// （上游 :465-466）。**不是累积位移**。交互模式下不退出（上游 :454-458）。
// DEBUG 模式下不退出。
func (st *inputState) moveExit(x, y float32) bool {
	if !st.haveInitial {
		st.haveInitial = true
		st.initialX, st.initialY = x, y
		return false
	}
	dx := float64(x - st.initialX)
	dy := float64(y - st.initialY)
	st.dist = math.Hypot(dx, dy)
	return !st.interaction && !st.debug && st.dist > mouseExitThresholdPx
}

// pressExit 左键点击 → 退出（上游 :424-429）；交互模式不退出（上游 :425-426）；
// DEBUG 模式不退出。
func (st *inputState) pressExit(buttons pointer.Buttons) bool {
	return buttons&pointer.ButtonPrimary != 0 && !st.interaction && !st.debug
}

// isHotkey 上游键策略里被消费、不触发退出的热键层
// （rendering/runtime_input.py:325-370：Z/X/C/←/→/Space/↑/↓/PageUp/PageDown/Home/End）。
func isHotkey(n key.Name) bool {
	switch n {
	case "Z", "X", "C",
		key.NameSpace,
		key.NameUpArrow, key.NameDownArrow,
		key.NameLeftArrow, key.NameRightArrow,
		key.NamePageUp, key.NamePageDown,
		key.NameHome, key.NameEnd:
		return true
	}
	return false
}

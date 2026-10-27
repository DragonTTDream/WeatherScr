package main

import (
	"testing"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// 退出条件对齐的判据测试。
// 判据出处：上游 rendering/runtime_input.py
// 111 MOUSE_EXIT_THRESHOLD = 10
// 462-466 delta = current_pos - self._initial_mouse_pos; hypot(delta) > 阈值 → 退出
// 424-429 左键按下 → 退出；:425-426 交互模式/Ctrl 下不退出
// 371-379 Esc/Q 退出；热键层被消费；其余任意键退出
// 254-268 handle_wheel 不请求退出

func TestMouseExitIsRelativeNotCumulative(t *testing.T) {
	st := &inputState{}
	if st.moveExit(500, 500) {
		t.Fatalf("首个移动只应记录初始位置")
	}
	// 9 px 位移 → 不退出
	if st.moveExit(509, 500) {
		t.Errorf("9px 相对位移不应退出（阈值 10）")
	}
	// 累积 100 px 的来回抖动，相对初始位置始终 9 px → 不得退出
	for i := 0; i < 50; i++ {
		if st.moveExit(509, 500) {
			t.Fatalf("第 %d 次抖动不应退出", i)
		}
		if st.moveExit(500, 500) {
			t.Fatalf("第 %d 次回到初始点不应退出", i)
		}
	}
	// 相对初始位置 11 px → 退出
	if !st.moveExit(511, 500) {
		t.Errorf("11px 相对位移应退出（> MOUSE_EXIT_THRESHOLD=10）")
	}
}

func TestMouseExitInitialPositionIsFirstEvent(t *testing.T) {
	st := &inputState{}
	st.moveExit(1000, 1000) // 初始位置
	if st.moveExit(1005, 1000) {
		t.Errorf("5px 不应退出")
	}
	if !st.moveExit(1011, 1000) {
		t.Errorf("相对初始 11px 应退出")
	}
}

func TestInteractionModeSuppressesMouse(t *testing.T) {
	st := &inputState{interaction: true}
	st.moveExit(0, 0)
	if st.moveExit(1000, 1000) {
		t.Errorf("interaction_mode=true 时鼠标位移不得退出")
	}
	if st.pressExit(pointer.ButtonPrimary) {
		t.Errorf("interaction_mode=true 时左键不得退出")
	}
}

func TestLeftClickExit(t *testing.T) {
	st := &inputState{}
	if !st.pressExit(pointer.ButtonPrimary) {
		t.Errorf("左键点击应退出")
	}
	if st.pressExit(pointer.ButtonSecondary) {
		t.Errorf("右键不应退出（上游 :420-423 右键只在交互模式弹菜单）")
	}
}

func TestHotkeyLayer(t *testing.T) {
	for _, n := range []key.Name{
		"Z", "X", "C",
		key.NameSpace,
		key.NameUpArrow, key.NameDownArrow,
		key.NameLeftArrow, key.NameRightArrow,
		key.NamePageUp, key.NamePageDown,
		key.NameHome, key.NameEnd,
	} {
		if !isHotkey(n) {
			t.Errorf("热键层应包含 %q", n)
		}
	}
	for _, n := range []key.Name{key.NameEscape, "Q", "A", "K", key.NameTab, "1"} {
		if isHotkey(n) {
			t.Errorf("%q 不属于热键层，应走其余任意键退出", n)
		}
	}
}

func TestMouseExitThresholdValue(t *testing.T) {
	if mouseExitThresholdPx != 10 {
		t.Errorf("上游 MOUSE_EXIT_THRESHOLD = 10（rendering/runtime_input.py:111），got %d",
			mouseExitThresholdPx)
	}
}

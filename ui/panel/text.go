package panel

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// RowFn 是"按给定颜色画一行文字"工厂：Shadowed 用它把整行画两遍
// （阴影副本 + 主字），内联多段文字的条件行也整体复制。
type RowFn = func(col color.NRGBA) layout.Widget

// Label 画一行文字（对齐口径由调用方给）。
func Label(th *material.Theme, txt string, sp unit.Sp, col color.NRGBA, align text.Alignment) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if txt == "" {
			return layout.Dimensions{}
		}
		l := material.Label(th, sp, txt)
		l.Color = col
		l.Alignment = align
		return layout.Center.Layout(gtx, l.Layout)
	}
}

// RowText 单行文字。mod 用于粗体/斜体等字形修饰。
func RowText(th *material.Theme, txt string, sp unit.Sp, mod func(*material.LabelStyle)) RowFn {
	return func(col color.NRGBA) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			if txt == "" {
				return layout.Dimensions{}
			}
			l := material.Label(th, sp, txt)
			l.Color = col
			l.Alignment = text.Middle
			if mod != nil {
				mod(&l)
			}
			return layout.Center.Layout(gtx, l.Layout)
		}
	}
}

// RowCondition 条件行：温度(0.81×) " - " + 条件
// rendering/quick/widgets/weather.py:758-772 的 0.81 口径。
//
// baseSp = 条件行字号（调用方传 ui 侧的 WeatherFontSize 档，逐字同旧实现）。
func RowCondition(th *material.Theme, temp, cond string, baseSp unit.Sp) RowFn {
	return func(col color.NRGBA) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{
					Axis:      layout.Horizontal,
					Alignment: layout.Middle,
				}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(th, unit.Sp(baseSp*0.81), temp)
						l.Color = col
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(th, unit.Sp(baseSp), " - "+cond)
						l.Color = col
						return l.Layout(gtx)
					}),
				)
			})
		}
	}
}

// Bold 粗体字形修饰。
func Bold(l *material.LabelStyle) { l.Font.Weight = font.Bold }

// Italic 斜体字形修饰。
func Italic(l *material.LabelStyle) { l.Font.Style = font.Italic }

// Shadowed 照抄上游 ShadowedText.qml 的语义：同一字形先在偏移
// (0, +偏移) 画一份 alpha 0x96 的暗色副本（上游 z:0），再以主字色画在 (0,0)
// （上游 z:1）。**无模糊、无 MultiEffect、无 layer capture**（该文件注释明说）。
// row 是"按给定颜色画这一行"工厂，整行（含内联多段文字）一起复制两份。
//
// 偏移 = ShadowOffsetForPx(gtx.Sp(sp))，随字号走
// （旧版固定 2 px 在 144 pt 大字上实测不可见 = 缺陷 2）。
//
// shadowOn = 调用方算好的"这一行要不要阴影"（旧实现 = st.Shadow 且图层掩码开着
// LayerShadow；图层掩码是 ui 帧调度侧的量，故由 ui 传入布尔，判据逐字不变）。
func Shadowed(st Style, sp unit.Sp, row RowFn, shadowOn bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if st.Shadow && shadowOn {
			off := ShadowOffsetForPx(gtx.Sp(sp))
			mac := op.Offset(image.Pt(0, off)).Push(gtx.Ops)
			row(ShadowInk)(gtx)
			mac.Pop()
		}
		return row(st.TextColor)(gtx)
	}
}

package main

// nmcdiag = 官方预警信号源的带外诊断。
// 用法：`nmcdiag <ISO3166-2 数字后缀>`（例：51 = CN-51 = 四川省，11 = CN-11 = 北京市）
// 打印形状照 cmd/alertdiag/main.go（只打印、不渲染）：
//	PROVINCE=<NMC 官方全称>
//	COUNT=<接口返回条数>
//	TOP_TYPE=<中文类型名>
//	TOP_LEVEL=<颜色词>
//	NOTE=<count=0 时逐字 = count0-unknown>
// 为什么 NOTE 必须有（决定性）：
//	`province=zzzz省`（故意无效）也返回 `code=0 msg=success count=0`
//	⇒ count=0 不能证明省名被接受 ⇒ **不得当"该省无预警"上报**（闸门第 5 步逐字禁这个说法）。
import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/DragonTTDream/WeatherScr/geo"
	"github.com/DragonTTDream/WeatherScr/weather"
)

func main() {
	arg := "51"
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	prov, ok := geo.NMCProvince(arg)
	if !ok {
		fmt.Printf("PROVINCE=\nCOUNT=0\nTOP_TYPE=\nTOP_LEVEL=\nNOTE=bad-iso-suffix %s\n", arg)
		os.Exit(1)
	}
	fmt.Printf("PROVINCE=%s\n", prov)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	page, err := weather.FindAlarms(ctx, &http.Client{Timeout: 25 * time.Second}, prov)
	if err != nil {
		fmt.Printf("COUNT=0\nTOP_TYPE=\nTOP_LEVEL=\nNOTE=request-fail %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("COUNT=%d\n", page.Count)
	top, ok := weather.NMCTop(page)
	if !ok {
		fmt.Printf("TOP_TYPE=\nTOP_LEVEL=\n")
	} else {
		fmt.Printf("TOP_TYPE=%s\nTOP_LEVEL=%s\n", top.TypeZh, top.TitleColor)
	}
	note := page.Note
	if note == "" && page.Count > 0 && !ok {
		note = "title-unparsed" // 已知的 3 条标题不匹配样本 ⇒ 降级，不静默丢弃
	}
	fmt.Printf("NOTE=%s\n", note)
}

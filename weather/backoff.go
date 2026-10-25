package weather

import "time"

// 批次 A1（spec/a1-fetch-polite.md S2/S3）：取数侧防堆积、防限流的两个纯函数。
//
// 纯函数 = 无副作用、无随机、不读 time.Now：同一入参永远同一输出 ⇒ 单测可逐值钉死，
// 实机日志可复现对拍。

// backoffLadder = 连续失败时的下周期阶梯（第 4 次及以后固定封顶 10 分钟）。
var backoffLadder = []time.Duration{
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
}

// NextRefreshInterval 按"连续失败次数"给出下一次取数前该等多久。
//
//   - failCount <= 0（本轮成功 ⇒ 复位）⇒ 原样返回 base；
//   - 否则按 1m, 2m, 5m, 10m 依次取，第 4 次及以后固定 10m 封顶；
//   - 结果**不得小于 base**（base=5m 时不会退到 1m，把 refresh_minutes 调大是用户意图）。
//
// 注意：本函数只决定"下周期"，不做周期内重试 —— 用户令「请求超时就下次再请求」保持。
func NextRefreshInterval(failCount int, base time.Duration) time.Duration {
	if failCount <= 0 {
		return base
	}
	i := failCount - 1
	if i >= len(backoffLadder) {
		i = len(backoffLadder) - 1
	}
	if d := backoffLadder[i]; d > base {
		return d
	}
	return base
}

// OfficialJitterMax = 官方观测请求的错峰抖动上限（spec S3：0–30 s）。
const OfficialJitterMax = 30 * time.Second

// OfficialJitter 由 seed 确定性地算出 [0, OfficialJitterMax] 内的抖动。
//
// 算法 = splitmix64 的收尾三步（纯算术、无状态、无随机源）：
// 同一 seed 两次调用必然相等；跨大量 seed 时均匀铺满区间 ⇒ 官方拉取不会
// 与天气刷新钉在同一刻齐发。
func OfficialJitter(seed int64) time.Duration {
	x := uint64(seed)
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	x ^= x >> 31
	return time.Duration(x % uint64(OfficialJitterMax+1))
}

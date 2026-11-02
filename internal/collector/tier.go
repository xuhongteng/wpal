package collector

import "time"

// shouldSample 报告分档采集项是否已到采样时刻。
//
// last 为零值表示从未采过（刚启动），此时立即采样，
// 保证服务启动瞬间快照就是完整的，不会出现进程榜/TCP 为空的帧。
func shouldSample(last, now time.Time, interval time.Duration) bool {
	if last.IsZero() {
		return true
	}
	if interval <= 0 {
		return true
	}
	return now.Sub(last) >= interval
}

// sampleDelta 返回分档采集项应使用的差分秒数。
//
// 速率类指标（进程 CPU 占比）必须用它而不是主循环的帧间隔：
// 进程榜按 3 秒采样时，若仍用 1 秒的帧间隔去除，占比会被稀释成真实值的 1/3。
// 首次采样（last 为零）没有可比基准，返回间隔本身作为兜底。
func sampleDelta(last, now time.Time, interval time.Duration) float64 {
	if last.IsZero() {
		if interval <= 0 {
			return 1
		}
		return interval.Seconds()
	}
	if d := now.Sub(last).Seconds(); d > 0 {
		return d
	}
	if interval <= 0 {
		return 1
	}
	return interval.Seconds()
}

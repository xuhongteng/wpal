package collector

import (
	"testing"
	"time"
)

// TestShouldSample 覆盖分档采集的两种路径：
// 首次（last 为零值）必须立即采样，未到点必须复用上次结果。
func TestShouldSample(t *testing.T) {
	now := time.Now()
	const interval = 3 * time.Second

	// 首次：last 为零值 → 立即采样，保证启动瞬间快照完整
	if !shouldSample(time.Time{}, now, interval) {
		t.Error("首次采样应立即触发，否则启动后进程榜/ TCP 会有一段时间为空")
	}

	// 刚采过 → 不应重复采样
	if shouldSample(now.Add(-time.Second), now, interval) {
		t.Error("距上次采样仅 1 秒时不应再次采样（间隔为 3 秒）")
	}

	// 已到点 → 应采样
	if !shouldSample(now.Add(-4*time.Second), now, interval) {
		t.Error("距上次采样 4 秒时应触发采样（间隔为 3 秒）")
	}
}

// TestSampleDelta 是回归用例：进程榜降频到 3 秒后，
// CPU 占比的差分分母必须跟着变成 3 秒，否则占比会被稀释成真实值的 1/3。
func TestSampleDelta(t *testing.T) {
	now := time.Now()
	const interval = 3 * time.Second

	// 关键场景：主循环每秒一帧，但进程榜每 3 秒才采一次，
	// 实际间隔约 3 秒 —— 分母必须是 3，不能是 1。
	got := sampleDelta(now.Add(-3*time.Second), now, interval)
	if got < 2.9 || got > 3.1 {
		t.Fatalf("差分间隔应为约 3 秒，实际 %v；若为 1 秒则进程 CPU 占比会被稀释成 1/3", got)
	}

	// 首次采样没有基准，退回间隔本身
	if v := sampleDelta(time.Time{}, now, interval); v != 3 {
		t.Errorf("首次采样应回退到间隔 3 秒，实际 %v", v)
	}

	// 时钟异常（now 早于 last）时不能返回非正数，否则会产生 NaN/负占比
	if v := sampleDelta(now.Add(time.Second), now, interval); v <= 0 {
		t.Errorf("时钟回退时应回退到间隔值，实际 %v", v)
	}
}

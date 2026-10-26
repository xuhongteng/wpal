package alerts

import (
	"testing"
	"time"

	"mon/internal/collector"
)

// 造一个可控的快照：cpu.total 可任意设定。
func snap(cpuTotal float64) *collector.Snapshot {
	return &collector.Snapshot{
		CPU: collector.CPU{Total: cpuTotal, Load1: 1, Cores: 4},
		Mem: collector.Mem{Percent: 50, SwapPercent: 0},
	}
}

func TestEngineFiresAfterHold(t *testing.T) {
	e := New(DefaultRules())
	now := time.Unix(1000, 0)

	// CPU 90% 超过 warn(80) 但未超过 crit(93)
	s := snap(90)

	// 第一次：只开始计时
	if evs := e.Update(s, now); len(evs) != 0 {
		t.Fatalf("第一次评估不应产生事件，得到 %d 个", len(evs))
	}

	// 未到 hold 时间：仍不触发
	if evs := e.Update(s, now.Add(10*time.Second)); len(evs) != 0 {
		t.Fatalf("未到 hold 时间不应触发，得到 %d 个", len(evs))
	}

	// 到达 hold 时间：触发 warning
	evs := e.Update(s, now.Add(31*time.Second))
	if len(evs) != 1 {
		t.Fatalf("应产生 1 个事件，得到 %d 个", len(evs))
	}
	ev := evs[0]
	if ev.Key != "cpu" || ev.Level != "warning" || ev.Status != "firing" {
		t.Fatalf("事件内容不对: %+v", ev)
	}
	if ev.LabelKey == "" {
		t.Error("事件应带 label_key 供前端翻译")
	}
}

func TestEngineEscalatesToCritical(t *testing.T) {
	e := New(DefaultRules())
	now := time.Unix(1000, 0)

	e.Update(snap(90), now)
	e.Update(snap(90), now.Add(31*time.Second)) // 触发 warning

	// 升到 95% → 立即触发 critical
	evs := e.Update(snap(95), now.Add(32*time.Second))
	if len(evs) != 1 || evs[0].Level != "critical" {
		t.Fatalf("应立即升级为 critical，得到 %+v", evs)
	}
}

func TestEngineResolvesAfterRecover(t *testing.T) {
	e := New(DefaultRules())
	now := time.Unix(1000, 0)

	e.Update(snap(90), now)
	e.Update(snap(90), now.Add(31*time.Second)) // firing

	// 回落但未满 recover：不产生事件
	evs := e.Update(snap(10), now.Add(32*time.Second))
	if len(evs) != 0 {
		t.Fatalf("回落未满 recover 不应产生事件，得到 %+v", evs)
	}

	// 满 recover：resolved
	evs = e.Update(snap(10), now.Add(32*time.Second+21*time.Second))
	if len(evs) != 1 || evs[0].Status != "resolved" {
		t.Fatalf("应产生 resolved 事件，得到 %+v", evs)
	}
}

func TestEngineActiveAlerts(t *testing.T) {
	e := New(DefaultRules())
	now := time.Unix(1000, 0)

	e.Update(snap(90), now)
	e.Update(snap(90), now.Add(31*time.Second))

	active := e.Active(snap(90))
	if len(active) != 1 || active[0].Key != "cpu" {
		t.Fatalf("应有 1 条活动告警，得到 %+v", active)
	}
	if active[0].DetailKey == "" || active[0].DetailParams == nil {
		t.Errorf("活动告警应带详情 key 与参数: %+v", active[0])
	}
}

func TestEngineBelowThresholdStaysQuiet(t *testing.T) {
	e := New(DefaultRules())
	now := time.Unix(1000, 0)
	for i := 0; i < 20; i++ {
		if evs := e.Update(snap(10), now.Add(time.Duration(i)*time.Second)); len(evs) != 0 {
			t.Fatalf("正常值不应产生事件，得到 %+v", evs)
		}
	}
	if len(e.Active(snap(10))) != 0 {
		t.Error("正常值不应有活动告警")
	}
}

func TestRuleValidation(t *testing.T) {
	for _, r := range DefaultRules() {
		if r.Warn <= 0 || r.Crit <= 0 {
			t.Errorf("规则 %s 阈值非法: warn=%v crit=%v", r.Key, r.Warn, r.Crit)
		}
		if r.Crit < r.Warn {
			t.Errorf("规则 %s 的 crit(%v) 不应小于 warn(%v)", r.Key, r.Crit, r.Warn)
		}
		if r.Hold <= 0 || r.Recover <= 0 {
			t.Errorf("规则 %s 的 hold/recover 应为正数", r.Key)
		}
		if r.LabelKey == "" {
			t.Errorf("规则 %s 缺少 label_key", r.Key)
		}
	}
}

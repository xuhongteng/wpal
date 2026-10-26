// Package alerts 实现阈值告警状态机。
//
// 状态流转：
//
//	正常 → 持续超阈 hold 秒 → firing(warning / critical)
//	firing → 等级升级 → 立即再出一条 firing
//	firing → 持续回落 recover 秒 → resolved
package alerts

import (
	"time"

	"mon/internal/collector"
)

// Rule 是一条阈值规则。
type Rule struct {
	Key      string
	Source   string
	Label    string // 中文兜底文案
	LabelKey string // i18n key
	Warn     float64
	Crit     float64
	Unit     string
	Hold     time.Duration
	Recover  time.Duration
	// Extract 从快照中取出该规则的当前值。
	Extract func(*collector.Snapshot) float64
	// Detail 生成中文兜底详情；Params 生成 i18n 参数。
	Detail func(*collector.Snapshot) string
	Params func(*collector.Snapshot) map[string]any
}

type ruleState struct {
	active string // "" | "warning" | "critical"
	since  time.Time
	value  float64
}

// Engine 是告警引擎。
type Engine struct {
	rules []Rule
	state map[string]*ruleState
}

// New 创建告警引擎。
func New(rules []Rule) *Engine {
	e := &Engine{rules: rules, state: map[string]*ruleState{}}
	for _, r := range rules {
		e.state[r.Key] = &ruleState{}
	}
	return e
}

// Update 用新快照推进状态机，返回本次新产生的事件。
func (e *Engine) Update(snap *collector.Snapshot, now time.Time) []collector.Event {
	events := []collector.Event{}
	for _, rule := range e.rules {
		value := 0.0
		if rule.Extract != nil {
			value = rule.Extract(snap)
		}
		st := e.state[rule.Key]
		target := levelFor(rule, value)

		switch {
		case target != "" && st.active == "":
			// 首次越过阈值：开始计时，持续 hold 秒后才真正触发
			if st.since.IsZero() {
				st.since = now
			} else if now.Sub(st.since) >= rule.Hold {
				st.active = target
				st.since = time.Time{}
				events = append(events, e.newEvent(rule, value, target, "firing", snap, now))
			}
		case target != "" && st.active != "" && target != st.active:
			// 等级变化（例如 warning 升级为 critical）立即出事件
			st.active = target
			st.since = time.Time{}
			events = append(events, e.newEvent(rule, value, target, "firing", snap, now))
		case target == "" && st.active != "":
			// 回落：持续 recover 秒后判定为恢复
			if st.since.IsZero() {
				st.since = now
			} else if now.Sub(st.since) >= rule.Recover {
				events = append(events, e.newEvent(rule, value, st.active, "resolved", snap, now))
				st.active = ""
				st.since = time.Time{}
			}
		default:
			st.since = time.Time{}
		}
		st.value = value
	}
	return events
}

func levelFor(rule Rule, value float64) string {
	if value >= rule.Crit {
		return "critical"
	}
	if value >= rule.Warn {
		return "warning"
	}
	return ""
}

func (e *Engine) newEvent(rule Rule, value float64, level, status string, snap *collector.Snapshot, now time.Time) collector.Event {
	ev := collector.Event{
		TS:       float64(now.UnixNano()) / 1e9,
		Key:      rule.Key,
		Label:    rule.Label,
		LabelKey: rule.LabelKey,
		Level:    level,
		Status:   status,
		Value:    round2(value),
		Unit:     rule.Unit,
	}
	if rule.Detail != nil {
		ev.Detail = rule.Detail(snap)
	}
	if rule.Params != nil {
		ev.DetailKey = rule.LabelKey + ".detail"
		ev.DetailParams = rule.Params(snap)
	}
	return ev
}

// Active 返回当前处于触发状态的告警。
func (e *Engine) Active(snap *collector.Snapshot) []collector.ActiveAlert {
	out := []collector.ActiveAlert{}
	for _, rule := range e.rules {
		st := e.state[rule.Key]
		if st.active != "" {
			a := collector.ActiveAlert{
				Key:      rule.Key,
				Label:    rule.Label,
				LabelKey: rule.LabelKey,
				Level:    st.active,
				Value:    round2(st.value),
				Unit:     rule.Unit,
				Source:   rule.Source,
			}
			if rule.Detail != nil {
				a.Detail = rule.Detail(snap)
			}
			if rule.Params != nil {
				a.DetailKey = rule.LabelKey + ".detail"
				a.DetailParams = rule.Params(snap)
			}
			out = append(out, a)
		}
	}
	return out
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

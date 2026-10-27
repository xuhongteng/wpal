// Package sampler 负责周期性采集、落盘、告警判定与广播。
//
// 关键设计：全局只有一个采样循环，所有客户端共享同一帧数据。
// 因此客户端数量增加时，采集开销不变，只有广播成本线性增长（可忽略）。
package sampler

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"mon/internal/alerts"
	"mon/internal/collector"
	"mon/internal/history"
)

// Sampler 是采样与广播中心。
type Sampler struct {
	col      collector.Collector
	interval time.Duration
	persist  time.Duration
	ring     *history.Ring
	events   *history.EventStore
	engine   *alerts.Engine

	mu      sync.RWMutex
	latest  *collector.Snapshot
	rawJSON []byte

	subMu sync.Mutex
	subs  map[int]chan []byte
	nextS int
}

// New 创建采样器。
func New(col collector.Collector, interval, persist time.Duration,
	ring *history.Ring, events *history.EventStore, engine *alerts.Engine) *Sampler {
	if interval <= 0 {
		interval = time.Second
	}
	if persist <= 0 {
		persist = 10 * time.Second
	}
	return &Sampler{
		col: col, interval: interval, persist: persist,
		ring: ring, events: events, engine: engine,
		subs: map[int]chan []byte{},
	}
}

// Run 启动采样循环，直到 ctx 被取消。
func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	lastPersist := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap, err := s.col.Collect()
			if err != nil {
				continue
			}
			now := time.Now()

			// 告警判定
			events := s.engine.Update(snap, now)
			for _, ev := range events {
				s.events.Add(ev)
			}
			snap.Events = events
			snap.ActiveAlerts = s.engine.Active(snap)

			// 落盘（按需降频）
			if s.ring != nil && now.Sub(lastPersist) >= s.persist {
				// 写盘失败只影响历史回看，不应中断实时流。
				_ = s.ring.Append(toSample(snap))
				lastPersist = now
			}

			// 广播
			if raw, err := json.Marshal(snap); err == nil {
				s.mu.Lock()
				s.latest = snap
				s.rawJSON = raw
				s.mu.Unlock()
				s.broadcast(raw)
			}
		}
	}
}

// Latest 返回最近一帧快照。
func (s *Sampler) Latest() *collector.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// LatestJSON 返回最近一帧的 JSON 编码结果。
func (s *Sampler) LatestJSON() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rawJSON
}

// Subscribe 订阅广播流，返回订阅 id 与接收通道。
func (s *Sampler) Subscribe() (int, chan []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextS
	s.nextS++
	ch := make(chan []byte, 4)
	s.subs[id] = ch
	return id, ch
}

// Unsubscribe 取消订阅。
func (s *Sampler) Unsubscribe(id int) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}

// broadcast 把一帧数据发给所有订阅者。通道满时丢弃旧数据（保实时性）。
func (s *Sampler) broadcast(raw []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- raw:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- raw:
			default:
			}
		}
	}
}

// toSample 把快照压缩成历史记录（只保留画图需要的数值）。
func toSample(s *collector.Snapshot) history.Sample {
	sm := history.Sample{
		TS:      int64(s.TS),
		CPU:     float32(s.CPU.Total),
		Mem:     float32(s.Mem.Percent),
		MemUsed: s.Mem.Used,
		Swap:    float32(s.Mem.SwapPercent),
		Load1:   float32(s.CPU.Load1),
	}
	sm.Brk = [6]float32{
		float32(s.CPU.Breakdown.Busy),
		float32(s.CPU.Breakdown.User),
		float32(s.CPU.Breakdown.System),
		float32(s.CPU.Breakdown.Iowait),
		float32(s.CPU.Breakdown.Steal),
		float32(s.CPU.Breakdown.Idle),
	}
	for i, c := range s.CPU.PerCore {
		if i >= len(sm.PerCore) {
			break
		}
		sm.PerCore[i] = float32(c.Busy)
	}
	for _, i := range s.Net.Interfaces {
		sm.NetRx += i.RxBPS
		sm.NetTx += i.TxBPS
	}
	for _, d := range s.Disk.IO {
		sm.IOR += d.ReadBPS
		sm.IOW += d.WriteBPS
	}
	return sm
}

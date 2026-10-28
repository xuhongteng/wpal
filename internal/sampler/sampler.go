// Package sampler 负责周期性采集、落盘、告警判定与广播。
//
// 关键设计：全局只有一个采样循环，所有客户端共享同一帧数据。
// 因此客户端数量增加时，采集开销不变，只有广播成本线性增长（可忽略）。
package sampler

import (
	"context"
	"encoding/json"
	"log"
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
	subs  map[int]*Subscriber
	nextS int

	// coreTruncWarned 保证「逐核数超过历史存储上限」只告警一次。
	coreTruncWarned bool
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
		subs: map[int]*Subscriber{},
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

			// 逐核数超过历史存储能力时明确告警一次，避免逐核序列被静默截断
			if !s.coreTruncWarned && s.ring != nil && snap.CPU.Cores > history.MaxPerCore {
				s.coreTruncWarned = true
				log.Printf("提示: 本机 %d 个逻辑核超过历史存储上限 %d，逐核历史只保留前 %d 个（实时帧不受影响）",
					snap.CPU.Cores, history.MaxPerCore, history.MaxPerCore)
			}

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

// Subscriber 是单个客户端的一帧订阅。
//
// 只保留「最新一帧」的语义：新帧覆盖未取走的旧帧，
// 因此慢客户端不会把整条链路拖慢，也永远不会反压采样循环。
// 相比原来的有界通道，这里由接收方独占取帧，
// 不存在「发送端把发给客户端的帧自己消费掉」的怪异行为。
type Subscriber struct {
	mu     sync.Mutex
	frame  []byte
	notify chan struct{}
	closed chan struct{}
	once   sync.Once
}

// set 覆盖为最新帧并唤醒等待者；已有未处理的唤醒信号时直接复用。
func (s *Subscriber) set(frame []byte) {
	s.mu.Lock()
	s.frame = frame
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Take 取走最新帧；没有新帧时返回 nil。
func (s *Subscriber) Take() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.frame
	s.frame = nil
	return f
}

// Notify 在有待取帧时收到信号。
func (s *Subscriber) Notify() <-chan struct{} { return s.notify }

// Closed 在订阅被取消时关闭。
func (s *Subscriber) Closed() <-chan struct{} { return s.closed }

func (s *Subscriber) close() {
	s.once.Do(func() { close(s.closed) })
}

// Subscribe 订阅广播流，返回订阅 id 与订阅句柄。
func (s *Sampler) Subscribe() (int, *Subscriber) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextS
	s.nextS++
	sub := &Subscriber{
		notify: make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
	s.subs[id] = sub
	return id, sub
}

// Unsubscribe 取消订阅。
func (s *Sampler) Unsubscribe(id int) {
	s.subMu.Lock()
	sub, ok := s.subs[id]
	if ok {
		delete(s.subs, id)
	}
	s.subMu.Unlock()
	if ok {
		sub.close()
	}
}

// broadcast 把最新一帧塞给所有订阅者。
//
// 这里先把订阅者快照成切片再逐个 set，避免在持锁期间唤醒客户端。
func (s *Sampler) broadcast(raw []byte) {
	s.subMu.Lock()
	subs := make([]*Subscriber, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subMu.Unlock()

	for _, sub := range subs {
		sub.set(raw)
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

package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"mon/internal/collector"
)

// EventStore 以 JSONL 追加方式保存告警事件，内存中保留最近 N 条。
type EventStore struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	limit  int
	items  []collector.Event
	nextID int64
}

// OpenEventStore 打开（必要时创建）事件文件。
func OpenEventStore(path string, limit int) (*EventStore, error) {
	if limit <= 0 {
		limit = 500
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	e := &EventStore{path: path, limit: limit, nextID: 1}

	// 读回历史事件
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var ev collector.Event
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			e.items = append(e.items, ev)
			if ev.ID >= e.nextID {
				e.nextID = ev.ID + 1
			}
		}
		f.Close()
	}
	if len(e.items) > limit {
		e.items = e.items[len(e.items)-limit:]
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	e.file = f
	return e, nil
}

// Add 记录一条事件并返回带 ID 的事件。
func (e *EventStore) Add(ev collector.Event) collector.Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	ev.ID = e.nextID
	e.nextID++
	line, err := json.Marshal(ev)
	if err == nil {
		e.file.Write(append(line, '\n'))
	}
	e.items = append(e.items, ev)
	if len(e.items) > e.limit {
		e.items = e.items[len(e.items)-e.limit:]
	}
	return ev
}

// Recent 返回最近 limit 条事件（倒序，最新在前）。
func (e *EventStore) Recent(limit int) []collector.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	if limit <= 0 || limit > len(e.items) {
		limit = len(e.items)
	}
	out := make([]collector.Event, 0, limit)
	for i := len(e.items) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, e.items[i])
	}
	return out
}

// Total 返回事件总数。
func (e *EventStore) Total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.items)
}

// Close 关闭文件。
func (e *EventStore) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.file.Close()
}

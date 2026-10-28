package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"mon/internal/collector"
)

// eventsCompactFactor 决定何时压缩文件：当文件里的行数超过
// limit 的这个倍数时，把文件重写成只剩最近 limit 条。
// 取 4 是在「写入放大」和「重启解析成本」之间取的折中。
const eventsCompactFactor = 4

// EventStore 以 JSONL 追加方式保存告警事件，内存中保留最近 N 条。
//
// 内存只保留最近 limit 条，但磁盘文件如果不做处理会无限增长；
// 因此每写满 limit*eventsCompactFactor 行就重写一次文件，
// 保证文件大小有上界，重启时的全量解析成本可控。
type EventStore struct {
	mu     sync.RWMutex
	file   *os.File
	path   string
	limit  int
	items  []collector.Event
	nextID int64
	// lines 是当前文件里的行数，用于触发压缩。
	lines int
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
			if len(sc.Bytes()) == 0 {
				continue
			}
			e.lines++
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
		// 落盘失败不应影响内存里的事件记录。
		if _, werr := e.file.Write(append(line, '\n')); werr == nil {
			e.lines++
		}
	}
	e.items = append(e.items, ev)
	if len(e.items) > e.limit {
		e.items = e.items[len(e.items)-e.limit:]
	}

	// 文件膨胀到阈值就重写为最近 limit 条，避免无限增长。
	if e.lines > e.limit*eventsCompactFactor {
		e.compactLocked()
	}
	return ev
}

// compactLocked 把事件文件重写为仅含内存里保留的最近 limit 条。
// 调用方需持有写锁。采用「写临时文件 + 原子 rename」，
// 中途失败时原文件保持完整，不会读出半截 JSONL。
func (e *EventStore) compactLocked() {
	tmp := e.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	n := 0
	for _, ev := range e.items {
		b, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return
		}
		n++
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, e.path); err != nil {
		os.Remove(tmp)
		return
	}

	// rename 后旧的追加句柄仍指向已被替换的 inode，必须重开。
	nf, err := os.OpenFile(e.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		// 打不开新文件时保留旧句柄（虽然指向旧 inode，但至少还能写）。
		return
	}
	e.file.Close()
	e.file = nf
	e.lines = n
}

// Recent 返回最近 limit 条事件（倒序，最新在前）。
func (e *EventStore) Recent(limit int) []collector.Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
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
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.items)
}

// Close 关闭文件。
func (e *EventStore) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.file.Close()
}

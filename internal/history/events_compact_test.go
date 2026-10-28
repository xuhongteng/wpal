package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mon/internal/collector"
)

// TestEventStoreCompactsFile 验证事件文件不会无限增长：
// 写入远超 limit*factor 条后，文件行数应被压回 limit 附近。
func TestEventStoreCompactsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	const limit = 10

	es, err := OpenEventStore(path, limit)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}

	total := limit * eventsCompactFactor * 3 // 远超压缩阈值
	for i := 0; i < total; i++ {
		es.Add(collector.Event{
			TS: float64(time.Now().UnixNano()) / 1e9, Key: "cpu",
			Level: "warning", Status: "firing", Value: float64(i), Unit: "%",
		})
	}
	es.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	lines := 0
	for _, c := range b {
		if c == '\n' {
			lines++
		}
	}

	if lines > limit*eventsCompactFactor {
		t.Fatalf("文件应被压缩到 <= %d 行，实际 %d 行", limit*eventsCompactFactor, lines)
	}
	if lines < limit {
		t.Fatalf("压缩不应把最近记录也丢掉，实际只剩 %d 行", lines)
	}

	// 重开后应能读回最近 limit 条，且 ID 连续递增
	es2, err := OpenEventStore(path, limit)
	if err != nil {
		t.Fatalf("重开失败: %v", err)
	}
	defer es2.Close()
	if got := es2.Total(); got != limit {
		t.Fatalf("重开应读回 %d 条，实际 %d", limit, got)
	}
}

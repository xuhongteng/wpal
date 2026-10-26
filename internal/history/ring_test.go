package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mon/internal/collector"
)

// collectorEvent 造一条测试用事件。
func collectorEvent(key string, v float64) collector.Event {
	return collector.Event{
		TS:     float64(time.Now().UnixNano()) / 1e9,
		Key:    key,
		Label:  "CPU 使用率",
		Level:  "warning",
		Status: "firing",
		Value:  v,
		Unit:   "%",
	}
}

func TestRingAppendAndQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	r, err := Open(path, 64, 8)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer r.Close()

	base := time.Now().Unix()
	for i := 0; i < 20; i++ {
		s := Sample{
			TS:      base + int64(i),
			CPU:     float32(i),
			Mem:     10,
			MemUsed: 100,
			Swap:    0,
			Load1:   0.5,
			NetRx:   float64(i) * 100,
			NetTx:   float64(i) * 50,
			IOR:     float64(i),
			IOW:     float64(i) * 2,
		}
		s.Brk[0] = float32(i)
		s.PerCore[0] = float32(i)
		if err := r.Append(s); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}

	all, err := r.Query(time.Unix(base, 0))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(all) != 20 {
		t.Fatalf("应返回 20 条，得到 %d 条", len(all))
	}
	if all[0].TS != base || all[19].TS != base+19 {
		t.Errorf("时间戳顺序不对: %d..%d", all[0].TS, all[19].TS)
	}
	if all[19].CPU != 19 {
		t.Errorf("CPU 值不对: %v", all[19].CPU)
	}
	if all[19].NetRx != 1900 || all[19].IOW != 38 {
		t.Errorf("数值字段不对: netRx=%v ioW=%v", all[19].NetRx, all[19].IOW)
	}
}

func TestRingWrapsAround(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	r, err := Open(path, 8, 4) // 容量 8 条
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer r.Close()

	base := time.Now().Unix()
	for i := 0; i < 20; i++ {
		if err := r.Append(Sample{TS: base + int64(i), CPU: float32(i)}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}

	all, err := r.Query(time.Unix(base, 0))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(all) != 8 {
		t.Fatalf("环形覆盖后应保留 8 条，得到 %d 条", len(all))
	}
	if all[0].TS != base+12 || all[7].TS != base+19 {
		t.Errorf("应保留最新的 8 条（%d..%d），实际 %d..%d",
			base+12, base+19, all[0].TS, all[7].TS)
	}
}

func TestRingPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	r, err := Open(path, 64, 8)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	base := time.Now().Unix()
	for i := 0; i < 5; i++ {
		if err := r.Append(Sample{TS: base + int64(i), CPU: float32(i * 2)}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}
	r.Close()

	r2, err := Open(path, 64, 8)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer r2.Close()

	all, err := r2.Query(time.Unix(base, 0))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("重开后应读到 5 条，得到 %d 条", len(all))
	}
	if all[4].CPU != 8 {
		t.Errorf("重开后数值不对: %v", all[4].CPU)
	}
}

func TestRingFileSizeIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	r, err := Open(path, 32, 8)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer r.Close()

	for i := 0; i < 500; i++ {
		if err := r.Append(Sample{TS: time.Now().Unix()}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	maxSize := int64(headerSize + 32*recordSize)
	if fi.Size() > maxSize {
		t.Errorf("文件超出上限: %d > %d", fi.Size(), maxSize)
	}
}

func TestEventStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	e, err := OpenEventStore(path, 3)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer e.Close()

	for i := 0; i < 5; i++ {
		e.Add(collectorEvent("cpu", float64(i)))
	}

	recent := e.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("应保留最近 3 条，得到 %d 条", len(recent))
	}
	if recent[0].Value != 4 {
		t.Errorf("最新一条的值应为 4，得到 %v", recent[0].Value)
	}
	if e.Total() != 3 {
		t.Errorf("总数应为 3，得到 %d", e.Total())
	}
}

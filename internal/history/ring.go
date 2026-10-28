// Package history 提供轻量历史存储：定长记录的环形文件 + 内存窗口。
//
// 之所以不用 SQLite：
//   - 少一个 cgo/SQL 依赖，静态编译更干净；
//   - 文件大小恒定（环形覆盖），不会把磁盘吃满；
//   - 只保留画图需要的数值，格式简单、跨平台一致。
//
// 磁盘上保存完整留存期（如 7 天），内存里只保留最近一小段窗口，
// 保证常驻内存不被历史数据撑大；长范围查询直接读文件。
package history

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	headerSize = 32
	// 单条记录 = TS(8) + CPU/Mem/Swap/Load1(16) + MemUsed(8) + Brk(24)
	// + PerCore(128) + NetRx/NetTx/IOR/IOW(32) = 216 字节
	recordSize = 216
	magic      = "LITESGT1"
	// version 2：PerCore 从 64 槽降到 32 槽（原 344 字节记录里 74% 是逐核数据，
	// 而历史回看并不消费逐核序列，砍半后单条记录体积下降 37%）。
	// 版本不匹配时 load() 会重建文件，旧历史会被清空。
	version      = 2
	perCoreSlots = 32
)

// MaxPerCore 是历史记录里能保存的逐核上限，超出的核心会被丢弃。
const MaxPerCore = perCoreSlots

// Sample 是一条历史采样。字段顺序即磁盘布局，改动必须同步 bump version。
type Sample struct {
	TS      int64
	CPU     float32
	Mem     float32
	MemUsed float64
	Swap    float32
	Load1   float32
	Brk     [6]float32 // busy, user, system, iowait, steal, idle
	PerCore [perCoreSlots]float32
	NetRx   float64
	NetTx   float64
	IOR     float64
	IOW     float64
}

type header struct {
	Magic    [8]byte
	Version  uint32
	Capacity uint32
	Head     uint64
	Count    uint64
}

// Ring 是环形历史存储。
type Ring struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	cap    int
	head   uint64
	mem    []Sample
	memCap int
}

// Open 打开（必要时创建）环形文件。
//
// capacity 是磁盘上保留的记录条数，memWindow 是常驻内存中保留的条数。
func Open(path string, capacity, memWindow int) (*Ring, error) {
	if capacity <= 0 {
		capacity = 60480 // 7 天 @ 10s
	}
	if memWindow <= 0 {
		memWindow = 360 // 1 小时 @ 10s
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	r := &Ring{file: f, path: path, cap: capacity, memCap: memWindow, mem: make([]Sample, 0, memWindow)}
	if err := r.load(); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// load 读取文件头，必要时初始化文件。
func (r *Ring) load() error {
	fi, err := r.file.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < headerSize {
		return r.initFile()
	}
	var h header
	hbuf := make([]byte, headerSize)
	if _, err := r.file.ReadAt(hbuf, 0); err != nil {
		return err
	}
	copy(h.Magic[:], hbuf[:8])
	h.Version = binary.LittleEndian.Uint32(hbuf[8:12])
	h.Capacity = binary.LittleEndian.Uint32(hbuf[12:16])
	h.Head = binary.LittleEndian.Uint64(hbuf[16:24])
	h.Count = binary.LittleEndian.Uint64(hbuf[24:32])

	if string(h.Magic[:]) != magic || h.Version != version {
		// 版本或魔数不匹配：重置文件，避免读到错乱数据
		return r.initFile()
	}
	r.head = h.Head
	// 把尾部 memWindow 条读进内存
	all, err := r.readAll()
	if err != nil {
		return err
	}
	if len(all) > r.memCap {
		all = all[len(all)-r.memCap:]
	}
	r.mem = append(r.mem[:0], all...)
	return nil
}

func (r *Ring) initFile() error {
	hbuf := make([]byte, headerSize)
	copy(hbuf[:8], magic)
	binary.LittleEndian.PutUint32(hbuf[8:12], version)
	binary.LittleEndian.PutUint32(hbuf[12:16], uint32(r.cap))
	binary.LittleEndian.PutUint64(hbuf[16:24], 0)
	binary.LittleEndian.PutUint64(hbuf[24:32], 0)
	if _, err := r.file.WriteAt(hbuf, 0); err != nil {
		return err
	}
	// 预分配文件，空间一次到位，避免后续写入时文件反复增长
	if err := r.file.Truncate(headerSize + int64(r.cap)*recordSize); err != nil {
		return err
	}
	r.head = 0
	r.mem = r.mem[:0]
	return nil
}

// Append 追加一条采样。
func (r *Ring) Append(s Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	buf := encodeSample(s)
	off := int64(headerSize) + int64(r.head%uint64(r.cap))*recordSize
	if _, err := r.file.WriteAt(buf, off); err != nil {
		return err
	}
	r.head++

	// 更新文件头
	hbuf := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(hbuf[8:12], version)
	binary.LittleEndian.PutUint32(hbuf[12:16], uint32(r.cap))
	binary.LittleEndian.PutUint64(hbuf[16:24], r.head)
	binary.LittleEndian.PutUint64(hbuf[24:32], uint64(len(r.mem)))
	copy(hbuf[:8], magic)
	if _, err := r.file.WriteAt(hbuf, 0); err != nil {
		return err
	}

	// 内存窗口
	if len(r.mem) >= r.memCap {
		copy(r.mem, r.mem[len(r.mem)-r.memCap+1:])
		r.mem = r.mem[:r.memCap-1]
	}
	r.mem = append(r.mem, s)
	return nil
}

// Query 返回 since 之后的采样（按时间升序）。
func (r *Ring) Query(since time.Time) ([]Sample, error) {
	return r.QueryDownsampled(since, 0)
}

// QueryDownsampled 返回 since 之后并降采样到最多 maxPoints 点的采样切片。
func (r *Ring) QueryDownsampled(since time.Time, maxPoints int) ([]Sample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cut := since.Unix()

	// 优先检查常驻内存窗口：若查询范围在内存环覆盖时间内，直接零磁盘 IO 返回
	if len(r.mem) > 0 && r.mem[0].TS <= cut {
		out := make([]Sample, 0, len(r.mem))
		for _, s := range r.mem {
			if s.TS >= cut {
				out = append(out, s)
			}
		}
		if maxPoints > 0 && len(out) > maxPoints {
			return Downsample(out, maxPoints), nil
		}
		return out, nil
	}

	all, err := r.readAll()
	if err != nil {
		return nil, err
	}
	out := make([]Sample, 0, len(all))
	for _, s := range all {
		if s.TS >= cut {
			out = append(out, s)
		}
	}
	if maxPoints > 0 && len(out) > maxPoints {
		return Downsample(out, maxPoints), nil
	}
	return out, nil
}

// readAll 读出文件中的全部有效记录。调用方需持有锁。
// 通过计算环形偏移，将数万次分散的系统调用优化为最多 2 次连续大块读取。
func (r *Ring) readAll() ([]Sample, error) {
	hbuf := make([]byte, headerSize)
	if _, err := r.file.ReadAt(hbuf, 0); err != nil {
		return nil, err
	}
	head := binary.LittleEndian.Uint64(hbuf[16:24])
	n := head
	if n > uint64(r.cap) {
		n = uint64(r.cap)
	}
	if n == 0 {
		return nil, nil
	}

	startIdx := head - n
	startSlot := int(startIdx % uint64(r.cap))

	out := make([]Sample, 0, n)
	seg1Count := int(n)
	if startSlot+seg1Count > r.cap {
		seg1Count = r.cap - startSlot
	}
	seg2Count := int(n) - seg1Count

	// 读取段 1（从 startSlot 至文件末尾或 head）
	if seg1Count > 0 {
		buf1 := make([]byte, seg1Count*recordSize)
		off1 := int64(headerSize) + int64(startSlot)*int64(recordSize)
		if _, err := r.file.ReadAt(buf1, off1); err != nil {
			return out, nil
		}
		for i := 0; i < seg1Count; i++ {
			out = append(out, decodeSample(buf1[i*recordSize:(i+1)*recordSize]))
		}
	}

	// 读取段 2（环形回绕到文件起始段）
	if seg2Count > 0 {
		buf2 := make([]byte, seg2Count*recordSize)
		off2 := int64(headerSize)
		if _, err := r.file.ReadAt(buf2, off2); err != nil {
			return out, nil
		}
		for i := 0; i < seg2Count; i++ {
			out = append(out, decodeSample(buf2[i*recordSize:(i+1)*recordSize]))
		}
	}

	return out, nil
}

// Close 关闭文件。
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

// Path 返回文件路径。
func (r *Ring) Path() string { return r.path }

// Stats 返回当前记录数与磁盘占用。
func (r *Ring) Stats() (count int, bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hbuf := make([]byte, headerSize)
	if _, err := r.file.ReadAt(hbuf, 0); err != nil {
		return 0, 0
	}
	head := binary.LittleEndian.Uint64(hbuf[16:24])
	if head > uint64(r.cap) {
		head = uint64(r.cap)
	}
	fi, err := r.file.Stat()
	if err != nil {
		return int(head), 0
	}
	return int(head), fi.Size()
}

func encodeSample(s Sample) []byte {
	buf := make([]byte, recordSize)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(s.TS))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(s.CPU))
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(s.Mem))
	binary.LittleEndian.PutUint64(buf[16:24], math.Float64bits(s.MemUsed))
	binary.LittleEndian.PutUint32(buf[24:28], math.Float32bits(s.Swap))
	binary.LittleEndian.PutUint32(buf[28:32], math.Float32bits(s.Load1))
	for i, v := range s.Brk {
		binary.LittleEndian.PutUint32(buf[32+i*4:36+i*4], math.Float32bits(v))
	}
	for i, v := range s.PerCore {
		binary.LittleEndian.PutUint32(buf[56+i*4:60+i*4], math.Float32bits(v))
	}
	binary.LittleEndian.PutUint64(buf[184:192], math.Float64bits(s.NetRx))
	binary.LittleEndian.PutUint64(buf[192:200], math.Float64bits(s.NetTx))
	binary.LittleEndian.PutUint64(buf[200:208], math.Float64bits(s.IOR))
	binary.LittleEndian.PutUint64(buf[208:216], math.Float64bits(s.IOW))
	return buf
}

func decodeSample(buf []byte) Sample {
	var s Sample
	s.TS = int64(binary.LittleEndian.Uint64(buf[0:8]))
	s.CPU = math.Float32frombits(binary.LittleEndian.Uint32(buf[8:12]))
	s.Mem = math.Float32frombits(binary.LittleEndian.Uint32(buf[12:16]))
	s.MemUsed = math.Float64frombits(binary.LittleEndian.Uint64(buf[16:24]))
	s.Swap = math.Float32frombits(binary.LittleEndian.Uint32(buf[24:28]))
	s.Load1 = math.Float32frombits(binary.LittleEndian.Uint32(buf[28:32]))
	for i := 0; i < 6; i++ {
		s.Brk[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[32+i*4 : 36+i*4]))
	}
	for i := 0; i < perCoreSlots; i++ {
		s.PerCore[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[56+i*4 : 60+i*4]))
	}
	s.NetRx = math.Float64frombits(binary.LittleEndian.Uint64(buf[184:192]))
	s.NetTx = math.Float64frombits(binary.LittleEndian.Uint64(buf[192:200]))
	s.IOR = math.Float64frombits(binary.LittleEndian.Uint64(buf[200:208]))
	s.IOW = math.Float64frombits(binary.LittleEndian.Uint64(buf[208:216]))
	return s
}

// String 用于调试。
func (s Sample) String() string {
	return fmt.Sprintf("Sample{ts=%d cpu=%.1f mem=%.1f}", s.TS, s.CPU, s.Mem)
}

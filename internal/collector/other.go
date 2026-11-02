//go:build !linux

package collector

import (
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// otherCollector 是 macOS / Windows 等非 Linux 平台的实现。
//
// Linux 走直读 /proc 的专用实现（更快、更准），
// 其余平台用 gopsutil 做跨平台抽象；字段语义与 Linux 版保持一致，
// 平台不具备的指标（inode、steal 等）填 0 而非报错。
type otherCollector struct {
	opts Options

	prevTotalCPU cpu.TimesStat
	hasPrevTotal bool
	prevPerCore  []cpu.TimesStat
	prevDisk     map[string]disk.IOCountersStat
	prevNet      map[string]net.IOCountersStat
	prevProc     map[int32]float64
	prevAt       time.Time

	// 温档（进程榜）缓存：到点才重新枚举，其余帧复用上次结果。
	procAt         time.Time
	cachedProcs    []Proc
	cachedProcsMem []Proc
	cachedProcSt   procStats

	// 冷档缓存：TCP 连接状态与线程总数。
	slowAt         time.Time
	cachedTCP      map[string]int
	cachedTCPTotal int
	cachedThreads  int
}

func newCollector(opts Options) Collector {
	return &otherCollector{
		opts:     opts,
		prevDisk: map[string]disk.IOCountersStat{},
		prevNet:  map[string]net.IOCountersStat{},
		prevProc: map[int32]float64{},
		prevAt:   time.Now(),
	}
}

func (c *otherCollector) Close() error { return nil }

func (c *otherCollector) Collect() (*Snapshot, error) {
	now := time.Now()
	dt := now.Sub(c.prevAt).Seconds()
	if dt <= 0 {
		dt = 1
	}

	snap := &Snapshot{
		TS:           float64(now.UnixNano()) / 1e9,
		Procs:        []Proc{},
		ProcsMem:     []Proc{},
		Events:       []Event{},
		ActiveAlerts: []ActiveAlert{},
	}

	// ---- 热档：每帧都采，实时看板的核心数据 ----
	snap.Host = c.collectHost(now)
	snap.CPU = c.collectCPU(dt)
	snap.Mem = c.collectMem()
	snap.Disk = c.collectDisk(dt)
	snap.Net = c.collectNet(dt)

	// ---- 温档：进程榜 ----
	//
	// 进程榜不需要秒级：排名变化远慢于 1Hz，而枚举全表在 macOS 上要 fork
	// ps 子进程（实测约 60ms CPU + 150KB 分配），是全场最贵的采集项。
	// 未到采样点时直接复用上次缓存，前端看到的仍是完整的 Snapshot。
	if shouldSample(c.procAt, now, c.opts.ProcessInterval) {
		// 差分间隔用「上次进程采样的实际间隔」，不能用主循环的 dt，
		// 否则占比会被降频比例稀释。见 sampleDelta 的说明。
		procDt := sampleDelta(c.procAt, now, c.opts.ProcessInterval)
		byCPU, byMem, st := c.collectProcs(procDt, snap.Mem.Total)
		c.cachedProcs, c.cachedProcsMem, c.cachedProcSt = byCPU, byMem, st
		// 非 macOS 平台线程数由进程枚举一并返回，这里顺带记下。
		if st.Threads > 0 {
			c.cachedThreads = st.Threads
		}
		c.procAt = now
	}
	snap.Procs, snap.ProcsMem = c.cachedProcs, c.cachedProcsMem

	// ---- 冷档：TCP 连接状态与线程数 ----
	//
	// 这两项数值缓变，且是全场最贵的采集（TCP 全表遍历约 135ms，
	// ps -axM 线程表约 40ms 且输出近 600KB），没有必要秒级刷新。
	if shouldSample(c.slowAt, now, c.opts.SlowInterval) {
		c.cachedTCP, c.cachedTCPTotal = c.sampleTCP()
		if perPID, total := sampleThreads(); total > 0 {
			c.cachedThreads = total
			// 线程数是进程属性，回填到已缓存的榜单条目上。
			applyProcThreads(c.cachedProcs, perPID)
			applyProcThreads(c.cachedProcsMem, perPID)
		}
		c.slowAt = now
	}
	snap.Net.TCP, snap.Net.TCPTotal = c.cachedTCP, c.cachedTCPTotal

	snap.Sys = c.collectSys(c.cachedProcSt)
	snap.Sys.Threads = c.cachedThreads

	c.prevAt = now
	return snap, nil
}

// applyProcThreads 把冷档采到的线程数回填到已缓存的进程榜条目上。
func applyProcThreads(rows []Proc, perPID map[int32]int) {
	if len(perPID) == 0 {
		return
	}
	for i := range rows {
		if n, ok := perPID[int32(rows[i].PID)]; ok {
			rows[i].Threads = n
		}
	}
}

func (c *otherCollector) collectHost(now time.Time) Host {
	hostname, _ := os.Hostname()
	uptime := getPlatformUptime(now)
	return Host{
		Hostname: hostname,
		Kernel:   runtime.GOOS + "/" + runtime.GOARCH,
		Label:    c.opts.Label,
		NodeID:   resolveNodeID(c.opts.NodeID, hostname),
		Uptime:   uptime,
		BootTime: float64(now.Unix()) - uptime,
	}
}

// timesPct 把 gopsutil 的 CPU 时间差换算成占比（口径与 Linux 版一致）。
func timesPct(cur, prev cpu.TimesStat) Breakdown {
	d := cpu.TimesStat{
		User:    cur.User - prev.User,
		Nice:    cur.Nice - prev.Nice,
		System:  cur.System - prev.System,
		Idle:    cur.Idle - prev.Idle,
		Iowait:  cur.Iowait - prev.Iowait,
		Irq:     cur.Irq - prev.Irq,
		Softirq: cur.Softirq - prev.Softirq,
		Steal:   cur.Steal - prev.Steal,
	}
	total := d.User + d.Nice + d.System + d.Idle + d.Iowait + d.Irq + d.Softirq + d.Steal
	if total <= 0 {
		return Breakdown{Idle: 100}
	}
	return Breakdown{
		Busy:   round2(100 * (total - d.Idle) / total),
		User:   round2(100 * (d.User + d.Nice) / total),
		System: round2(100 * (d.System + d.Irq + d.Softirq) / total),
		Iowait: round2(100 * d.Iowait / total),
		Steal:  round2(100 * d.Steal / total),
		Idle:   round2(100 * d.Idle / total),
	}
}

func (c *otherCollector) collectCPU(dt float64) CPU {
	curTotal, curPerCore, err := getPlatformCPUTimes()
	if err != nil {
		curPerCore = nil
	}

	total := Breakdown{Idle: 100}
	if c.hasPrevTotal {
		total = timesPct(curTotal, c.prevTotalCPU)
	}
	c.prevTotalCPU = curTotal
	c.hasPrevTotal = (err == nil)

	prevPerCore := c.prevPerCore
	c.prevPerCore = curPerCore

	percore := []Core{}
	for i := range curPerCore {
		p := Breakdown{Idle: 100}
		if i < len(prevPerCore) {
			p = timesPct(curPerCore[i], prevPerCore[i])
		}
		percore = append(percore, Core{
			Busy: p.Busy, User: p.User, System: p.System,
			Iowait: p.Iowait, Steal: p.Steal,
		})
	}

	logical, _ := cpu.Counts(true)
	physical, _ := cpu.Counts(false)

	l1, l5, l15 := 0.0, 0.0, 0.0
	if avg, err := load.Avg(); err == nil {
		l1, l5, l15 = avg.Load1, avg.Load5, avg.Load15
	}

	// gopsutil v4 用 cpu.Info() 提供频率信息（InfoStat.Mhz）
	freq := 0.0
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		sum := 0.0
		n := 0
		for _, in := range infos {
			if in.Mhz > 0 {
				sum += in.Mhz
				n++
			}
		}
		if n > 0 {
			freq = round1(sum / float64(n))
		}
	}

	return CPU{
		Total:     total.Busy,
		Breakdown: total,
		PerCore:   percore,
		Cores:     logical,
		Physical:  physical,
		Load1:     l1,
		Load5:     l5,
		Load15:    l15,
		FreqMHz:   freq,
	}
}

func (c *otherCollector) collectMem() Mem {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return Mem{}
	}
	sw, err := mem.SwapMemory()
	if err != nil {
		sw = &mem.SwapMemoryStat{}
	}
	// gopsutil 在 macOS 上不填 Cached/Buffers/Shared，这里用 Mach 的
	// 文件页统计补齐缓存一栏；其他平台仍使用 gopsutil 的返回值。
	cached, shared := getPlatformMemCache()
	if cached == 0 {
		cached = vm.Cached
	}
	if shared == 0 {
		shared = vm.Shared
	}

	return Mem{
		Total:       float64(vm.Total),
		Used:        float64(vm.Used),
		Available:   float64(vm.Available),
		Free:        float64(vm.Free),
		Percent:     round1(vm.UsedPercent),
		Cached:      float64(cached),
		Buffers:     float64(vm.Buffers),
		Shared:      float64(shared),
		SwapTotal:   float64(sw.Total),
		SwapUsed:    float64(sw.Used),
		SwapFree:    float64(sw.Free),
		SwapPercent: round1(sw.UsedPercent),
	}
}

func (c *otherCollector) collectDisk(dt float64) Disk {
	parts := []Partition{}
	if ps, err := disk.Partitions(false); err == nil {
		for _, p := range ps {
			if !realFS[p.Fstype] {
				continue
			}
			usage, err := disk.Usage(p.Mountpoint)
			if err != nil {
				continue
			}
			parts = append(parts, Partition{
				Device:  p.Device,
				Mount:   p.Mountpoint,
				Fstype:  p.Fstype,
				Total:   usage.Total,
				Used:    usage.Used,
				Free:    usage.Free,
				Percent: round1(usage.UsedPercent),
				// disk.Usage 本来就返回 inode 数据，之前被整体丢弃，
				// 导致 macOS/Windows 上「inode 使用率」告警规则永远无法触发。
				InodePercent: round1(usage.InodesUsedPercent),
				InodeTotal:   usage.InodesTotal,
				InodeFree:    usage.InodesFree,
			})
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Mount < parts[j].Mount })

	ioList := []DiskIO{}
	cur, err := disk.IOCounters()
	if err == nil {
		for name, c2 := range cur {
			p, ok := c.prevDisk[name]
			if !ok {
				continue
			}
			readBPS := float64(int64(c2.ReadBytes)-int64(p.ReadBytes)) / dt
			writeBPS := float64(int64(c2.WriteBytes)-int64(p.WriteBytes)) / dt
			readIOPS := float64(int64(c2.ReadCount)-int64(p.ReadCount)) / dt
			writeIOPS := float64(int64(c2.WriteCount)-int64(p.WriteCount)) / dt
			busy := 0.0
			dTime := float64(int64(c2.ReadTime)-int64(p.ReadTime)) + float64(int64(c2.WriteTime)-int64(p.WriteTime))
			if dTime > 0 {
				busy = dTime / 1000.0 / dt * 100.0
				if busy > 100 {
					busy = 100
				}
			}
			ioList = append(ioList, DiskIO{
				Name:      name,
				ReadBPS:   max0(readBPS),
				WriteBPS:  max0(writeBPS),
				ReadIOPS:  max0(readIOPS),
				WriteIOPS: max0(writeIOPS),
				Busy:      round2(busy),
			})
		}
		c.prevDisk = cur
	}
	sort.Slice(ioList, func(i, j int) bool {
		return ioList[i].ReadBPS+ioList[i].WriteBPS > ioList[j].ReadBPS+ioList[j].WriteBPS
	})
	if len(ioList) > 8 {
		ioList = ioList[:8]
	}
	return Disk{Partitions: parts, IO: ioList}
}

func (c *otherCollector) collectNet(dt float64) Net {
	// gopsutil v4 的 IOCounters 返回切片（每项含 Name）
	stats, err := net.IOCounters(true)
	if err != nil {
		stats = nil
	}
	cur := map[string]net.IOCountersStat{}
	for _, s := range stats {
		cur[s.Name] = s
	}

	ifaces := []Iface{}
	for name, c2 := range cur {
		virtual := hasAnyPrefix(name, virtualNicPrefix)
		if virtual && !c.opts.IncludeVirtualNics {
			continue
		}
		p := c.prevNet[name]
		ifaces = append(ifaces, Iface{
			Name:    name,
			RxBPS:   max0(float64(int64(c2.BytesRecv)-int64(p.BytesRecv)) / dt),
			TxBPS:   max0(float64(int64(c2.BytesSent)-int64(p.BytesSent)) / dt),
			RxPPS:   max0(float64(int64(c2.PacketsRecv)-int64(p.PacketsRecv)) / dt),
			TxPPS:   max0(float64(int64(c2.PacketsSent)-int64(p.PacketsSent)) / dt),
			RxTotal: c2.BytesRecv,
			TxTotal: c2.BytesSent,
			ErrIn:   c2.Errin,
			ErrOut:  c2.Errout,
			DropIn:  c2.Dropin,
			DropOut: c2.Dropout,
			Virtual: virtual,
		})
	}
	c.prevNet = cur
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].RxBPS+ifaces[i].TxBPS > ifaces[j].RxBPS+ifaces[j].TxBPS
	})

	// TCP 状态属于冷档，由 sampleTCP 单独采样并在 Collect 中合并。
	return Net{Interfaces: ifaces}
}

// sampleTCP 枚举全系统 TCP 连接并按状态归类（冷档）。
//
// 这是非 Linux 平台上第二个昂贵的采集项：gopsutil 的 Connections 会遍历
// 整张 TCP 连接表，实测在数百连接时约 135ms CPU。而它只用于界面上
// 一行「连接数」提示，完全没有秒级刷新的必要。
func (c *otherCollector) sampleTCP() (map[string]int, int) {
	tcp := map[string]int{}
	if conns, err := net.Connections("tcp"); err == nil {
		for _, conn := range conns {
			tcp[conn.Status]++
		}
	}
	total := 0
	for _, v := range tcp {
		total += v
	}
	return tcp, total
}

// collectSys 汇总系统级指标。procCount 由 collectProcs 一并返回，
// 避免为了拿进程数量再枚举一次进程表。
func (c *otherCollector) collectSys(st procStats) Sys {
	used, max := platformFileDescriptors()
	return Sys{
		Procs:        st.Procs,
		Threads:      st.Threads,
		ProcsRunning: st.Running,
		ProcsBlocked: st.Blocked,
		FDUsed:       used,
		FDMax:        max,
	}
}

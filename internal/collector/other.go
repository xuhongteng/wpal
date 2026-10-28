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
	"github.com/shirou/gopsutil/v4/process"
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

	snap.Host = c.collectHost(now)
	snap.CPU = c.collectCPU(dt)
	snap.Mem = c.collectMem()
	snap.Disk = c.collectDisk(dt)
	snap.Net = c.collectNet(dt)
	byCPU, byMem, threads, procCount := c.collectProcs(dt, snap.Mem.Total)
	snap.Procs, snap.ProcsMem = byCPU, byMem
	snap.Sys = c.collectSys(threads, procCount)

	c.prevAt = now
	return snap, nil
}

func (c *otherCollector) collectHost(now time.Time) Host {
	hostname, _ := os.Hostname()
	uptime := getPlatformUptime(now)
	return Host{
		Hostname: hostname,
		Kernel:   runtime.GOOS + "/" + runtime.GOARCH,
		Label:    c.opts.Label,
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
	return Mem{
		Total:       float64(vm.Total),
		Used:        float64(vm.Used),
		Available:   float64(vm.Available),
		Free:        float64(vm.Free),
		Percent:     round1(vm.UsedPercent),
		Cached:      float64(vm.Cached),
		Buffers:     float64(vm.Buffers),
		Shared:      float64(vm.Shared),
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
	return Net{Interfaces: ifaces, TCP: tcp, TCPTotal: total}
}

// collectProcs 采集进程榜，并顺带返回进程总数与线程总数。
//
// 关键性能设计：把进程属性按「成本」分成两级。
//   - 便宜级（Times / MemoryInfo / NumThreads）：每进程几十微秒，
//     可以对全部进程跑，用于算出 CPU 与内存排名。
//   - 昂贵级（Status / Cmdline / Username）：每进程约 1~3 毫秒，
//     在 macOS 上 p.Status() 走 sysctl 取 kinfo_proc，全量遍历
//     近千个进程要 3 秒以上——这正是曾经把 1 秒采样周期撑到 18 秒的元凶。
//
// 因此只对最终上榜的前 N 个进程补取昂贵字段。
// 实测 947 进程场景下，进程模块耗时从 2.3s 降到约 53ms。
func (c *otherCollector) collectProcs(dt float64, totalMem float64) (byCPU, byMem []Proc, threads, procCount int) {
	procs, err := process.Processes()
	if err != nil {
		return []Proc{}, []Proc{}, 0, 0
	}
	procCount = len(procs)

	// 第一级：只取便宜字段，建立完整排名依据。
	type candidate struct {
		p     *process.Process
		ticks float64
		rss   uint64
	}
	cands := make([]candidate, 0, len(procs))
	cur := map[int32]float64{}
	for _, p := range procs {
		times, err := p.Times()
		if err != nil {
			continue
		}
		ticks := times.User + times.System
		cur[p.Pid] = ticks

		nthreads, _ := p.NumThreads()
		threads += int(nthreads)

		memInfo, _ := p.MemoryInfo()
		var rss uint64
		if memInfo != nil {
			rss = memInfo.RSS
		}
		cands = append(cands, candidate{p: p, ticks: ticks, rss: rss})
	}
	// 必须在覆盖 prevProc 之前完成差值计算，否则 CPU 占比恒为 0。
	prevProc := c.prevProc
	c.prevProc = cur

	// 第二级：先按便宜字段各自排序，各取前 N，再对这批进程补昂贵字段。
	n := c.opts.TopProcesses
	byCPURank := append([]candidate{}, cands...)
	sort.SliceStable(byCPURank, func(i, j int) bool {
		return byCPURank[i].ticks > byCPURank[j].ticks
	})
	byMemRank := append([]candidate{}, cands...)
	sort.SliceStable(byMemRank, func(i, j int) bool {
		return byMemRank[i].rss > byMemRank[j].rss
	})
	if len(byCPURank) > n {
		byCPURank = byCPURank[:n]
	}
	if len(byMemRank) > n {
		byMemRank = byMemRank[:n]
	}

	// 两个榜单取并集，同一个进程只补取一次昂贵字段。
	enrich := map[int32]Proc{}
	pick := append(append([]candidate{}, byCPURank...), byMemRank...)
	for _, cand := range pick {
		pid := cand.p.Pid
		if _, ok := enrich[pid]; ok {
			continue
		}

		// gopsutil v4 的 Cmdline() 直接返回整条命令字符串
		cmdStr, _ := cand.p.Cmdline()
		if len(cmdStr) > 180 {
			cmdStr = cmdStr[:180]
		}
		statusList, _ := cand.p.Status()
		status := ""
		if len(statusList) > 0 {
			status = statusList[0]
		}
		user, _ := cand.p.Username()
		name, _ := cand.p.Name()
		nthreads, _ := cand.p.NumThreads()

		cpuPct := 0.0
		if prevTicks, ok := prevProc[pid]; ok && dt > 0 {
			delta := cand.ticks - prevTicks
			if delta > 0 {
				cpuPct = round1(delta / dt * 100)
			}
		}
		memPct := 0.0
		if totalMem > 0 {
			memPct = round3(float64(cand.rss) / totalMem * 100)
		}
		enrich[pid] = Proc{
			PID:     int(pid),
			Name:    name,
			User:    user,
			Status:  status,
			CPU:     cpuPct,
			RSS:     cand.rss,
			MemPct:  memPct,
			Threads: int(nthreads),
			Cmd:     cmdStr,
		}
	}

	byCPU = make([]Proc, 0, len(byCPURank))
	for _, cand := range byCPURank {
		if pr, ok := enrich[cand.p.Pid]; ok {
			byCPU = append(byCPU, pr)
		}
	}
	byMem = make([]Proc, 0, len(byMemRank))
	for _, cand := range byMemRank {
		if pr, ok := enrich[cand.p.Pid]; ok {
			byMem = append(byMem, pr)
		}
	}
	return byCPU, byMem, threads, procCount
}

// collectSys 汇总系统级指标。procCount 由 collectProcs 一并返回，
// 避免为了拿进程数量再枚举一次进程表。
func (c *otherCollector) collectSys(threads, procCount int) Sys {
	return Sys{
		Procs:   procCount,
		Threads: threads,
	}
}

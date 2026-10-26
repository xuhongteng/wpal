//go:build !linux

package collector

import (
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

	prevCPU  []cpu.TimesStat
	prevDisk map[string]disk.IOCountersStat
	prevNet  map[string]net.IOCountersStat
	prevProc map[int32]float64
	prevAt   time.Time
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
	byCPU, byMem, threads := c.collectProcs(dt, snap.Mem.Total)
	snap.Procs, snap.ProcsMem = byCPU, byMem
	snap.Sys = c.collectSys(threads)

	c.prevAt = now
	return snap, nil
}

func (c *otherCollector) collectHost(now time.Time) Host {
	hostname, _ := os.Hostname()
	uptime := 0.0
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
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
	times, err := cpu.Times(true)
	if err != nil {
		times = nil
	}
	cur := times
	prev := c.prevCPU
	c.prevCPU = cur

	total := Breakdown{Idle: 100}
	if len(cur) > 0 && len(prev) == len(cur) {
		total = timesPct(cur[0], prev[0])
	}
	percore := []Core{}
	for i := range cur {
		p := Breakdown{Idle: 100}
		if i < len(prev) {
			p = timesPct(cur[i], prev[i])
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

func (c *otherCollector) collectProcs(dt float64, totalMem float64) (byCPU, byMem []Proc, threads int) {
	procs, err := process.Processes()
	if err != nil {
		return []Proc{}, []Proc{}, 0
	}
	rows := []Proc{}
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

		cpuPct := 0.0
		if prev, ok := c.prevProc[p.Pid]; ok {
			delta := ticks - prev
			if delta > 0 && dt > 0 {
				cpuPct = round1(delta / dt * 100)
			}
		}
		memInfo, _ := p.MemoryInfo()
		rss := uint64(0)
		if memInfo != nil {
			rss = memInfo.RSS
		}
		memPct := 0.0
		if totalMem > 0 {
			memPct = round3(float64(rss) / totalMem * 100)
		}
		// gopsutil v4 的 Cmdline() 直接返回整条命令字符串
		cmdStr, _ := p.Cmdline()
		if len(cmdStr) > 180 {
			cmdStr = cmdStr[:180]
		}
		statusList, _ := p.Status()
		status := ""
		if len(statusList) > 0 {
			status = statusList[0]
		}
		user, _ := p.Username()
		name, _ := p.Name()
		rows = append(rows, Proc{
			PID:     int(p.Pid),
			Name:    name,
			User:    user,
			Status:  status,
			CPU:     cpuPct,
			RSS:     rss,
			MemPct:  memPct,
			Threads: int(nthreads),
			Cmd:     cmdStr,
		})
	}
	c.prevProc = cur

	byCPU = append([]Proc{}, rows...)
	sort.SliceStable(byCPU, func(i, j int) bool { return byCPU[i].CPU > byCPU[j].CPU })
	byMem = append([]Proc{}, rows...)
	sort.SliceStable(byMem, func(i, j int) bool { return byMem[i].RSS > byMem[j].RSS })
	n := c.opts.TopProcesses
	if len(byCPU) > n {
		byCPU = byCPU[:n]
	}
	if len(byMem) > n {
		byMem = byMem[:n]
	}
	return byCPU, byMem, threads
}

func (c *otherCollector) collectSys(threads int) Sys {
	procs := 0
	if ps, err := process.Processes(); err == nil {
		procs = len(ps)
	}
	return Sys{
		Procs:   procs,
		Threads: threads,
	}
}

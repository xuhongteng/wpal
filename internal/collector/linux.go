//go:build linux

package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// clockTicks 是 /proc 中 CPU 时间片的频率（USER_HZ）。Linux 内核固定为 100。
const clockTicks = 100

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal float64
}

func (c cpuTimes) sub(o cpuTimes) cpuTimes {
	return cpuTimes{
		user: c.user - o.user, nice: c.nice - o.nice,
		system: c.system - o.system, idle: c.idle - o.idle,
		iowait: c.iowait - o.iowait, irq: c.irq - o.irq,
		softirq: c.softirq - o.softirq, steal: c.steal - o.steal,
	}
}

// pct 把时间片差值换算成百分比构成。
//
// 口径与 psutil 保持一致：total 不含 guest/guest_nice（内核已在 user/nice 中计过），
// busy = (total - idle) / total。
func (c cpuTimes) pct() Breakdown {
	total := c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
	if total <= 0 {
		return Breakdown{Busy: 0, Idle: 100}
	}
	return Breakdown{
		Busy:   round2(100 * (total - c.idle) / total),
		User:   round2(100 * (c.user + c.nice) / total),
		System: round2(100 * (c.system + c.irq + c.softirq) / total),
		Iowait: round2(100 * c.iowait / total),
		Steal:  round2(100 * c.steal / total),
		Idle:   round2(100 * c.idle / total),
	}
}

type linuxCollector struct {
	opts Options

	prevCPU  []cpuTimes
	prevDisk map[string]diskCounter
	prevNet  map[string]netCounter
	prevProc map[int]procTimes
	prevCtxt float64
	prevAt   time.Time

	pageSize  int64
	uidNames  map[int]string
	diskMajor map[string]bool // major:minor -> 整盘
}

func newCollector(opts Options) Collector {
	c := &linuxCollector{
		opts:      opts,
		prevDisk:  map[string]diskCounter{},
		prevNet:   map[string]netCounter{},
		prevProc:  map[int]procTimes{},
		uidNames:  map[int]string{},
		diskMajor: map[string]bool{},
		pageSize:  int64(os.Getpagesize()),
		prevAt:    time.Now(),
	}
	return c
}

func (c *linuxCollector) Close() error { return nil }

// procPath 返回 procfs 路径（容器内可通过 MON_PROC_ROOT 指向宿主 procfs）。
func (c *linuxCollector) procPath(elems ...string) string {
	root := os.Getenv("MON_PROC_ROOT")
	if root == "" {
		root = "/proc"
	}
	return filepath.Join(append([]string{root}, elems...)...)
}

func (c *linuxCollector) Collect() (*Snapshot, error) {
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
	snap.CPU = c.collectCPU(now, dt)
	snap.Mem = c.collectMem()
	snap.Disk = c.collectDisk(dt)
	snap.Net = c.collectNet(dt)
	byCPU, byMem, threads := c.collectProcs(dt, snap.Mem.Total)
	snap.Procs, snap.ProcsMem = byCPU, byMem
	snap.Sys = c.collectSys(threads, snap.CPU)

	c.prevAt = now
	return snap, nil
}

// ---------------------------------------------------------------- 主机信息

func (c *linuxCollector) collectHost(now time.Time) Host {
	hostname, _ := os.Hostname()
	kernel := "unknown"
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err == nil {
		kernel = charsToString(uts.Release[:])
	}
	uptime := 0.0
	if b, err := os.ReadFile(c.procPath("uptime")); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	return Host{
		Hostname: hostname,
		Kernel:   kernel,
		Label:    c.opts.Label,
		Uptime:   uptime,
		BootTime: float64(now.Unix()) - uptime,
	}
}

func charsToString(ch []int8) string {
	b := make([]byte, 0, len(ch))
	for _, v := range ch {
		if v == 0 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)
}

// ---------------------------------------------------------------- CPU

func (c *linuxCollector) collectCPU(now time.Time, dt float64) CPU {
	agg, cores, ctxt, _, _, _ := c.readProcStat()

	// 与上一次采样做差得到占比
	prev := c.prevCPU
	cur := append([]cpuTimes{agg}, cores...)
	c.prevCPU = cur

	total := Breakdown{}
	if len(prev) == len(cur) && len(cur) > 0 {
		total = cur[0].sub(prev[0]).pct()
	} else {
		total = Breakdown{Idle: 100}
	}

	percore := make([]Core, 0, len(cores))
	for i := range cores {
		var p Breakdown
		if i+1 < len(prev) {
			p = cur[i+1].sub(prev[i+1]).pct()
		} else {
			p = Breakdown{Idle: 100}
		}
		percore = append(percore, Core{
			Busy: p.Busy, User: p.User, System: p.System,
			Iowait: p.Iowait, Steal: p.Steal,
		})
	}

	ctxRate := 0.0
	if c.prevCtxt > 0 && ctxt > c.prevCtxt {
		ctxRate = round1((ctxt - c.prevCtxt) / dt)
	}
	c.prevCtxt = ctxt

	load1, load5, load15 := c.readLoadavg()

	return CPU{
		Total:         total.Busy,
		Breakdown:     total,
		PerCore:       percore,
		Cores:         len(cores),
		Physical:      physicalCores(),
		Load1:         load1,
		Load5:         load5,
		Load15:        load15,
		CtxSwitchRate: ctxRate,
		FreqMHz:       c.readCPUFreq(),
	}
}

// readProcStat 解析 /proc/stat：CPU 总量、每核、上下文切换、可运行/阻塞进程数。
func (c *linuxCollector) readProcStat() (agg cpuTimes, cores []cpuTimes, ctxt float64, running, blocked, started int) {
	f, err := os.Open(c.procPath("stat"))
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "cpu "):
			agg = parseCPUFields(strings.Fields(line[4:]))
		case strings.HasPrefix(line, "cpu"):
			rest := line[3:]
			sp := strings.IndexAny(rest, " \t")
			if sp < 0 {
				continue
			}
			// 只有 cpuN 才是单核
			if _, err := strconv.Atoi(rest[:sp]); err != nil {
				continue
			}
			cores = append(cores, parseCPUFields(strings.Fields(rest[sp:])))
		case strings.HasPrefix(line, "ctxt "):
			ctxt, _ = strconv.ParseFloat(strings.TrimSpace(line[5:]), 64)
		case strings.HasPrefix(line, "processes "):
			started, _ = strconv.Atoi(strings.TrimSpace(line[10:]))
		case strings.HasPrefix(line, "procs_running "):
			running, _ = strconv.Atoi(strings.TrimSpace(line[14:]))
		case strings.HasPrefix(line, "procs_blocked "):
			blocked, _ = strconv.Atoi(strings.TrimSpace(line[14:]))
		}
	}
	return
}

func parseCPUFields(f []string) cpuTimes {
	get := func(i int) float64 {
		if i < len(f) {
			v, _ := strconv.ParseFloat(f[i], 64)
			return v
		}
		return 0
	}
	return cpuTimes{
		user: get(0), nice: get(1), system: get(2), idle: get(3),
		iowait: get(4), irq: get(5), softirq: get(6), steal: get(7),
	}
}

func (c *linuxCollector) readLoadavg() (l1, l5, l15 float64) {
	b, err := os.ReadFile(c.procPath("loadavg"))
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) >= 3 {
		l1, _ = strconv.ParseFloat(f[0], 64)
		l5, _ = strconv.ParseFloat(f[1], 64)
		l15, _ = strconv.ParseFloat(f[2], 64)
	}
	return
}

// readCPUFreq 读取当前 CPU 频率（MHz）。虚拟机通常不提供，返回 0。
func (c *linuxCollector) readCPUFreq() float64 {
	f, err := os.Open(c.procPath("cpuinfo"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var sum float64
	var n int
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu MHz") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err == nil {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return round1(sum / float64(n))
}

// physicalCores 通过 cpuinfo 的 core id / physical id 组合估算物理核数。
func physicalCores() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	phys, core := "", ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "physical id") {
			phys = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
		if strings.HasPrefix(line, "core id") {
			core = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if phys != "" && core != "" {
				seen[phys+"/"+core] = true
			}
		}
	}
	if len(seen) == 0 {
		return 0
	}
	return len(seen)
}

// ---------------------------------------------------------------- 内存

// collectMem 解析 /proc/meminfo。
//
// 口径与 psutil 一致：used = total - available，percent = (total-avail)/total 取 1 位小数。
func (c *linuxCollector) collectMem() Mem {
	mems := map[string]uint64{}
	f, err := os.Open(c.procPath("meminfo"))
	if err != nil {
		return Mem{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		mems[fields[0]] = v * 1024
	}
	kb := func(k string) uint64 { return mems[k] }

	total := kb("MemTotal:")
	free := kb("MemFree:")
	buffers := kb("Buffers:")
	cached := kb("Cached:") + kb("SReclaimable:")
	shared := kb("Shmem:")

	avail, ok := mems["MemAvailable:"]
	if !ok {
		avail = free + buffers + cached
	}
	if avail > total {
		avail = free
	}
	used := total - avail
	percent := 0.0
	if total > 0 {
		percent = round1(float64(total-avail) / float64(total) * 100)
	}

	swapTotal := kb("SwapTotal:")
	swapFree := kb("SwapFree:")
	swapUsed := swapTotal - swapFree
	swapPct := 0.0
	if swapTotal > 0 {
		swapPct = round1(float64(swapUsed) / float64(swapTotal) * 100)
	}

	return Mem{
		Total:       float64(total),
		Used:        float64(used),
		Available:   float64(avail),
		Free:        float64(free),
		Percent:     percent,
		Cached:      float64(cached),
		Buffers:     float64(buffers),
		Shared:      float64(shared),
		SwapTotal:   float64(swapTotal),
		SwapUsed:    float64(swapUsed),
		SwapFree:    float64(swapFree),
		SwapPercent: swapPct,
	}
}

// ---------------------------------------------------------------- 系统

func (c *linuxCollector) collectSys(threads int, cpu CPU) Sys {
	_, _, _, running, blocked, started := c.readProcStat()
	_ = cpu

	fdUsed, fdMax := 0, 0
	if b, err := os.ReadFile(c.procPath("sys/fs/file-nr")); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			alloc, _ := strconv.Atoi(f[0])
			unused, _ := strconv.Atoi(f[1])
			fdUsed = alloc - unused
			fdMax, _ = strconv.Atoi(f[2])
		}
	}
	entropy := 0.0
	if b, err := os.ReadFile(c.procPath("sys/kernel/random/entropy_avail")); err == nil {
		entropy, _ = strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	}

	return Sys{
		Procs:            len(c.prevProc),
		Threads:          threads,
		ProcsRunning:     running,
		ProcsBlocked:     blocked,
		FDUsed:           fdUsed,
		FDMax:            fdMax,
		Entropy:          entropy,
		ProcessesStarted: started,
	}
}

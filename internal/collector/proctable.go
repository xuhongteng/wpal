//go:build !linux

package collector

import "sort"

// procSample 是进程表中的一个样本，承载排名与展示所需的全部字段。
//
// 各平台通过 readProcTable 一次性批量产出全部进程样本，
// 避免「每个进程都发起一次系统调用」这种在 macOS 上会放大到数秒的实现方式。
type procSample struct {
	PID     int32
	Name    string
	User    string
	Status  string
	Cmd     string
	CPUTime float64 // 累计 CPU 秒数（user+system），用于做差分算占比
	RSS     uint64  // 常驻内存字节数
	Threads int
}

// procStats 汇总一次进程采集的聚合结果。
type procStats struct {
	Threads int
	Procs   int
	Running int
	Blocked int
}

// collectProcs 采集进程榜，并顺带返回进程计数类聚合值。
//
// 三级流水线：
//  1. readProcTable 批量取全表（每平台一次或极少数几次系统调用）；
//  2. 与上一周期做 CPU 时间差分得到占比，并按 CPU / RSS 各取前 N；
//  3. fillCommands 只为最终上榜的进程补全命令行，避免全表取命令行。
func (c *otherCollector) collectProcs(dt float64, totalMem float64) (byCPU, byMem []Proc, st procStats) {
	// Headless 探针模式只需要进程数与线程数这类聚合值，
	// Top-N 明细既不上报给 Prometheus，也没有 UI 会展示，直接跳过。
	if c.opts.SkipProcessDetails {
		n, t := countProcsAndThreads()
		return []Proc{}, []Proc{}, procStats{Threads: t, Procs: n}
	}

	samples, threads, err := readProcTable()
	if err != nil {
		return []Proc{}, []Proc{}, procStats{}
	}
	st = procStats{Threads: threads, Procs: len(samples)}
	for _, sp := range samples {
		switch sp.Status {
		case "running":
			st.Running++
		case "waiting", "stopped":
			// 近似映射：macOS 的 waiting/stopped 与 Linux 的 D/T 态语义接近，
			// 都表示进程当前无法进入运行队列。
			st.Blocked++
		}
	}

	// 必须在覆盖 prevProc 之前完成差值计算，否则 CPU 占比恒为 0。
	prevProc := c.prevProc
	cur := make(map[int32]float64, len(samples))

	// ranked 保留累计 CPU 时间，用于排名。
	// 排名必须按累计时间而非占比：首帧没有上一周期数据，占比全为 0，
	// 若按占比排序会退化成按 PID 顺序，榜单毫无意义。
	type ranked struct {
		Proc
		cpuTime float64
	}
	rows := make([]ranked, 0, len(samples))

	for _, s := range samples {
		cur[s.PID] = s.CPUTime

		cpuPct := 0.0
		if prevTicks, ok := prevProc[s.PID]; ok && dt > 0 {
			if delta := s.CPUTime - prevTicks; delta > 0 {
				cpuPct = round1(delta / dt * 100)
			}
		}
		memPct := 0.0
		if totalMem > 0 && s.RSS > 0 {
			memPct = round3(float64(s.RSS) / totalMem * 100)
		}

		rows = append(rows, ranked{
			cpuTime: s.CPUTime,
			Proc: Proc{
				PID:     int(s.PID),
				Name:    s.Name,
				User:    s.User,
				Status:  s.Status,
				CPU:     cpuPct,
				RSS:     s.RSS,
				MemPct:  memPct,
				Threads: s.Threads,
				Cmd:     s.Cmd,
			},
		})
	}
	c.prevProc = cur

	// 按累计 CPU 时间与 RSS 各自排名，各取前 N。
	cpuRank := append([]ranked{}, rows...)
	sort.SliceStable(cpuRank, func(i, j int) bool { return cpuRank[i].cpuTime > cpuRank[j].cpuTime })
	memRank := append([]ranked{}, rows...)
	sort.SliceStable(memRank, func(i, j int) bool { return memRank[i].RSS > memRank[j].RSS })

	n := c.opts.TopProcesses
	if len(cpuRank) > n {
		cpuRank = cpuRank[:n]
	}
	if len(memRank) > n {
		memRank = memRank[:n]
	}

	byCPU = make([]Proc, 0, len(cpuRank))
	for _, r := range cpuRank {
		byCPU = append(byCPU, r.Proc)
	}
	byMem = make([]Proc, 0, len(memRank))
	for _, r := range memRank {
		byMem = append(byMem, r.Proc)
	}

	// 只对最终上榜的进程补全命令行，取两个榜单的并集，同一进程只查一次。
	union := make([]Proc, 0, len(byCPU)+len(byMem))
	seen := make(map[int]struct{}, len(byCPU)+len(byMem))
	for _, list := range [][]Proc{byCPU, byMem} {
		for _, p := range list {
			if _, ok := seen[p.PID]; ok {
				continue
			}
			seen[p.PID] = struct{}{}
			union = append(union, p)
		}
	}
	if cmds := fillCommands(union); len(cmds) > 0 {
		for i := range byCPU {
			if cmd, ok := cmds[byCPU[i].PID]; ok {
				byCPU[i].Cmd = cmd
			}
		}
		for i := range byMem {
			if cmd, ok := cmds[byMem[i].PID]; ok {
				byMem[i].Cmd = cmd
			}
		}
	}

	return byCPU, byMem, st
}

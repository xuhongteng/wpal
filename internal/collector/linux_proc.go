//go:build linux

package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type procTimes struct {
	ticks float64 // utime + stime，单位 clock tick
}

// collectProcs 遍历 /proc 采集进程榜。
//
// CPU% 的口径与 psutil 一致：(本次 utime+stime 增量 / clockTicks) / 墙钟增量 * 100，
// 首次采样因为没有上一次值而恒为 0。
func (c *linuxCollector) collectProcs(dt float64, totalMem float64) (byCPU, byMem []Proc, threads int) {
	dir := c.procPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Proc{}, []Proc{}, 0
	}

	rows := []Proc{}
	cur := map[int]procTimes{}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name(), "stat"))
		if err != nil {
			continue
		}
		st, ok := parseProcStat(string(raw))
		if !ok {
			continue
		}
		cur[pid] = procTimes{ticks: st.utime + st.stime}
		threads += st.threads

		cpuPct := 0.0
		if prev, ok := c.prevProc[pid]; ok {
			delta := (st.utime + st.stime) - prev.ticks
			if delta > 0 && dt > 0 {
				cpuPct = round1(delta / clockTicks / dt * 100)
			}
		}
		rss := uint64(st.rss) * uint64(c.pageSize)
		memPct := 0.0
		if totalMem > 0 {
			memPct = round3(float64(rss) / totalMem * 100)
		}
		rows = append(rows, Proc{
			PID:     pid,
			Name:    st.name,
			User:    c.username(pid),
			Status:  st.state,
			CPU:     cpuPct,
			RSS:     rss,
			MemPct:  memPct,
			Threads: st.threads,
			Cmd:     c.readCmdline(pid),
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

type procStat struct {
	name    string
	state   string
	utime   float64
	stime   float64
	threads int
	rss     int64
}

// parseProcStat 解析 /proc/<pid>/stat。
//
// comm 字段可能包含空格与括号，因此从最后一个 ')' 之后再切分。
func parseProcStat(s string) (procStat, bool) {
	var st procStat
	lp := strings.Index(s, "(")
	rp := strings.LastIndex(s, ")")
	if lp < 0 || rp < lp {
		return st, false
	}
	st.name = s[lp+1 : rp]
	rest := strings.Fields(s[rp+1:])
	// /proc/<pid>/stat 的字段下标：
	// rest[0]=state rest[11]=utime rest[12]=stime rest[17]=num_threads rest[21]=rss
	if len(rest) < 22 {
		return st, false
	}
	st.state = rest[0]
	st.utime, _ = strconv.ParseFloat(rest[11], 64)
	st.stime, _ = strconv.ParseFloat(rest[12], 64)
	st.threads, _ = strconv.Atoi(rest[17])
	st.rss, _ = strconv.ParseInt(rest[21], 10, 64)
	return st, true
}

// readCmdline 读取进程的完整命令行（以空格连接，最长 180 字符）。
func (c *linuxCollector) readCmdline(pid int) string {
	b, err := os.ReadFile(c.procPath(strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	cmd := strings.Join(parts, " ")
	if len(cmd) > 180 {
		cmd = cmd[:180]
	}
	return cmd
}

// username 通过 /proc/<pid> 属主 uid 解析用户名（结果缓存）。
func (c *linuxCollector) username(pid int) string {
	fi, err := os.Stat(c.procPath(strconv.Itoa(pid)))
	if err != nil {
		return ""
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid := int(sys.Uid)
	if name, ok := c.uidNames[uid]; ok {
		return name
	}
	name := lookupUser(uid)
	c.uidNames[uid] = name
	return name
}

// lookupUser 从 /etc/passwd 查用户名。
func lookupUser(uid int) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 3 {
			continue
		}
		u, err := strconv.Atoi(fields[2])
		if err == nil && u == uid {
			return fields[0]
		}
	}
	return ""
}

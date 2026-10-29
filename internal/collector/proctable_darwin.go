//go:build darwin

package collector

import (
	"os/exec"
	"strconv"
	"strings"
)

// macBulkProcArgs 是一次性拉取整张进程表的字段清单。
//
// 关键点：comm 必须放在最后。前面 7 个字段（pid/ppid/uid/user/rss/time/stat）
// 都不会包含空格，因此可以把行尾剩余部分整体当作进程名，
// 从而正确处理形如 "Google Chrome Helper" 这种带空格的进程名。
var macBulkProcArgs = []string{"-axo", "pid=,ppid=,uid=,user=,rss=,time=,stat=,comm="}

// readProcTable 用一次 ps 调用批量取回全部进程，再用一次 ps -axM 统计线程数。
//
// 背景：gopsutil 在 CGO_ENABLED=0 的 darwin 实现里，Process.Times /
// MemoryInfo / NumThreads 每次都会 fork 一个 ps 子进程。近千个进程乘以数次调用
// 约等于 4700 次进程创建，实测单次采集被拖到 10 秒以上。
// 这里改为「整表两次调用」，把子进程数量从数千降到 2。
func readProcTable() ([]procSample, int, error) {
	cmd := exec.Command("ps", macBulkProcArgs...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, 0, err
	}

	lines := strings.Split(string(out), "\n")
	samples := make([]procSample, 0, len(lines))

	for _, line := range lines {
		fields, comm := splitBulkProcLine(line)
		if fields == nil {
			continue
		}
		pid, err1 := strconv.ParseInt(fields[0], 10, 32)
		rssKB, err2 := strconv.ParseFloat(fields[4], 64)
		if err1 != nil {
			continue
		}
		if err2 != nil {
			rssKB = 0
		}

		name := strings.TrimSpace(comm)
		if name == "" {
			name = fields[6]
		}

		samples = append(samples, procSample{
			PID:     int32(pid),
			Name:    name,
			User:    fields[3],
			Status:  mapPsStat(fields[6]),
			CPUTime: parsePsTime(fields[5]),
			RSS:     uint64(rssKB * 1024),
		})
	}

	threads := countThreads(samples)
	return samples, threads, nil
}

// splitBulkProcLine 把一行 ps 输出切成前 7 个定长字段与行尾的进程名。
// 字段不足时返回 nil，由调用方跳过。
func splitBulkProcLine(line string) ([]string, string) {
	const want = 7
	fields := make([]string, 0, want)

	i := 0
	n := len(line)
	for len(fields) < want {
		for i < n && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= n {
			return nil, ""
		}
		start := i
		for i < n && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		fields = append(fields, line[start:i])
	}
	return fields, strings.TrimSpace(line[i:])
}

// parsePsTime 把 ps 的累计 CPU 时间解析成秒。
// 支持 "MM:SS.ss"、"HH:MM:SS.ss" 以及跨天的 "D-HH:MM:SS"。
func parsePsTime(s string) float64 {
	if s == "" {
		return 0
	}
	var days float64
	if idx := strings.IndexByte(s, '-'); idx > 0 {
		if d, err := strconv.ParseFloat(s[:idx], 64); err == nil {
			days = d
		}
		s = s[idx+1:]
	}

	parts := strings.Split(s, ":")
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return days * 86400
		}
		total = total*60 + v
	}
	return days*86400 + total
}

// mapPsStat 把 ps 的状态码（可能带修饰符，如 Ss、R+）映射成可读状态。
func mapPsStat(stat string) string {
	if stat == "" {
		return ""
	}
	switch stat[0] {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'T':
		return "stopped"
	case 'Z':
		return "zombie"
	case 'I':
		return "idle"
	case 'D', 'U':
		return "waiting"
	default:
		return "unknown"
	}
}

// countThreads 通过 ps -axM 让每个线程占一行，统计线程数。
//
// ps -M 的输出有两个坑：
//  1. 它会忽略 -o 自定义格式，固定打印默认列；
//  2. 同一进程的第 2 个及之后的线程行，USER 与 PID 两列是空的。
//
// 但 ps -M 会把同一进程的所有线程连续输出，因此可以「沿用上一个有效 PID」
// 来归属续行。为避免把 %CPU 列的整数误判成 PID，
// 只有当解析出的数字确实存在于本次进程表中时才认定为新 PID。
func countThreads(samples []procSample) int {
	cmd := exec.Command("ps", "-axM")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	known := make(map[int32]struct{}, len(samples))
	for _, s := range samples {
		known[s.PID] = struct{}{}
	}

	perPID := make(map[int32]int, len(samples))
	total := 0
	last := int32(-1)

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid := last
		if v, err := strconv.ParseInt(fields[1], 10, 32); err == nil {
			if _, ok := known[int32(v)]; ok {
				pid = int32(v)
				last = pid
			}
		}
		if pid < 0 {
			continue
		}
		perPID[pid]++
		total++
	}

	for i := range samples {
		if n, ok := perPID[samples[i].PID]; ok {
			samples[i].Threads = n
		}
	}
	return total
}

// fillCommands 用一次带 PID 列表的 ps 调用补齐上榜进程的完整命令行。
// 只处理传入的少量进程（榜单长度级别），因此开销可忽略。
func fillCommands(rows []Proc) map[int]string {
	if len(rows) == 0 {
		return nil
	}
	pids := make([]string, 0, len(rows))
	for _, p := range rows {
		pids = append(pids, strconv.Itoa(p.PID))
	}

	cmd := exec.Command("ps", "-p", strings.Join(pids, ","), "-o", "pid=,command=")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	res := make(map[int]string, len(rows))
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		idx := strings.IndexAny(trimmed, " \t")
		if idx <= 0 {
			continue
		}
		pid, err := strconv.Atoi(trimmed[:idx])
		if err != nil {
			continue
		}
		c := strings.TrimSpace(trimmed[idx:])
		// 与既有实现保持一致：命令行超长时截断，避免前端展示溢出。
		if len(c) > 180 {
			c = c[:180]
		}
		res[pid] = c
	}
	return res
}

// countProcsAndThreads 是为 Headless 探针模式准备的轻量计数：
// 只取进程数与线程数，不构建进程明细，也不补全命令行。
func countProcsAndThreads() (procs, threads int) {
	if out, err := exec.Command("ps", "-axo", "pid=").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				procs++
			}
		}
	}
	// ps -axM 会打印表头，这里只统计能解析出 PID 的行（含沿用 PID 的续行）。
	if out, err := exec.Command("ps", "-axM").Output(); err == nil {
		for i, line := range strings.Split(string(out), "\n") {
			if i == 0 {
				continue // 表头
			}
			if len(strings.Fields(line)) >= 2 {
				threads++
			}
		}
	}
	return procs, threads
}

//go:build !linux && !darwin

package collector

import "github.com/shirou/gopsutil/v4/process"

// readProcTable 是 Windows 等非 Linux、非 macOS 平台的实现，走 gopsutil。
//
// 这些平台的 gopsutil 后端使用原生系统接口（Windows 走 NtQuerySystemInformation /
// PDH），不像 macOS 的纯 Go 后端那样需要为每个进程 fork 一个 ps 子进程，
// 因此可以一次性全量读取，无需额外的批量优化。
func readProcTable() ([]procSample, int, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, 0, err
	}

	samples := make([]procSample, 0, len(procs))
	threads := 0

	for _, p := range procs {
		times, err := p.Times()
		if err != nil {
			continue
		}
		nthreads, _ := p.NumThreads()
		threads += int(nthreads)

		var rss uint64
		if memInfo, err := p.MemoryInfo(); err == nil && memInfo != nil {
			rss = memInfo.RSS
		}

		name, _ := p.Name()
		user, _ := p.Username()
		cmdStr, _ := p.Cmdline()
		if len(cmdStr) > 180 {
			cmdStr = cmdStr[:180]
		}
		status := ""
		if list, err := p.Status(); err == nil && len(list) > 0 {
			status = list[0]
		}

		samples = append(samples, procSample{
			PID:     p.Pid,
			Name:    name,
			User:    user,
			Status:  status,
			Cmd:     cmdStr,
			CPUTime: times.User + times.System,
			RSS:     rss,
			Threads: int(nthreads),
		})
	}

	return samples, threads, nil
}

// fillCommands 在本平台无需额外补齐：命令行已在 readProcTable 中一并取回。
func fillCommands(rows []Proc) map[int]string { return nil }

// countProcsAndThreads 在非 macOS 平台上复用一次完整读取来得到计数。
func countProcsAndThreads() (int, int) {
	samples, threads, err := readProcTable()
	if err != nil {
		return 0, 0
	}
	return len(samples), threads
}

// platformFileDescriptors 在非 macOS 平台上暂不提供句柄计数，返回 0。
func platformFileDescriptors() (used, max int) { return 0, 0 }

// getPlatformMemCache 在非 macOS 平台上返回 0（由 gopsutil 的 Cached 字段提供）。
func getPlatformMemCache() (cached, shared uint64) { return 0, 0 }

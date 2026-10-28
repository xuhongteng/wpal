//go:build !linux && !darwin

package collector

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
)

// getPlatformCPUTimes 针对其他平台（如 Windows）通过 gopsutil 采集。
func getPlatformCPUTimes() (cpu.TimesStat, []cpu.TimesStat, error) {
	percore, err := cpu.Times(true)
	if err != nil {
		return cpu.TimesStat{}, nil, err
	}
	all, err := cpu.Times(false)
	var total cpu.TimesStat
	if err == nil && len(all) > 0 {
		total = all[0]
	} else if len(percore) > 0 {
		// 若底层未提供全局聚合，则汇总所有核心 ticks
		for _, c := range percore {
			total.User += c.User
			total.System += c.System
			total.Idle += c.Idle
			total.Nice += c.Nice
			total.Iowait += c.Iowait
			total.Irq += c.Irq
			total.Softirq += c.Softirq
			total.Steal += c.Steal
		}
	}
	return total, percore, nil
}

// getPlatformUptime 为其他平台提供 Uptime 采集。
func getPlatformUptime(now time.Time) float64 {
	if up, err := host.Uptime(); err == nil && up > 0 {
		return float64(up)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				return v
			}
		}
	}
	return 0.0
}

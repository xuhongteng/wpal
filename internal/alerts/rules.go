package alerts

import (
	"math"
	"time"

	"mon/internal/collector"
	"mon/internal/i18n"
)

// round1 保留 1 位小数，避免详情文案里出现一长串浮点尾数。
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// DefaultRules 返回内置的默认阈值规则。
//
// 阈值与既有 Python 实现保持一致，保证迁移前后行为不变。
func DefaultRules() []Rule {
	return []Rule{
		{
			Key: "cpu", Source: "cpu",
			Label: "CPU 使用率", LabelKey: "alert.cpu",
			Warn: 80, Crit: 93, Unit: "%",
			Hold: 30 * time.Second, Recover: 20 * time.Second,
			Extract: func(s *collector.Snapshot) float64 { return s.CPU.Total },
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.cpu.detail", map[string]any{
					"load1": s.CPU.Load1, "cores": s.CPU.Cores,
				})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"load1": round1(s.CPU.Load1), "cores": s.CPU.Cores}
			},
		},
		{
			Key: "mem", Source: "mem",
			Label: "内存使用率", LabelKey: "alert.memory",
			Warn: 85, Crit: 94, Unit: "%",
			Hold: 20 * time.Second, Recover: 20 * time.Second,
			Extract: func(s *collector.Snapshot) float64 { return s.Mem.Percent },
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.memory.detail", map[string]any{
					"available_gib": round1(s.Mem.Available / (1 << 30)),
				})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"available_gib": round1(s.Mem.Available / (1 << 30))}
			},
		},
		{
			Key: "swap", Source: "mem",
			Label: "Swap 使用率", LabelKey: "alert.swap",
			Warn: 50, Crit: 85, Unit: "%",
			Hold: 60 * time.Second, Recover: 30 * time.Second,
			Extract: func(s *collector.Snapshot) float64 { return s.Mem.SwapPercent },
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.swap.detail", map[string]any{
					"used_gib": round1(s.Mem.SwapUsed / (1 << 30)),
				})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"used_gib": round1(s.Mem.SwapUsed / (1 << 30))}
			},
		},
		{
			Key: "disk", Source: "disk",
			Label: "磁盘使用率", LabelKey: "alert.disk",
			Warn: 80, Crit: 90, Unit: "%",
			Hold: 60 * time.Second, Recover: 60 * time.Second,
			Extract: maxPartitionPercent,
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.disk.detail", map[string]any{"mount": maxPartitionMount(s)})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"mount": maxPartitionMount(s)}
			},
		},
		{
			Key: "inode", Source: "disk",
			Label: "inode 使用率", LabelKey: "alert.inode",
			Warn: 85, Crit: 95, Unit: "%",
			Hold: 60 * time.Second, Recover: 60 * time.Second,
			Extract: maxInodePercent,
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.inode.detail", map[string]any{"mount": maxInodeMount(s)})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"mount": maxInodeMount(s)}
			},
		},
		{
			Key: "load", Source: "cpu",
			Label: "负载/核数", LabelKey: "alert.load",
			Warn: 1.5, Crit: 3.0, Unit: "×",
			Hold: 45 * time.Second, Recover: 30 * time.Second,
			Extract: func(s *collector.Snapshot) float64 {
				cores := s.CPU.Cores
				if cores < 1 {
					cores = 1
				}
				return s.CPU.Load1 / float64(cores)
			},
			Detail: func(s *collector.Snapshot) string {
				return i18n.Detail("alert.load.detail", map[string]any{"load1": s.CPU.Load1})
			},
			Params: func(s *collector.Snapshot) map[string]any {
				return map[string]any{"load1": round1(s.CPU.Load1)}
			},
		},
	}
}

func maxPartitionPercent(s *collector.Snapshot) float64 {
	peak := 0.0
	for _, p := range s.Disk.Partitions {
		if p.Percent > peak {
			peak = p.Percent
		}
	}
	return peak
}

func maxInodePercent(s *collector.Snapshot) float64 {
	peak := 0.0
	for _, p := range s.Disk.Partitions {
		if p.InodePercent > peak {
			peak = p.InodePercent
		}
	}
	return peak
}

func maxPartitionMount(s *collector.Snapshot) string {
	mount := ""
	peak := -1.0
	for _, p := range s.Disk.Partitions {
		if p.Percent > peak {
			peak = p.Percent
			mount = p.Mount
		}
	}
	return mount
}

func maxInodeMount(s *collector.Snapshot) string {
	mount := ""
	peak := -1.0
	for _, p := range s.Disk.Partitions {
		if p.InodePercent > peak {
			peak = p.InodePercent
			mount = p.Mount
		}
	}
	return mount
}

package server

import (
	"strings"
	"testing"
	"time"

	"mon/internal/collector"
	"mon/internal/config"
)

func TestFormatPrometheusMetrics(t *testing.T) {
	snap := &collector.Snapshot{
		TS: float64(time.Now().Unix()),
		Host: collector.Host{
			Hostname: "node-c2-01",
			Uptime:   3600,
			BootTime: 1700000000,
		},
		CPU: collector.CPU{
			Total: 35.5,
			Load1: 1.25,
			PerCore: []collector.Core{
				{Busy: 40.0},
				{Busy: 31.0},
			},
		},
		Mem: collector.Mem{
			Total:     16 * 1024 * 1024 * 1024,
			Used:      8 * 1024 * 1024 * 1024,
			Available: 8 * 1024 * 1024 * 1024,
			Percent:   50.0,
		},
		Disk: collector.Disk{
			Partitions: []collector.Partition{
				{Device: "/dev/sda1", Mount: "/", Fstype: "ext4", Total: 1000, Free: 500, Percent: 50.0},
			},
			IO: []collector.DiskIO{
				{Name: "sda", ReadBPS: 1024, WriteBPS: 2048},
			},
		},
		Net: collector.Net{
			Interfaces: []collector.Iface{
				{Name: "eth0", RxTotal: 10000, TxTotal: 20000, RxBPS: 500, TxBPS: 800, Virtual: false},
			},
		},
		Sys: collector.Sys{
			Procs:   200,
			Threads: 800,
		},
	}

	cfg := config.AgentConfig{
		Cluster: "k8s-prod-bj",
		NodeID:  "node-c2-01",
		Labels: map[string]string{
			"env":  "production",
			"tier": "backend",
		},
	}

	payload := formatPrometheusMetrics(snap, cfg)
	text := string(payload)

	// 1. 验证必须包含 HELP 与 TYPE 声明
	for _, expectedHeader := range []string{
		"# HELP wpal_cpu_usage_ratio",
		"# TYPE wpal_cpu_usage_ratio gauge",
		"# HELP node_memory_MemTotal_bytes",
		"# TYPE node_memory_MemTotal_bytes gauge",
		"# HELP node_network_receive_bytes_total",
		"# TYPE node_network_receive_bytes_total counter",
	} {
		if !strings.Contains(text, expectedHeader) {
			t.Errorf("缺少 Prometheus 头声明: %q", expectedHeader)
		}
	}

	// 2. 验证多维标签注入与值准确性
	expectedSnippet := `wpal_cpu_usage_ratio{cluster="k8s-prod-bj",env="production",instance="node-c2-01",tier="backend"} 0.355`
	if !strings.Contains(text, expectedSnippet) {
		t.Errorf("未找到格式化后的指标行: %q\n输出全貌:\n%s", expectedSnippet, text)
	}

	// 3. 验证多核核心标签
	if !strings.Contains(text, `wpal_cpu_core_usage_ratio{cluster="k8s-prod-bj",cpu="0",env="production",instance="node-c2-01",tier="backend"} 0.4000`) {
		t.Errorf("多核 cpu 0 标签缺失或格式不正确")
	}

	// 4. 验证磁盘挂载标签
	if !strings.Contains(text, `node_filesystem_size_bytes{cluster="k8s-prod-bj",device="/dev/sda1",env="production",fstype="ext4",instance="node-c2-01",mount="/",tier="backend"} 1000`) {
		t.Errorf("磁盘挂载分区标签缺失或格式不正确")
	}
}

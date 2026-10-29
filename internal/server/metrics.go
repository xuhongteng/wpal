package server

import (
	"bytes"
	"net/http"
	"sort"
	"strconv"

	"mon/internal/collector"
	"mon/internal/config"
	"mon/internal/version"
)

// formatPrometheusMetrics 将当前采样快照格式化为标准 Prometheus 文本行协议 (version 0.0.4)。
//
// 设计原则：
//  1. 零第三方依赖：不引入重量级 Prometheus SDK，纯依靠 bytes.Buffer 与 strconv 实现；
//  2. 极低内存开销：单次全量指标导出在微秒级完成，内存分配仅数十 KB；
//  3. 命名契约严格对齐官方 Prometheus node_exporter，便于直接复用社区已有 Grafana 看板。
func formatPrometheusMetrics(snap *collector.Snapshot, cfg config.AgentConfig) []byte {
	if snap == nil {
		return []byte("# wpal: no snapshot available yet\n")
	}

	var buf bytes.Buffer
	buf.Grow(4096)

	// 构建全局固定标签：node_id、cluster 以及用户配置的 labels
	globalLabels := make(map[string]string)
	for k, v := range cfg.Labels {
		globalLabels[k] = v
	}
	if cfg.Cluster != "" {
		globalLabels["cluster"] = cfg.Cluster
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = snap.Host.Hostname
	}
	if nodeID != "" {
		globalLabels["instance"] = nodeID
	}

	// 辅助格式化函数
	writeMetric := func(name, help, mtype string, val float64, extraLabels map[string]string) {
		buf.WriteString("# HELP ")
		buf.WriteString(name)
		buf.WriteByte(' ')
		buf.WriteString(help)
		buf.WriteByte('\n')

		buf.WriteString("# TYPE ")
		buf.WriteString(name)
		buf.WriteByte(' ')
		buf.WriteString(mtype)
		buf.WriteByte('\n')

		buf.WriteString(name)
		writeLabels(&buf, globalLabels, extraLabels)
		buf.WriteByte(' ')
		buf.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
		buf.WriteByte('\n')
	}

	// 1. 节点与版本元信息
	ver := version.Get()
	writeMetric("wpal_build_info", "Build and version information for Wpal agent", "gauge", 1, map[string]string{
		"version": ver.Version,
		"commit":  ver.Commit,
		"go":      ver.Go,
		"os":      ver.OS,
		"arch":    ver.Arch,
	})

	if snap.Host.Uptime > 0 {
		writeMetric("node_boot_time_seconds", "Node boot time in seconds since epoch", "gauge", snap.Host.BootTime, nil)
		writeMetric("wpal_uptime_seconds", "Number of seconds since host booted", "counter", snap.Host.Uptime, nil)
	}

	// 2. CPU 负载与核心利用率
	writeMetric("wpal_cpu_usage_ratio", "Total CPU usage ratio (0.0 - 1.0)", "gauge", snap.CPU.Total/100.0, nil)
	writeMetric("node_load1", "1 minute load average", "gauge", snap.CPU.Load1, nil)
	writeMetric("node_load5", "5 minute load average", "gauge", snap.CPU.Load5, nil)
	writeMetric("node_load15", "15 minute load average", "gauge", snap.CPU.Load15, nil)

	if len(snap.CPU.PerCore) > 0 {
		buf.WriteString("# HELP wpal_cpu_core_usage_ratio Per-core CPU usage ratio (0.0 - 1.0)\n")
		buf.WriteString("# TYPE wpal_cpu_core_usage_ratio gauge\n")
		for i, c := range snap.CPU.PerCore {
			buf.WriteString("wpal_cpu_core_usage_ratio")
			writeLabels(&buf, globalLabels, map[string]string{"cpu": strconv.Itoa(i)})
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(c.Busy/100.0, 'f', 4, 64))
			buf.WriteByte('\n')
		}
	}

	// 3. 内存与交换区指标（字节）
	writeMetric("node_memory_MemTotal_bytes", "Total memory in bytes", "gauge", snap.Mem.Total, nil)
	writeMetric("node_memory_MemUsed_bytes", "Used memory in bytes", "gauge", snap.Mem.Used, nil)
	writeMetric("node_memory_MemAvailable_bytes", "Available memory in bytes", "gauge", snap.Mem.Available, nil)
	writeMetric("node_memory_MemFree_bytes", "Free memory in bytes", "gauge", snap.Mem.Free, nil)
	writeMetric("wpal_memory_usage_ratio", "Memory usage ratio (0.0 - 1.0)", "gauge", snap.Mem.Percent/100.0, nil)

	if snap.Mem.SwapTotal > 0 {
		writeMetric("node_memory_SwapTotal_bytes", "Total swap in bytes", "gauge", snap.Mem.SwapTotal, nil)
		writeMetric("node_memory_SwapUsed_bytes", "Used swap in bytes", "gauge", snap.Mem.SwapUsed, nil)
		writeMetric("node_memory_SwapFree_bytes", "Free swap in bytes", "gauge", snap.Mem.SwapFree, nil)
	}

	// 4. 磁盘挂载点容量与 Inode
	if len(snap.Disk.Partitions) > 0 {
		buf.WriteString("# HELP node_filesystem_size_bytes Filesystem size in bytes\n")
		buf.WriteString("# TYPE node_filesystem_size_bytes gauge\n")
		buf.WriteString("# HELP node_filesystem_free_bytes Filesystem free space in bytes\n")
		buf.WriteString("# TYPE node_filesystem_free_bytes gauge\n")
		buf.WriteString("# HELP node_filesystem_usage_ratio Filesystem usage ratio (0.0 - 1.0)\n")
		buf.WriteString("# TYPE node_filesystem_usage_ratio gauge\n")

		for _, p := range snap.Disk.Partitions {
			labels := map[string]string{
				"device": p.Device,
				"mount":  p.Mount,
				"fstype": p.Fstype,
			}
			buf.WriteString("node_filesystem_size_bytes")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatUint(p.Total, 10))
			buf.WriteByte('\n')

			buf.WriteString("node_filesystem_free_bytes")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatUint(p.Free, 10))
			buf.WriteByte('\n')

			buf.WriteString("node_filesystem_usage_ratio")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(p.Percent/100.0, 'f', 4, 64))
			buf.WriteByte('\n')
		}
	}

	// 5. 磁盘设备 I/O 速率
	if len(snap.Disk.IO) > 0 {
		buf.WriteString("# HELP node_disk_read_bytes_per_second Disk read throughput in bytes/s\n")
		buf.WriteString("# TYPE node_disk_read_bytes_per_second gauge\n")
		buf.WriteString("# HELP node_disk_written_bytes_per_second Disk write throughput in bytes/s\n")
		buf.WriteString("# TYPE node_disk_written_bytes_per_second gauge\n")

		for _, d := range snap.Disk.IO {
			labels := map[string]string{"device": d.Name}
			buf.WriteString("node_disk_read_bytes_per_second")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(d.ReadBPS, 'f', 2, 64))
			buf.WriteByte('\n')

			buf.WriteString("node_disk_written_bytes_per_second")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(d.WriteBPS, 'f', 2, 64))
			buf.WriteByte('\n')
		}
	}

	// 6. 网络接口累计与瞬时吞吐
	if len(snap.Net.Interfaces) > 0 {
		buf.WriteString("# HELP node_network_receive_bytes_total Total received network traffic in bytes\n")
		buf.WriteString("# TYPE node_network_receive_bytes_total counter\n")
		buf.WriteString("# HELP node_network_transmit_bytes_total Total transmitted network traffic in bytes\n")
		buf.WriteString("# TYPE node_network_transmit_bytes_total counter\n")
		buf.WriteString("# HELP wpal_network_receive_bytes_per_second Network receive speed in bytes/s\n")
		buf.WriteString("# TYPE wpal_network_receive_bytes_per_second gauge\n")
		buf.WriteString("# HELP wpal_network_transmit_bytes_per_second Network transmit speed in bytes/s\n")
		buf.WriteString("# TYPE wpal_network_transmit_bytes_per_second gauge\n")

		for _, iface := range snap.Net.Interfaces {
			labels := map[string]string{
				"device":  iface.Name,
				"virtual": strconv.FormatBool(iface.Virtual),
			}
			buf.WriteString("node_network_receive_bytes_total")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatUint(iface.RxTotal, 10))
			buf.WriteByte('\n')

			buf.WriteString("node_network_transmit_bytes_total")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatUint(iface.TxTotal, 10))
			buf.WriteByte('\n')

			buf.WriteString("wpal_network_receive_bytes_per_second")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(iface.RxBPS, 'f', 2, 64))
			buf.WriteByte('\n')

			buf.WriteString("wpal_network_transmit_bytes_per_second")
			writeLabels(&buf, globalLabels, labels)
			buf.WriteByte(' ')
			buf.WriteString(strconv.FormatFloat(iface.TxBPS, 'f', 2, 64))
			buf.WriteByte('\n')
		}
	}

	// 7. 系统级进程与线程数
	writeMetric("wpal_system_processes", "Total number of processes", "gauge", float64(snap.Sys.Procs), nil)
	writeMetric("wpal_system_threads", "Total number of threads", "gauge", float64(snap.Sys.Threads), nil)

	return buf.Bytes()
}

// writeLabels 将多个 map 的标签合并并按键名升序格式化输出为 {k1="v1",k2="v2"}
func writeLabels(buf *bytes.Buffer, global, extra map[string]string) {
	if len(global) == 0 && len(extra) == 0 {
		return
	}

	keys := make([]string, 0, len(global)+len(extra))
	merged := make(map[string]string, len(global)+len(extra))
	for k, v := range global {
		if v != "" {
			keys = append(keys, k)
			merged[k] = v
		}
	}
	for k, v := range extra {
		if v != "" {
			if _, exists := merged[k]; !exists {
				keys = append(keys, k)
			}
			merged[k] = v
		}
	}
	sort.Strings(keys)

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(k)
		buf.WriteString("=\"")
		escapeLabelValue(buf, merged[k])
		buf.WriteByte('"')
	}
	buf.WriteByte('}')
}

// escapeLabelValue 对标签值进行 Prometheus 规范转义（转义反斜杠、双引号和换行符）
func escapeLabelValue(buf *bytes.Buffer, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\n':
			buf.WriteString(`\n`)
		default:
			buf.WriteByte(c)
		}
	}
}

// handleMetrics 处理 /metrics 的 HTTP 请求，输出 Prometheus 文本指标。
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	snap := s.sampler.Latest()
	payload := formatPrometheusMetrics(snap, s.opts.Agent)
	_, _ = w.Write(payload)
}

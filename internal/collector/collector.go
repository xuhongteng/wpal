// Package collector 定义监控快照的数据契约与采集接口。
//
// 本文件是 JSON 契约的唯一真源：字段名与既有 Python 实现（monitor/app/collector.py）
// 严格一致，前端因此可以零改动复用。任何字段变更必须是"只增不减"，
// 以保证旧前端仍可工作。
package collector

import "time"

// Snapshot 是一次采样的完整快照。
type Snapshot struct {
	TS       float64 `json:"ts"`
	Host     Host    `json:"host"`
	CPU      CPU     `json:"cpu"`
	Mem      Mem     `json:"mem"`
	Disk     Disk    `json:"disk"`
	Net      Net     `json:"net"`
	Procs    []Proc  `json:"procs"`
	ProcsMem []Proc  `json:"procs_mem"`
	Sys      Sys     `json:"sys"`

	// 以下两字段由采样器填充，采集器保持为空切片以序列化成 [] 而非 null。
	Events       []Event       `json:"events"`
	ActiveAlerts []ActiveAlert `json:"active_alerts"`
}

// Host 主机静态信息。
type Host struct {
	Hostname string  `json:"hostname"`
	Kernel   string  `json:"kernel"`
	Label    string  `json:"label"`
	Uptime   float64 `json:"uptime"`
	BootTime float64 `json:"boot_time"`
}

// Breakdown CPU 时间构成（百分比）。
type Breakdown struct {
	Busy   float64 `json:"busy"`
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Iowait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
	Idle   float64 `json:"idle"`
}

// Core 单核负载。
type Core struct {
	Busy   float64 `json:"busy"`
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Iowait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
}

// CPU CPU 相关指标。
type CPU struct {
	Total         float64   `json:"total"`
	Breakdown     Breakdown `json:"breakdown"`
	PerCore       []Core    `json:"percore"`
	Cores         int       `json:"cores"`
	Physical      int       `json:"physical"`
	Load1         float64   `json:"load1"`
	Load5         float64   `json:"load5"`
	Load15        float64   `json:"load15"`
	CtxSwitchRate float64   `json:"ctx_switch_rate"`
	FreqMHz       float64   `json:"freq_mhz"`
}

// Mem 内存与交换分区指标（单位：字节）。
type Mem struct {
	Total       float64 `json:"total"`
	Used        float64 `json:"used"`
	Available   float64 `json:"available"`
	Free        float64 `json:"free"`
	Percent     float64 `json:"percent"`
	Cached      float64 `json:"cached"`
	Buffers     float64 `json:"buffers"`
	Shared      float64 `json:"shared"`
	SwapTotal   float64 `json:"swap_total"`
	SwapUsed    float64 `json:"swap_used"`
	SwapFree    float64 `json:"swap_free"`
	SwapPercent float64 `json:"swap_percent"`
}

// Partition 挂载点容量与 inode 使用情况。
type Partition struct {
	Device       string  `json:"device"`
	Mount        string  `json:"mount"`
	Fstype       string  `json:"fstype"`
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	Percent      float64 `json:"percent"`
	InodePercent float64 `json:"inode_percent"`
	InodeTotal   uint64  `json:"inode_total"`
	InodeFree    uint64  `json:"inode_free"`
}

// DiskIO 单块设备的 IO 速率。
type DiskIO struct {
	Name      string  `json:"name"`
	ReadBPS   float64 `json:"read_bps"`
	WriteBPS  float64 `json:"write_bps"`
	ReadIOPS  float64 `json:"read_iops"`
	WriteIOPS float64 `json:"write_iops"`
	Busy      float64 `json:"busy"`
}

// Disk 磁盘分区与 IO 汇总。
type Disk struct {
	Partitions []Partition `json:"partitions"`
	IO         []DiskIO    `json:"io"`
}

// Iface 单个网卡的流量计数。
type Iface struct {
	Name    string  `json:"name"`
	RxBPS   float64 `json:"rx_bps"`
	TxBPS   float64 `json:"tx_bps"`
	RxPPS   float64 `json:"rx_pps"`
	TxPPS   float64 `json:"tx_pps"`
	RxTotal uint64  `json:"rx_total"`
	TxTotal uint64  `json:"tx_total"`
	ErrIn   uint64  `json:"err_in"`
	ErrOut  uint64  `json:"err_out"`
	DropIn  uint64  `json:"drop_in"`
	DropOut uint64  `json:"drop_out"`
	Virtual bool    `json:"virtual"`
}

// Net 网络流量与 TCP 连接状态。
type Net struct {
	Interfaces []Iface        `json:"interfaces"`
	TCP        map[string]int `json:"tcp"`
	TCPTotal   int            `json:"tcp_total"`
}

// Proc 单个进程的信息。
type Proc struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	RSS     uint64  `json:"rss"`
	MemPct  float64 `json:"mem_pct"`
	Threads int     `json:"threads"`
	Cmd     string  `json:"cmd"`
}

// Sys 系统级健康指标。
type Sys struct {
	Procs            int     `json:"procs"`
	Threads          int     `json:"threads"`
	ProcsRunning     int     `json:"procs_running"`
	ProcsBlocked     int     `json:"procs_blocked"`
	FDUsed           int     `json:"fd_used"`
	FDMax            int     `json:"fd_max"`
	Entropy          float64 `json:"entropy"`
	ProcessesStarted int     `json:"processes_started"`
}

// Event 一条告警事件。
type Event struct {
	ID           int64          `json:"id"`
	TS           float64        `json:"ts"`
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	LabelKey     string         `json:"label_key"`
	Level        string         `json:"level"`
	Status       string         `json:"status"`
	Value        float64        `json:"value"`
	Unit         string         `json:"unit"`
	Detail       string         `json:"detail"`
	DetailKey    string         `json:"detail_key,omitempty"`
	DetailParams map[string]any `json:"detail_params,omitempty"`
}

// ActiveAlert 当前处于触发状态的告警。
type ActiveAlert struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	LabelKey string  `json:"label_key"`
	Level    string  `json:"level"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Source   string  `json:"source"`
	// Detail / DetailKey / DetailParams 供前端渲染详情（可为空）。
	Detail       string         `json:"detail,omitempty"`
	DetailKey    string         `json:"detail_key,omitempty"`
	DetailParams map[string]any `json:"detail_params,omitempty"`
}

// Options 采集器配置。
type Options struct {
	// HostRoot 是宿主根目录的挂载点（容器场景用，如 /host/root）；
	// 裸机部署留空即可，此时直接使用真实路径。
	HostRoot string
	// Label 覆盖展示用的主机名。
	Label string
	// TopProcesses 进程榜长度（CPU 榜与内存榜各取前 N）。
	TopProcesses int
	// IncludeVirtualNics 是否统计 docker0/veth/br-* 等虚拟网卡。
	IncludeVirtualNics bool
}

// Collector 采集器接口。
type Collector interface {
	// Collect 采集一次快照。
	Collect() (*Snapshot, error)
	// Close 释放资源。
	Close() error
}

// New 按当前平台创建采集器。
func New(opts Options) Collector {
	if opts.TopProcesses <= 0 {
		opts.TopProcesses = 15
	}
	return newCollector(opts)
}

// nowFunc 便于测试注入时钟。
var nowFunc = time.Now

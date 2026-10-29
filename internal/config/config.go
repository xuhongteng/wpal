// Package config 负责配置加载：YAML 文件 + 环境变量 + 命令行 flag。
//
// 优先级（从低到高）：内置默认值 → 配置文件 → 环境变量 → 命令行 flag。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是完整配置。
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Collect CollectConfig `yaml:"collect"`
	History HistoryConfig `yaml:"history"`
	Alerts  AlertsConfig  `yaml:"alerts"`
	Agent   AgentConfig   `yaml:"agent"`
}

// ServerConfig 是服务监听相关配置。
type ServerConfig struct {
	Listen   string `yaml:"listen"`
	BasePath string `yaml:"base_path"`
}

// CollectConfig 是采集相关配置。
type CollectConfig struct {
	Interval           string `yaml:"interval"`
	IncludeVirtualNics bool   `yaml:"include_virtual_nics"`
	TopProcesses       int    `yaml:"top_processes"`
	HostRoot           string `yaml:"host_root"`
	Label              string `yaml:"label"`
}

// HistoryConfig 是历史存储相关配置。
type HistoryConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Retain          string `yaml:"retain"`
	PersistInterval string `yaml:"persist_interval"`
	DataDir         string `yaml:"data_dir"`
}

// AlertsConfig 是告警阈值配置。
type AlertsConfig struct {
	CPU    Threshold `yaml:"cpu"`
	Memory Threshold `yaml:"memory"`
	Swap   Threshold `yaml:"swap"`
	Disk   Threshold `yaml:"disk"`
	Inode  Threshold `yaml:"inode"`
	Load   Threshold `yaml:"load"`
}

// AgentConfig 是标准化边缘采集探针相关配置。
type AgentConfig struct {
	// Headless 为 true 时关闭本地 Web 前端挂载与本地历史落盘，
	// 以极致轻量探针形态运行（内存 < 6MB），专供 Prometheus 拉取或集中纳管。
	Headless bool `yaml:"headless"`
	// Cluster 是节点归属的集群或分组标识（例如 prod-k8s, edge-beijing）。
	Cluster string `yaml:"cluster"`
	// NodeID 是节点唯一标识，留空默认使用系统 Hostname。
	NodeID string `yaml:"node_id"`
	// Labels 是一组键值对标签，自动注入到导出的 Prometheus 指标中。
	Labels map[string]string `yaml:"labels"`
}

// Threshold 是一条阈值规则。
//
// Enabled 为 nil 时视为启用（保持旧配置文件的兼容性）；
// 显式写 `enabled: false` 可关闭该规则，不必再靠塞一个 100 之类的假阈值绕过。
type Threshold struct {
	Enabled *bool   `yaml:"enabled"`
	Warn    float64 `yaml:"warn"`
	Crit    float64 `yaml:"crit"`
	Hold    string  `yaml:"hold"`
	Recover string  `yaml:"recover"`
}

// IsEnabled 报告该规则是否启用。
func (t Threshold) IsEnabled() bool {
	return t.Enabled == nil || *t.Enabled
}

// Default 返回内置默认配置。
func Default() Config {
	return Config{
		Server: ServerConfig{Listen: "127.0.0.1:8891", BasePath: ""},
		Collect: CollectConfig{
			Interval: "1s", IncludeVirtualNics: false, TopProcesses: 15,
		},
		History: HistoryConfig{
			Enabled: true, Retain: "168h", PersistInterval: "10s", DataDir: "data",
		},
		Alerts: AlertsConfig{
			CPU:    Threshold{Warn: 80, Crit: 93, Hold: "30s", Recover: "20s"},
			Memory: Threshold{Warn: 85, Crit: 94, Hold: "20s", Recover: "20s"},
			Swap:   Threshold{Warn: 50, Crit: 85, Hold: "60s", Recover: "30s"},
			Disk:   Threshold{Warn: 80, Crit: 90, Hold: "60s", Recover: "60s"},
			Inode:  Threshold{Warn: 85, Crit: 95, Hold: "60s", Recover: "60s"},
			Load:   Threshold{Warn: 1.5, Crit: 3.0, Hold: "45s", Recover: "30s"},
		},
		Agent: AgentConfig{
			Headless: false,
			Cluster:  "",
			NodeID:   "",
			Labels:   map[string]string{},
		},
	}
}

// Load 读取配置文件并与环境变量合并。
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("读取配置文件失败: %w", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("解析配置文件失败: %w", err)
		}
	}
	cfg.applyEnv()
	return cfg, cfg.validate()
}

func (c *Config) applyEnv() {
	getEnv := func(k string) string {
		// WPAL_* 是当前规范名；LITESIGHT_* 是改名之前的老名字，
		// 保留为兼容回退，老环境变量仍然可用。
		if v := os.Getenv("WPAL_" + k); v != "" {
			return v
		}
		return os.Getenv("LITESIGHT_" + k)
	}

	if v := getEnv("LISTEN"); v != "" {
		c.Server.Listen = v
	}
	if v := getEnv("BASE_PATH"); v != "" {
		c.Server.BasePath = v
	}
	if v := getEnv("INTERVAL"); v != "" {
		c.Collect.Interval = v
	}
	if v := getEnv("DATA_DIR"); v != "" {
		c.History.DataDir = v
	}
	if v := getEnv("RETAIN"); v != "" {
		c.History.Retain = v
	}
	if v := getEnv("HOST_ROOT"); v != "" {
		c.Collect.HostRoot = v
	}
	if v := getEnv("LABEL"); v != "" {
		c.Collect.Label = v
	}
	if v := getEnv("TOP_PROCESSES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Collect.TopProcesses = n
		}
	}
	if v := getEnv("VIRTUAL_NICS"); v != "" {
		c.Collect.IncludeVirtualNics = v == "1" || strings.EqualFold(v, "true")
	}
	if v := getEnv("HEADLESS"); v != "" {
		c.Agent.Headless = v == "1" || strings.EqualFold(v, "true")
	}
	if v := getEnv("CLUSTER"); v != "" {
		c.Agent.Cluster = v
	}
	if v := getEnv("NODE_ID"); v != "" {
		c.Agent.NodeID = v
	}
}

var prometheusLabelRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (c *Config) validate() error {
	if _, err := c.CollectDuration(); err != nil {
		return err
	}
	if _, err := c.PersistDuration(); err != nil {
		return err
	}
	if _, err := c.RetainDuration(); err != nil {
		return err
	}
	for name, th := range map[string]Threshold{
		"cpu": c.Alerts.CPU, "memory": c.Alerts.Memory, "swap": c.Alerts.Swap,
		"disk": c.Alerts.Disk, "inode": c.Alerts.Inode, "load": c.Alerts.Load,
	} {
		// 显式关闭的规则不参与校验，允许留空阈值
		if !th.IsEnabled() {
			continue
		}
		if th.Warn <= 0 || th.Crit <= 0 {
			return fmt.Errorf("告警阈值 %s 无效（warn/crit 必须为正数）", name)
		}
		if th.Crit < th.Warn {
			return fmt.Errorf("告警阈值 %s 无效（crit 不能小于 warn）", name)
		}
	}
	for k := range c.Agent.Labels {
		if !prometheusLabelRegex.MatchString(k) {
			return fmt.Errorf("agent.labels 键名 %q 不合法（必须符合 Prometheus 标签格式 ^[a-zA-Z_][a-zA-Z0-9_]*$）", k)
		}
	}
	return nil
}

// CollectDuration 返回采样周期。
func (c *Config) CollectDuration() (time.Duration, error) {
	return parseDur(c.Collect.Interval, "collect.interval")
}

// PersistDuration 返回历史落盘周期。
func (c *Config) PersistDuration() (time.Duration, error) {
	return parseDur(c.History.PersistInterval, "history.persist_interval")
}

// RetainDuration 返回历史留存时长。
func (c *Config) RetainDuration() (time.Duration, error) {
	return parseDur(c.History.Retain, "history.retain")
}

func parseDur(s, field string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("%s 不能为空", field)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s 解析失败: %w", field, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s 必须为正数", field)
	}
	return d, nil
}

// HistoryCapacity 根据留存时长与落盘周期估算环形文件容量（条）。
func (c *Config) HistoryCapacity() int {
	persist, err := c.PersistDuration()
	if err != nil || persist <= 0 {
		persist = 10 * time.Second
	}
	retain, err := c.RetainDuration()
	if err != nil || retain <= 0 {
		retain = 7 * 24 * time.Hour
	}
	n := int(retain/persist) + 16
	if n < 64 {
		n = 64
	}
	return n
}

// ResolveDataDir 解析数据目录（相对路径按工作目录展开）。
func (c *Config) ResolveDataDir() string {
	d := c.History.DataDir
	if d == "" {
		d = "data"
	}
	if !filepath.IsAbs(d) {
		if wd, err := os.Getwd(); err == nil {
			d = filepath.Join(wd, d)
		}
	}
	return d
}

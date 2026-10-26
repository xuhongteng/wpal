package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	c := Default()
	if err := c.validate(); err != nil {
		t.Fatalf("默认配置应合法: %v", err)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
server:
  listen: "0.0.0.0:9999"
  base_path: "/monitor"
collect:
  interval: 2s
  top_processes: 8
history:
  enabled: false
  retain: 24h
  data_dir: "/var/lib/wpal"
alerts:
  cpu: { warn: 70, crit: 90, hold: 10s, recover: 5s }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if c.Server.Listen != "0.0.0.0:9999" {
		t.Errorf("listen 不对: %s", c.Server.Listen)
	}
	if c.Server.BasePath != "/monitor" {
		t.Errorf("base_path 不对: %s", c.Server.BasePath)
	}
	if c.Collect.TopProcesses != 8 {
		t.Errorf("top_processes 不对: %d", c.Collect.TopProcesses)
	}
	if c.Alerts.CPU.Warn != 70 || c.Alerts.CPU.Crit != 90 {
		t.Errorf("告警阈值不对: %+v", c.Alerts.CPU)
	}
	if d, _ := c.CollectDuration(); d != 2*time.Second {
		t.Errorf("采样周期不对: %v", d)
	}
	if d, _ := c.RetainDuration(); d != 24*time.Hour {
		t.Errorf("留存时长不对: %v", d)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: \"1.2.3.4:1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITESIGHT_LISTEN", "127.0.0.1:8888")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != "127.0.0.1:8888" {
		t.Errorf("环境变量应覆盖文件，得到 %s", c.Server.Listen)
	}
}

func TestValidateRejectsBadThresholds(t *testing.T) {
	c := Default()
	c.Alerts.CPU.Crit = 10 // crit < warn
	if err := c.validate(); err == nil {
		t.Error("crit < warn 应报错")
	}
	c = Default()
	c.Alerts.Memory.Warn = 0
	if err := c.validate(); err == nil {
		t.Error("非正阈值应报错")
	}
}

func TestHistoryCapacity(t *testing.T) {
	c := Default()
	c.History.Retain = "168h"
	c.History.PersistInterval = "10s"
	// 7 天 / 10 秒 = 60480 条，加上一点余量
	if n := c.HistoryCapacity(); n < 60000 || n > 70000 {
		t.Errorf("容量估算不对: %d", n)
	}
}

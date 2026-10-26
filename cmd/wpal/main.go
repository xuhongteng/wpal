// Command wpal 是轻量级服务器监控工具的命令行入口。
//
// 用法：
//
//	wpal                    # 以默认配置启动服务
//	wpal --config /etc/wpal/config.yaml
//	wpal --dump             # 采集一次快照并输出 JSON（便于脚本/比对）
//	wpal --version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"mon/internal/alerts"
	"mon/internal/collector"
	"mon/internal/config"
	"mon/internal/history"
	"mon/internal/i18n"
	"mon/internal/sampler"
	"mon/internal/server"
	"mon/internal/version"
	"mon/web"
)

func main() {
	var (
		cfgPath  = flag.String("config", "", "配置文件路径（YAML）")
		showVer  = flag.Bool("version", false, "打印版本信息并退出")
		dump     = flag.Bool("dump", false, "采集一次快照并以 JSON 输出后退出")
		dumpAll  = flag.Bool("dump-all", false, "输出全部采样帧（JSON 数组）")
		listen   = flag.String("listen", "", "监听地址，覆盖配置文件")
		basePath = flag.String("base-path", "", "部署子路径，如 /monitor")
		dataDir  = flag.String("data-dir", "", "数据目录")
		hostRoot = flag.String("host-root", "", "宿主根目录挂载点（容器场景）")
		label    = flag.String("label", "", "展示用的主机名")
		topProcs = flag.Int("top", 0, "进程榜长度")
		repeat   = flag.Int("repeat", 1, "--dump 的采样轮数")
		interval = flag.Duration("interval", 0, "--dump 多轮采样之间的间隔")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version.Get().String())
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}
	// 命令行覆盖配置文件
	if *listen != "" {
		cfg.Server.Listen = *listen
	}
	if *basePath != "" {
		cfg.Server.BasePath = *basePath
	}
	if *dataDir != "" {
		cfg.History.DataDir = *dataDir
	}
	if *hostRoot != "" {
		cfg.Collect.HostRoot = *hostRoot
	}
	if *label != "" {
		cfg.Collect.Label = *label
	}
	if *topProcs > 0 {
		cfg.Collect.TopProcesses = *topProcs
	}

	col := collector.New(collector.Options{
		HostRoot:           cfg.Collect.HostRoot,
		Label:              cfg.Collect.Label,
		TopProcesses:       cfg.Collect.TopProcesses,
		IncludeVirtualNics: cfg.Collect.IncludeVirtualNics,
	})
	defer col.Close()

	if *dump || *dumpAll {
		if err := runDump(col, *dump, *dumpAll, *repeat, *interval); err != nil {
			fmt.Fprintf(os.Stderr, "采集失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runServer(cfg, col); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

// runDump 执行一次或多次采样并输出 JSON。
func runDump(col collector.Collector, single, all bool, repeat int, interval time.Duration) error {
	if repeat <= 0 {
		repeat = 1
	}
	if interval <= 0 {
		interval = time.Second
	}
	samples := make([]*collector.Snapshot, 0, repeat)
	for i := 0; i < repeat; i++ {
		s, err := col.Collect()
		if err != nil {
			return err
		}
		samples = append(samples, s)
		if i < repeat-1 {
			time.Sleep(interval)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	var payload any = samples[len(samples)-1]
	if all {
		payload = samples
	}
	return enc.Encode(payload)
}

// runServer 启动 HTTP + WebSocket 服务。
func runServer(cfg config.Config, col collector.Collector) error {
	collectDur, err := cfg.CollectDuration()
	if err != nil {
		return err
	}
	persistDur, err := cfg.PersistDuration()
	if err != nil {
		return err
	}

	dataDir := cfg.ResolveDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	// 历史环形存储
	var ring *history.Ring
	if cfg.History.Enabled {
		ring, err = history.Open(filepath.Join(dataDir, "history.db"), cfg.HistoryCapacity(), 360)
		if err != nil {
			return fmt.Errorf("打开历史存储失败: %w", err)
		}
		defer ring.Close()
	}

	events, err := history.OpenEventStore(filepath.Join(dataDir, "events.jsonl"), 500)
	if err != nil {
		return fmt.Errorf("打开事件存储失败: %w", err)
	}
	defer events.Close()

	engine := alerts.New(buildRules(cfg))

	smp := sampler.New(col, collectDur, persistDur, ring, events, engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go smp.Run(ctx)

	srv := server.New(smp, ring, events, web.Sub(), server.Options{BasePath: cfg.Server.BasePath})

	// 启动阶段（前端解压/压缩等）产生的临时堆内存还给操作系统，
	// 让常驻 RSS 贴近真实工作集。
	debug.FreeOSMemory()

	// 服务是"每秒采一帧、其余时间空闲"的形态，实时堆很小。
	// 收紧 GC 阈值（默认 100 → 40）几乎不增加 CPU（采样本身才有开销），
	// 但能把常驻 RSS 压到接近真实工作集的水平。
	debug.SetGCPercent(40)

	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// 不设 WriteTimeout：WebSocket 是长连接，由 IdleTimeout 之外的连接自行管理
		IdleTimeout: 120 * time.Second,
	}

	// 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\n收到退出信号，正在关闭…")
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	fmt.Printf("wpal %s\n", version.Get().String())
	fmt.Printf("  监听   : %s%s\n", cfg.Server.Listen, cfg.Server.BasePath)
	fmt.Printf("  采样   : 每 %s\n", collectDur)
	if ring != nil {
		fmt.Printf("  历史   : %s（留存 %s）\n", filepath.Join(dataDir, "history.db"), cfg.History.Retain)
	}
	fmt.Printf("  看板   : http://%s%s/\n", cfg.Server.Listen, cfg.Server.BasePath)

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// buildRules 依据配置生成告警规则。
func buildRules(cfg config.Config) []alerts.Rule {
	rules := alerts.DefaultRules()
	byKey := map[string]alerts.Rule{}
	for _, r := range rules {
		byKey[r.Key] = r
	}
	apply := func(key string, th config.Threshold) {
		r, ok := byKey[key]
		if !ok {
			return
		}
		r.Warn = th.Warn
		r.Crit = th.Crit
		if d, err := time.ParseDuration(th.Hold); err == nil && d > 0 {
			r.Hold = d
		}
		if d, err := time.ParseDuration(th.Recover); err == nil && d > 0 {
			r.Recover = d
		}
		byKey[key] = r
	}
	apply("cpu", cfg.Alerts.CPU)
	apply("mem", cfg.Alerts.Memory)
	apply("swap", cfg.Alerts.Swap)
	apply("disk", cfg.Alerts.Disk)
	apply("inode", cfg.Alerts.Inode)
	apply("load", cfg.Alerts.Load)

	out := make([]alerts.Rule, 0, len(byKey))
	for _, r := range rules {
		out = append(out, byKey[r.Key])
	}
	// 让标签文案走 i18n 包，避免中文散落在各处
	for i := range out {
		out[i].Label = i18n.Label(out[i].LabelKey)
	}
	return out
}

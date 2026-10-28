// Package server 提供 HTTP API 与 WebSocket 推送。
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mon/internal/collector"
	"mon/internal/history"
	"mon/internal/sampler"
	"mon/internal/version"
)

// Options 是服务端配置。
type Options struct {
	// BasePath 是部署在子路径下时的前缀，例如 "/monitor"。
	BasePath string
}

// Server 是 HTTP 服务。
type Server struct {
	opts    Options
	sampler *sampler.Sampler
	ring    *history.Ring
	events  *history.EventStore
	static  *staticHandler
	ws      *wsHandler
	mux     *http.ServeMux
}

// New 创建 HTTP 服务。
func New(s *sampler.Sampler, ring *history.Ring, events *history.EventStore, webFS fs.FS, opts Options) *Server {
	srv := &Server{
		opts:    opts,
		sampler: s,
		ring:    ring,
		events:  events,
		static:  newStaticHandler(webFS),
		ws:      &wsHandler{sampler: s},
		mux:     http.NewServeMux(),
	}
	srv.routes()
	return srv
}

// Handler 返回根处理器。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	base := strings.TrimSuffix(s.opts.BasePath, "/")

	s.mux.HandleFunc(base+"/api/health", s.handleHealth)
	s.mux.HandleFunc(base+"/api/snapshot", s.handleSnapshot)
	s.mux.HandleFunc(base+"/api/history", s.handleHistory)
	s.mux.HandleFunc(base+"/api/events", s.handleEvents)
	s.mux.HandleFunc(base+"/api/procs", s.handleProcs)
	s.mux.HandleFunc(base+"/ws/stream", s.ws.ServeWS)
	s.mux.HandleFunc(base+"/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if base == "" {
		s.mux.Handle("/", s.static)
	} else {
		s.mux.Handle(base+"/", http.StripPrefix(base, s.static))
		s.mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, base+"/", http.StatusFound)
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	for k, val := range noCache {
		w.Header().Set(k, val)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok":      true,
		"ts":      time.Now().Unix(),
		"version": version.Get().Version,
	})
}

// handleSnapshot 返回最近一帧快照。
//
// 注意：这里**不重新采集**，直接返回采样循环缓存的帧。
// 早期实现每次请求都重新跑一遍全量采集（40ms/次），50 个客户端轮询就能把 CPU 打满。
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := s.sampler.Latest()
	if snap == nil {
		writeJSON(w, &collector.Snapshot{
			Procs: []collector.Proc{}, ProcsMem: []collector.Proc{},
			Events: []collector.Event{}, ActiveAlerts: []collector.ActiveAlert{},
		})
		return
	}
	writeJSON(w, snap)
}

// handleProcs 返回进程明细（按需拉取，不占用实时推送带宽）。
func (s *Server) handleProcs(w http.ResponseWriter, r *http.Request) {
	snap := s.sampler.Latest()
	if snap == nil {
		writeJSON(w, map[string]any{"procs": []collector.Proc{}, "procs_mem": []collector.Proc{}})
		return
	}
	sortBy := r.URL.Query().Get("sort")
	list := snap.Procs
	if sortBy == "mem" {
		list = snap.ProcsMem
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	writeJSON(w, map[string]any{"procs": list, "procs_mem": snap.ProcsMem})
}

// handleEvents 返回告警事件流（倒序）。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	evs := s.events.Recent(limit)
	if evs == nil {
		evs = []collector.Event{}
	}
	writeJSON(w, map[string]any{"events": evs, "total": s.events.Total()})
}

// historyRanges 是支持的时间范围档位（秒）。
var historyRanges = map[string]int{
	"5m":  300,
	"1h":  3600,
	"6h":  21600,
	"24h": 86400,
	"7d":  604800,
}

// handleHistory 返回历史序列，格式与既有实现保持一致，
// 前端无需任何改动即可切换到历史回看。
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	seconds, ok := historyRanges[rng]
	if !ok {
		rng = "1h"
		seconds = historyRanges["1h"]
	}
	if s.ring == nil {
		writeJSON(w, map[string]any{"ts": []int64{}, "count": 0, "range": rng})
		return
	}
	samples, err := s.ring.QueryDownsampled(time.Now().Add(-time.Duration(seconds)*time.Second), 400)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error(), "range": rng})
		return
	}

	// 预先按样本数分配容量，避免循环里反复扩容。
	n := len(samples)
	var (
		ts      = make([]int64, 0, n)
		cpu     = make([]float32, 0, n)
		mem     = make([]float32, 0, n)
		memUsed = make([]float64, 0, n)
		swap    = make([]float32, 0, n)
		load1   = make([]float32, 0, n)
		percore = make([][]float32, 0, n)
		brk     = make([]map[string]float64, 0, n)
		net     = make([]map[string][]float64, 0, n)
		diskio  = make([]map[string][]float64, 0, n)
	)

	for _, sm := range samples {
		ts = append(ts, sm.TS)
		cpu = append(cpu, sm.CPU)
		mem = append(mem, sm.Mem)
		memUsed = append(memUsed, sm.MemUsed)
		swap = append(swap, sm.Swap)
		load1 = append(load1, sm.Load1)

		pc := make([]float32, 0, len(sm.PerCore))
		for _, v := range sm.PerCore {
			if v != 0 {
				pc = append(pc, v)
			}
		}
		percore = append(percore, pc)

		brk = append(brk, map[string]float64{
			"busy": float64(sm.Brk[0]), "user": float64(sm.Brk[1]),
			"system": float64(sm.Brk[2]), "iowait": float64(sm.Brk[3]),
			"steal": float64(sm.Brk[4]), "idle": float64(sm.Brk[5]),
		})
		net = append(net, map[string][]float64{"total": {sm.NetRx, sm.NetTx}})
		diskio = append(diskio, map[string][]float64{"total": {sm.IOR, sm.IOW}})
	}

	writeJSON(w, map[string]any{
		"ts": ts, "cpu": cpu, "mem": mem, "mem_used": memUsed,
		"swap": swap, "load1": load1,
		"cpu_percore": percore, "cpu_brk": brk,
		"net": net, "diskio": diskio,
		"count": len(samples), "range": rng,
	})
}

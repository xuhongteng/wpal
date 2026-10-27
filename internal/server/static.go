package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"mon/internal/version"
)

// noCache 是所有动态响应统一使用的缓存头。
//
// 背景：静态资源若不带 Cache-Control，浏览器会按 Last-Modified 做启发式缓存，
// 导致前端改版后用户仍加载旧 CSS/JS（结构与脚本错配 → 页面崩）。
var noCache = map[string]string{
	"Cache-Control": "no-cache, no-store, must-revalidate",
	"Pragma":        "no-cache",
	"Expires":       "0",
}

type asset struct {
	body        []byte // gzip 压缩后的内容
	contentType string
	origSize    int
}

// staticHandler 提供前端静态资源。
//
// 所有文件在启动时读入内存并 gzip 压缩：前端约 1.1 MB（其中 ECharts 占 1 MB），
// 压缩后约 380 KB，直接省下约 700 KB 常驻内存；浏览器原生支持
// Content-Encoding: gzip，传输和解压都在浏览器侧完成。
type staticHandler struct {
	files   map[string]asset
	buildID string
}

// newStaticHandler 构造静态资源处理器。
func newStaticHandler(fsys fs.FS) *staticHandler {
	h := &staticHandler{files: map[string]asset{}}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		h.files[p] = asset{
			body:        gzipBytes(raw),
			contentType: contentTypeOf(p),
			origSize:    len(raw),
		}
		return nil
	})
	h.buildID = buildID(h.files)
	return h
}

// gzipBytes 压缩内容；压缩失败时返回原文（保证不会因为压缩出错而丢内容）。
func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return b
	}
	if _, err := w.Write(b); err != nil {
		return b
	}
	if err := w.Close(); err != nil {
		return b
	}
	return buf.Bytes()
}

// buildID 按前端文件内容算指纹。文件一改，指纹必变，
// 配合 index.html 里的 ?v=<指纹> 可确保浏览器一定拉取新版本。
func buildID(files map[string]asset) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	h := sha1.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte(files[n].contentType))
		h.Write(files[n].body)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// contentTypeOf 按扩展名推断 Content-Type。
func contentTypeOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// ServeHTTP 处理 / 与 /static/*。
func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" || p == "index.html" {
		h.serveIndex(w, r)
		return
	}
	if strings.HasPrefix(p, "static/") {
		p = strings.TrimPrefix(p, "static/")
	} else {
		http.NotFound(w, r)
		return
	}
	a, ok := h.files[p]
	if !ok {
		http.NotFound(w, r)
		return
	}
	for k, v := range noCache {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Length", itoa(len(a.body)))
	_, _ = w.Write(a.body)
}

// serveIndex 输出首页，并把 __BUILD__ 占位符替换成内容指纹。
// 首页本身也压缩下发。
func (h *staticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	a, ok := h.files["index.html"]
	if !ok {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	html := strings.ReplaceAll(string(decompress(a.body)), "__BUILD__", h.buildID)
	html = strings.ReplaceAll(html, "__VERSION__", version.Get().Version)
	body := gzipBytes([]byte(html))

	for k, v := range noCache {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Length", itoa(len(body)))
	_, _ = w.Write(body)
}

// decompress 用于取回原始文本（首页需要替换占位符后再压缩）。
func decompress(b []byte) []byte {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return b
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return b
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

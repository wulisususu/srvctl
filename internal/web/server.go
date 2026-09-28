// Package web 提供本地 HTTP 服务与内嵌的可视化界面。
//
// 界面完全内嵌在二进制里（go:embed），没有 npm、没有构建步骤、没有
// node_modules —— 改一行 HTML 重新 go build 即可，不到一秒。
//
// 安全模型：
//   - 只监听 127.0.0.1，不暴露到局域网
//   - 所有 /api/* 都要求 X-Srvctl-Token 头，token 随机生成并只出现在
//     启动时打开的 URL 里，防止本机其他进程随便读走凭据
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"srvctl/internal/netid"
	"srvctl/internal/reach"
	"srvctl/internal/vault"
)

//go:embed assets
var assetsFS embed.FS

// Options 是启动界面的参数。
type Options struct {
	Store    *vault.Store
	Portable bool
	Open     bool
	Logger   *log.Logger
	LogPath  string
}

// Server 是本地界面服务。
type Server struct {
	store    *vault.Store
	portable bool
	token    string
	log      *log.Logger
	logPath  string

	mu      sync.RWMutex
	ssid    string
	results map[string]reach.Result

	quitOnce sync.Once
	quitCh   chan struct{}
	httpSrv  *http.Server
}

// Run 启动界面服务并阻塞，直到用户点击「退出」或进程被结束。
func Run(opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	token, err := randomToken()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("监听本地端口失败: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	s := &Server{
		store:    opts.Store,
		portable: opts.Portable,
		token:    token,
		log:      logger,
		logPath:  opts.LogPath,
		ssid:     netid.SSID(),
		results:  map[string]reach.Result{},
		quitCh:   make(chan struct{}),
	}

	mux := http.NewServeMux()
	s.routes(mux)
	s.httpSrv = &http.Server{
		Handler: mux,

		// 只限制"读请求头"的耗时，不用 ReadTimeout。
		//
		// 关键：http.Server 的 IdleTimeout 为 0 时会**沿用 ReadTimeout**
		// 作为空闲连接的存活时间。原先 ReadTimeout=15s，于是浏览器
		// keep-alive 连接空闲 15 秒就被服务端关掉；浏览器若复用这条死连接
		// 发 POST（非幂等，不会自动重试），就会报 "Failed to fetch"。
		ReadHeaderTimeout: 10 * time.Second,

		// 空闲连接给足时间，让浏览器能安全复用。
		IdleTimeout: 5 * time.Minute,

		// /api/test 在服务器多、超时高的时候会比较久，留够余量。
		WriteTimeout: 3 * time.Minute,
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	logger.Printf("界面地址: %s", url)
	logger.Printf("vault: %s", s.store.Path())
	logger.Printf("当前网络: %s", displaySSID(s.ssid))

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	if opts.Open {
		if err := openBrowser(url); err != nil {
			logger.Printf("自动打开浏览器失败，请手动访问上面的地址: %v", err)
		}
	}

	select {
	case err := <-errCh:
		return err
	case <-s.quitCh:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(ctx)
		logger.Printf("界面已退出")
		return nil
	}
}

func (s *Server) requestQuit() {
	s.quitOnce.Do(func() { close(s.quitCh) })
}

func (s *Server) routes(mux *http.ServeMux) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err) // 内嵌资源缺失属于构建期错误
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/ping", s.route(s.handlePing))
	mux.HandleFunc("/api/state", s.route(s.handleState))
	mux.HandleFunc("/api/init", s.route(s.handleInit))
	mux.HandleFunc("/api/unlock", s.route(s.handleUnlock))
	mux.HandleFunc("/api/lock", s.route(s.handleLock))
	mux.HandleFunc("/api/test", s.route(s.handleTest))
	mux.HandleFunc("/api/servers", s.route(s.handleServers))
	mux.HandleFunc("/api/delete", s.route(s.handleDelete))
	mux.HandleFunc("/api/snippet", s.route(s.handleSnippet))
	mux.HandleFunc("/api/quit", s.route(s.handleQuit))
}

// route 把 panic 兜底和 token 校验串起来。
func (s *Server) route(h http.HandlerFunc) http.HandlerFunc {
	return s.recoverPanic(s.guard(h))
}

// trackingWriter 记录响应头是否已经写出 —— panic 兜底需要据此决定
// 还能不能补一个 500。
type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackingWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

// recoverPanic 把 panic 变成可以看见的错误。
//
// 没有它的话，handler 一旦 panic，net/http 只会静默关闭连接 —— 浏览器端
// 表现为 "Failed to fetch"，用户拿不到任何原因，日志里也只有一行默认堆栈。
// 有了它：日志写明哪个请求 panic 了、堆栈是什么；浏览器至少能拿到 500 和
// 一句话；而且服务本身不会因为单个请求出错而中断。
func (s *Server) recoverPanic(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			s.log.Printf("PANIC %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
			if !tw.wrote {
				writeError(tw, http.StatusInternalServerError,
					fmt.Sprintf("服务端内部错误（已写入日志 %s）: %v", s.logPath, rec))
			}
		}()
		next(tw, r)
	}
}

// guard 校验请求头里的 token。
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Srvctl-Token") != s.token {
			writeError(w, http.StatusForbidden, "token 无效")
			return
		}
		next(w, r)
	}
}

func (s *Server) setSSID(v string) {
	s.mu.Lock()
	s.ssid = v
	s.mu.Unlock()
}

func (s *Server) getSSID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ssid
}

func (s *Server) setResults(rs map[string]reach.Result) {
	s.mu.Lock()
	s.results = rs
	s.mu.Unlock()
}

func (s *Server) getResults() map[string]reach.Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]reach.Result, len(s.results))
	for k, v := range s.results {
		out[k] = v
	}
	return out
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机 token 失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func displaySSID(ssid string) string {
	if ssid == "" {
		return "未知（有线或无 WLAN 网卡）"
	}
	return ssid
}

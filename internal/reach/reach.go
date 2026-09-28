// Package reach 做 TCP 连通性探测，产出四态结果。
//
// 只做 TCP connect，不做 ICMP。原因：
//   - ICMP 在 Go 里需要原始套接字，Windows 上要管理员权限
//   - "端口能不能连"比"能不能 ping 通"更贴近真实可用性
//   - SSH / RDP 本身就是 TCP
//
// ── 四态判定规则 ────────────────────────────────────────────────
//
//	required_ssid  当前 SSID      探测结果   判定
//	─────────────  ────────────  ─────────  ────────────────
//	（空）         任意          成功       🟢 可连接
//	（空）         任意          失败       🔴 不可连接
//	Corp-Dev       其它 WiFi     不探测     ⚪ 网络环境不满足
//	Corp-Dev       未知 / 未连   成功       🟢 可连接
//	Corp-Dev       未知 / 未连   失败       ⚪ 网络环境不满足
//	Corp-Dev       Corp-Dev      成功       🟢 可连接
//	Corp-Dev       Corp-Dev      失败       🔴 不可连接
//
// 关键在第 5 行：当读不到当前 SSID（有线连接、WiFi 断开、读取失败）而条目
// 又要求特定 WiFi 时，探测失败更可能是网络环境问题，而不是服务器挂了 ——
// 所以判灰而不是判红。但如果探测成功，仍然如实报绿（公司有线也能通内网）。
package reach

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"srvctl/internal/model"
)

// State 是探测结果状态。
type State string

const (
	// StateOK 可连接（绿）。
	StateOK State = "ok"
	// StateDown 不可连接（红）。
	StateDown State = "down"
	// StateWrongNet 当前网络环境不满足（灰）。
	StateWrongNet State = "wrong_net"
	// StateSkipped 不检测（➖）。
	StateSkipped State = "skipped"
)

// Result 是单条服务器的探测结果。
type Result struct {
	Name      string    `json:"name"`
	State     State     `json:"state"`
	LatencyMs int64     `json:"latency_ms"`
	Err       string    `json:"err,omitempty"`
	Note      string    `json:"note,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

const (
	// DialTimeout 单次连接超时。内网 2 秒足够；公网慢的机器可以调大。
	DialTimeout = 2 * time.Second
	// Concurrency 并发上限。几百台服务器同时 dial 会打满 fd，
	// 也容易触发对端防火墙的扫描告警。
	Concurrency = 12
)

// CheckAll 并发探测全部条目。
//
// currentSSID 为空字符串表示"无法识别当前网络"。
func CheckAll(ctx context.Context, servers []model.Server, currentSSID string) []Result {
	results := make([]Result, len(servers))
	sem := make(chan struct{}, Concurrency)
	var wg sync.WaitGroup

	for i, s := range servers {
		// 已知处于错误的网络：直接判灰，不做无谓的 TCP 等待。
		if isWrongNetwork(s, currentSSID) {
			results[i] = Result{
				Name:      s.Name,
				State:     StateWrongNet,
				Note:      networkNote(s, currentSSID),
				CheckedAt: time.Now(),
			}
			continue
		}

		if !s.Checkable() {
			results[i] = Result{Name: s.Name, State: StateSkipped, CheckedAt: time.Now()}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s model.Server) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = probe(ctx, s, currentSSID)
		}(i, s)
	}

	wg.Wait()
	return results
}

// Check 探测单条服务器，返回带网络判定的四态结果。
func Check(ctx context.Context, s model.Server, currentSSID string) Result {
	if isWrongNetwork(s, currentSSID) {
		return Result{
			Name: s.Name, State: StateWrongNet,
			Note: networkNote(s, currentSSID), CheckedAt: time.Now(),
		}
	}
	if !s.Checkable() {
		return Result{Name: s.Name, State: StateSkipped, CheckedAt: time.Now()}
	}
	return probe(ctx, s, currentSSID)
}

// probe 只做 TCP 连接，失败时再决定是红还是灰。
func probe(ctx context.Context, s model.Server, currentSSID string) Result {
	res := Result{Name: s.Name, CheckedAt: time.Now()}
	if s.Host == "" {
		res.State = StateSkipped
		return res
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.EffectivePort()))
	start := time.Now()
	conn, err := (&net.Dialer{Timeout: DialTimeout}).DialContext(ctx, "tcp", addr)
	res.LatencyMs = time.Since(start).Milliseconds()

	if err == nil {
		_ = conn.Close()
		res.State = StateOK
		return res
	}

	res.Err = err.Error()
	switch {
	case s.RequiredSSID == "":
		// 不依赖特定网络 —— 连不上就是连不上。
		res.State = StateDown
	case currentSSID == s.RequiredSSID:
		// 网络对了还不通 —— 是服务器或服务的问题。
		res.State = StateDown
	default:
		// 网络未知或已知错误 —— 无法排除网络原因，如实说"不确定"。
		res.State = StateWrongNet
		res.Note = networkNote(s, currentSSID)
	}
	return res
}

// isWrongNetwork 判断是否已知处于错误的网络。
//
// 只在**明确读到**一个不同的 SSID 时才为真。当前网络未知时返回 false ——
// 这时不能跳过探测，因为公司有线照样可能通内网。
func isWrongNetwork(s model.Server, currentSSID string) bool {
	return s.RequiredSSID != "" && currentSSID != "" && s.RequiredSSID != currentSSID
}

func networkNote(s model.Server, currentSSID string) string {
	if currentSSID == "" {
		return fmt.Sprintf("未检测到 WiFi（需要 %s），连接失败也可能是网络原因", s.RequiredSSID)
	}
	return fmt.Sprintf("当前 WiFi 是 %s，需要 %s", currentSSID, s.RequiredSSID)
}

// Index 把结果切片转成按名称索引的 map。
func Index(results []Result) map[string]Result {
	m := make(map[string]Result, len(results))
	for _, r := range results {
		m[r.Name] = r
	}
	return m
}

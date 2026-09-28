package reach

import (
	"context"
	"net"
	"testing"

	"srvctl/internal/model"
)

// 起一个临时监听器，拿到一个"活着"的端口和地址。
func liveAddr(t *testing.T) (host string, port int, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起监听器失败: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { _ = ln.Close() }
}

// 拿一个确定没人监听的端口：起一个再关掉。
func deadPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起监听器失败: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func srv(host string, port int, ssid string) model.Server {
	s := model.Server{
		Name: "t", Host: host, Port: port,
		Platform: model.PlatformLinux, Username: "u",
		AuthMode: model.AuthPassword, Password: "p",
		RequiredSSID: ssid,
	}
	s.Normalize()
	return s
}

func TestCheckLivePortIsOK(t *testing.T) {
	host, port, closeFn := liveAddr(t)
	defer closeFn()

	got := Check(context.Background(), srv(host, port, ""), "")
	if got.State != StateOK {
		t.Errorf("活端口应为 ok，得到 %s (err=%s)", got.State, got.Err)
	}
}

func TestCheckNoSSIDRequirementDeadPortIsDown(t *testing.T) {
	s := srv("127.0.0.1", deadPort(t), "")
	got := Check(context.Background(), s, "")
	if got.State != StateDown {
		t.Errorf("不依赖网络 + 连不上 应为 down，得到 %s", got.State)
	}
}

func TestCheckKnownWrongNetworkSkipsProbe(t *testing.T) {
	// 即使端口是活的，只要明确处于错误网络也应该判灰。
	host, port, closeFn := liveAddr(t)
	defer closeFn()

	s := srv(host, port, "Corp-Dev")
	got := Check(context.Background(), s, "Home-WiFi")
	if got.State != StateWrongNet {
		t.Errorf("已知错误网络应为 wrong_net，得到 %s", got.State)
	}
	if got.Note == "" {
		t.Error("wrong_net 应带说明文字")
	}
}

func TestCheckUnknownNetworkButReachableIsOK(t *testing.T) {
	// 关键场景：有线连接（读不到 SSID）+ 要求特定 WiFi，但服务器其实通。
	// 必须如实报绿，不能因为网络未知就判灰。
	host, port, closeFn := liveAddr(t)
	defer closeFn()

	s := srv(host, port, "Corp-Dev")
	got := Check(context.Background(), s, "")
	if got.State != StateOK {
		t.Errorf("网络未知但可达 应为 ok，得到 %s", got.State)
	}
}

func TestCheckUnknownNetworkUnreachableIsGreyNotRed(t *testing.T) {
	// 核心场景：在家用有线，要求公司 WiFi 的内网服务器连不上。
	// 应该判灰（网络环境不满足），而不是红（服务器挂了）。
	s := srv("127.0.0.1", deadPort(t), "Corp-Dev")
	got := Check(context.Background(), s, "")
	if got.State != StateWrongNet {
		t.Errorf("网络未知 + 连不上 应为 wrong_net，得到 %s", got.State)
	}
	if got.Note == "" {
		t.Error("wrong_net 应带说明文字")
	}
}

func TestCheckCorrectNetworkUnreachableIsRed(t *testing.T) {
	// 网络对了还连不上 —— 这是服务器的问题，必须报红。
	s := srv("127.0.0.1", deadPort(t), "Corp-Dev")
	got := Check(context.Background(), s, "Corp-Dev")
	if got.State != StateDown {
		t.Errorf("网络正确 + 连不上 应为 down，得到 %s", got.State)
	}
}

func TestCheckModeNoneIsSkipped(t *testing.T) {
	s := srv("127.0.0.1", deadPort(t), "")
	s.CheckMode = model.CheckNone
	got := Check(context.Background(), s, "")
	if got.State != StateSkipped {
		t.Errorf("check_mode=none 应为 skipped，得到 %s", got.State)
	}
}

func TestCheckAllAssignsPerServerState(t *testing.T) {
	host, port, closeFn := liveAddr(t)
	defer closeFn()

	servers := []model.Server{
		srv(host, port, ""), // -> ok
		{Name: "skip", CheckMode: model.CheckNone, Host: "x"}, // -> skipped
		srv("127.0.0.1", deadPort(t), "Corp-Dev"),             // -> wrong_net (ssid 未知)
	}
	results := CheckAll(context.Background(), servers, "")

	if len(results) != 3 {
		t.Fatalf("结果数应为 3，得到 %d", len(results))
	}
	if results[0].State != StateOK {
		t.Errorf("第 1 条应为 ok，得到 %s", results[0].State)
	}
	if results[1].State != StateSkipped {
		t.Errorf("第 2 条应为 skipped，得到 %s", results[1].State)
	}
	if results[2].State != StateWrongNet {
		t.Errorf("第 3 条应为 wrong_net，得到 %s", results[2].State)
	}
}

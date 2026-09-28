package snippet

import (
	"strings"
	"testing"

	"srvctl/internal/model"
	"srvctl/internal/reach"
)

func linuxServer() model.Server {
	return model.Server{
		Name: "prod-db-1", Host: "10.0.1.23", Port: 22,
		Platform: model.PlatformLinux, Username: "root",
		AuthMode: model.AuthPassword, Category: "内网/数据库",
		Notes: "火山云 2核4G\n到期 2026-03",
	}
}

// srvctl 在 PATH 上时，给 AI 的就是干净的裸命令名。
func TestForUsesBareCommandWhenOnPath(t *testing.T) {
	got := For(linuxServer(), nil, "", "srvctl")

	if !strings.Contains(got, "srvctl exec prod-db-1") {
		t.Errorf("应包含裸命令调用，实际:\n%s", got)
	}
	if strings.Contains(got, "未加入 PATH") {
		t.Errorf("在 PATH 上时不该出现 PATH 说明:\n%s", got)
	}
}

// srvctl 不在 PATH 上时必须给出完整路径，并且明确告诉 AI 不要简写。
// 否则 AI 会把长路径"优化"成 srvctl，一试就是 command not found，
// 然后开始自己猜怎么连服务器 —— 那正是这个工具要避免的。
func TestForUsesFullPathWithWarningWhenNotOnPath(t *testing.T) {
	const full = `D:\srvctl\dist\srvctl.exe`
	got := For(linuxServer(), nil, "", full)

	if !strings.Contains(got, full+" exec prod-db-1") {
		t.Errorf("应使用完整路径，实际:\n%s", got)
	}
	if !strings.Contains(got, "未加入 PATH") {
		t.Errorf("应说明为什么是长路径，实际:\n%s", got)
	}
	if !strings.Contains(got, "不要简写成 srvctl") {
		t.Errorf("应明确禁止简写，实际:\n%s", got)
	}
}

// 空/空白 cmd 按裸命令名兜底，不能生成 " exec x" 这种断掉的命令。
func TestForEmptyCommandFallsBack(t *testing.T) {
	for _, cmd := range []string{"", "   "} {
		got := For(linuxServer(), nil, "", cmd)
		if !strings.Contains(got, "srvctl exec prod-db-1") {
			t.Errorf("cmd=%q 应兜底为 srvctl，实际:\n%s", cmd, got)
		}
	}
}

// 备注要带进给 AI 的说明里 —— 这是用户写这台机器上下文的唯一去处。
func TestForIncludesNotes(t *testing.T) {
	got := For(linuxServer(), nil, "", "srvctl")

	if !strings.Contains(got, "备注：") {
		t.Errorf("应包含备注区块:\n%s", got)
	}
	if !strings.Contains(got, "火山云 2核4G") || !strings.Contains(got, "到期 2026-03") {
		t.Errorf("多行备注应完整保留:\n%s", got)
	}
	// 多行备注要缩进，不能挤成一行
	if !strings.Contains(got, "    到期 2026-03") {
		t.Errorf("备注第二行应缩进:\n%s", got)
	}
}

func TestForOmitsNotesSectionWhenEmpty(t *testing.T) {
	s := linuxServer()
	s.Notes = ""
	got := For(s, nil, "", "srvctl")
	if strings.Contains(got, "备注：") {
		t.Errorf("没备注时不该出现备注区块:\n%s", got)
	}
}

// 状态说明要带上（含灰色时的网络原因）。
func TestForIncludesStatusNote(t *testing.T) {
	r := reach.Result{Name: "prod-db-1", State: reach.StateWrongNet, Note: "当前 WiFi 是 Home，需要 Corp-Dev"}
	s := linuxServer()
	s.RequiredSSID = "Corp-Dev"

	got := For(s, &r, "Home", "srvctl")
	if !strings.Contains(got, "当前网络环境不满足") {
		t.Errorf("应包含灰色状态:\n%s", got)
	}
	if !strings.Contains(got, "需要 Corp-Dev") {
		t.Errorf("应包含网络原因:\n%s", got)
	}
}

// Windows 条目不该给出 srvctl exec，而是远程桌面命令。
func TestForWindowsUsesMstsc(t *testing.T) {
	s := model.Server{
		Name: "win-build", Host: "10.0.2.31",
		Platform: model.PlatformWindows, Username: "administrator",
		AuthMode: model.AuthPassword, CheckMode: model.CheckNone,
	}
	got := For(s, nil, "", "srvctl")

	if !strings.Contains(got, "mstsc /v:10.0.2.31") {
		t.Errorf("Windows 条目应给出 mstsc:\n%s", got)
	}
	if !strings.Contains(got, "仅登记，不检测连通性") {
		t.Errorf("应说明不检测:\n%s", got)
	}
}

func TestForAllUsesProvidedCommand(t *testing.T) {
	const full = `D:\srvctl\dist\srvctl.exe`
	servers := []model.Server{linuxServer()}

	got := ForAll(servers, nil, "", full)
	if !strings.Contains(got, full+" exec") {
		t.Errorf("索引也应使用完整路径:\n%s", got)
	}
	if !strings.Contains(got, "未加入 PATH") {
		t.Errorf("索引应提示 PATH 情况:\n%s", got)
	}

	plain := ForAll(servers, nil, "", "srvctl")
	if strings.Contains(plain, "未加入 PATH") {
		t.Errorf("在 PATH 上时索引不该有 PATH 提示:\n%s", plain)
	}
}

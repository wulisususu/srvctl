package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"srvctl/internal/config"
	"srvctl/internal/model"
	"srvctl/internal/netid"
	"srvctl/internal/reach"
	"srvctl/internal/sessionkey"
	"srvctl/internal/snippet"
	"srvctl/internal/sshx"
)

// Run 是 CLI 入口，返回进程退出码。
func Run(args []string) int {
	g, rest := parseGlobals(args)

	if len(rest) == 0 {
		// 无参数 = 双击 exe = 启动界面
		return RunGUI(g, true)
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "gui":
		return cmdGUI(g, cmdArgs)
	case "init":
		return cmdInit(g)
	case "login":
		return cmdLogin(g)
	case "logout":
		return cmdLogout(g)
	case "passwd", "password":
		return cmdPasswd(g)
	case "add":
		return cmdAdd(g, cmdArgs)
	case "list", "ls":
		return cmdList(g, cmdArgs)
	case "get", "show":
		return cmdGet(g, cmdArgs)
	case "rm", "delete":
		return cmdRemove(g, cmdArgs)
	case "test":
		return cmdTest(g, cmdArgs)
	case "net", "ssid":
		return cmdNet()
	case "config", "cfg":
		return cmdConfig(g, cmdArgs)
	case "snippet", "copy":
		return cmdSnippet(g, cmdArgs)
	case "exec", "run":
		return cmdExec(g, cmdArgs)
	case "shell", "ssh":
		return cmdShell(g, cmdArgs)
	case "version", "--version", "-V":
		fmt.Println(appName + " " + Version())
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "%s: 未知命令 %q\n\n", appName, cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `%s %s —— 轻量服务器管理 + AI 连接入口

用法：
    %s [全局开关] <命令> [参数]

全局开关：
    --portable          数据放到可执行文件同目录（U 盘便携模式）
    --vault <path>      指定 vault 文件（也可以设 SRVCTL_VAULT 环境变量）

界面：
    gui                 启动可视化界面（无参数时默认执行）

vault：
    init                创建新 vault（设置主密码）
    login               输入并记住主密码（AI 调用 CLI 的前置条件）
    logout              清除记住的主密码
    passwd              修改主密码

服务器：
    add <名称> [开关]    新增/修改一条记录
    list                列出全部记录
    get <名称>          查看单条详情
    rm <名称>           删除一条记录

网络与连通性：
    net                 显示当前 WiFi 名称
    test [名称...]      做 TCP 连通性检测（不指定名称则检测全部）

配置：
    config                      显示 vault / session.key / 日志等路径
    config set-vault <路径>      把 vault 指到别处（例如云盘目录，多机共用）
    config clear-vault          恢复默认位置

给 AI 用：
    snippet [名称]      输出"复制给 AI"的连接说明
    exec <名称> <命令>   在服务器上执行命令
    shell <名称>        打开交互式 SSH 会话

add 的开关：
    --host <地址>        必填
    --port <端口>        留空按平台默认（Linux=22，Windows=3389）
    --user <用户名>
    --password <密码>    与 --key-file 二选一
    --password-stdin    从标准输入读密码（推荐，不会留在命令行/进程列表/历史里）
    --key-file <路径>    从文件读私钥
    --passphrase <口令>  私钥口令
    --platform <平台>    linux（默认）| windows
    --category <分类>
    --tags <a,b,c>
    --ssid <名称>        需要连接该 WiFi 才能访问
    --check <策略>       auto（默认，做探测）| none（只记录）
    --notes <备注>

示例：
    %s add prod-db-1 --host 10.0.1.23 --user root --password 'xxx' --category 内网/数据库 --ssid Corp-Dev
    %s list --test
    %s snippet prod-db-1
    %s exec prod-db-1 "uptime"
`, appName, Version(), appName, appName, appName, appName, appName)
}

// ---------- 生命周期 ----------

func cmdGUI(g globals, args []string) int {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return RunGUI(g, !*noOpen)
}

func cmdInit(g globals) int {
	st := openStore(g)
	if st.Exists() {
		fmt.Fprintf(os.Stderr, "vault 已存在: %s\n", st.Path())
		return 1
	}
	pw := passwordFromEnv()
	if pw == "" {
		var err error
		pw, err = promptPassword("设置主密码: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
		again, err := promptPassword("再输一次: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
		if pw != again {
			fmt.Fprintln(os.Stderr, "两次输入不一致")
			return 1
		}
	}
	if err := st.Init(pw); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}

	fmt.Printf("已创建 vault: %s\n", st.Path())
	fmt.Printf("提示：想让 AI 通过 CLI 调用，请再运行一次 `%s login` 记住主密码。\n", appName)
	return 0
}

func cmdLogin(g globals) int {
	st := openStore(g)
	if !st.Exists() {
		fmt.Fprintf(os.Stderr, "vault 不存在: %s\n请先运行 `%s init`\n", st.Path(), appName)
		return 1
	}
	pw := passwordFromEnv()
	if pw == "" {
		var err error
		pw, err = promptPassword("主密码: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
	}
	if err := st.Unlock(pw); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	path := config.SessionKeyPath(g.portable)
	if err := sessionkey.Save(path, pw); err != nil {
		fmt.Fprintf(os.Stderr, "%s: 保存失败: %v\n", appName, err)
		return 1
	}
	n, _ := st.Count()
	fmt.Printf("已解锁并记住主密码: %s\n", path)
	fmt.Printf("当前 vault 有 %d 条记录。现在 AI 可以直接调用 `%s exec` 了。\n", n, appName)
	return 0
}

func cmdLogout(g globals) int {
	path := config.SessionKeyPath(g.portable)
	if err := sessionkey.Clear(path); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	fmt.Printf("已清除记住的主密码: %s\n", path)
	return 0
}

func cmdPasswd(g globals) int {
	st := openStore(g)
	if !st.Exists() {
		fmt.Fprintf(os.Stderr, "vault 不存在: %s\n", st.Path())
		return 1
	}
	old := passwordFromEnv()
	if old == "" {
		var err error
		old, err = promptPassword("当前主密码: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
	}
	nw, err := promptPassword("新主密码: ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	again, err := promptPassword("再输一次: ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	if nw != again {
		fmt.Fprintln(os.Stderr, "两次输入不一致")
		return 1
	}
	if err := st.ChangePassword(old, nw); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	// 同步更新记住的密码，否则 CLI 下次就失效了
	if sessionkey.Exists(config.SessionKeyPath(g.portable)) {
		_ = sessionkey.Save(config.SessionKeyPath(g.portable), nw)
		fmt.Println("已同步更新记住的主密码。")
	}
	fmt.Println("主密码已修改。")
	return 0
}

// ---------- 记录管理 ----------

func cmdAdd(g globals, args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	host := fs.String("host", "", "主机地址")
	port := fs.Int("port", 0, "端口，留空按平台默认（Linux=22，Windows=3389）")
	user := fs.String("user", "", "用户名")
	password := fs.String("password", "", "密码")
	passwordStdin := fs.Bool("password-stdin", false, "从标准输入读取密码（避免密码出现在命令行和进程列表里）")
	keyFile := fs.String("key-file", "", "私钥文件路径")
	passphrase := fs.String("passphrase", "", "私钥口令")
	platform := fs.String("platform", model.PlatformLinux, "linux | windows")
	category := fs.String("category", "", "分类")
	tags := fs.String("tags", "", "标签，逗号分隔")
	ssid := fs.String("ssid", "", "需要连接的 WiFi 名称")
	check := fs.String("check", "", "auto | none（留空自动判断）")
	notes := fs.String("notes", "", "备注")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "用法: srvctl add <名称> --host <地址> [...]")
		return 2
	}
	name := fs.Arg(0)

	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}

	srv := model.Server{
		Name:         name,
		Host:         *host,
		Port:         *port,
		Platform:     *platform,
		Username:     *user,
		Category:     *category,
		RequiredSSID: *ssid,
		CheckMode:    *check,
		Notes:        *notes,
	}
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				srv.Tags = append(srv.Tags, t)
			}
		}
	}

	switch {
	case *keyFile != "":
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: 读取私钥失败: %v\n", appName, err)
			return 1
		}
		srv.AuthMode = model.AuthKey
		srv.PrivateKey = string(raw)
		srv.Passphrase = *passphrase
	case *passwordStdin:
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: 读取标准输入失败: %v\n", appName, err)
			return 1
		}
		// 只去掉一个结尾换行 —— 密码内部如果本来就有换行必须保留
		pw := strings.TrimSuffix(string(raw), "\n")
		pw = strings.TrimSuffix(pw, "\r")
		// 去掉开头的 BOM。
		//
		// 用记事本把密码存成 "UTF-8" 会带 BOM，用 PowerShell 的
		// Process.StandardInput 转发也会带上 —— 两者都会让认证失败，而且
		// 现象是"密码明明正确却连不上"，极难排查。以 U+FEFF 开头的密码在
		// 现实中不存在，所以这是纯粹的编码残留，直接去掉。
		pw = strings.TrimPrefix(pw, "\ufeff")
		if pw == "" {
			fmt.Fprintf(os.Stderr, "%s: 标准输入为空\n", appName)
			return 1
		}
		srv.AuthMode = model.AuthPassword
		srv.Password = pw
	case *password != "":
		srv.AuthMode = model.AuthPassword
		srv.Password = *password
	default:
		pw, err := promptPassword("密码: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
		srv.AuthMode = model.AuthPassword
		srv.Password = pw
	}

	srv.Normalize()
	if err := st.Upsert(srv); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	fmt.Printf("已保存: %s（%s，%s）\n", srv.Name, srv.Address(), srv.Platform)
	return 0
}

func cmdList(g globals, args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	doTest := fs.Bool("test", false, "同时做连通性检测")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return 2
	}

	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	servers, err := st.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}

	results := map[string]reach.Result{}
	if *doTest {
		ssid := netid.SSID()
		if !*asJSON {
			fmt.Fprintf(os.Stderr, "当前网络: %s\n", displayOrUnknown(ssid))
		}
		results = reach.Index(reach.CheckAll(context.Background(), servers, ssid))
	}

	if *asJSON {
		// 刻意用白名单而不是直接序列化 model.Server —— 列表接口绝不能把
		// 密码/私钥带出去。代价是新增非敏感字段时要记得加进来
		// （notes 就漏过一次，导致 list --json 看不到备注）。
		type listItem struct {
			Name         string   `json:"name"`
			Host         string   `json:"host"`
			Port         int      `json:"port"`
			Platform     string   `json:"platform"`
			Username     string   `json:"username"`
			AuthMode     string   `json:"auth_mode"`
			Category     string   `json:"category"`
			Tags         []string `json:"tags"`
			RequiredSSID string   `json:"required_ssid"`
			CheckMode    string   `json:"check_mode"`
			Notes        string   `json:"notes,omitempty"`
			State        string   `json:"state,omitempty"`
			LatencyMs    int64    `json:"latency_ms,omitempty"`
			Err          string   `json:"err,omitempty"`
			Note         string   `json:"note,omitempty"`
		}
		out := make([]listItem, 0, len(servers))
		for _, s := range servers {
			it := listItem{
				Name: s.Name, Host: s.Host, Port: s.EffectivePort(),
				Platform: s.Platform, Username: s.Username, AuthMode: s.AuthMode,
				Category: s.Category, Tags: s.Tags,
				RequiredSSID: s.RequiredSSID, CheckMode: s.CheckMode,
				Notes: s.Notes,
			}
			if r, ok := results[s.Name]; ok {
				it.State = string(r.State)
				it.LatencyMs = r.LatencyMs
				it.Err = r.Err
				it.Note = r.Note
			}
			out = append(out, it)
		}
		printJSON(out)
		return 0
	}

	if len(servers) == 0 {
		fmt.Println("（没有记录）用 `srvctl add <名称> --host <地址> ...` 添加")
		return 0
	}

	fmt.Printf("%s %s %s %s %s\n",
		padRight("名称", 22), padRight("地址", 22), padRight("平台", 9),
		padRight("分类", 16), "状态")
	for _, s := range servers {
		state := "—"
		if r, ok := results[s.Name]; ok {
			state = snippet.StateLabel(r)
		} else if !s.Checkable() {
			state = "不检测"
		}
		fmt.Printf("%s %s %s %s %s\n",
			padRight(s.Name, 22),
			padRight(s.Address(), 22),
			padRight(s.Platform, 9),
			padRight(s.Category, 16),
			state)
	}
	fmt.Printf("\n共 %d 条\n", len(servers))
	return 0
}

func cmdGet(g globals, args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	reveal := fs.Bool("reveal", false, "显示密码/私钥")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "用法: srvctl get <名称> [--reveal]")
		return 2
	}
	name := fs.Arg(0)

	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	srv, err := st.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}

	if *asJSON {
		printJSON(srv)
		return 0
	}

	fmt.Printf("名称:       %s\n", srv.Name)
	fmt.Printf("地址:       %s\n", srv.Address())
	fmt.Printf("平台:       %s\n", srv.Platform)
	fmt.Printf("用户名:     %s\n", srv.Username)
	fmt.Printf("认证方式:   %s\n", srv.AuthMode)
	fmt.Printf("分类:       %s\n", srv.Category)
	if len(srv.Tags) > 0 {
		fmt.Printf("标签:       %s\n", strings.Join(srv.Tags, ", "))
	}
	if srv.RequiredSSID != "" {
		fmt.Printf("需要 WiFi:  %s（当前 %s）\n", srv.RequiredSSID, displayOrUnknown(netid.SSID()))
	}
	fmt.Printf("检测策略:   %s\n", srv.CheckMode)
	if srv.Notes != "" {
		fmt.Printf("备注:       %s\n", srv.Notes)
	}
	if *reveal {
		if srv.Password != "" {
			fmt.Printf("密码:       %s\n", srv.Password)
		}
		if srv.PrivateKey != "" {
			fmt.Printf("私钥:\n%s\n", srv.PrivateKey)
		}
	} else {
		if srv.Password != "" {
			fmt.Printf("密码:       ******（--reveal 显示）\n")
		}
		if srv.PrivateKey != "" {
			fmt.Printf("私钥:       ******（--reveal 显示）\n")
		}
	}
	return 0
}

func cmdRemove(g globals, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: srvctl rm <名称>")
		return 2
	}
	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	if err := st.Delete(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	fmt.Printf("已删除: %s\n", args[0])
	return 0
}

// ---------- 网络与探测 ----------

func cmdNet() int {
	ssid := netid.SSID()
	if ssid == "" {
		fmt.Println("未知（有线连接、无 WLAN 网卡，或读取失败）")
		return 0
	}
	fmt.Println(ssid)
	return 0
}

func cmdTest(g globals, args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return 2
	}

	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	servers, err := st.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}

	if fs.NArg() > 0 {
		want := map[string]bool{}
		for _, n := range fs.Args() {
			want[n] = true
		}
		filtered := servers[:0]
		for _, s := range servers {
			if want[s.Name] {
				filtered = append(filtered, s)
			}
		}
		servers = filtered
	}

	ssid := netid.SSID()
	results := reach.CheckAll(context.Background(), servers, ssid)
	indexed := reach.Index(results)

	if *asJSON {
		printJSON(map[string]any{"ssid": ssid, "results": indexed})
		return 0
	}

	fmt.Printf("当前网络: %s\n\n", displayOrUnknown(ssid))
	fmt.Printf("%s %s %s\n", padRight("名称", 22), padRight("地址", 22), "状态")
	bad := 0
	for _, s := range servers {
		r := indexed[s.Name]
		if r.State == reach.StateDown {
			bad++
		}
		fmt.Printf("%s %s %s\n",
			padRight(s.Name, 22), padRight(s.Address(), 22), snippet.StateLabel(r))
	}
	fmt.Printf("\n共 %d 条，其中 %d 条不可连接\n", len(servers), bad)
	return 0
}

// ---------- 配置 ----------

func cmdConfig(g globals, args []string) int {
	if len(args) == 0 {
		return showConfig(g)
	}

	switch args[0] {
	case "set-vault":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: srvctl config set-vault <vault 文件或目录>")
			return 2
		}
		p := args[1]
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		// 给目录就自动补上文件名，省得记
		if filepath.Ext(p) == "" {
			p = filepath.Join(p, "vault.enc")
		}

		cfg := config.ReadConfig()
		cfg.VaultPath = p
		if err := config.WriteConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 写入配置失败: %v\n", appName, err)
			return 1
		}
		fmt.Printf("已设置 vault 位置: %s\n", p)

		if _, err := os.Stat(p); err != nil {
			fmt.Println()
			fmt.Println("注意：这个文件目前不存在。")
			fmt.Println("  · 如果是云盘同步目录 —— 等同步完成")
			fmt.Println("  · 如果是新位置 —— 用 `" + appName + " init` 在那里创建")
			fmt.Println()
			fmt.Println("⚠️  同步还没下来之前，界面会显示「创建 vault」。")
			fmt.Println("    那时千万别点创建 —— 会覆盖掉已有数据。")
		}

		fmt.Printf("\n本机 session.key 仍在: %s\n", config.SessionKeyPath(g.portable))
		fmt.Println("（它存的是主密码明文，不要放进云盘目录 —— 每台机器各自 login 一次）")
		return 0

	case "clear-vault":
		cfg := config.ReadConfig()
		cfg.VaultPath = ""
		if err := config.WriteConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
			return 1
		}
		fmt.Printf("已恢复默认位置: %s\n", config.VaultPath(g.portable))
		return 0

	default:
		fmt.Fprintf(os.Stderr, "%s: config 的子命令只有 set-vault / clear-vault\n", appName)
		return 2
	}
}

func showConfig(g globals) int {
	vaultPath := config.VaultPath(g.portable)
	keyPath := config.SessionKeyPath(g.portable)

	fmt.Printf("vault:         %s\n", vaultPath)
	fmt.Printf("  来源:        %s\n", config.VaultSource(g.portable))
	if _, err := os.Stat(vaultPath); err != nil {
		fmt.Printf("  状态:        文件不存在\n")
	} else {
		fmt.Printf("  状态:        存在\n")
	}
	fmt.Printf("session.key:   %s\n", keyPath)
	fmt.Printf("日志:          %s\n", config.LogPath(g.portable))
	fmt.Printf("配置文件:      %s\n", config.ConfigPath())
	fmt.Printf("CLI 调用名:    %s\n", config.CLICommand())

	// session.key 存的是主密码明文。默认状态下它和 vault 同在本地数据目录，
	// 这是正常的；只有当 vault 被显式指到别处（多半是云盘）而 session.key
	// 还跟着一起在那边时，才是真的把主密码明文同步出去了。
	if config.VaultIsConfigured(g.portable) && filepath.Dir(keyPath) == filepath.Dir(vaultPath) {
		fmt.Println()
		fmt.Println("⚠️  session.key 和 vault 在同一个目录，而这个 vault 位置是你指定的。")
		fmt.Println("    如果这个目录在云盘里，等于把主密码明文同步了出去。")
		fmt.Println("    建议：让 session.key 留在本地数据目录，每台机器各自 login 一次。")
	}
	return 0
}

// ---------- 给 AI 用 ----------

func cmdSnippet(g globals, args []string) int {
	st := openStore(g)
	// snippet 允许不指定名称（输出全部索引），所以不能要求登录
	if err := unlockStore(st, g, false); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	servers, err := st.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	ssid := netid.SSID()

	if len(args) == 0 {
		// 索引模式：给结果里带上状态，所以顺手做一次探测
		results := reach.Index(reach.CheckAll(context.Background(), servers, ssid))
		fmt.Print(snippet.ForAll(servers, results, ssid, config.CLICommand()))
		return 0
	}

	name := args[0]
	for _, s := range servers {
		if s.Name != name {
			continue
		}
		var ptr *reach.Result
		if s.Checkable() {
			r := reach.Check(context.Background(), s, ssid)
			ptr = &r
		}
		fmt.Print(snippet.For(s, ptr, ssid, config.CLICommand()))
		return 0
	}
	fmt.Fprintf(os.Stderr, "%s: 未找到服务器: %s\n", appName, name)
	return 1
}

func cmdExec(g globals, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `用法: srvctl exec <名称> "<命令>"`)
		return 2
	}
	name := args[0]
	command := strings.Join(args[1:], " ")

	st := openStore(g)
	// AI 调用场景：不交互，只接受已记住的密码
	if err := unlockStore(st, g, false); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	srv, err := st.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	if srv.Platform == model.PlatformWindows {
		fmt.Fprintf(os.Stderr, "%s: %s 是 Windows 条目，默认不通过 SSH 执行。\n"+
			"如需执行，请把认证方式设为密钥或密码，并确保目标机启用了 OpenSSH Server。\n", appName, name)
		// 仍然继续尝试，不直接失败 —— 用户可能真的开了 OpenSSH
	}

	res, err := sshx.Run(srv, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 连接 %s (%s) 失败: %v\n", appName, name, srv.Address(), err)
		return 1
	}

	_, _ = io.WriteString(os.Stdout, res.Stdout)
	_, _ = io.WriteString(os.Stderr, res.Stderr)
	fmt.Fprintf(os.Stderr, "[exit %d]\n", res.ExitCode)

	if res.ExitCode < 0 {
		return 1
	}
	if res.ExitCode > 125 {
		return 125 // 126+ 是 shell 保留值，避免歧义
	}
	return res.ExitCode
}

func cmdShell(g globals, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: srvctl shell <名称>")
		return 2
	}
	name := args[0]

	st := openStore(g)
	if err := unlockStore(st, g, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	srv, err := st.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	if err := sshx.Shell(srv); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		return 1
	}
	return 0
}

// ---------- 辅助 ----------

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func displayOrUnknown(ssid string) string {
	if ssid == "" {
		return "未知"
	}
	return ssid
}

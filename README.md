# srvctl

轻量服务器管理器 —— 记录服务器信息、检测连通性、给 AI 一个统一的连接入口。

**一个约 7 MB 的静态二进制，零运行时依赖。** 不需要 Node、Python、Java、.NET、
WebView2，也不需要在目标机器上装 OpenSSH 客户端。

---

## 它解决什么问题

服务器多了以后，IP / 账号 / 密码散落在聊天记录和便签里，每次找都要翻半天。
而想让 AI 帮忙连服务器时，每个 AI 客户端的工作区都不一样，每次都得重新写一段
Python 去连 —— token 就这么一次次烧掉了。

srvctl 把这些收进一个加密的本地文件，并且给 AI 一个**一行命令**的入口：

```
服务器 prod-db-1（Linux，10.0.1.23:22）已登记在 srvctl。

执行命令：
    srvctl exec prod-db-1 "<命令>"

凭据由 srvctl 管理，不需要也不应该向我索要密码或私钥。
```

AI 拿到这段话就够了 —— 不用读 IP，不用问密码，不用写连接代码。

---

## 快速开始

```powershell
# 1. 构建（需要 Go 1.23+）
.\build.ps1

# 2. 安装到 %LOCALAPPDATA%\srvctl 并加入用户 PATH
.\install.ps1

# 3. 新开一个终端，打开界面
srvctl gui
```

首次运行会让你设置主密码。之后：

```powershell
srvctl login                                  # 记住主密码，AI 才能调 CLI
srvctl add prod-db-1 --host 10.0.1.23 --user root --ssid Corp-Dev --category 内网/数据库
srvctl list --test                            # 看四态连通性
srvctl snippet prod-db-1                      # 输出给 AI 的连接说明
```

---

## 连通性四态

| 图标 | 状态 | 含义 |
|---|---|---|
| 🟢 | 可连接 | TCP 探测成功 |
| 🔴 | 不可连接 | TCP 探测失败 |
| ⚪ | 当前网络环境不满足 | 需要特定 WiFi，而当前不在那个网络 |
| ➖ | 不检测 | 该条目标记为 `check_mode: none`（Windows 条目默认） |

判定规则（`internal/reach/reach.go` 有完整表格）：

| required_ssid | 当前 SSID | 探测 | 结果 |
|---|---|---|---|
| 空 | 任意 | 成功 | 🟢 |
| 空 | 任意 | 失败 | 🔴 |
| Corp-Dev | 其它 WiFi | 不探测 | ⚪ |
| Corp-Dev | 未知（有线/未连） | 成功 | 🟢 |
| **Corp-Dev** | **未知（有线/未连）** | **失败** | **⚪** |
| Corp-Dev | Corp-Dev | 成功 | 🟢 |
| Corp-Dev | Corp-Dev | 失败 | 🔴 |

第 5 行是关键：在家用有线时，要求公司 WiFi 的内网服务器连不上 —— 这时**判灰而不是判红**，
因为无法排除网络原因。但如果在公司用有线且能连通，仍然如实报绿。

**读不到 SSID 时绝不误判。** 有线连接、没有 WLAN 网卡、macOS 缺定位权限、
`netsh` 解析失败 —— 这些情况一律退化为"只按 TCP 判定"，不会把所有服务器染灰。

---

## 命令

```
srvctl                          启动界面（等同 gui）
srvctl gui [--no-open]          启动界面，不自动开浏览器

srvctl init                     创建 vault（设置主密码）
srvctl login                    输入并记住主密码 ← AI 用 CLI 的前置条件
srvctl logout                   清除记住的主密码
srvctl passwd                   修改主密码

srvctl add <名称> [开关]         新增/修改记录
srvctl list [--test] [--json]   列出记录
srvctl get <名称> [--reveal]     查看详情
srvctl rm <名称>                 删除记录

srvctl net                      显示当前 WiFi 名称
srvctl test [名称...] [--json]   连通性检测

srvctl snippet [名称]            输出"复制给 AI"的连接说明
srvctl exec <名称> <命令>        在服务器上执行命令
srvctl shell <名称>             交互式 SSH 会话
```

全局开关（可放任意位置）：

| 开关 | 作用 |
|---|---|
| `--portable` | 数据放在可执行文件同目录（U 盘便携模式） |
| `--vault <路径>` | 指定 vault 文件 |

`add` 的开关：

| 开关 | 说明 |
|---|---|
| `--host <地址>` | 必填 |
| `--port <端口>` | 默认 22 |
| `--user <用户名>` | |
| `--password <密码>` | 与 `--key-file` 二选一；都不给则交互输入 |
| `--key-file <路径>` | 从文件读私钥 |
| `--passphrase <口令>` | 私钥口令 |
| `--platform <平台>` | `linux`（默认）\| `windows` |
| `--category <分类>` | 例如 `内网/数据库` |
| `--tags <a,b,c>` | 逗号分隔 |
| `--ssid <名称>` | 需要连接该 WiFi 才能访问 |
| `--check <策略>` | `auto`（默认）\| `none`（只记录） |
| `--notes <备注>` | |

---

## 给 AI 用

### 方式一：复制粘贴（最简单）

界面上点「复制给 AI」，或者：

```powershell
srvctl snippet            # 全部服务器的索引
srvctl snippet prod-db-1  # 单台的详细说明
```

粘进任意 AI 客户端的对话框即可。**任何 AI 都能用，不需要配置。**

### 方式二：AI 直接调 CLI

前提是先跑过一次 `srvctl login`。之后 AI 只需要：

```powershell
srvctl exec prod-db-1 "uptime && df -h /"
```

输出：

```
 14:23:01 up 42 days,  3:11,  2 users,  load average: 0.31, 0.28, 0.25
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        98G   61G   33G  65% /
[exit 0]
```

命令输出走 stdout，`[exit N]` 走 stderr，退出码透传 —— 方便脚本和 AI 判断。

### 为什么这比让 AI 自己写 Python 好

1. **凭据不出工具**。AI 只传服务器名称，密码由 srvctl 解密后直接交给 SSH 库。
   工作区里不会出现明文密码文件。
2. **AI 环境不需要能连到目标**。TCP 连接由 srvctl 发起。
3. **跨 AI 客户端通用**。脚本/CLI 不依赖任何特定 AI 客户端的配置机制 ——
   换 Claude Code、Cursor、Cline、还是别的，只要能跑 shell 就行。
4. **token 成本恒定**。一段固定的说明文本，不随命令复杂度增长。

### 关于 Windows 服务器

Windows 条目目前**只记录、不检测**（`check_mode: none`），这是默认行为。
`snippet` 会给 `mstsc /v:<host>` 的远程桌面命令。

如果你的 Windows 机器启用了 OpenSSH Server（Win10 1809+ / Server 2019+ 内置，
可选功能），把 `--check` 改成 `auto` 就能像 Linux 一样探测和执行。

---

## 数据与安全

### 存储

| 内容 | 位置 |
|---|---|
| vault | `%APPDATA%\srvctl\vault.enc` |
| 记住的主密码 | `%APPDATA%\srvctl\session.key` |
| 日志 | `%APPDATA%\srvctl\srvctl.log`（仅 GUI 模式写） |

`--portable` 时三者都放在 exe 同目录。

### 加密

- 密钥派生：**PBKDF2-SHA256，600,000 次迭代**，32 字节输出
- 加密算法：**AES-256-GCM**（带认证，密文被改动会解密失败）
- 文件布局：`magic(8) | salt(32) | nonce(12) | ciphertext | tag(16)`
- 写入方式：先写 `.tmp` 再原子 `rename` —— 中途崩溃不会损坏 vault

整个 vault 是一个加密文件，所以**服务器信息、用户名、备注全部加密**，
不只是密码。实测确认文件里搜不到任何明文字段。

### 安全边界（请知悉）

| 项 | 现状 | 说明 |
|---|---|---|
| 会话密钥 | `session.key` 里存**主密码明文** | 为了 AI 能无交互调用 CLI。不想要就 `srvctl logout` |
| SSH 主机密钥 | **不校验**（`InsecureIgnoreHostKey`） | 有 MITM 风险。后续可加 TOFU fingerprint 记录 |
| 界面服务 | 只监听 `127.0.0.1`，带随机 token | 本机其他进程拿不到 token 就读不到凭据 |
| 多机同步 | 各自独立 vault | 见下 |

**多机共用**：见下面「部署到另一台电脑」一节。

---

## 部署到另一台电脑

需要带过去的东西只有两样：**程序** 和 **vault 文件**。

### 一、程序（约 16 MB，零运行时依赖）

```powershell
# 在主力机上构建
.\build.ps1
```

把 `dist\` 里的文件拷到另一台机器即可。**不需要装 Go、Node、.NET 或 OpenSSH** ——
Go 编译出的原生二进制，静态链接，`CGO_ENABLED=0`。

或者在那台机器上从源码构建：

```powershell
git clone git@github.com:wulisususu/srvctl.git
cd srvctl
.\build.ps1
.\install.ps1     # 装到 %LOCALAPPDATA%\srvctl 并加入 PATH
```

### 二、vault（服务器数据）

vault 是**一个加密文件**，拷过去就能用 —— 但更省事的是让它自动同步。

**方式 A：云盘共享（推荐，适合 2–3 台常驻机器）**

把 vault 放进 OneDrive / 坚果云等同步目录，然后每台机器指向它：

```powershell
# 两台机器上都执行，路径按各自的实际盘符写
srvctl config set-vault "D:\OneDrive\srvctl\vault.enc"

# 查看当前生效的位置和来源
srvctl config
```

`config set-vault` 会写进 `%APPDATA%\srvctl\config.json`，所以**双击启动的界面也能读到** ——
不必依赖命令行参数或环境变量。

每台机器再执行一次 `srvctl login` 输入同一个主密码即可（主密码不会跟着同步，见下）。

由于写入是「先写临时文件再原子 rename」，云盘冲突最坏结果是多出一个
`vault-冲突副本.enc`，**不会损坏数据**。程序每次读写前都会检查文件是否被
外部改过并自动重载，界面也是 4 秒一次探活、发现变化就自动刷新。

**方式 B：U 盘便携（适合随身带）**

把 `srvctl.exe`、`srvctl-gui.exe`、`vault.enc`、`session.key` 一起放进 U 盘，
用 `--portable` 启动，数据就在 exe 同目录：

```powershell
srvctl.exe --portable list
```

或者给 U 盘上的 `srvctl-gui.exe` 建个快捷方式，目标写：

```
D:\srvctl-gui.exe --portable
```

**方式 C：手动拷贝** —— 直接把 `vault.enc` 复制过去。数据变动不频繁的话完全够用。

### ⚠️ 一条硬规矩：`session.key` 不要放进云盘

| 文件 | 内容 | 能进云盘吗 |
|---|---|---|
| `vault.enc` | 服务器信息 + 凭据，**AES-256-GCM 加密** | ✅ 可以 |
| `session.key` | **主密码明文** | ❌ **绝对不要** |

`session.key` 是 `srvctl login` 写下的，为了让 AI 能无交互调用 CLI。它在你本机
`%APPDATA%\srvctl\` 下，**不要**把它复制到共享目录 —— 那等于把主密码明文交出去，
vault 的加密就白做了。

正确做法：vault 放云盘，`session.key` 留在各机器本地，每台 `srvctl login` 一次。

`config` 命令会在检测到这种危险布局时给出警告。

### AI 读不到密码吗

默认读不到。`srvctl exec` 由工具内部解密凭据交给 SSH 库，密码不会出现在
命令行参数、输出或日志里。唯一的例外是你主动运行 `srvctl get <名称> --reveal`。

---

## 从源码构建

```powershell
.\build.ps1                  # 只构建 Windows（3 个 exe，共约 16 MB）
.\build.ps1 -AllPlatforms    # 额外交叉编译 Linux / macOS
go test ./...                # 单元测试
go vet ./...                 # 静态检查
```

首次构建需要联网拉 3 个依赖（`golang.org/x/crypto`、`x/term`、`x/text`）。
国内网络慢的话：

```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'
```

---

## 项目结构

```
srvctl/
├── cmd/
│   ├── srvctl/           控制台版入口（给命令行和 AI）
│   └── srvctl-gui/       无窗口版入口（双击开界面）
├── internal/
│   ├── config/           数据目录与路径解析
│   ├── model/            Server 数据模型
│   ├── vault/            AES-256-GCM 加密存储
│   ├── sessionkey/       "记住主密码"文件
│   ├── netid/            当前 WiFi (SSID) 识别
│   ├── reach/            TCP 四态连通性探测
│   ├── sshx/             SSH 执行与交互 shell
│   ├── snippet/          "给 AI 的连接说明"生成
│   ├── web/              本地 HTTP 服务
│   │   └── assets/       内嵌界面（HTML/CSS/JS）
│   └── app/              CLI 分发与 GUI 启动
├── tools/ssidtest/       SSID 探测验证工具
├── build.ps1
└── install.ps1
```

约 2,100 行 Go + 630 行前端。全部依赖 3 个（都是 Go 官方维护的 `x/` 仓库），
`CGO_ENABLED=0`，因此能交叉编译出真正的静态单文件。

---

## 排错

**界面起不来 / 白屏**
看 `%APPDATA%\srvctl\srvctl.log`。无窗口版本没有控制台，日志是唯一线索。

**SmartScreen 拦截**
首次运行未签名 exe 会弹警告 —— 点「更多信息」→「仍要运行」。

**AI 说找不到 srvctl**
PATH 加了但没重开终端。PATH 改动只对新进程生效。

**AI 说 vault 已锁定**
忘了 `srvctl login`，或者 `session.key` 被删了（比如运行过 `srvctl logout`）。

**WiFi 名读不到**
先跑 `srvctl-ssidtest.exe -raw` 看 `netsh` 的原始输出。
读不到不影响使用，只是不显示灰色状态。

**杀毒软件拦截**
未签名的、会主动外连的 Go 二进制可能触发启发式告警。
**不要用 UPX 加壳**，那会显著提高误报率。

# Trojan-Go Desktop 客户端开发文档

> 版本：v1（设计基线）
> 目标读者：负责 `desktop/` 目录的后端、前端与发布工程师
> 技术基线：Go 1.26 + Wails `v3.0.0-beta.34` + 原生 HTML/CSS/ES Modules

---

## 1. 项目概述

### 1.1 目标

为 `trojan-go` 提供一个跨平台桌面客户端，把命令行客户端包装成可日常使用的图形界面程序：

- 管理多份客户端配置（新增、列表、修改、删除），支持一键复制/导入 AES-256-GCM 密文配置信息。
- 首页通过下拉列表选择配置，选中即激活（启动 `trojan-go` 子进程）。
- 首页下方实时显示 `trojan-go` 日志。
- 设置页：开机自动激活、日志上限、主题、更新策略等。
- 显示客户端版本号与 `trojan-go` 版本。
- 自动升级：检测 GitHub 最新 Release，下载、校验、安装并重启。
- 发布安装包时，把**当前平台对应的 `trojan-go` 二进制**一起放到安装目录，客户端零配置即可找到它。

### 1.2 非目标

- 不在桌面端内嵌/重写 trojan-go 代理逻辑，始终通过子进程调用官方二进制。
- 不实现服务端管理、流量统计、订阅导入（后续版本可扩展）。
- 不支持浏览器远程访问（Wails 桌面模式即可）。
- v1 不实现开机自启注册；关闭窗口默认隐藏到系统托盘，托盘菜单可显示主界面或退出。

### 1.3 设计原则

1. **零前端构建**：不使用 npm、Vite、Webpack、框架；浏览器原生 ES Modules 直接运行。
2. **后端单一事实来源**：配置、设置、运行状态都由 Go 侧持有，前端只渲染与调用。
3. **进程隔离**：`trojan-go` 作为独立子进程运行，崩溃不影响桌面 UI，桌面退出时必须回收子进程。
4. **可验证的更新**：自动升级必须校验 SHA-256（后续可加签名），失败时保留旧版本。
5. **最小权限**：Windows 安装到用户目录，避免更新时请求管理员权限。

---

## 2. 技术栈与版本锁定

| 组件 | 版本/选型 | 说明 |
| --- | --- | --- |
| Go | 1.26.4（与根模块一致） | 桌面端为独立 Go module |
| Wails | `github.com/wailsapp/wails/v3 v3.0.0-beta.34` | 固定版本，不使用 `latest` |
| Wails CLI | `wails3@v3.0.0-beta.34` | 构建、打包、生成运行时 |
| 前端 | HTML5 + CSS3 + JavaScript（ES2022） | 原生 ES Modules，零框架、零打包 |
| 前端运行时 | `/wails/runtime.js`（Wails 内置） | 通过 `import { Call, Events } from "/wails/runtime.js"` 调用 Go |
| 配置存储 | JSON 文件 | 原子写入，无需数据库 |
| 子进程 | `os/exec` + `trojan-go -config <file>` | 标准输出/错误接入日志管道 |
| 自动更新 | Wails `app.Updater` + 自定义 GitHub Provider | 内置下载、校验、原子替换、重启 |
| Windows 安装包 | NSIS（Wails 内置打包流程） | 安装目录包含 `trojan-go.exe` |
| macOS 安装包 | `.app`（Wails 打包） | `Contents/MacOS/trojan-go` |
| Linux 安装包 | `tar.gz`（当前实现，可扩展 AppImage/deb/rpm） | 二进制放入应用目录 |

> 注意：Wails v3 仍处于 beta，升级 SDK 时必须同步验证：
> `application.Options`、`Service` 绑定、`app.Updater`、`build/windows/nsis/project.nsi` 模板。

---

## 3. 总体架构

```
┌────────────────────────────────────────────────────────────┐
│ WebView (frontend/)                                        │
│  index.html + css/app.css + js/*.js (native ES modules)    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ 首页     │ │ 配置列表 │ │ 配置编辑 │ │ 设置/关于     │  │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └──────┬────────┘  │
│       └────────────┴────────────┴──────────────┘           │
│                 api.js / events.js / store.js              │
└───────────────────────────┬────────────────────────────────┘
                            │ Wails Bridge
                            │ Call.ByName("main.XxxService.Method")
                            │ Events.On("runtime:log" ...)
┌───────────────────────────┴────────────────────────────────┐
│ Go 主进程 (desktop/)                                       │
│  ProfileService  SettingsService  RuntimeService           │
│  UpdateService   SystemService                             │
│        │              │                 │                  │
│        ▼              ▼                 ▼                  │
│  config.Store   settings.Store   trojan.Manager            │
│        │                              │   ├─ LogHub        │
│        │                              │   └─ exec.Command  │
│        ▼                              ▼                  │
│  %APPDATA%/trojan-go-desktop/    trojan-go(.exe) 子进程    │
│    profiles.json / settings.json / runtime/active.json     │
└────────────────────────────────────────────────────────────┘
                            │
                            ▼
                 GitHub Releases API（自动更新）
```

### 3.1 进程与生命周期

1. `main()` 首先调用 `application.New(...)`（必须尽早，更新助手模式依赖它）。
2. 初始化存储、日志中心、进程管理器，注册服务。
3. 初始化 `app.Updater`（自定义 GitHub Provider）。
4. 创建主窗口，`app.Run()`。
5. 服务 `ServiceStartup` 中加载配置；若开启“启动时激活上次配置”，延迟启动子进程。
6. 应用退出（`ServiceShutdown`）时停止 `trojan-go` 子进程，等待退出或超时强杀。

---

## 4. 目录结构

```text
desktop/
├── go.mod                          # module github.com/litai686/trojan-go/desktop
├── go.sum
├── main.go                         # 应用入口、窗口、服务注册、Updater 初始化
├── services.go                     # 服务结构体定义（Profile/Runtime/Settings/Update/System）
├── version.go                      # 版本注入变量
├── internal/
│   ├── store/
│   │   ├── models.go               # Profile / Settings / RuntimeState / LogEntry
│   │   ├── store.go                # 原子读写、默认值、迁移
│   │   └── store_test.go
│   ├── trojan/
│   │   ├── config.go               # ProfileConfig -> trojan-go JSON、校验
│   │   ├── manager.go              # 子进程状态机
│   │   ├── binary.go               # 二进制定位
│   │   ├── logs.go                 # LogHub + 环形缓冲 + 批量事件
│   │   ├── proc_windows.go         # Windows 进程终止/隐藏窗口
│   │   ├── proc_unix.go            # Unix 进程组终止
│   │   └── *_test.go
│   ├── update/
│   │   ├── provider.go             # 自定义 GitHub Release Provider
│   │   ├── semver.go               # tag 解析与比较
│   │   └── provider_test.go
│   └── paths/
│       └── paths.go                # 数据目录、安装目录、可执行文件目录
├── frontend/
│   ├── index.html
│   ├── assets.go                   # //go:embed index.html css js
│   ├── css/app.css
│   └── js/
│       ├── main.js                 # 启动、路由、全局事件
│       ├── api.js                  # Wails 调用封装 + 浏览器 Mock
│       ├── store.js                # 轻量状态容器
│       ├── router.js               # hash 路由
│       ├── ui.js                   # DOM 工具、Toast、确认框
│       └── views/
│           ├── home.js             # 下拉选择、状态卡、日志
│           ├── profiles.js         # 配置列表
│           ├── profile-form.js     # 新增/修改
│           ├── settings.js         # 设置/关于/升级
│           └── not-found.js
├── build/                          # Wails 打包资源（图标、NSIS 等）
│   ├── config.yml
│   ├── appicon.png
│   ├── windows/
│   │   ├── icon.ico
│   │   ├── info.json
│   │   ├── wails.exe.manifest
│   │   └── nsis/project.nsi        # 需要加入 trojan-go.exe
│   ├── darwin/
│   └── linux/
├── Taskfile.yml                    # Wails 构建/打包任务（由模板生成后改造）
├── scripts/
│   └── stage-trojan.go             # 把根 build/<os>-<arch>/ 复制到打包暂存目录
└── README.md
```

> 根仓库的 `.gitignore` 需要把 `build/` 改为 `/build/`，否则 `desktop/build/` 会被误忽略；
> 同时新增 `desktop/bin/` 忽略编译产物。

---

## 5. 数据与存储设计

### 5.1 数据目录

| 平台 | 路径 |
| --- | --- |
| Windows | `%APPDATA%\trojan-go-desktop\` |
| macOS | `~/Library/Application Support/trojan-go-desktop/` |
| Linux | `$XDG_CONFIG_HOME/trojan-go-desktop/` 或 `~/.config/trojan-go-desktop/` |

目录内容：

```text
trojan-go-desktop/
├── profiles.json          # 配置列表
├── settings.json          # 应用设置
├── runtime/active.json    # 当前激活配置生成的 trojan-go 配置
├── logs/desktop.log       # 桌面端自身日志（可选，滚动）
└── cache/updates/         # 更新下载临时文件（由 Wails updater 管理）
```

文件权限：目录 `0700`，文件 `0600`（包含密码）。

### 5.2 Profile

```go
type Profile struct {
    ID        string        `json:"id"`        // crypto/rand 生成的 32 位 hex
    Name      string        `json:"name"`      // 唯一，1..64 字符
    Config    ProfileConfig `json:"config"`
    CreatedAt time.Time     `json:"createdAt"`
    UpdatedAt time.Time     `json:"updatedAt"`
}

type ProfileConfig struct {
    ServerAddr string `json:"serverAddr"` // 域名或 IP，不含协议
    ServerPort int    `json:"serverPort"` // 1..65535
    Password   string `json:"password"`
    SNI        string `json:"sni"`        // 为空时使用 ServerAddr
    LocalAddr  string `json:"localAddr"`  // 默认 127.0.0.1
    LocalPort  int    `json:"localPort"`  // 默认 1080
    Mux        bool   `json:"mux"`        // 默认 false
    Router     bool   `json:"router"`     // 需要 geoip.dat/geosite.dat，默认 false
    VerifyTLS  bool   `json:"verifyTls"`  // 默认 false
    LogLevel   int    `json:"logLevel"`   // 0..5，默认 1（INFO）
}
```

### 5.3 Settings

```go
type Settings struct {
    AutoStart        bool   `json:"autoStart"`        // 启动桌面端时激活上次配置
    AutoCheckUpdate  bool   `json:"autoCheckUpdate"`  // 启动后自动检查更新
    MaxLogLines      int    `json:"maxLogLines"`      // 100..20000，默认 2000
    Theme            string `json:"theme"`            // system|light|dark
    BinaryPath       string `json:"binaryPath"`       // 手动指定 trojan-go，空=自动查找
    LastProfileID    string `json:"lastProfileId"`
    UpdateChannel    string `json:"updateChannel"`    // stable|prerelease
    UpdateRepository string `json:"updateRepository"` // owner/repo，空=内置默认
}
```

### 5.4 RuntimeState

```go
type RuntimeState struct {
    Status      string `json:"status"`      // stopped|starting|running|stopping|error
    ProfileID   string `json:"profileId"`
    ProfileName string `json:"profileName"`
    Pid         int    `json:"pid"`
    StartedAt   int64  `json:"startedAt"`   // Unix 毫秒
    LocalAddr   string `json:"localAddr"`   // 实际监听地址，如 127.0.0.1:1080
    ServerAddr  string `json:"serverAddr"`
    BinaryPath  string `json:"binaryPath"`
    LastError   string `json:"lastError"`
}
```

### 5.5 LogEntry

```go
type LogEntry struct {
    Seq    int64  `json:"seq"`
    Time   int64  `json:"time"`   // Unix 毫秒
    Stream string `json:"stream"` // stdout|stderr|desktop
    Level  string `json:"level"`  // TRACE|DEBUG|INFO|WARN|ERROR|FATAL|SYSTEM
    Text   string `json:"text"`
}
```

### 5.6 原子写入与容错

- 保存时先写 `xxx.json.tmp`，`Sync()` 后 `os.Rename` 覆盖。
- 加载失败时将坏文件重命名为 `xxx.json.bad.<时间戳>`，使用默认值并在日志中告警。
- 所有 Store 方法加 `sync.RWMutex`，禁止并发写。
- 首次启动生成空 `profiles.json` 和默认 `settings.json`。
- 不对密码做“假加密”：v1 明确为本地明文（权限 0600），文档与 UI 提示风险；
  v2 可接入 Windows DPAPI / macOS Keychain。

---

## 6. 后端模块设计

### 6.1 ProfileService

绑定名：`main.ProfileService`

| 方法 | 签名 | 说明 |
| --- | --- | --- |
| `List` | `() ([]store.Profile, error)` | 按 `UpdatedAt` 倒序 |
| `Get` | `(id string) (store.Profile, error)` | 不存在返回错误 |
| `Save` | `(input store.Profile) (store.Profile, error)` | ID 为空=新增；否则更新 |
| `Delete` | `(id string) error` | 若为当前激活配置，先停止进程 |
| `Duplicate` | `(id string) (store.Profile, error)` | 名称追加“副本”，新 ID |
| `ExportConfig` | `(id string) (string, error)` | 加密导出配置（`TGD1:` 前缀） |
| `DecodeConfig` | `(token string) (store.Profile, error)` | 解密为未保存草稿，名称冲突时自动追加“导入” |

校验规则（`internal/trojan/config.go`）：

- `Name` 去空格后非空、≤64；不区分大小写唯一。
- `ServerAddr` 非空，不能包含 `://`、`/`、空格；允许 IPv6（保存时去掉方括号）。
- `ServerPort`、`LocalPort` ∈ [1,65535]。
- `Password` 非空。
- `SNI` 可为空；非空时不得包含空格/协议。
- `LogLevel` ∈ [0,5]。

保存后触发事件 `profiles:changed`，前端重新拉取列表。

### 6.2 RuntimeService + ProcessManager

绑定名：`main.RuntimeService`

| 方法 | 签名 | 说明 |
| --- | --- | --- |
| `GetState` | `() store.RuntimeState` | 当前运行状态 |
| `Activate` | `(profileID string) (store.RuntimeState, error)` | 停止旧进程→写配置→启动新进程 |
| `Stop` | `() (store.RuntimeState, error)` | 手动停止 |
| `Restart` | `() (store.RuntimeState, error)` | 重启当前配置 |
| `GetLogs` | `(limit int) []store.LogEntry` | 取最近 N 条（前端初始化用） |
| `ClearLogs` | `() error` | 清空环形缓冲 |

状态机：

```
stopped --Activate--> starting --成功--> running
   ^                     |                 |
   |                     |失败             |进程退出/Stop
   +------- error <------+-----------------+
```

启动流程：

1. 校验 profile。
2. 若已有进程，调用 `Stop()` 并等待退出（最长 5 秒）。
3. 生成 trojan-go JSON 配置，写入 `<data>/runtime/active.json`（0600）。
4. 解析二进制路径（见 6.3）。
5. `exec.Command(bin, "-config", cfgPath)`：
   - Windows：`SysProcAttr{CreationFlags: CREATE_NO_WINDOW}`，避免弹出黑窗口。
   - Unix：`SysProcAttr{Setpgid: true}`，便于整组终止。
6. 管道接入 `StdoutPipe` / `StderrPipe`，启动两个扫描 goroutine。
7. 启动 `cmd.Start()`；成功后写 `running`、`Pid`、`StartedAt`。
8. 单独 goroutine `cmd.Wait()`；若并非主动停止，状态置 `error` 并记录退出码。
9. 广播 `runtime:state`。

停止流程：

- Windows：`Process.Kill()`（子进程无控制台，SIGTERM 不可用）。
- Unix：先向进程组发送 `SIGTERM`，5 秒后 `SIGKILL`。
- 等待 `Wait()` 返回，确保端口释放；状态置 `stopped`。
- 应用退出时同步调用，避免残留进程。

激活切换：新配置启动失败时，保持 `error` 状态并保留旧配置 ID（不自动回滚，UI 提示）。

### 6.3 二进制定位顺序

`internal/trojan/binary.go`：

1. 环境变量 `TROJAN_GO_BINARY`。
2. 设置项 `Settings.BinaryPath`。
3. 可执行文件同目录：`<exeDir>/trojan-go[.exe]`（安装后的标准位置）。
4. 开发回退：
   - `<exeDir>/../build/<goos>-<goarch>/trojan-go[.exe]`
   - `<cwd>/../build/<goos>-<goarch>/trojan-go[.exe]`
   - `<cwd>/build/<goos>-<goarch>/trojan-go[.exe]`
5. `exec.LookPath("trojan-go")`。

找不到时返回明确错误，并在首页状态卡提供“打开设置”入口。
找到后执行一次 `trojan-go -version` 解析版本（若该二进制不支持则显示“未知”），缓存 10 分钟。

### 6.4 LogHub（日志中心）

- 环形缓冲：`[]LogEntry`，容量 = `Settings.MaxLogLines`。
- 每条日志分配递增 `Seq`。
- 解析 `[INFO] `、`[WARN]  `、`[ERROR] ` 等前缀得到 `Level`；无前缀归为 `INFO`。
- **批量发送**：100ms 定时 flush，一次性发出 `runtime:log` 事件，payload：
  ```json
  { "lines": [ { "seq": 1, "time": 1730000000000, "stream": "stdout", "level": "INFO", "text": "..." } ] }
  ```
- 单行上限 64KB，超长截断，防止日志卡死。
- 同时把桌面端自身事件（启动、停止、错误）以 `SYSTEM` 级别写入同一缓冲。
- `ClearLogs` 清空缓冲并广播 `runtime:cleared`，前端清屏。
- 不默认落盘；设置页可后续增加“保存日志到文件”。

### 6.5 SettingsService

绑定名：`main.SettingsService`

| 方法 | 签名 |
| --- | --- |
| `Get` | `() (store.Settings, error)` |
| `Save` | `(s store.Settings) (store.Settings, error)` |

`Save` 时归一化：

- `MaxLogLines` 夹在 [100, 20000]。
- `Theme` ∈ {system, light, dark}。
- `UpdateChannel` ∈ {stable, prerelease}。
- `UpdateRepository` 为空时使用内置 `litai686/trojan-go`；必须匹配 `owner/repo`。
- `BinaryPath` 若填写，检查文件存在且可执行（不强制通过，允许用户稍后放置）。
- 保存后触发 `settings:changed`。

### 6.6 UpdateService

绑定名：`main.UpdateService`

| 方法 | 签名 | 说明 |
| --- | --- | --- |
| `VersionInfo` | `() (VersionInfo, error)` | 版本、构建信息、路径 |
| `Check` | `() (*UpdateInfo, error)` | 调 `app.Updater.Check`，返回摘要 |
| `Install` | `() error` | 调 `app.Updater.DownloadAndInstall`（后台进度事件） |
| `Restart` | `() error` | 调 `app.Updater.Restart`（替换并重启） |
| `OpenReleasePage` | `() error` | 浏览器打开 Release 页面 |

自动更新细节见第 11 节。

### 6.7 SystemService

绑定名：`main.SystemService`

| 方法 | 说明 |
| --- | --- |
| `GetInfo() (VersionInfo, error)` | 与 `VersionInfo` 合并，供关于页使用 |
| `OpenDataDir() error` | 打开数据目录 |
| `OpenInstallDir() error` | 打开安装目录 |
| `OpenURL(url string) error` | 仅允许 https，交给 `app.Browser.OpenURL` |

### 6.8 版本号

```go
// desktop/version.go
package main

var (
    version   = "dev"
    commit    = "none"
    buildTime = "unknown"
)
```

构建时注入：

```bash
go build -ldflags "\
 -X main.version=0.1.0 \
 -X main.commit=$(git rev-parse --short HEAD) \
 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

`VersionInfo` 同时返回：

```go
type VersionInfo struct {
    AppVersion    string `json:"appVersion"`
    TrojanVersion string `json:"trojanVersion"`
    Commit        string `json:"commit"`
    BuildTime     string `json:"buildTime"`
    GoVersion     string `json:"goVersion"`
    Platform      string `json:"platform"`
    WailsVersion  string `json:"wailsVersion"`
    DataDir       string `json:"dataDir"`
    InstallDir    string `json:"installDir"`
    BinaryPath    string `json:"binaryPath"`
}
```

> 发布前同步 `build/config.yml`、`build/windows/info.json`、`build/darwin/Info*.plist`
> 中的版本号；`-ldflags` 只影响程序内显示的版本，不会自动更新安装包元数据。

---

## 7. 前端设计

### 7.1 约束

- 无框架、无 npm、无打包器；浏览器直接执行 ES Modules。
- 不使用 CDN；所有资源随应用内置。
- 使用 `import { Call, Events } from "/wails/runtime.js"`（Wails 内置运行时，无需生成绑定）。
- 通过 Hash 路由切换页面，刷新后保持当前页。
- 不使用 `innerHTML` 渲染用户输入（配置名、日志），统一 `textContent`，防 XSS。

### 7.2 页面与路由

| 路由 | 页面 | 内容 |
| --- | --- | --- |
| `#/` | 首页 | 配置下拉列表、运行状态卡、日志面板 |
| `#/profiles` | 配置列表 | 表格、搜索、新增/编辑/复制密文/删除 |
| `#/profiles/new` | 新增配置 | 表单 |
| `#/profiles/:id/edit` | 修改配置 | 表单（回填） |
| `#/settings` | 设置/关于 | 通用设置、更新、版本信息 |
| 其他 | 404 | 返回首页 |

### 7.3 首页交互

1. `<select>` 展示所有配置，第一项为“添加配置”；有配置时默认选中第一个，选中“添加配置”会跳转到新增配置页。
2. 当前激活项选中；`change` 事件：
   - 立即调用 `RuntimeService.Activate(id)`。
   - 请求期间禁用下拉，显示“启动中…”。
   - 失败：Toast 错误，恢复原选中项。
3. 状态卡：
   - 状态徽标（运行中/已停止/错误）。
   - 本地 SOCKS 地址、服务器地址、延迟、运行时长；延迟为 TCP 连接服务器耗时，运行中每 15 秒刷新。
   - 按钮：启动 / 停止 / 复制本地地址。
4. 日志面板：
   - 初始 `GetLogs(settings.maxLogLines)` 回填。
   - 订阅 `runtime:log` 追加；DOM 节点上限 = `maxLogLines`。
   - 工具条：自动滚动开关、级别过滤、搜索、清空、复制全部。
   - 日志行等宽字体，ERROR 红色、WARN 黄色、DEBUG 灰色。
5. 没有配置时显示空状态与“新增配置”按钮。

### 7.4 配置列表页

- 卡片/表格两选一，默认表格：名称、服务器、本地端口、更新时间、状态、操作。
- 操作：编辑、复制配置（密文）、删除（二次确认）。
- 删除当前激活配置时提示“将先停止代理”。
- 顶部“新增配置”按钮。
- 搜索框按名称/服务器过滤（前端本地过滤）。

### 7.5 配置表单

字段分三组：

1. 基本信息：名称、服务器地址、端口、密码（可切换显示）、SNI。
2. 本地与协议：本地地址、本地端口、Mux、TLS 校验、路由。
3. 日志：日志级别下拉（0 Debug … 5 Off）。

交互：

- 实时校验 + 提交时后端校验；错误显示在字段下方。
- 保存成功后 Toast 并跳回列表。
- 取消返回列表；有未保存修改时确认。
- 表单只使用原生 `<input>` / `<select>` / `<button>`，无第三方组件。

### 7.6 设置/关于页

- 通用：启动时激活上次配置、启动时检查更新、日志上限、主题。
- 二进制：自定义 `trojan-go` 路径（文本 + “浏览”按钮可选，v1 文本即可）。
- 更新：当前版本、最新版本、检查按钮、更新说明、下载/安装/重启按钮、进度条。
- 关于：客户端版本、提交、构建时间、Go/Wails 版本、trojan-go 版本、数据目录、安装目录。
- 调试：打开数据目录、复制诊断信息。

### 7.7 状态与事件

轻量 `store.js`：

```js
export const state = {
  profiles: [],
  settings: null,
  runtime: { status: "stopped" },
  logs: [],
  update: null,
};
```

- 全局订阅 `runtime:state`、`runtime:log`、`runtime:cleared`、`profiles:changed`、`settings:changed`。
- 页面卸载时取消订阅（`Events.On` 返回取消函数）。
- 事件只更新状态与必要 DOM，避免整页重绘。

### 7.8 样式

- CSS 变量定义颜色、间距、圆角；`data-theme="light|dark"` 切换。
- 默认跟随系统 `prefers-color-scheme`，用户选择持久化。
- 布局：左侧固定导航（200px）+ 右侧内容；窄窗口（<900px）折叠为顶部导航。
- 组件类：`.btn`、`.card`、`.field`、`.badge`、`.log-view`、`.toast`。
- 不使用图标库；必要的图标用内联 SVG。

### 7.9 浏览器 Mock

`api.js` 在检测不到 Wails 运行时时启用 Mock：

```js
let backend;
try {
  const { Call } = await import("/wails/runtime.js");
  backend = { call: (name, ...args) => Call.ByName(name, ...args) };
} catch {
  backend = createMockBackend(); // 内存数据 + 定时假日志
}
```

用途：直接双击 `index.html` 或本地静态服务器调试 UI，无需启动 Wails。

---

## 8. Wails 绑定与事件契约

### 8.1 服务注册（main.go）

```go
app := application.New(application.Options{
    Name:        "Trojan-Go Desktop",
    Description: "Trojan-Go 桌面客户端",
    Assets: application.AssetOptions{
        Handler: application.AssetFileServerFS(frontend.Assets),
    },
    Mac: application.MacOptions{
        ApplicationShouldTerminateAfterLastWindowClosed: true,
    },
    SingleInstance: &application.SingleInstanceOptions{
        UniqueID: "com.litai686.trojan-go-desktop",
        ExitCode: 0,
    },
})

app.RegisterService(application.NewService(&ProfileService{...}))
app.RegisterService(application.NewService(&RuntimeService{...}))
app.RegisterService(application.NewService(&SettingsService{...}))
app.RegisterService(application.NewService(&UpdateService{...}))
app.RegisterService(application.NewService(&SystemService{...}))

app.Window.NewWithOptions(application.WebviewWindowOptions{
    Title:            "Trojan-Go Desktop",
    Width:            1120,
    Height:           760,
    MinWidth:         900,
    MinHeight:        600,
    BackgroundColour: application.NewRGB(15, 18, 26),
    URL:              "/",
})
```

> 服务必须放在 `main` 包，FQN 才是 `main.ProfileService.List`。
> 若放到子包，调用名会变成 `<module-path>.ProfileService.List`。

### 8.2 前端调用

```js
import { Call, Events } from "/wails/runtime.js";

const profiles = await Call.ByName("main.ProfileService.List");
await Call.ByName("main.RuntimeService.Activate", id);
const state = await Call.ByName("main.RuntimeService.GetState");
```

### 8.3 事件表

| 事件 | 方向 | Payload | 触发 |
| --- | --- | --- | --- |
| `runtime:state` | Go→JS | `RuntimeState` | 状态变化 |
| `runtime:log` | Go→JS | `{ lines: LogEntry[] }` | 日志批量刷新（100ms） |
| `runtime:cleared` | Go→JS | `null` | 清空日志 |
| `profiles:changed` | Go→JS | `null` | 增删改后 |
| `settings:changed` | Go→JS | `Settings` | 设置保存后 |
| `wails:updater:update-available` | Go→JS | `*updater.Release` | 发现新版本 |
| `wails:updater:no-update` | Go→JS | `null` | 已是最新 |
| `wails:updater:download-progress` | Go→JS | `{written,total,rate}` | 下载进度 |
| `wails:updater:update-ready` | Go→JS | `*updater.Release` | 已暂存，等待重启 |
| `wails:updater:error` | Go→JS | `{stage,message}` | 更新失败 |

前端同时监听 Wails 内置更新事件与自身状态，避免重复弹窗。

---

## 9. trojan-go 配置生成

### 9.1 映射

| 表单字段 | trojan-go JSON |
| --- | --- |
| ServerAddr | `remote_addr` |
| ServerPort | `remote_port` |
| Password | `password: ["..."]` |
| SNI（空则 ServerAddr） | `ssl.sni` |
| LocalAddr | `local_addr` |
| LocalPort | `local_port` |
| Mux | `mux.enabled` |
| Router | `router.enabled` |
| VerifyTLS | `ssl.verify` + `ssl.verify_hostname` |
| LogLevel | `log_level` |
| 固定 | `run_type: "client"` |

### 9.2 生成示例

```json
{
  "run_type": "client",
  "local_addr": "127.0.0.1",
  "local_port": 1080,
  "remote_addr": "example.com",
  "remote_port": 443,
  "password": ["your-password"],
  "log_level": 1,
  "ssl": {
    "sni": "example.com",
    "verify": true,
    "verify_hostname": true,
    "alpn": ["http/1.1"]
  },
  "mux": { "enabled": true },
  "router": { "enabled": false }
}
```

### 9.3 校验与安全

- 生成的配置只写到数据目录，不写到安装目录（安装目录可能只读）。
- 配置文件名固定 `active.json`，每次激活覆盖；保留上一份为 `active.json.bak` 便于排查。
- 日志中不打印密码；错误信息如包含配置内容需脱敏。
- 启动前用 `encoding/json` 自检可序列化；trojan-go 的解析错误通过 stderr 反馈到日志。

---

## 10. 打包与安装

### 10.1 产物命名

| 平台 | 安装包 | 便携/更新资产 |
| --- | --- | --- |
| Windows amd64 | `trojan-go-desktop-windows-amd64-installer.exe` | `trojan-go-desktop-windows-amd64.exe` |
| Windows arm64 | 同上（arm64） | 同上（arm64） |
| macOS amd64/arm64 | `trojan-go-desktop-darwin-<arch>.app.zip` | 同左（zip 内单个 .app） |
| Linux amd64/arm64 | `trojan-go-desktop-linux-<arch>.tar.gz`（当前实现，内含主程序与 trojan-go） | 同左；后续可扩展 AppImage / deb / rpm |

`SHA256SUMS-desktop.txt` 与资产一起发布，格式：

```text
<sha256>  trojan-go-desktop-windows-amd64.exe
<sha256>  trojan-go-desktop-windows-amd64-installer.exe
```

### 10.2 trojan-go 二进制来源

- 根目录 `build/<goos>-<goarch>/trojan-go[.exe]`（由根仓库 `build.sh` 生成）。
- 桌面端打包前由 `scripts/stage-trojan` 复制到 `desktop/build/<platform>/trojan-go[.exe]`。
- 若源文件不存在，打包任务直接失败并提示先构建 trojan-go。
- 开发/测试可设置 `TROJAN_GO_BINARY` 指向任意二进制。

### 10.3 Windows（NSIS）

1. `wails3 build` 生成 `bin/trojan-go-desktop.exe`。
2. `wails3 package` 调用 NSIS，脚本位于 `build/windows/nsis/project.nsi`。
3. 在 `project.nsi` 的 `Section "Install"` 中加入：

```nsis
SetOutPath "$INSTDIR"
File "/oname=trojan-go.exe" "..\..\..\build\windows\trojan-go.exe"
```

路径以实际生成脚本的相对位置为准，推荐用 Taskfile 把二进制复制到 `build/windows/` 后引用。

4. 安装目录设为**用户目录**，保证自动更新可写：

```nsis
InstallDir "$LOCALAPPDATA\Programs\Trojan-Go Desktop"
RequestExecutionLevel user
```

5. 卸载时同时删除 `$INSTDIR\trojan-go.exe`。
6. `build/windows/info.json` 写入产品名、版本、公司、文件描述。
7. 可选：代码签名（`build/windows/Taskfile.yml` 配置证书）。

### 10.4 macOS

- `wails3 package` 生成 `.app`。
- 打包后把 `trojan-go` 复制到 `Trojan-Go Desktop.app/Contents/MacOS/trojan-go` 并 `chmod +x`。
- 更新资产为 `.app` 的 zip（zip 内仅一个顶层 `.app`，符合 Wails updater 解包规则）。
- 未签名时需在文档中说明 Gatekeeper 绕过方式；正式发布需 Developer ID + 公证。

### 10.5 Linux

- 当前 Taskfile 产出便携 `tar.gz`，解压后包含 `trojan-go-desktop` 与 `trojan-go`。
- 后续可基于 Wails 模板扩展 AppImage / deb / rpm；扩展时需确保 `trojan-go` 也被放入安装目录（例如 AppDir 的 `usr/bin` 或 nfpm 的 extra files）。
- GTK4/WebKitGTK 6.0 依赖按 Wails 文档声明。

### 10.6 Taskfile 任务（建议）

```yaml
tasks:
  stage:trojan:
    cmds:
      - go run ./scripts/stage-trojan.go --goos "{{.GOOS | default OS}}" --goarch "{{.ARCH | default ARCH}}"
  build:
    deps: [stage:trojan]
    cmds:
      - wails3 build
  package:
    deps: [stage:trojan]
    cmds:
      - wails3 package
```

> Wails 生成的 Taskfile 结构以 `wails3 init` 的实际输出为准，上述为叠加逻辑。

---

## 11. 自动升级

### 11.1 版本与 Release 策略

- 桌面端版本使用 SemVer，例如 `0.1.0`。
- Release tag 使用 `desktop-v0.1.0`，避免与 trojan-go 的 `v0.10.x` 混淆。
- 每次 Release 至少包含：
  - 当前平台便携资产（用于原子替换）
  - 安装包
  - `SHA256SUMS-desktop.txt`
  - Release Notes（Markdown）

### 11.2 自定义 GitHub Provider

Wails 内置 GitHub Provider 只认 `/releases/latest`，无法过滤 tag 前缀；因此实现
`internal/update/provider.go`，满足 `updater.Provider`：

```go
type Provider struct {
    Repository string // owner/repo
    TagPrefix  string // "desktop-v"
    Prerelease bool
    Client     *http.Client
}

func (p *Provider) Name() string { return "github-desktop" }

func (p *Provider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
    // 1. GET https://api.github.com/repos/{repo}/releases?per_page=50
    // 2. 过滤 draft、prerelease（按设置）、tag 前缀
    // 3. 按 SemVer 取最大版本；与 req.CurrentVersion 比较
    // 4. 选择资产：
    //    - 必须包含 goos 与 goarch
    //    - 排除 installer、.sha256、.sig、checksums
    //    - Windows 优先 .exe；macOS 优先 .zip；Linux 优先 .AppImage
    // 5. 读取 SHA256SUMS-desktop.txt 中对应行，填充 Verification
    // 6. 返回 Release{Version, Name, Notes, Artifact, Verification, Metadata}
}

func (p *Provider) Download(ctx context.Context, r *updater.Release, dst io.Writer,
    onProgress func(written, total int64)) error {
    // 流式下载，带进度回调
}
```

### 11.3 初始化

```go
prov := update.NewProvider(update.Config{
    Repository: "litai686/trojan-go",
    TagPrefix:  "desktop-v",
    Prerelease: settings.UpdateChannel == "prerelease",
})
err := app.Updater.Init(updater.Config{
    CurrentVersion: version,
    Providers:      []updater.Provider{prov},
    PublicKey:      publicKey, // 可选；配置后强制签名校验
})
```

> 若后续接入 Ed25519 签名，使用 `wails3 updater genkey` 生成密钥，
> 私钥仅存 CI，公钥 `go:embed` 进客户端。

### 11.4 流程

```
用户点击“检查更新”
  → UpdateService.Check()
  → app.Updater.Check(ctx)
      → Provider.Check()
          → GitHub API
          → 版本比较 / 资产匹配 / SHA256 提取
  → 返回 UpdateInfo 给前端
  → 无更新：提示“已是最新”
  → 有更新：展示版本、大小、Release Notes
用户点击“下载并安装”
  → UpdateService.Install()
  → app.Updater.DownloadAndInstall(ctx)
      → 下载 → 校验 → 解包 → 暂存
      → 事件：download-progress / update-ready
用户点击“重启并应用”
  → UpdateService.Restart()
  → app.Updater.Restart(ctx)
      → 启动 helper → 等待主进程退出 → 替换二进制 → 重新启动
```

### 11.5 失败处理

- 网络/限流：显示错误，保留“重试”；GitHub API 未认证限流 60 次/小时，检查按钮加 30 秒冷却。
- 校验失败：删除临时文件，提示“更新包校验失败”，不允许安装。
- 替换失败：helper 保留旧二进制并写日志；下次启动仍为旧版本。
- 安装目录不可写：提示用户使用安装包升级或把应用安装到用户目录。
- 更新期间禁止激活/停止代理？允许，但重启会终止子进程；`ServiceShutdown` 负责回收。

### 11.6 手动更新入口

设置页始终提供“打开发布页”，即使自动更新失败也可手动下载安装包。

---

## 12. 安全设计

- **密码存储**：本地文件 0600；v1 明文，UI 明确提示；后续接系统密钥链。
- **配置分享**：复制/导入使用 AES-256-GCM（`TGD1:` 前缀）；密钥内置客户端，避免明文进入剪贴板，但不提供口令级保密。
- **日志脱敏**：不打印配置内容、密码、URL 中的凭据；错误信息只保留路径与退出码。
- **命令执行**：只使用 `exec.Command` 参数数组，不拼接 shell 字符串。
- **更新校验**：必须校验 SHA-256；发布流程生成校验文件；建议后续启用 Ed25519 签名。
- **URL 打开**：仅允许 `https://`，拒绝 `file://`、`javascript:`。
- **前端渲染**：用户数据与日志一律 `textContent`；禁止把配置名拼进 `innerHTML`。
- **单实例**：Wails `SingleInstance` 防止重复启动导致多个代理进程。
- **最小权限**：Windows 安装到 `%LOCALAPPDATA%\Programs`，不请求管理员权限。

---

## 13. 测试计划

### 13.1 单元测试（Go）

| 包 | 用例 |
| --- | --- |
| `internal/store` | 原子保存/加载、损坏文件回退、默认值、并发读写 |
| `internal/trojan` | 配置生成字段映射、校验边界（端口/名称/SNI）、二进制查找顺序 |
| `internal/trojan` | LogHub 环形缓冲、级别解析、批量 flush、超长行 |
| `internal/update` | 版本比较、tag 前缀过滤、资产匹配、checksum 解析 |
| `main` | 服务方法参数校验（可抽纯函数） |

### 13.2 集成测试

- 用临时假二进制（脚本输出日志并阻塞）测试：启动→日志→停止→状态。
- 用真实 `trojan-go` 在本地起一个不可达服务器，验证启动失败日志与状态。
- 激活切换：配置 A 运行→激活 B→A 进程退出、B 启动。
- 应用退出：`ServiceShutdown` 后无残留进程（`tasklist`/`ps` 检查）。

### 13.3 前端手工测试矩阵

- 首次启动（无配置）空状态。
- 新增/修改/删除/复制配置。
- 下拉激活、切换、停止与再次启动。
- 日志滚动、过滤、清空、复制。
- 设置保存、主题切换、版本信息。
- 更新检查：无更新、有更新、下载失败、校验失败（可用本地 mock API）。
- 窗口缩放 900×600 ~ 2K 分辨率。

### 13.4 打包冒烟

- Windows 安装到用户目录，确认 `trojan-go.exe` 与主程序同目录。
- 安装后从开始菜单启动，激活配置能连通本地 SOCKS 端口。
- 卸载后无残留进程/文件（数据目录保留由用户选择）。
- 自动更新后版本号变化，`trojan-go.exe` 仍在。

---

## 14. 里程碑

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| M1 | Wails 骨架、静态前端、内嵌资源、窗口 | 能启动并显示页面 |
| M2 | 存储 + ProfileService + 配置列表/表单 | 增删改查可用 |
| M3 | ProcessManager + LogHub + 首页 | 激活/停止/日志实时显示 |
| M4 | Settings + 版本信息 + 关于页 | 设置持久化、版本正确 |
| M5 | 自动更新（Provider/Check/Install/Restart） | 模拟 Release 完成升级 |
| M6 | 打包与 trojan-go 捆绑（Windows 优先） | 安装包即装即用 |
| M7 | 测试、文档、CI、签名/公证 | 发布 v0.1.0 |

---

## 15. 风险与后续

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| Wails v3 beta API 变动 | 编译失败/行为变化 | 固定 beta.34；升级时跑完整回归 |
| Windows 安装目录不可写 | 自动更新失败 | 默认用户目录安装；错误提示手动安装包 |
| macOS 公证/签名 | 用户无法打开 | 正式发布前完成 Developer ID + notarize |
| Linux WebKit 依赖 | 启动失败 | 文档声明 GTK4/WebKitGTK 6.0；AppImage 自带依赖 |
| GitHub API 限流 | 检查更新失败 | 30 秒冷却、错误提示、手动入口 |
| 配置密码明文 | 本地泄露 | 0600 + UI 提示；后续接系统密钥链 |
| 路由模式需要 geoip/geosite | 启动失败 | 默认关闭；开启时提示放置数据文件 |
| 子进程残留 | 端口占用 | 退出钩子 + 进程组/强杀 + 单实例 |

### 后续版本

- 开机自启、系统代理设置。
- 订阅链接/二维码导入、分享链接解析。
- 流量统计（读取 trojan-go API）。
- 批量配置导入导出与管理。
- 多语言（i18n）、日志落盘与导出。
- 更新签名（Ed25519）与灰度通道。

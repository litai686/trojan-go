# Trojan-Go Desktop

基于 **Wails v3.0.0-beta.34** 的 trojan-go 桌面客户端。前端为原生 HTML/CSS/ES Modules，零框架、零打包工具。

## 功能

- 配置列表、新增、修改、删除
- 配置列表一键复制密文配置信息，新增配置时一键导入
- 首页下拉选择配置并立即激活
- 首页显示与服务器的 TCP 延迟（运行中每 15 秒刷新）
- 实时显示 trojan-go 标准输出/错误输出（级别过滤、搜索、自动滚动）
- 设置：启动时激活上次配置、启动时检查更新、日志缓冲、主题、自定义二进制路径
- 版本信息与关于页
- GitHub Releases 自动更新（SHA-256 校验、原子替换、重启生效）
- 关闭窗口默认隐藏到系统托盘，托盘菜单可显示主界面或退出
- 打包时自动把 `build/<goos>-<goarch>/trojan-go[.exe]` 放到安装目录

> 复制出的配置信息为 `TGD1:` 开头的 AES-256-GCM 密文，包含配置名称与连接参数。
> 加密密钥内置在客户端中，用于避免明文出现在剪贴板/聊天记录里，不等同于口令加密。

## 目录

```text
desktop/
├── main.go / services.go / version.go   # Wails 应用与后端服务
├── internal/store                        # profiles.json / settings.json
├── internal/trojan                       # 配置生成、进程管理、日志
├── internal/update                       # GitHub Release Provider
├── frontend                              # 原生前端（内嵌）
├── build                                 # Wails 打包资源（NSIS/.app/图标）
├── scripts                               # 打包辅助（同步版本号、复制 trojan-go）
└── Taskfile.yml                          # 版本号唯一来源（vars.VERSION）
```

## 环境要求

- Go 1.26+
- Wails CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34`
- Windows：WebView2 Runtime；打包 NSIS 需要安装 [NSIS](https://nsis.sourceforge.io/)（`makensis` 在 PATH）
- macOS：Xcode Command Line Tools
- Linux：GTK4 / WebKitGTK 6.0、gcc

## 开发

先构建当前平台的 trojan-go（客户端模式）：

```bash
# 仓库根目录
go build -tags client -trimpath -o build/windows-amd64/trojan-go.exe .
# Linux/macOS 对应 build/<goos>-<goarch>/trojan-go
```

再运行桌面端：

```bash
cd desktop
wails3 dev
```

`wails3 dev` 会监听 Go/JS/CSS/HTML 变化并重新构建。也可以直接：

```bash
wails3 build
./bin/trojan-go-desktop.exe
```

> `wails3 dev` 默认会设置 `FRONTEND_DEVSERVER_URL`（假设使用 Vite）。本项目在 `run` 任务中设置
> `TROJAN_DESKTOP_EMBEDDED_FRONTEND=1`，主程序会忽略该地址并使用内嵌前端；修改前端后 watcher
> 会自动重新构建并重启。

> 前端没有 npm/Vite。`frontend/` 通过 `//go:embed` 编进二进制，修改前端后需要重新构建。
> 直接用浏览器打开 `frontend/index.html` 会进入 Mock 模式（页面底部有提示），便于调试 UI。

## 测试

```bash
cd desktop
go test ./...
```

## 数据目录

| 平台 | 路径 |
| --- | --- |
| Windows | `%APPDATA%\trojan-go-desktop\` |
| macOS | `~/Library/Application Support/trojan-go-desktop/` |
| Linux | `~/.config/trojan-go-desktop/` |

包含 `profiles.json`、`settings.json`、`runtime/active.json`、`desktop.log`。

## 打包

Windows（默认用户目录安装，更新无需管理员权限）：

```bash
cd desktop
wails3 package
# 产物：bin/trojan-go-desktop-windows-amd64-installer.exe
#       bin/trojan-go-desktop.exe + bin/trojan-go.exe（便携）
```

便携 zip：

```bash
wails3 task windows:package:portable
```

macOS：

```bash
wails3 package GOOS=darwin
# 产物：bin/trojan-go-desktop.app（内含 Contents/MacOS/trojan-go）
```

Linux：

```bash
wails3 package GOOS=linux
# 产物：bin/trojan-go-desktop-linux-<arch>.AppImage（内含主程序与 trojan-go）
```

打包前 `scripts/stage-trojan.go` 会检查并复制 `../build/<goos>-<goarch>/trojan-go[.exe]`；
如果文件不存在会直接失败，请先构建 trojan-go。

## 自动更新

- 更新请求会智能选路：当前配置正在运行且本地端口可连接时，通过 trojan-go 的本地 SOCKS5 代理访问 GitHub；否则直连。代理请求失败会自动回退直连。
- 桌面端 Release tag 使用 `desktop-v<semver>`（例如 `desktop-v0.1.0`），与 trojan-go 的 `v0.10.x` 区分。
- Release 资产：
  - `trojan-go-desktop-windows-amd64.exe`（便携单文件，用于原子替换）
  - `trojan-go-desktop-windows-amd64-installer.exe`
  - `trojan-go-desktop-darwin-<arch>.zip`（zip 内仅一个 `.app`，安装与自动更新共用）
  - `trojan-go-desktop-linux-<arch>.AppImage`（安装与自动更新共用）
  - `SHA256SUMS-desktop.txt`（必须提供，缺失时客户端拒绝更新）
- 更新由 Wails `app.Updater` 完成下载、校验、暂存与重启；自定义 Provider 会过滤 `desktop-v*` tag。

## 版本注入

版本号只在 `Taskfile.yml` 的 `vars.VERSION` 中维护，发布时改这一处即可；
临时构建也可以用命令行覆盖：

```bash
wails3 build VERSION=0.2.0
```

Taskfile 会注入 `main.version`、`main.commit`、`main.buildTime`，并在构建前运行
`scripts/sync-version`，把版本号写入 `build/config.yml`、`build/windows/info.json`、
`build/windows/nsis/wails_tools.nsh`、`build/darwin/Info*.plist`，安装包与 macOS
Bundle 元数据无需手动同步。

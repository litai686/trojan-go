#!/usr/bin/env bash
#
# trojan-go 一键编译脚本
#
# 兼容平台: Linux / macOS / Windows Git Bash (--release 需要 zip 命令)
# 功能: 本机编译、交叉编译、发布打包、桌面客户端打包、可选安装
#
# 用法:
#   ./build.sh                              # 编译当前平台并打包桌面客户端, 产物在 build/
#   ./build.sh --all                        # 交叉编译 linux/darwin 的 amd64 与 arm64
#   ./build.sh --release                    # 编译全部发布平台并打包 zip
#   ./build.sh --target linux-arm64         # 编译指定平台(可逗号分隔多个)
#   ./build.sh --tags mini                  # 指定构建标签, 默认 full
#   ./build.sh --no-desktop                 # 只编译 trojan-go, 跳过桌面客户端
#   ./build.sh --clean                      # 清理 build 目录
#   ./build.sh --install /usr/local/bin     # 编译并安装本机产物
#
set -euo pipefail

NAME="trojan-go"
BUILD_DIR="build"
GO="${GO:-go}"
WAILS_VERSION="${WAILS_VERSION:-v3.0.0-beta.34}"
RELEASE_TARGETS="linux-386,linux-amd64,linux-arm,linux-armv5,linux-armv6,linux-armv7,linux-arm64,linux-mips-softfloat,linux-mips-hardfloat,linux-mipsle-softfloat,linux-mipsle-hardfloat,linux-mips64,linux-mips64le,darwin-amd64,darwin-arm64,freebsd-386,freebsd-amd64,windows-386,windows-amd64,windows-arm,windows-armv6,windows-armv7,windows-arm64"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { printf '%b[*]%b %s\n' "$GREEN" "$NC" "$*"; }
info() { printf '%b    %b %s\n' "$CYAN" "$NC" "$*"; }
warn() { printf '%b[!]%b %s\n' "$YELLOW" "$NC" "$*" >&2; }
die()  { printf '%b[x]%b %s\n' "$RED" "$NC" "$*" >&2; exit 1; }

usage() {
	cat <<'EOF'
trojan-go 编译脚本 (Linux / macOS / Windows Git Bash)
Windows 用户可在 cmd/PowerShell 中直接运行 build.cmd (自动调用 Git Bash)

用法:
  ./build.sh [选项]

选项:
  --all                   交叉编译 linux/darwin 的 amd64 与 arm64
  --release               编译全部发布平台并打包 zip
  --target <平台列表>     编译指定平台, 逗号分隔, 如 linux-amd64,darwin-arm64
                          支持: linux|darwin|windows|freebsd
                                amd64|arm64|386|arm|armv5|armv6|armv7|mips|mipsle|mips64|mips64le
  --tags <标签>           构建标签, 默认 full (可用 mini / client / server / custom)
  --no-desktop            跳过桌面客户端打包
  --desktop               强制打包桌面客户端, 缺少 wails3 时自动安装并追加本机目标
  --desktop-version <版本> 桌面客户端版本, 默认取 desktop/Taskfile.yml
  --clean                 清理 build 目录后退出
  --install [目录]        编译本机平台并安装到指定目录, 默认 /usr/local/bin
  -h, --help              显示帮助

环境变量:
  GO                      指定 go 可执行文件, 默认 go
  WAILS_VERSION           自动安装的 wails3 版本, 默认 v3.0.0-beta.34

说明:
  Windows 桌面客户端只生成 NSIS 安装包(需 makensis, 内含 trojan-go.exe)
  Linux/macOS 只生成 AppImage / .app.zip, 不生成裸可执行文件

示例:
  ./build.sh                          编译当前平台并打包桌面客户端
  ./build.sh --no-desktop             只编译 trojan-go
  ./build.sh --all                    交叉编译四个常见平台
  ./build.sh --release                编译全部平台并打包 zip
  ./build.sh --target darwin-arm64    仅编译 darwin/arm64
  ./build.sh --tags mini              Mini 版本
EOF
}

# ---------------- 基础检测 ----------------

check_go() {
	command -v "$GO" >/dev/null 2>&1 || die "未找到 go 命令, 请先安装 Go: https://go.dev/dl/"
	local version
	version=$("$GO" version | awk '{print $3}')
	log "使用 ${version} ($("$GO" env GOOS)/$("$GO" env GOARCH))"
}

# ---------------- 平台识别 ----------------

normalize_arch() {
	case "$1" in
		x86_64|amd64)   echo "amd64" ;;
		aarch64|arm64)  echo "arm64" ;;
		armv7l|armv7)   echo "armv7" ;;
		armv6l|armv6)   echo "armv6" ;;
		armv5l|armv5)   echo "armv5" ;;
		i386|i486|i586|i686|386) echo "386" ;;
		*) echo "$1" ;;
	esac
}

host_target() {
	local os arch
	if command -v uname >/dev/null 2>&1; then
		os=$(uname -s | tr '[:upper:]' '[:lower:]')
		case "$os" in
			linux*)  os="linux" ;;
			darwin*) os="darwin" ;;
			mingw*|msys*|cygwin*) os="windows" ;;
			*) die "不支持的操作系统: ${os} (支持 Linux / macOS / Windows Git Bash)" ;;
		esac
		arch=$(normalize_arch "$(uname -m)")
	else
		os=$("$GO" env GOOS)
		arch=$("$GO" env GOARCH)
	fi

	case "$arch" in
		amd64|arm64|386|arm) ;;
		armv7|armv6|armv5) ;;
		*) warn "未识别的 CPU 架构 ${arch}, 按 amd64 处理"; arch="amd64" ;;
	esac

	echo "${os}-${arch}"
}

# ---------------- 编译 ----------------

# build_one <os> <arch> [goarm] [gomips] [ext]
build_one() {
	local goos="$1" goarch="$2" goarm="${3:-}" gomips="${4:-}"
	local target="${goos}-${goarch}"
	[ -n "$goarm" ]  && target="${target}v${goarm#v}"
	[ -n "$gomips" ] && target="${target}-${gomips}"

	local go_env out_dir
	go_env=""
	out_dir="${BUILD_DIR}/${target}"
	local out_name="${NAME}"
	[ "$goos" = "windows" ] && out_name="${NAME}.exe"
	local out="${out_dir}/${out_name}"

	mkdir -p "$out_dir"

	log "编译 ${goos}/${goarch}${goarm:+ arm=v${goarm#v}}${gomips:+ mips=${gomips}}"

	go_env="CGO_ENABLED=0 GOOS=${goos} GOARCH=${goarch}"
	[ -n "$goarm" ]  && go_env="${go_env} GOARM=${goarm#v}"
	[ -n "$gomips" ] && go_env="${go_env} GOMIPS=${gomips}"

	# shellcheck disable=SC2086
	env ${go_env} "$GO" build \
		-tags "${TAGS}" \
		-trimpath \
		-ldflags "-s -w -buildid=" \
		-o "${out}" .

	[ -f "${out}" ] || die "编译失败: ${goos}/${goarch}"
	local size
	size=$(du -h "${out}" 2>/dev/null | awk '{print $1}' || true)
	[ -n "$size" ] || size="$(wc -c <"${out}" | awk '{print $1}') B"
	info "-> ${out} (${size})"

	if [ "$DO_ZIP" -eq 1 ]; then
		pack_zip "$target" "$out_dir" "$out_name"
	fi
}

pack_zip() {
	local target="$1" out_dir="$2" out_name="$3"
	local zip_name="${NAME}-${target}.zip" zip_path dat
	zip_path="$(pwd)/${zip_name}"
	rm -f "$zip_name"
	log "打包 ${zip_name}"
	(cd "$out_dir" && zip -q "$zip_path" "$out_name")
	if [ -d example ]; then
		zip -q "$zip_name" example/*
	fi
	for dat in geoip.dat geosite.dat geoip-only-cn-private.dat; do
		if [ -f "$dat" ]; then
			zip -q "$zip_name" "$dat"
		fi
	done
}

fetch_geodata() {
	local url="$1" out="$2"
	if [ -f "$out" ]; then
		return
	fi
	if command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$out" || { rm -f "$out"; warn "下载 ${out} 失败, 已跳过"; }
	elif command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$out" || { rm -f "$out"; warn "下载 ${out} 失败, 已跳过"; }
	else
		warn "未找到 wget 或 curl, 跳过下载 ${out}"
	fi
}

# ---------------- 桌面客户端 ----------------

desktop_version() {
	[ -f desktop/Taskfile.yml ] || return 0
	awk -F'"' '/^[[:space:]]*VERSION:/ { print $2; exit }' desktop/Taskfile.yml
}

# make_zip <zip_path> <workdir> <条目...>
make_zip() {
	local zip_path="$1" workdir="$2"
	shift 2
	if command -v zip >/dev/null 2>&1; then
		(cd "$workdir" && zip -q -r "$zip_path" "$@")
		return
	fi
	if command -v powershell.exe >/dev/null 2>&1; then
		local win_zip list="" entry path
		win_zip=$(cygpath -w "$zip_path" 2>/dev/null || echo "$zip_path")
		for entry in "$@"; do
			path="$workdir/$entry"
			path=$(cygpath -w "$path" 2>/dev/null || echo "$path")
			list="${list:+${list},}'${path}'"
		done
		powershell.exe -NoProfile -Command "Compress-Archive -Force -Path @(${list}) -DestinationPath '${win_zip}'" >/dev/null
		return
	fi
	return 1
}

# wails_gobin: 输出包含 wails3 的 GOPATH/bin 目录(已存在二进制时)
wails_gobin() {
	local gobin
	gobin=$("$GO" env GOBIN)
	[ -n "$gobin" ] || gobin="$("$GO" env GOPATH)/bin"
	if command -v cygpath >/dev/null 2>&1; then
		gobin=$(cygpath -u "$gobin")
	fi
	if [ -x "${gobin}/wails3" ] || [ -x "${gobin}/wails3.exe" ]; then
		echo "$gobin"
	fi
}

# ensure_wails: 设置全局 WAILS3, 缺少时按需自动安装
WAILS3=""
ensure_wails() {
	[ -n "$WAILS3" ] && return 0
	if command -v wails3 >/dev/null 2>&1; then
		WAILS3=$(command -v wails3)
		return 0
	fi
	local gobin
	gobin=$(wails_gobin)
	if [ -n "$gobin" ]; then
		export PATH="${gobin}:${PATH}"
		WAILS3=$(command -v wails3)
		return 0
	fi
	[ "$DESKTOP_REQUIRED" -eq 1 ] || return 1
	log "未找到 wails3, 正在安装 ${WAILS_VERSION} ..."
	"$GO" install "github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}" \
		|| die "wails3 安装失败, 请手动安装: go install github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}"
	gobin=$(wails_gobin)
	[ -n "$gobin" ] || die "wails3 安装后仍不可用, 请检查 PATH"
	export PATH="${gobin}:${PATH}"
	WAILS3=$(command -v wails3)
}

# find_makensis: 定位 NSIS 并把所在目录加入 PATH(Taskfile 里直接调用 makensis)
MAKENSIS=""
find_makensis() {
	[ -n "$MAKENSIS" ] && return 0
	if command -v makensis >/dev/null 2>&1; then
		MAKENSIS=$(command -v makensis)
	else
		local candidate
		for candidate in "/c/Program Files (x86)/NSIS/makensis.exe" "/c/Program Files/NSIS/makensis.exe"; do
			if [ -f "$candidate" ]; then
				MAKENSIS="$candidate"
				break
			fi
		done
	fi
	[ -n "$MAKENSIS" ] || return 1
	export PATH="$(dirname "$MAKENSIS"):${PATH}"
}

check_desktop_toolchain() {
	case "$1" in
		linux)
			if ! command -v gcc >/dev/null 2>&1 || ! command -v pkg-config >/dev/null 2>&1 \
				|| ! pkg-config --exists gtk4 webkitgtk-6.0 || ! pkg-config --atleast-version=4.10 gtk4; then
				warn "缺少 Linux 桌面依赖 (gcc / pkg-config / GTK4 >= 4.10 / WebKitGTK 6.0)"
				warn "Ubuntu 24.04+: sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev"
				warn "Ubuntu 22.04 的 GTK 4.6 版本过低, 请改用 24.04+ 或在 Windows/macOS 本机打包"
				return 1
			fi
			;;
		darwin)
			if ! command -v xcode-select >/dev/null 2>&1 || ! xcode-select -p >/dev/null 2>&1; then
				warn "未找到 Xcode Command Line Tools, 请先运行 xcode-select --install"
				return 1
			fi
			;;
	esac
	return 0
}

package_desktop() {
	local host os arch ext src dist app version
	host=$(host_target)
	case ",${TARGETS}," in
		*",${host},"*) ;;
		*)
			warn "本次未编译本机平台 ${host}, 跳过桌面客户端打包"
			warn "需要桌面客户端时直接运行 ./build.sh, 或把 ${host} 加入 --target"
			return 0
			;;
	esac
	os="${host%%-*}"
	arch="${host#*-}"
	ext=""
	[ "$os" = "windows" ] && ext=".exe"
	src="${BUILD_DIR}/${host}/${NAME}${ext}"
	[ -f "$src" ] || die "桌面客户端需要本机产物 ${src}"
	dist="$(pwd)/${BUILD_DIR}/desktop"

	if ! ensure_wails; then
		warn "未找到 wails3, 跳过桌面客户端打包"
		warn "安装后重试: go install github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}"
		return 0
	fi
	if ! check_desktop_toolchain "$os"; then
		if [ "$DESKTOP_REQUIRED" -eq 1 ]; then
			die "缺少桌面客户端打包依赖"
		fi
		warn "跳过桌面客户端打包"
		return 0
	fi

	local args=("GOOS=${os}" "GOARCH=${arch}")
	if [ -n "$DESKTOP_VERSION" ]; then
		version="${DESKTOP_VERSION#v}"
		args+=("VERSION=${version}")
	else
		version=$(desktop_version)
	fi
	log "打包桌面客户端 ${os}/${arch}${version:+ (v${version#v})}"

	case "$os" in
		windows)
			find_makensis || die "未找到 NSIS (makensis), Windows 桌面客户端只生成安装包, 请先安装: winget install NSIS.NSIS"
			(cd desktop && "$WAILS3" package "${args[@]}") || die "桌面客户端打包失败"
			;;
		*)
			(cd desktop && "$WAILS3" package "${args[@]}") || die "桌面客户端打包失败"
			;;
	esac

	mkdir -p "$dist"
	case "$os" in
		windows)
			local f found=0
			for f in desktop/bin/*installer*.exe; do
				if [ -f "$f" ]; then
					found=1
					cp -f "$f" "${dist}/"
					info "-> ${BUILD_DIR}/desktop/$(basename "$f")"
				fi
			done
			[ "$found" -eq 1 ] || die "未生成 Windows 安装包 (desktop/bin/*installer*.exe)"
			;;
		darwin)
			app="desktop/bin/trojan-go-desktop.app"
			[ -d "$app" ] || die "未找到桌面客户端产物 ${app}"
			if command -v ditto >/dev/null 2>&1; then
				ditto -c -k --norsrc --noextattr --keepParent "$app" "${dist}/trojan-go-desktop-darwin-${arch}.zip" || die "打包 .app 失败"
			else
				make_zip "${dist}/trojan-go-desktop-darwin-${arch}.zip" desktop/bin "trojan-go-desktop.app" || die "打包 .app 失败"
			fi
			info "-> ${BUILD_DIR}/desktop/trojan-go-desktop-darwin-${arch}.zip"
			;;
		linux)
			app="desktop/bin/trojan-go-desktop-linux-${arch}.AppImage"
			[ -f "$app" ] || die "未找到桌面客户端产物 ${app}"
			cp -f "$app" "${dist}/"
			info "-> ${BUILD_DIR}/desktop/trojan-go-desktop-linux-${arch}.AppImage"
			;;
	esac
}

# 解析 target, 生成 "os arch goarm gomips"
parse_target() {
	local raw="$1" os arch variant
	os="${raw%%-*}"
	arch="${raw#*-}"
	variant=""
	if [ "$os" = "$raw" ]; then
		die "无法解析目标平台 '${raw}', 格式应为 os-arch, 例如 linux-amd64"
	fi
	case "$arch" in
		*-*) variant="${arch#*-}"; arch="${arch%%-*}" ;;
	esac

	case "$os" in
		linux|darwin|windows|freebsd) ;;
		*) die "不支持的目标系统: ${os}" ;;
	esac

	case "$arch" in
		amd64|arm64|386) ;;
		arm) : ;;
		armv5|armv6|armv7) variant="$arch"; arch="arm" ;;
		mips|mipsle|mips64|mips64le) ;;
		*) die "不支持的架构: ${arch}" ;;
	esac

	local goarm="" gomips=""
	case "${arch}-${variant}" in
		arm-armv5) goarm="5" ;;
		arm-armv6) goarm="6" ;;
		arm-armv7) goarm="7" ;;
		arm-) ;;
		mips-*|mipsle-*) gomips="${variant:-softfloat}" ;;
	esac

	printf '%s|%s|%s|%s\n' "$os" "$arch" "$goarm" "$gomips"
}

# ---------------- 参数解析 ----------------

TARGETS=""
TAGS="full"
DO_CLEAN=0
DO_ZIP=0
INSTALL_DIR=""
DESKTOP=1
DESKTOP_REQUIRED=0
DESKTOP_VERSION=""

while [ $# -gt 0 ]; do
	case "$1" in
		--all)
			TARGETS="linux-amd64,linux-arm64,darwin-amd64,darwin-arm64"
			shift
			;;
		--release)
			TARGETS="$RELEASE_TARGETS"
			DO_ZIP=1
			shift
			;;
		--target)
			[ $# -ge 2 ] || die "--target 需要指定平台"
			TARGETS="$2"
			shift 2
			;;
		--target=*) TARGETS="${1#*=}"; shift ;;
		--tags)
			[ $# -ge 2 ] || die "--tags 需要指定标签"
			TAGS="$2"
			shift 2
			;;
		--tags=*) TAGS="${1#*=}"; shift ;;
		--no-desktop) DESKTOP=0; shift ;;
		--desktop) DESKTOP=1; DESKTOP_REQUIRED=1; shift ;;
		--desktop-version)
			[ $# -ge 2 ] || die "--desktop-version 需要指定版本"
			DESKTOP_VERSION="$2"
			shift 2
			;;
		--desktop-version=*) DESKTOP_VERSION="${1#*=}"; shift ;;
		--clean) DO_CLEAN=1; shift ;;
		--install)
			if [ $# -ge 2 ] && [ "${2#-}" = "$2" ]; then
				INSTALL_DIR="$2"
				shift 2
			else
				INSTALL_DIR="/usr/local/bin"
				shift
			fi
			;;
		--install=*) INSTALL_DIR="${1#*=}"; shift ;;
		-h|--help) usage; exit 0 ;;
		*) die "未知参数: $1 (使用 --help 查看帮助)" ;;
	esac
done

cd "$(dirname "$0")"

if [ "$DO_CLEAN" -eq 1 ]; then
	log "清理 ${BUILD_DIR}/ ..."
	rm -rf "$BUILD_DIR"
	log "清理完成"
	exit 0
fi

check_go
log "构建标签 ${TAGS}"

if [ "$DO_ZIP" -eq 1 ]; then
	command -v zip >/dev/null 2>&1 || die "打包需要 zip 命令 (Windows 请安装 zip 或使用 WSL)"
	log "下载 geo 数据文件 ..."
	fetch_geodata "https://github.com/v2fly/geoip/raw/release/geoip.dat" "geoip.dat"
	fetch_geodata "https://github.com/v2fly/geoip/raw/release/geoip-only-cn-private.dat" "geoip-only-cn-private.dat"
	fetch_geodata "https://github.com/v2fly/domain-list-community/raw/release/dlc.dat" "geosite.dat"
fi

HOST_TARGET=$(host_target)
if [ -z "$TARGETS" ]; then
	TARGETS="$HOST_TARGET"
fi

if [ "$DESKTOP" -eq 1 ] && [ "$DESKTOP_REQUIRED" -eq 1 ]; then
	case ",${TARGETS}," in
		*",${HOST_TARGET},"*) ;;
		*)
			info "桌面客户端需要本机 trojan-go, 追加目标 ${HOST_TARGET}"
			TARGETS="${TARGETS},${HOST_TARGET}"
			;;
	esac
fi

old_ifs="$IFS"
IFS=','
for target in $TARGETS; do
	IFS="$old_ifs"
	[ -n "$target" ] || continue
	parse_out=$(parse_target "$target")
	t_os="${parse_out%%|*}"
	rest="${parse_out#*|}"
	t_arch="${rest%%|*}"
	rest="${rest#*|}"
	t_arm="${rest%%|*}"
	t_mips="${rest#*|}"
	[ -n "$t_os" ] && [ -n "$t_arch" ] || die "无法解析目标平台 '${target}'"
	build_one "$t_os" "$t_arch" "$t_arm" "$t_mips"
	IFS=','
done
IFS="$old_ifs"

if [ -n "$INSTALL_DIR" ]; then
	host="$HOST_TARGET"
	os="${host%%-*}"
	ext=""
	[ "$os" = "windows" ] && ext=".exe"
	bin="${BUILD_DIR}/${host}/${NAME}${ext}"
	inst_name="${NAME}${ext}"
	[ -f "$bin" ] || die "未找到本机产物 ${bin}, 请先不带 --install 编译或确保 --target 包含本机平台"
	if [ "$os" != "windows" ] && [ "$(id -u)" -ne 0 ]; then
		die "安装到 ${INSTALL_DIR} 需要 root 权限, 请使用 sudo 运行"
	fi
	log "安装到 ${INSTALL_DIR}/${inst_name}"
	mkdir -p "$INSTALL_DIR"
	if command -v install >/dev/null 2>&1; then
		install -m 0755 "$bin" "${INSTALL_DIR}/${inst_name}"
	else
		cp -f "$bin" "${INSTALL_DIR}/${inst_name}"
	fi
	info "安装完成"
fi

if [ "$DESKTOP" -eq 1 ]; then
	package_desktop
fi

log "编译完成, 产物位于 ${BUILD_DIR}/"

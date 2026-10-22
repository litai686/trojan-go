#!/usr/bin/env bash
#
# trojan-go 一键编译脚本
#
# 兼容平台: Linux / macOS (bash 3.2+, --release 需要 zip 命令)
# 功能: 本机编译、交叉编译、发布打包、可选安装
#
# 用法:
#   ./build.sh                              # 编译当前平台, 产物在 build/
#   ./build.sh --all                        # 交叉编译 linux/darwin 的 amd64 与 arm64
#   ./build.sh --release                    # 编译全部发布平台并打包 zip
#   ./build.sh --target linux-arm64         # 编译指定平台(可逗号分隔多个)
#   ./build.sh --tags mini                  # 指定构建标签, 默认 full
#   ./build.sh --clean                      # 清理 build 目录
#   ./build.sh --install /usr/local/bin     # 编译并安装本机产物
#
set -euo pipefail

NAME="trojan-go"
BUILD_DIR="build"
GO="${GO:-go}"
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
trojan-go 编译脚本 (Linux / macOS)

用法:
  ./build.sh [选项]

选项:
  --all                   交叉编译 linux/darwin 的 amd64 与 arm64
  --release               编译全部发布平台并打包 zip
  --target <平台列表>     编译指定平台, 逗号分隔, 如 linux-amd64,darwin-arm64
                          支持: linux|darwin|windows|freebsd
                                amd64|arm64|386|arm|armv5|armv6|armv7|mips|mipsle|mips64|mips64le
  --tags <标签>           构建标签, 默认 full (可用 mini / client / server / custom)
  --clean                 清理 build 目录后退出
  --install [目录]        编译本机平台并安装到指定目录, 默认 /usr/local/bin
  -h, --help              显示帮助

环境变量:
  GO                      指定 go 可执行文件, 默认 go

示例:
  ./build.sh                          编译当前平台
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
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in
		linux)  os="linux" ;;
		darwin) os="darwin" ;;
		*) die "不支持的操作系统: ${os} (仅支持 Linux / macOS)" ;;
	esac

	arch=$(normalize_arch "$(uname -m)")
	case "$arch" in
		amd64|arm64|386) ;;
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
	info "-> ${out} ($(du -h "${out}" | awk '{print $1}'))"

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
	command -v zip >/dev/null 2>&1 || die "打包需要 zip 命令"
	log "下载 geo 数据文件 ..."
	fetch_geodata "https://github.com/v2fly/geoip/raw/release/geoip.dat" "geoip.dat"
	fetch_geodata "https://github.com/v2fly/geoip/raw/release/geoip-only-cn-private.dat" "geoip-only-cn-private.dat"
	fetch_geodata "https://github.com/v2fly/domain-list-community/raw/release/dlc.dat" "geosite.dat"
fi

if [ -z "$TARGETS" ]; then
	TARGETS=$(host_target)
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
	host=$(host_target)
	bin="${BUILD_DIR}/${host}/${NAME}"
	[ -f "$bin" ] || die "未找到本机产物 ${bin}, 请先不带 --install 编译或确保 --target 包含本机平台"
	if [ "$(id -u)" -ne 0 ]; then
		die "安装到 ${INSTALL_DIR} 需要 root 权限, 请使用 sudo 运行"
	fi
	log "安装到 ${INSTALL_DIR}/${NAME}"
	install -m 0755 "$bin" "${INSTALL_DIR}/${NAME}"
	info "安装完成"
fi

log "编译完成, 产物位于 ${BUILD_DIR}/"

#!/usr/bin/env bash
# 一键本地启动:
# - Milvus: Podman 容器
# - Redis: 本机进程
# - API 服务: 本机进程

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

log() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
die() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "未找到命令: $1"
}

wait_for() {
    local name="$1"
    local cmd="$2"
    local timeout="${3:-60}"
    local i
    for i in $(seq 1 "$timeout"); do
        if eval "$cmd" >/dev/null 2>&1; then
            log "$name 就绪"
            return 0
        fi
        sleep 1
    done
    return 1
}

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}AI Agent API 一键本地启动${NC}"
echo -e "${GREEN}========================================${NC}"

if [ -f ".env" ]; then
    set -a
    # shellcheck disable=SC1091
    source ".env"
    set +a
    log "已加载 .env"
else
    warn "未找到 .env，将仅使用当前 shell 环境变量"
fi

require_cmd go
require_cmd curl
require_cmd podman

log "Go: $(go version)"

export MILVUS_MODE="${MILVUS_MODE:-milvus}"
export REDIS_MODE="${REDIS_MODE:-redis}"
export MILVUS_ADDRESS="${MILVUS_ADDRESS:-127.0.0.1:19530}"
export REDIS_ADDRESS="${REDIS_ADDRESS:-127.0.0.1:6379}"
export PORT="${PORT:-8080}"

PODMAN_MACHINE="${PODMAN_MACHINE:-codex-milvus}"
MILVUS_CONTAINER="${MILVUS_CONTAINER:-milvus-standalone}"
MILVUS_DATA_DIR="${MILVUS_DATA_DIR:-$HOME/.podman/milvus}"
MILVUS_PORT="${MILVUS_PORT:-19530}"
MILVUS_HEALTH_PORT="${MILVUS_HEALTH_PORT:-9091}"
REDIS_HOST="${REDIS_ADDRESS%:*}"
REDIS_PORT="${REDIS_ADDRESS##*:}"

if [ "${MILVUS_MODE}" != "milvus" ]; then
    warn "检测到 MILVUS_MODE=${MILVUS_MODE}，脚本将覆盖为 milvus"
    export MILVUS_MODE="milvus"
fi
if [ "${REDIS_MODE}" != "redis" ]; then
    warn "检测到 REDIS_MODE=${REDIS_MODE}，脚本将覆盖为 redis"
    export REDIS_MODE="redis"
fi

if [ -z "${AUTH_TOKEN:-}" ]; then
    warn "AUTH_TOKEN 未设置，自动使用 demo-token"
    export AUTH_TOKEN="demo-token"
fi

if ! podman machine inspect "${PODMAN_MACHINE}" >/dev/null 2>&1; then
    info "初始化 Podman machine: ${PODMAN_MACHINE}"
    podman machine init "${PODMAN_MACHINE}" \
        --cpus "${PODMAN_CPUS:-2}" \
        --memory "${PODMAN_MEMORY:-4096}" \
        --disk-size "${PODMAN_DISK_SIZE:-40}" \
        --rootful
fi

MACHINE_STATE="$(podman machine inspect "${PODMAN_MACHINE}" --format '{{.State}}' 2>/dev/null || echo unknown)"
if [ "${MACHINE_STATE}" != "running" ]; then
    info "启动 Podman machine: ${PODMAN_MACHINE}"
    podman machine start "${PODMAN_MACHINE}"
fi

podman system connection default "${PODMAN_MACHINE}-root" >/dev/null 2>&1 || true
wait_for "Podman API" "podman info" 30 || die "Podman API 未就绪，请检查 podman machine 状态"

MILVUS_IMAGE_FINAL=""
if [ -n "${MILVUS_IMAGE:-}" ]; then
    MILVUS_IMAGE_FINAL="${MILVUS_IMAGE}"
else
    MILVUS_CANDIDATES=()
    ARCH="$(uname -m)"
    if [ "${ARCH}" = "arm64" ] || [ "${ARCH}" = "aarch64" ]; then
        MILVUS_CANDIDATES+=("swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/milvusdb/milvus:v2.4.17-20241122-fc961333-arm64-linuxarm64")
    fi
    MILVUS_CANDIDATES+=(
        "docker.io/milvusdb/milvus:v2.4.4"
        "swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/milvusdb/milvus:v2.4.4"
    )
fi

if podman container exists "${MILVUS_CONTAINER}"; then
    RUNNING="$(podman inspect -f '{{.State.Running}}' "${MILVUS_CONTAINER}")"
    if [ "${RUNNING}" != "true" ]; then
        info "启动已存在 Milvus 容器: ${MILVUS_CONTAINER}"
        podman start "${MILVUS_CONTAINER}" >/dev/null
    else
        log "Milvus 容器已运行: ${MILVUS_CONTAINER}"
    fi
else
    if [ -z "${MILVUS_IMAGE_FINAL}" ]; then
        for img in "${MILVUS_CANDIDATES[@]}"; do
            if podman image exists "${img}" >/dev/null 2>&1; then
                MILVUS_IMAGE_FINAL="${img}"
                break
            fi
            info "尝试拉取 Milvus 镜像: ${img}"
            if podman pull "${img}" >/dev/null; then
                MILVUS_IMAGE_FINAL="${img}"
                break
            else
                warn "拉取失败: ${img}"
            fi
        done
    fi

    [ -n "${MILVUS_IMAGE_FINAL}" ] || die "无法拉取可用 Milvus 镜像，请手动设置 MILVUS_IMAGE"
    mkdir -p "${MILVUS_DATA_DIR}"

    info "创建 Milvus 容器: ${MILVUS_CONTAINER}"
    podman run -d \
        --name "${MILVUS_CONTAINER}" \
        -p "${MILVUS_PORT}:19530" \
        -p "${MILVUS_HEALTH_PORT}:9091" \
        -e ETCD_USE_EMBED=true \
        -e COMMON_STORAGETYPE=local \
        -v "${MILVUS_DATA_DIR}:/var/lib/milvus" \
        "${MILVUS_IMAGE_FINAL}" \
        milvus run standalone >/dev/null
fi

wait_for "Milvus" "curl -fsS --max-time 2 http://127.0.0.1:${MILVUS_HEALTH_PORT}/healthz | grep -q '^OK$'" 180 || {
    podman logs --tail 100 "${MILVUS_CONTAINER}" || true
    die "Milvus 未在预期时间内就绪"
}

if ! command -v redis-cli >/dev/null 2>&1 || ! redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" ping >/dev/null 2>&1; then
    if [ "${REDIS_HOST}" != "127.0.0.1" ] && [ "${REDIS_HOST}" != "localhost" ]; then
        die "REDIS_ADDRESS=${REDIS_ADDRESS} 不是本机地址，且当前不可连通"
    fi
    require_cmd redis-server
    info "启动本机 Redis: ${REDIS_HOST}:${REDIS_PORT}"
    redis-server --daemonize yes --bind "${REDIS_HOST}" --port "${REDIS_PORT}" >/dev/null
fi

wait_for "Redis" "redis-cli -h \"${REDIS_HOST}\" -p \"${REDIS_PORT}\" ping | grep -q PONG" 20 || die "Redis 未就绪"

if lsof -nP -iTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
    die "端口 ${PORT} 已被占用，请先关闭已有进程后重试"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}环境就绪，启动 API 服务${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "MILVUS_MODE=${MILVUS_MODE}"
echo -e "REDIS_MODE=${REDIS_MODE}"
echo -e "MILVUS_ADDRESS=${MILVUS_ADDRESS}"
echo -e "REDIS_ADDRESS=${REDIS_ADDRESS}"
echo -e "PORT=${PORT}"
echo ""

exec go run ./cmd/server run

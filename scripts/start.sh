#!/bin/bash
# 快速启动脚本 - 自动检测并启动服务

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}AI Agent API 快速启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查 Go 环境
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: 未找到 Go 环境，请先安装 Go 1.21+${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Go 环境检测通过: $(go version)"

# 检查环境变量
if [ -z "$AUTH_TOKEN" ]; then
    echo -e "${YELLOW}警告: 未设置 AUTH_TOKEN，使用默认值 'demo-token'${NC}"
    export AUTH_TOKEN="demo-token"
fi

echo -e "${GREEN}✓${NC} AUTH_TOKEN: $AUTH_TOKEN"

# 检查 LLM 配置
if [ -z "$LLM_API_KEY" ]; then
    echo -e "${YELLOW}警告: 未设置 LLM_API_KEY，将使用规则式 LLM（无真实推理能力）${NC}"
    echo -e "${YELLOW}提示: 设置 LLM_API_KEY 以使用真实 LLM${NC}"
else
    echo -e "${GREEN}✓${NC} LLM_API_KEY: ${LLM_API_KEY:0:10}..."
    echo -e "${GREEN}✓${NC} LLM_MODEL: ${LLM_MODEL:-gpt-4o-mini}"
fi

# 检查存储模式
MILVUS_MODE=${MILVUS_MODE:-memory}
REDIS_MODE=${REDIS_MODE:-memory}

echo -e "${GREEN}✓${NC} MILVUS_MODE: $MILVUS_MODE"
echo -e "${GREEN}✓${NC} REDIS_MODE: $REDIS_MODE"

# 如果使用真实 Milvus/Redis，检查服务是否运行
if [ "$MILVUS_MODE" = "milvus" ] || [ "$REDIS_MODE" = "redis" ]; then
    echo ""
    echo -e "${YELLOW}检测到使用真实存储服务，检查 Docker 服务状态...${NC}"

    if ! command -v docker &> /dev/null; then
        echo -e "${RED}错误: 未找到 Docker，请先安装 Docker${NC}"
        exit 1
    fi

    # 检查 docker-compose 文件
    if [ ! -f "deploy/docker-compose.yaml" ]; then
        echo -e "${RED}错误: 未找到 deploy/docker-compose.yaml${NC}"
        exit 1
    fi

    # 检查服务是否运行
    if [ "$MILVUS_MODE" = "milvus" ]; then
        if ! docker ps | grep -q milvus-standalone; then
            echo -e "${YELLOW}Milvus 未运行，正在启动...${NC}"
            cd deploy && docker-compose up -d milvus-standalone etcd minio && cd ..
            echo -e "${YELLOW}等待 Milvus 启动（30 秒）...${NC}"
            sleep 30
        else
            echo -e "${GREEN}✓${NC} Milvus 已运行"
        fi
    fi

    if [ "$REDIS_MODE" = "redis" ]; then
        if ! docker ps | grep -q redis; then
            echo -e "${YELLOW}Redis 未运行，正在启动...${NC}"
            cd deploy && docker-compose up -d redis && cd ..
            echo -e "${YELLOW}等待 Redis 启动（5 秒）...${NC}"
            sleep 5
        else
            echo -e "${GREEN}✓${NC} Redis 已运行"
        fi
    fi
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}启动服务...${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 启动服务
go run cmd/server/main.go run

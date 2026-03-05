#!/bin/bash

# 加载环境变量
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
fi

echo "=========================================="
echo "🧪 AI Agent API - 第三方模型测试套件"
echo "=========================================="
echo ""

# 测试 1: LLM API
echo "1️⃣  测试 LLM API (对话生成)"
echo "=========================================="
go run test_llm.go
if [ $? -ne 0 ]; then
    echo ""
    echo "❌ LLM 测试失败"
    exit 1
fi
echo ""

# 测试 2: Embedding API
echo "2️⃣  测试 Embedding API (文本向量化)"
echo "=========================================="
go run test_embedding.go
if [ $? -ne 0 ]; then
    echo ""
    echo "❌ Embedding 测试失败"
    exit 1
fi
echo ""

# 测试 3: Rerank API
echo "3️⃣  测试 Rerank API (结果重排序)"
echo "=========================================="
go run test_rerank.go
if [ $? -ne 0 ]; then
    echo ""
    echo "❌ Rerank 测试失败"
    exit 1
fi
echo ""

echo "=========================================="
echo "✅ 所有测试通过！"
echo "=========================================="
echo ""
echo "📊 测试总结:"
echo "  ✓ LLM API (glm-5) - 正常"
echo "  ✓ Embedding API (text-embedding-v4) - 正常"
echo "  ✓ Rerank API (qwen3-vl-rerank) - 正常"
echo ""
echo "🎉 你的 AI Agent API 已准备就绪！"

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go-based AI Agent engine that exposes enterprise API documentation querying via a custom MCP (Model Context Protocol) JSON-RPC HTTP server. Users query API docs in natural language; the agent orchestrates multiple tools to search, retrieve details, and generate examples.

## Build & Test Commands

```bash
make dev      # Start infrastructure (Milvus, etcd, MinIO, Redis) via docker-compose
make run      # go run cmd/server/main.go run
make ingest   # Ingest testdata/petstore.json into knowledge base (supports --service flag)
make test     # go test ./...

# Single package test
go test ./internal/rag/ -v -run TestSearchRanking

# Build check
go build ./...
```

## Architecture

### Request Flow

```
HTTP POST /mcp (JSON-RPC 2.0)
  → mcp.Server (auth → logging → rateLimit → validation)
    → tools.Registry.Dispatch(toolName, args)
      → query_api tool → agent.AgentEngine.Run()
        → LLMClient.Next() loop (up to maxSteps)
          → Dispatches tools: search_api → get_api_detail → generate_example → summarize
```

### Key Layers

- **`cmd/server/`** — Entry point. `runServer` wires all dependencies; `newKnowledgeBase` switches between memory/milvus mode based on `cfg.Milvus.Mode`. Health check at `/healthz`.
- **`internal/mcp/`** — Custom MCP server (no external MCP SDK). Single endpoint `POST /mcp` accepting JSON-RPC 2.0 where `method` = tool name, `params` = tool args.
- **`internal/agent/`** — `AgentEngine` runs a multi-step tool-calling loop. `LLMClient` interface has two implementations: `RuleBasedLLMClient` (deterministic fallback) and `OpenAICompatibleLLMClient` (real LLM via `/v1/chat/completions`).
- **`internal/tools/`** — 8 tools registered via `Registry`. `KnowledgeBase` is the central hub connecting ingestor, RAG engine, and cache.
- **`internal/rag/`** — `Store` interface with `MemoryStore` (keyword matching) and `MilvusStore` (vector search via embedding + Milvus SDK). `Engine` wraps Store with chunking logic.
- **`internal/embedding/`** — `Client` interface: `NoopClient` (zero vectors for memory mode) and `OpenAIClient` (calls `/v1/embeddings`).
- **`internal/store/`** — `MilvusClient` interface: `InMemoryMilvusClient` (dev/test) and `SDKMilvusClient` (real Milvus). `RedisClient` interface: in-memory or go-redis.
- **`internal/knowledge/`** — Swagger 2.0 parser → `Endpoint` structs → chunked into 4 types per endpoint (overview, request, response, dependency).
- **`internal/observability/`** — Prometheus metrics and monitoring.
- **`internal/e2e/`** — End-to-end integration tests.
- **`internal/config/`** — Configuration loading from `config/config.yaml` with env var overrides.

### Dual-Mode Storage

Controlled by `MILVUS_MODE` env var (default `"memory"`):
- **memory** — `MemoryStore` with keyword matching, no external deps needed. Used in tests and local dev.
- **milvus** — `MilvusStore` with `OpenAIClient` embeddings + `SDKMilvusClient`. Requires running Milvus and an embedding API.

## Code Conventions

- All tool implementations satisfy `tools.Tool` interface: `Name()`, `Description()`, `Schema()`, `Execute(ctx, args)`.
- `KnowledgeBase` methods that do I/O take `context.Context` as first parameter.
- Storage interfaces follow the pattern: in-memory implementation for tests, SDK implementation for production, factory function to switch via config mode string.
- Module path is `ai-agent-api` (no domain prefix in go.mod).
- Chinese comments and descriptions are used for user-facing tool descriptions.

## Key Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MILVUS_MODE` | `memory` | `"memory"` or `"milvus"` |
| `MILVUS_ADDRESS` | `localhost:19530` | Milvus server address |
| `REDIS_MODE` | `memory` | `"memory"` or `"redis"` |
| `LLM_API_KEY` | (empty) | OpenAI-compatible API key |
| `LLM_BASE_URL` | (empty) | Custom LLM endpoint (e.g. DeepSeek) |
| `LLM_PROVIDER` | `openai` | LLM provider identifier |
| `LLM_MODEL` | (empty) | Model name (e.g. `gpt-4o-mini`) |
| `LLM_TIMEOUT_SECONDS` | — | LLM request timeout |
| `LLM_MAX_RETRIES` | — | Max retry attempts for LLM calls |
| `LLM_RETRY_BACKOFF_MS` | — | Retry backoff interval in ms |
| `REDIS_ADDRESS` | `127.0.0.1:6379` | Redis server address |
| `AUTH_TOKEN` | (empty) | Bearer token for `/mcp` endpoint |

## Testing the Server

```bash
# Ingest sample data and query
go run cmd/server/main.go ingest --file testdata/petstore.json --service petstore
AUTH_TOKEN=demo-token go run cmd/server/main.go run

# Call the MCP endpoint
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"query_api","params":{"query":"查询用户登录接口"}}'

# Health check
curl http://localhost:8080/healthz
```

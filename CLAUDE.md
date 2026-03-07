# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go-based AI Agent engine that exposes enterprise API documentation querying via a custom MCP (Model Context Protocol) JSON-RPC HTTP server. Users query API docs in natural language; the agent orchestrates multiple tools to search, retrieve details, and generate examples.

## Build & Test Commands

```bash
make dev      # Start infrastructure (Milvus, etcd, MinIO, Redis) via docker-compose
make run      # go run cmd/server/main.go run
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
      → query_api tool → agent.AdaptiveAgentEngine.Run()
        → StrategySelector (simple / complex / ambiguous)
          → QueryRewriter / Planner / Reflector
            → agent.AgentEngine.Run() or direct tool dispatch

HTTP POST /webhook/sync
  → webhook.Handler (signature/token auth)
    → ingest.Service.SyncFiles()
      → tools.KnowledgeBase.IngestBytes()
```

### Key Layers

- **`cmd/server/`** — Entry point. `runServer` wires Redis/Milvus, `AdaptiveAgentEngine`, MCP server and `/webhook/sync` route. Health check at `/healthz`.
- **`internal/mcp/`** — Custom MCP server (no external MCP SDK). Single endpoint `POST /mcp` accepting JSON-RPC 2.0 where `method` = tool name, `params` = tool args.
- **`internal/agent/`** — `AgentEngine` runs the base ReAct loop; `AdaptiveAgentEngine` adds strategy selection, rewriting, planning and reflection. `LLMClient` has `RuleBasedLLMClient` and `OpenAICompatibleLLMClient`.
- **`internal/tools/`** — 8 tools registered via `Registry`. `KnowledgeBase` is the central hub connecting ingestor, RAG engine, and cache.
- **`internal/ingest/`** — Syncs local files / embedded content / remote URLs into the knowledge base.
- **`internal/webhook/`** — Bearer-token / signature protected sync endpoint for GitHub Actions.
- **`internal/rag/`** — `Store` interface with `MemoryStore` (keyword matching) and `MilvusStore` (vector search via embedding + Milvus SDK). `RerankStore` wraps any Store to add reranking capability. `Engine` wraps Store with chunking logic.
- **`internal/embedding/`** — `Client` interface: `NoopClient` (zero vectors for memory mode) and `OpenAIClient` (calls `/v1/embeddings`).
- **`internal/rerank/`** — `Client` interface: `NoopClient` (no reranking) and `DashScopeClient` (calls Alibaba Cloud rerank API).
- **`internal/store/`** — `MilvusClient` interface: `InMemoryMilvusClient` (dev/test) and `SDKMilvusClient` (real Milvus). `RedisClient` interface: in-memory or go-redis.
- **`internal/knowledge/`** — Swagger 2.0 parser → `Endpoint` structs → chunked into 4 types per endpoint (overview, request, response, dependency).
- **`internal/observability/`** — Prometheus metrics and monitoring.
- **`internal/e2e/`** — End-to-end integration tests.
- **`internal/config/`** — Configuration loading from `config/config.yaml` with env var overrides.

### Runtime Storage

Service runtime defaults to real Redis + Milvus. Start dependencies with `make dev` before `make run`.

### Rerank Integration

The system supports optional reranking to improve search accuracy:
- **Two-stage retrieval**: Initial recall (3x topK) → Rerank → Final results (topK)
- **Automatic fallback**: If rerank API fails, returns original search results
- **Configuration**: Set `RERANK_API_KEY` and `RERANK_MODEL` to enable
- **Models supported**: `qwen3-vl-rerank` (multimodal), `qwen3-rerank`, `gte-rerank-v2`

## Code Conventions

- All tool implementations satisfy `tools.Tool` interface: `Name()`, `Description()`, `Schema()`, `Execute(ctx, args)`.
- `KnowledgeBase` methods that do I/O take `context.Context` as first parameter.
- Storage interfaces keep in-memory implementations for tests, while the service runtime uses real Redis/Milvus dependencies.
- Module path is `ai-agent-api` (no domain prefix in go.mod).
- Chinese comments and descriptions are used for user-facing tool descriptions.

## Key Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MILVUS_ADDRESS` | `localhost:19530` | Milvus server address |
| `LLM_API_KEY` | (empty) | OpenAI-compatible API key |
| `LLM_BASE_URL` | (empty) | Custom LLM endpoint (e.g. DeepSeek) |
| `LLM_PROVIDER` | `openai` | LLM provider identifier |
| `LLM_MODEL` | (empty) | Model name (e.g. `gpt-4o-mini`) |
| `LLM_TIMEOUT_SECONDS` | — | LLM request timeout |
| `LLM_MAX_RETRIES` | — | Max retry attempts for LLM calls |
| `LLM_RETRY_BACKOFF_MS` | — | Retry backoff interval in ms |
| `EMBEDDING_API_KEY` | (empty) | Embedding API key (falls back to LLM_API_KEY) |
| `EMBEDDING_BASE_URL` | (empty) | Embedding API endpoint |
| `EMBEDDING_MODEL` | `bge-large-zh-v1.5` | Embedding model name |
| `EMBEDDING_DIM` | `1024` | Embedding vector dimension |
| `RERANK_API_KEY` | (empty) | Rerank API key (falls back to EMBEDDING_API_KEY) |
| `RERANK_BASE_URL` | (empty) | Rerank API endpoint |
| `RERANK_MODEL` | `qwen3-vl-rerank` | Rerank model name |
| `REDIS_ADDRESS` | `127.0.0.1:6379` | Redis server address |
| `AUTH_TOKEN` | (empty) | Bearer token for `/mcp` endpoint |
| `WEBHOOK_SECRET` | (empty) | GitHub webhook HMAC secret for `/webhook/sync` |

## Testing the Server

```bash
# Start server (auto-loads testdata/petstore.json when present)
AUTH_TOKEN=demo-token go run cmd/server/main.go run

# Optional: ingest a custom spec during runtime
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"parse_swagger","params":{"file_path":"testdata/petstore.json","service":"petstore"}}'

# Call the MCP endpoint
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"query_api","params":{"query":"查询用户登录接口"}}'

# Health check
curl http://localhost:8080/healthz

# Webhook sync
curl -X POST http://localhost:8080/webhook/sync \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"files":[{"path":"docs/api/user-service.json","content":"{\"swagger\":\"2.0\",\"info\":{\"title\":\"User Service\",\"version\":\"1.0.0\"},\"paths\":{}}"}]}'

# Prometheus metrics
curl http://localhost:8080/metrics
```

## Resilience Features

- **Circuit Breaker** — `internal/resilience/circuitbreaker.go` implements state machine (Closed → Open → HalfOpen) to prevent cascading failures when external services (LLM, Milvus, Redis) are down.
- **Rate Limiting** — Token bucket algorithm in `internal/mcp/middleware.go` limits requests per IP/token to prevent abuse.
- **Request ID Tracking** — Every request gets a unique ID for distributed tracing across middleware and tools.
- **LLM Degradation** — When LLM API is unavailable or not configured, system automatically falls back to `RuleBasedLLMClient` for deterministic responses.

## Response Format

`query_api` returns both human-readable summary and structured trace:

```json
{
  "summary": "找到登录接口 POST /user/login...",
  "trace": [
    {"step": 1, "tool": "search_api", "input": {...}, "output": {...}},
    {"step": 2, "tool": "get_api_detail", "input": {...}, "output": {...}}
  ]
}
```

The `trace` array enables observability and debugging of agent decision flow.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `connection refused` to Milvus/Redis | Run `make dev` to start infrastructure |
| LLM timeout | Increase `LLM_TIMEOUT_SECONDS` or check API key |
| Empty search results | Verify default petstore bootstrap or ingest via `parse_swagger` / `/webhook/sync` |
| `401 Unauthorized` | Set `AUTH_TOKEN` environment variable |
| `connect: connection refused` to Milvus/Redis | Run `make dev` first |
| Circuit breaker open | Check `/healthz` endpoint for service status |

## Documentation

- `docs/design.md` — Full architecture and design decisions
- `docs/local-setup-guide.md` — Step-by-step setup instructions
- `docs/resilience-implementation.md` — Circuit breaker and retry patterns
- `QUICKSTART.md` — Quick reference card for common commands
- `.env.example` — Environment variable templates

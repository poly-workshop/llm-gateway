# Agent Notes (Living Doc)

This file is the **single source of truth** for AI/Agent summaries and implementation conventions. Please keep updating this file (instead of `README.md`) as the project evolves.

## Proto layout (buf best practices)

- Protos live under `proto/`
- Package names are versioned (e.g. `llmgateway.v1`)
- Directory structure follows package/version:
  - `proto/llmgateway/v1/*.proto`

Generated artifacts (gRPC / gRPC-Gateway / OpenAPI) are written to `gen/` (see `buf.gen.yaml`).

## Runtime layout (current)

This repo is intentionally split into **two binaries**:

- **HTTP data-plane gateway**: `cmd/llm-gateway-http`
  - Listens on `:8080` by default (`http.listen`)
  - Serves the API via gRPC-Gateway **HandlerServer** (no gRPC dial / proxy)
- **gRPC admin control-plane**: `cmd/llm-gateway-admin-grpc`
  - Listens on `:50051` by default (`grpc.listen`)

## Clean architecture layout (application / domain / infrastructure)

We organize runtime code under `internal/` using clean-architecture layers:

- **Domain**: `internal/domain/...`
  - Pure business types & errors, no transport/codegen deps
  - Example: `internal/domain/llm` (models, embeddings, domain errors)
- **Application**: `internal/application/...`
  - Use-cases / orchestration, depends only on `domain` (and stdlib)
  - Example: `internal/application/llmgateway` (ListModels/GetModel/CreateEmbeddings)
- **Infrastructure**: `internal/infrastructure/...`
  - Adapters & IO (gRPC server, gRPC-Gateway HTTP server, config loading, health handlers)
  - Examples:
    - gRPC adapter (protobuf ↔ domain): `internal/infrastructure/transport/grpcadapter`
    - HTTP gateway (gRPC-Gateway): `internal/infrastructure/server/httpgateway`
    - Admin gRPC server wiring: `internal/infrastructure/server/adminserver`
    - Config: `internal/infrastructure/config`
    - Health: `internal/infrastructure/health`

Dependency direction: **infrastructure → application → domain**.
`cmd/*` should stay thin and only do wiring (init, load config, start servers).

### Health checks

Health is not modeled in proto. We use plain HTTP handlers:

- `/livez`
- `/readyz`

In the HTTP gateway process, `/readyz` is a simple process-level readiness check (no gRPC dial).

## Config conventions (dev-first TOML)

We prefer **TOML** for development configs, while keeping the option to use YAML in environments like Kubernetes.

- Default config directory: `./configs`
- Override config directory via env: `CONFIG_PATH=/path/to/configs`
- Env overlay mode: `MODE=development|...` (see `go-webmods/app`)

Layered config files (loaded by `go-webmods/app`):

- `default.*`
- `<cmd>/default.*`
- `<MODE>.*` (optional)
- `<cmd>/<MODE>.*` (optional)

Current default configs:

- `configs/default.toml`
- `configs/llm-gateway-http/default.toml`
- `configs/llm-gateway-admin-grpc/default.toml`

## Config loading (split by binary)

Config types are intentionally **different** per process:

- HTTP gateway uses `internal/infrastructure/config.LoadHTTP()` (expects `http.listen` + `llm.*` + `auth.*`)
- Admin gRPC uses `internal/infrastructure/config.LoadAdminGRPC()` (expects `grpc.listen` + `auth.admin.*` + `auth.jwt_signing.*`)

## go-webmods integration

We use `github.com/poly-workshop/go-webmods@v0.4.2`:

- `app.InitWithConfigPath(cmdName, configPath)` for config + logging initialization
- `grpcutils.BuildRequestIDInterceptor()` and `grpcutils.BuildLogInterceptor(...)` for gRPC unary interceptors

## Dynamic config (DB-backed): Storage backend

LLM Gateway 本身无状态可横向扩容。Dynamic configuration (e.g., LLM provider configs, model catalog) is stored in a shared backend.

- `storage.backend = "gorm" | "mongodb"`
  - `gorm`: shared in a relational DB (Postgres/MySQL/SQLite)
  - `mongodb`: shared in MongoDB

### GORM backend

Config keys:

- `storage.gorm.driver` (`postgres|mysql|sqlite`)
- `storage.gorm.host`
- `storage.gorm.port`
- `storage.gorm.username`
- `storage.gorm.password`
- `storage.gorm.dbname`
- `storage.gorm.sslmode` (postgres)

### MongoDB backend

Config keys:

- `storage.mongodb.uri`
- `storage.mongodb.database`
- `storage.mongodb.collection` (default: `llm_gateway`)

## Codegen import path convention (conservative)

We keep Go generated code under `gen/go/`.
Each proto sets `option go_package` to match its generated directory under `gen/go/` to avoid import-path drift.

## Minimal API surface (current)

All HTTP endpoints are exposed via gRPC-Gateway annotations on `LLMGatewayService`:

- **Models**
  - `GET /v1/models` → `ListModels`
  - `GET /v1/models/{id}` → `GetModel`
- **Chat Completions**
  - `POST /v1/chat/completions` → `CreateChatCompletion`
    - Supports `stream=true` parameter for streaming responses (OpenAI-compatible SSE format)
    - Note: Streaming is not yet implemented and returns 501 Not Implemented
  - ~~`POST /v1/chat/completions:stream`~~ → **DEPRECATED and removed** (returns 404)
- **Embeddings**
  - `POST /v1/embeddings` → `CreateEmbeddings`
- **Generation (usage query)**
  - `GET /v1/generation/{id}` → `GetGeneration`

OpenAPI is emitted as a single merged swagger:

- `gen/openapi/llmgateway.swagger.json`

## LLM providers (current)

### DashScope (阿里云百炼) - OpenAI compatible mode

Provider implementation: `internal/infrastructure/llmprovider/dashscope`

Supported capabilities:

- **Chat Completions (non-stream)**: implemented and routed via `CreateChatCompletion`
- **Embeddings**: implemented and routed via `CreateEmbeddings`
- **Chat Completions (stream)**: still **unimplemented** (use `stream=true` on standard endpoint)

Config:

- Provider config is managed via Admin API (stored centrally in DB).

### OpenRouter - Multi-model gateway

Provider implementation: `internal/infrastructure/llmprovider/openrouter`

OpenRouter is a unified API gateway that provides access to multiple LLM providers (OpenAI, Anthropic, Google, etc.) through a single OpenAI-compatible API.

Supported capabilities:

- **Chat Completions (non-stream)**: implemented and routed via `CreateChatCompletion`
- **Embeddings**: implemented and routed via `CreateEmbeddings`
- **Chat Completions (stream)**: still **unimplemented** (use `stream=true` on standard endpoint)

Config:

- Provider config is managed via Admin API (stored centrally in DB).

Model catalog is managed via Admin API (stored centrally in DB).

### OpenAI Wire Types (Shared)

Both DashScope and OpenRouter providers use OpenAI-compatible wire protocols for HTTP requests and responses. To reduce duplication and improve maintainability, all OpenAI wire types are defined in a shared package:

**Package**: `internal/infrastructure/llmprovider/openaiwire`

**Files**:
- `chat.go`: Chat completion request/response types (Message, ContentPart, ImageURL, Usage, Choice, ChatCompletionRequest, ChatCompletionResponse, etc.)
- `embeddings.go`: Embeddings request/response types (EmbeddingRequest, EmbeddingResponse, EmbeddingDatum, EmbeddingUsage)

**Key principles**:
- Wire types are strictly isolated from domain types (`internal/domain/llm`)
- Providers must explicitly convert between wire ↔ domain types
- Wire types follow OpenAI's official schema for JSON encoding/decoding
- All providers using OpenAI-compatible endpoints should use these shared types

This design prevents protocol drift, reduces maintenance burden, and ensures consistency when adding new providers or protocol features (e.g., tool calls, streaming).

### Model routing convention

- Gateway-facing model IDs are `provider/model`, e.g. `dashscope/qwen-turbo`, `openrouter/openai/gpt-4o`
- The `provider` prefix selects the upstream implementation; the `model` suffix is sent upstream as `model` (unless overridden)
- Optional upstream override via Admin-managed model field `upstream_model`
  - (No billing-related fields are modeled.)

### Admin config API conventions (control-plane)

- Provider is modeled as a **proto enum** `ProviderType` (closed set):
  - `PROVIDER_TYPE_DASHSCOPE` → `dashscope`
  - `PROVIDER_TYPE_OPENROUTER` → `openrouter`
- Model upsert input is **simplified**: clients provide `ModelConfig { provider, upstream_model, capabilities }`.
  - The server computes the routed model ID as `"<provider>/<upstream_model>"` (trims surrounding `/` on `upstream_model`).
  - Model `name` is derived (not part of the control-plane input).
  - `UpsertModelResponse` returns the computed `id`.
  - `capabilities` is modeled as a **proto enum** `ModelCapability` (closed set) and mapped to stored string values:
    - `MODEL_CAPABILITY_TEXT` → `text`
    - `MODEL_CAPABILITY_IMAGES` → `images`
    - `MODEL_CAPABILITY_AUDIO` → `audio`
    - `MODEL_CAPABILITY_VIDEO` → `video`
    - `MODEL_CAPABILITY_TOOLS` → `tools`
    - `MODEL_CAPABILITY_PROMPT_CACHE` → `prompt_cache`
    - `MODEL_CAPABILITY_STREAMING` → `streaming`
    - `MODEL_CAPABILITY_REASONING` → `reasoning`

### GenerationRepository (interface only)

The `GenerationRepository` interface is defined in `internal/application/llmgateway/ports.go`:

```go
type GenerationRepository interface {
    Save(ctx context.Context, gen llm.Generation) error
    Get(ctx context.Context, id string) (llm.Generation, error)
}
```

**Note:** Currently no concrete implementation is provided. Pass `nil` to skip generation storage (generation queries will fail). Implement in-memory or database storage as needed.

## Health check (not in proto)

Health is not modeled as a proto service. Prefer plain HTTP handlers (e.g. `/livez`, `/readyz`).

## buf usage (deps & codegen)

Update remote proto dependencies (written to `buf.lock`):

```bash
buf dep update
```

Generate code (gRPC / gRPC-Gateway / OpenAPI):

```bash
buf generate
```

If you need to **reset** the `gen/` directory, use:

```bash
buf generate --clean
```

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

- **gRPC server**: `cmd/llm-gateway-grpc`
  - Listens on `:50051` by default (`grpc.listen`)
  - Exposes health endpoints on a dedicated HTTP port `:8081` by default (`health.listen`)
- **HTTP gateway**: `cmd/llm-gateway-http`
  - Listens on `:8080` by default (`http.listen`)
  - Proxies to gRPC via gRPC-Gateway dial target `127.0.0.1:50051` by default (`grpc.target`)

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
    - gRPC server wiring: `internal/infrastructure/server/grpcserver`
    - HTTP gateway (gRPC-Gateway): `internal/infrastructure/server/httpgateway`
    - Config: `internal/infrastructure/config`
    - Health: `internal/infrastructure/health`

Dependency direction: **infrastructure → application → domain**.
`cmd/*` should stay thin and only do wiring (init, load config, start servers).

### Health checks

Health is not modeled in proto. We use plain HTTP handlers:

- `/livez`
- `/readyz`

In the HTTP gateway process, `/readyz` performs a short gRPC dial check against `grpc.target`.

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
- `configs/llm-gateway-grpc/default.toml`
- `configs/llm-gateway-http/default.toml`

## Config loading (split by binary)

Config types are intentionally **different** per process:

- gRPC server uses `internal/infrastructure/config.LoadGRPC()` (expects `grpc.listen` + `health.listen` + `llm.*` for provider wiring)
- HTTP gateway uses `internal/infrastructure/config.LoadHTTP()` (expects `http.listen` + `grpc.target` + `grpc.insecure`)

## go-webmods integration

We use `github.com/poly-workshop/go-webmods@v0.4.2`:

- `app.InitWithConfigPath(cmdName, configPath)` for config + logging initialization
- `grpcutils.BuildRequestIDInterceptor()` and `grpcutils.BuildLogInterceptor(...)` for gRPC unary interceptors

## Codegen import path convention (conservative)

We keep Go generated code under `gen/go/`.
All protos use `option go_package = "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/v1;llmgatewayv1"` to avoid import-path drift.

## Minimal API surface (current)

All HTTP endpoints are exposed via gRPC-Gateway annotations on `LLMGatewayService`:

- **Models**
  - `GET /v1/models` → `ListModels`
  - `GET /v1/models/{id}` → `GetModel`
- **Chat Completions**
  - `POST /v1/chat/completions` → `CreateChatCompletion`
  - `POST /v1/chat/completions:stream` → `CreateChatCompletionStream`（server-streaming）
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
- **Chat Completions (stream)**: still **unimplemented** (`CreateChatCompletionStream`)

Config keys:

- `llm.providers.dashscope.base_url` (default: `https://dashscope.aliyuncs.com/compatible-mode/v1`)
- `llm.providers.dashscope.api_key` (required for real upstream calls)
- `llm.providers.dashscope.timeout` (e.g. `20s`)

### OpenRouter - Multi-model gateway

Provider implementation: `internal/infrastructure/llmprovider/openrouter`

OpenRouter is a unified API gateway that provides access to multiple LLM providers (OpenAI, Anthropic, Google, etc.) through a single OpenAI-compatible API.

Supported capabilities:

- **Chat Completions (non-stream)**: implemented and routed via `CreateChatCompletion`
- **Embeddings**: implemented and routed via `CreateEmbeddings`
- **Chat Completions (stream)**: still **unimplemented** (`CreateChatCompletionStream`)

Config keys:

- `llm.providers.openrouter.base_url` (default: `https://openrouter.ai/api/v1`)
- `llm.providers.openrouter.api_key` (required for real upstream calls)
- `llm.providers.openrouter.timeout` (default: `60s`, longer due to potential routing latency)

Example model config (using `upstream_model` for OpenRouter's `provider/model` format):

```toml
[[llm.models]]
id = "openrouter/openai/gpt-4o"
name = "GPT-4o (OpenRouter)"
provider = "openrouter"
upstream_model = "openai/gpt-4o"
capabilities = ["chat"]
```

### Model routing convention

- Gateway-facing model IDs are `provider/model`, e.g. `dashscope/qwen-turbo`, `openrouter/openai/gpt-4o`
- The `provider` prefix selects the upstream implementation; the `model` suffix is sent upstream as `model` (unless overridden)
- Optional upstream override via config field `llm.models[].upstream_model`
- `llm.models[]` (static model catalog served by `ListModels`)
  - (No billing-related fields are modeled.)

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


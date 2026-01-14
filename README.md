# LLM Gateway

An OpenRouter-like LLM Gateway exposed via **HTTP (gRPC-Gateway HandlerServer)**, with **buf** managing protobuf modules and code generation.

Project implementation notes and AI/Agent summaries are maintained in `Agent.md`.

## Run

This repo provides two binaries:

- **HTTP data-plane gateway**: `cmd/llm-gateway-http` (default `:8080`)
- **gRPC admin control-plane**: `cmd/llm-gateway-admin-grpc` (default `:50051`)

## Docker images

This repo publishes **two** container images (one per binary):

- `ghcr.io/poly-workshop/llm-gateway-http`
- `ghcr.io/poly-workshop/llm-gateway-admin-grpc`

They include `configs/` in the image and default `CONFIG_PATH=/app/configs`.

### Build locally

```bash
docker build --build-arg APP=llm-gateway-http -t llm-gateway-http:dev .
docker build --build-arg APP=llm-gateway-admin-grpc -t llm-gateway-admin-grpc:dev .
```

### Release (publish to GHCR)

Pushing a git tag matching `v*` triggers GitHub Actions to build and push both images.

```bash
git tag v0.1.0
git push origin v0.1.0
```

### Config

By default (dev), configs are **TOML** under `./configs/`.

In Kubernetes you can still use **YAML** by mounting a config directory and setting `CONFIG_PATH` to that directory (examples in `deployments/k8s/configs/`).

Configs are loaded via `go-webmods/app` layered config from `CONFIG_PATH` (default: `configs`):

- `default.(toml|yaml|json|...)`
- `<cmd>/default.(toml|yaml|json|...)`
- `<MODE>.(toml|yaml|json|...)` (optional)
- `<cmd>/<MODE>.(toml|yaml|json|...)` (optional)

JWT Ed25519 keys are configured via PEM files (recommended for secrets):

- Admin gRPC signer: `auth.jwt_signing.private_key_file`
- HTTP gateway verifier: `auth.jwt.public_key_file`

### HTTP gateway

```bash
go run ./cmd/llm-gateway-http
```

### Admin gRPC

```bash
go run ./cmd/llm-gateway-admin-grpc
```

## Model Configuration

Models are configured via the admin gRPC service and stored in a database (PostgreSQL, MySQL, SQLite via GORM, or MongoDB).

### Max Output Tokens Limits

Each model can be configured with a `max_output_tokens` limit to prevent requests from exceeding a specific token count. This is useful for:

- **Cost control**: Limiting token usage per model
- **Rate limiting**: Preventing excessive resource consumption
- **Provider compliance**: Enforcing upstream provider limits

#### Configuring max_output_tokens

When upserting a model via the admin gRPC API:

```protobuf
message ModelConfig {
  ProviderType provider = 1;
  repeated ModelCapability capabilities = 2;
  string upstream_model = 3;
  uint32 max_output_tokens = 4;  // Optional: 0 = no limit
}
```

Example using grpcurl:

```bash
grpcurl -plaintext -d '{
  "model": {
    "provider": "PROVIDER_TYPE_DASHSCOPE",
    "upstream_model": "qwen-turbo",
    "capabilities": ["MODEL_CAPABILITY_TEXT"],
    "max_output_tokens": 2000
  }
}' localhost:50051 llmgateway.admin.v1.LLMGatewayAdminService/UpsertModel
```

#### Request Validation

When a request is made to `/v1/chat/completions`:

1. **If `max_tokens` exceeds the model's limit**: The request is rejected with an OpenAI-compatible `invalid_request_error`:
   ```json
   {
     "error": {
       "message": "max_tokens (3000) exceeds model limit (2000)",
       "type": "invalid_request_error"
     }
   }
   ```

2. **If `max_tokens` is not provided**: The request is allowed and the upstream provider's default is used (following OpenAI behavior).

3. **If `max_output_tokens` is 0 or not configured**: No limit is enforced.


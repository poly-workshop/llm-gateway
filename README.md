# LLM Gateway

An OpenRouter-like LLM Gateway exposed via **gRPC + gRPC-Gateway**, with **buf** managing protobuf modules and code generation.

Project implementation notes and AI/Agent summaries are maintained in `Agent.md`.

## Run

This repo provides two binaries:

- **gRPC server**: `cmd/llm-gateway-grpc` (default `:50051`, health `:8081`)
- **HTTP gateway**: `cmd/llm-gateway-http` (default `:8080`, proxies to `127.0.0.1:50051`)

### Config

By default (dev), configs are **TOML** under `./configs/`.

In Kubernetes you can still use **YAML** by mounting a config directory and setting `CONFIG_PATH` to that directory (examples in `deployments/k8s/configs/`).

Configs are loaded via `go-webmods/app` layered config from `CONFIG_PATH` (default: `configs`):

- `default.(toml|yaml|json|...)`
- `<cmd>/default.(toml|yaml|json|...)`
- `<MODE>.(toml|yaml|json|...)` (optional)
- `<cmd>/<MODE>.(toml|yaml|json|...)` (optional)

### gRPC server

```bash
go run ./cmd/llm-gateway-grpc
```

### HTTP gateway

```bash
go run ./cmd/llm-gateway-http
```

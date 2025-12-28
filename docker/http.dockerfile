## HTTP gateway image.
##
## Example:
##   docker build -f docker/http.dockerfile -t llm-gateway-http:dev .

ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/llm-gateway-http ./cmd/llm-gateway-http


FROM gcr.io/distroless/base-debian12:nonroot AS runtime

WORKDIR /app

ENV CONFIG_PATH=/app/configs

COPY --from=build /out/llm-gateway-http /app/llm-gateway-http
COPY --from=build /src/configs /app/configs

EXPOSE 8080

ENTRYPOINT ["/app/llm-gateway-http"]

# syntax=docker/dockerfile:1

# --- converter stage ---
# Stage isolado para que a conversão de 12 min fique em cache separado do código.
# Só re-executa quando resources/references.json.gz ou cmd/convert/ mudam.
FROM golang:1.25-alpine AS converter

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY resources/references.json.gz ./resources/
COPY cmd/ ./cmd/
RUN go run cmd/convert/main.go

# --- build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
# Cache mount: módulos baixados persistem entre builds — go mod download roda
# só quando go.mod/go.sum mudam, não a cada docker build.
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# Copia o .bin gerado pelo converter, sobrescrevendo qualquer versão local.
COPY --from=converter /app/resources/references.bin ./resources/references.bin

# Cache mount: o build cache do Go persiste entre builds — recompila apenas
# pacotes que mudaram, em vez de recompilar tudo do zero.
# CGO_ENABLED=0 garante binário estático; GOARCH=amd64 exigido pela Rinha.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o api .

# --- final stage ---
# alpine em vez de scratch para ter wget disponível no HEALTHCHECK.
FROM alpine

COPY --from=builder /app/api /api

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9999/ready || exit 1

EXPOSE 9999
ENTRYPOINT ["/api"]

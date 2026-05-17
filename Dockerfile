# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Converte references.json.gz → references.bin (binário flat, sem parser JSON).
# Isso acontece uma vez no build; o binário final embute o .bin já convertido.
RUN go run cmd/convert/main.go
# CGO_ENABLED=0 garante binário estático — sem dependência de libc no container final.
# GOARCH=amd64 exigido pela spec da Rinha (linux/amd64).
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o api .

# --- final stage ---
# alpine em vez de scratch para ter wget disponível no HEALTHCHECK.
# scratch seria menor, mas o Docker não consegue executar o probe sem shell/binários.
FROM alpine

COPY --from=builder /app/api /api

# O Docker executa esse probe periodicamente; falha 3x consecutivas → container unhealthy.
# O nginx só inicia após api1 e api2 ficarem healthy (ver docker-compose.yml).
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9999/ready || exit 1

EXPOSE 9999
ENTRYPOINT ["/api"]

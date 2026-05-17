# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 garante binário estático — sem dependência de libc no container final.
# GOARCH=amd64 exigido pela spec da Rinha (linux/amd64).
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o api .

# --- final stage ---
# scratch = imagem vazia; só o binário vai para o container, sem shell, sem libs.
FROM scratch

COPY --from=builder /app/api /api

EXPOSE 9999
ENTRYPOINT ["/api"]

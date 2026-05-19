CONCURRENCY ?= 4
REQUESTS    ?= 200

# ── Desenvolvimento local ──────────────────────────────────────────────────────

# Gera resources/references.bin a partir do references.json.gz.
# Executar uma vez antes de `make run` ou `make up`.
convert:
	go run cmd/convert/main.go

run:
	air

build:
	go build -o bin/api ./...

test:
	go test ./...

# ── Docker (load balancer nginx + 2 instâncias) ────────────────────────────────

docker-build:
	docker compose up --build -d

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

# ── Testes contra :9999 (funciona com `make run` ou `make up`) ────────────────

test-payloads:
	@bash scripts/test-payloads.sh

bench:
	@CONCURRENCY=$(CONCURRENCY) REQUESTS=$(REQUESTS) bash scripts/bench.sh

# ── Testes oficiais k6 (requer k6 instalado) ──────────────────────────────────

# Baixa o dataset de teste oficial (~26 MB) caso ainda não exista.
test/test-data.json:
	@echo "Baixando test-data.json (~26 MB)..."
	@curl -fL --progress-bar \
		https://raw.githubusercontent.com/zanfranceschi/rinha-de-backend-2026/main/test/test-data.json \
		-o test/test-data.json

smoke:
	k6 run test/smoke.js

test-k6: test/test-data.json
	k6 run test/test.js

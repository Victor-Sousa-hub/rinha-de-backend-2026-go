CONCURRENCY ?= 4
REQUESTS    ?= 200

# ── Desenvolvimento local ──────────────────────────────────────────────────────

run:
	air

build:
	go build -o bin/api ./...

test:
	go test ./...

# ── Docker (load balancer nginx + 2 instâncias) ────────────────────────────────

up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f

# ── Testes contra :9999 (funciona com `make run` ou `make up`) ────────────────

test-payloads:
	@bash scripts/test-payloads.sh

bench:
	@CONCURRENCY=$(CONCURRENCY) REQUESTS=$(REQUESTS) bash scripts/bench.sh

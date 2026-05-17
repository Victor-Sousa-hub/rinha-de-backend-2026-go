CONCURRENCY ?= 4
REQUESTS    ?= 200

run:
	air

build:
	go build -o bin/api ./...

test-payloads:
	@bash scripts/test-payloads.sh

test-payloads-verbose:
	@FRAUD_SCORE_URL="http://localhost:9999/fraud-score?verbose=true" bash scripts/test-payloads.sh

test-verbose:
	@jq '.[0]' resources/example-payloads.json \
		| curl -s -X POST "http://localhost:9999/fraud-score?verbose=true" \
		  -H 'Content-Type: application/json' -d @- \
		| jq .

bench:
	@CONCURRENCY=$(CONCURRENCY) REQUESTS=$(REQUESTS) bash scripts/bench.sh

test:
	go test ./...

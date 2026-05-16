run:
	air

build:
	go build -o bin/api ./...

test-payloads:
	@bash scripts/test-payloads.sh

test:
	go test ./...

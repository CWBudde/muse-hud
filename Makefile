.PHONY: build test lint run fmt fixture

build:
	go build ./...
	mkdir -p build
	go build -o build/muse-hud ./cmd/muse-hud

test:
	go test ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l cmd internal testdata)" || { echo "gofmt needed:"; gofmt -l cmd internal testdata; exit 1; }

fmt:
	gofmt -w cmd internal testdata

run:
	go run ./cmd/muse-hud print

fixture:
	go run ./testdata/genfixture -out testdata/fixtures/synthetic-session.jsonl

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BINARY  := agentanycast-relay
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build build-web clean test bench lint

all: build

## build-web: Build the frontend SPA and copy into Go embed directory.
build-web:
	cd web && npm ci --silent && npm run build
	cp web/src/index.html web/dist/
	rm -rf internal/api/web
	mkdir -p internal/api/web
	cp web/dist/* internal/api/web/

## build: Build the relay binary (includes embedded web UI).
build: build-web
	go build $(LDFLAGS) -o $(BINARY) ./cmd/relay

## test: Run all tests.
test:
	go test ./...

## bench: Run all benchmarks.
bench:
	go test -bench=. -benchmem -count=3 ./...

## lint: Run golangci-lint.
lint:
	golangci-lint run

## clean: Remove build artifacts.
clean:
	rm -f $(BINARY)
	rm -rf web/dist web/node_modules
	rm -rf internal/api/web

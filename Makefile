BIN := bin/kubectl-traefik
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet fmt install snapshot clean

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/kubectl-traefik

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

install: build
	install -m 0755 $(BIN) $(HOME)/.local/bin/kubectl-traefik

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist

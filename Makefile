VERSION ?= $(shell git describe --tags --always --dirty)

.PHONY: build test check snapshot

build:
	go build -ldflags "-X main.version=$(VERSION)" -o stower ./cmd/stower

test:
	go test ./...

check:
	@files=$$(gofmt -l .); if [ -n "$$files" ]; then echo "gofmt needed:"; echo "$$files"; exit 1; fi
	go vet ./...
	go build ./...
	go test ./...

snapshot:
	@command -v goreleaser >/dev/null || { echo "goreleaser is not on PATH: install it from https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

VERSION ?= $(shell git describe --tags --always --dirty)

.PHONY: build test check demo snapshot

build:
	go build -ldflags "-X main.version=$(VERSION)" -o stower ./cmd/stower

test:
	go test ./...

check:
	@files=$$(gofmt -l .); if [ -n "$$files" ]; then echo "gofmt needed:"; echo "$$files"; exit 1; fi
	go vet ./...
	go build ./...
	go test ./...

demo: build
	@command -v vhs >/dev/null || { echo "vhs is not on PATH: install it from https://github.com/charmbracelet/vhs"; exit 1; }
	PATH="$(CURDIR):$$PATH" vhs docs/assets/demo.tape

snapshot:
	@command -v goreleaser >/dev/null || { echo "goreleaser is not on PATH: install it from https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean

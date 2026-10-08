.PHONY: dev

GOBIN := $(shell go env GOPATH 2>/dev/null)/bin
AIR := $(shell command -v air 2>/dev/null || echo $(GOBIN)/air)

dev:
	@command -v go > /dev/null 2>&1 || { echo "Error: go is not installed."; echo "Install it from https://go.dev/dl/"; exit 1; }
	@command -v air > /dev/null 2>&1 || [ -x "$(GOBIN)/air" ] || { echo "Error: air is not installed."; echo "Install it with:"; echo "  go install github.com/air-verse/air@latest"; exit 1; }
	$(AIR)

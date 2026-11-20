GO ?= go
WASM_EXEC := $(shell $(GO) env GOROOT)/lib/wasm/wasm_exec.js

.PHONY: build test lint generate site serve

build:
	$(GO) build -trimpath -o sphinx ./cmd/sphinx

test:
	$(GO) test -race ./...

lint:
	test -z "$$(gofmt -l .)"
	$(GO) vet ./...

generate:
	$(GO) generate ./...

# The website runs Sphinx compiled to WebAssembly in the visitor's browser.
site:
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags="-s -w" -o site/sphinx.wasm ./cmd/sphinx-wasm
	cp "$(WASM_EXEC)" site/wasm_exec.js
	cp assets/logo.svg site/logo.svg

serve: site
	cd site && python3 -m http.server 8000

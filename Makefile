# CelikPanel build. Uses the vendored toolchains in .bin/ when present,
# otherwise falls back to whatever go/node are on PATH.
# CelikPanel derlemesi. Varsa .bin/ altındaki gömülü araç zincirlerini,
# yoksa PATH'teki go/node'u kullanır.

GO      ?= $(shell [ -x .bin/go/bin/go ] && echo $(PWD)/.bin/go/bin/go || echo go)
NPM     ?= $(shell [ -x .bin/node/bin/npm ] && echo $(PWD)/.bin/node/bin/npm || echo npm)
NODEDIR := $(PWD)/.bin/node/bin
LDFLAGS := -s -w

.PHONY: all build panel agent web clean

all: build

build: panel agent web ## Build binaries and frontend

panel: ## Build the panel binary
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel

agent: ## Build the agent binary
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

web: ## Build the frontend (web/dist)
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) ci --no-audit --no-fund 2>/dev/null || (cd web && PATH="$(NODEDIR):$$PATH" $(NPM) install --no-audit --no-fund)
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) run build

clean: ## Remove build outputs
	rm -rf bin web/dist

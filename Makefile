# CelikPanel build. Uses the vendored toolchains in .bin/ when present,
# otherwise falls back to whatever go/node are on PATH.
# CelikPanel derlemesi. Varsa .bin/ altındaki gömülü araç zincirlerini,
# yoksa PATH'teki go/node'u kullanır.

GO      ?= $(shell [ -x .bin/go/bin/go ] && echo $(PWD)/.bin/go/bin/go || echo go)
NPM     ?= $(shell [ -x .bin/node/bin/npm ] && echo $(PWD)/.bin/node/bin/npm || echo npm)
NODEDIR := $(PWD)/.bin/node/bin
LDFLAGS := -s -w

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST    := celikpanel-$(VERSION)

.PHONY: all build panel agent web clean dist

all: build

build: panel agent web ## Build binaries and frontend

panel: ## Build the panel binary
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel

agent: ## Build the agent binary
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

web: ## Build the frontend (web/dist)
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) ci --no-audit --no-fund 2>/dev/null || (cd web && PATH="$(NODEDIR):$$PATH" $(NPM) install --no-audit --no-fund)
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) run build

dist: build ## Assemble a self-contained release tarball (no toolchain needed on target)
	# Mirror the repo layout (bin/, web/dist/, deploy/systemd/) so install.sh
	# runs identically from a checkout or from an extracted release.
	# install.sh'nin bir checkout'tan da açılmış release'ten de aynı çalışması
	# için depo düzenini (bin/, web/dist/, deploy/systemd/) yansıt.
	rm -rf dist/$(DIST)
	mkdir -p dist/$(DIST)/bin dist/$(DIST)/web/dist dist/$(DIST)/deploy
	cp bin/panel bin/agent dist/$(DIST)/bin/
	cp -r web/dist/. dist/$(DIST)/web/dist/
	cp -r deploy/systemd dist/$(DIST)/deploy/
	cp install.sh Makefile dist/$(DIST)/
	tar -czf dist/$(DIST).tar.gz -C dist $(DIST)
	rm -rf dist/$(DIST)
	@echo "→ dist/$(DIST).tar.gz"

clean: ## Remove build outputs
	rm -rf bin web/dist dist

# CelikPanel build. Uses the vendored toolchains in .bin/ when present,
# otherwise falls back to whatever go/node are on PATH.
# CelikPanel derlemesi. Varsa .bin/ altındaki gömülü araç zincirlerini,
# yoksa PATH'teki go/node'u kullanır.

GO      ?= $(shell [ -x .bin/go/bin/go ] && echo $(PWD)/.bin/go/bin/go || echo go)
NPM     ?= $(shell [ -x .bin/node/bin/npm ] && echo $(PWD)/.bin/node/bin/npm || echo npm)
NODEDIR := $(PWD)/.bin/node/bin
override REQUIRED_GO_VERSION := go1.26.5
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
# One version, in both binaries. The UI reads it back over the API instead of
# carrying a hand-typed literal.
# Tek sürüm, iki binary'de de. Arayüz onu elle yazılmış bir metin taşımak
# yerine API üzerinden geri okur.
LDFLAGS := -s -w -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT)
DIST    := celikpanel-$(VERSION)

.PHONY: all build check-go test vet panel agent schema17-bridge distro-matrix freebsd-cross web clean dist dist-sign

all: build

build: panel agent schema17-bridge web ## Build binaries and frontend

check-go: ## Require the exact reviewed Go compiler without auto-download
	@actual="$$(env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" env GOVERSION 2>/dev/null)" || { \
		echo "Go $(REQUIRED_GO_VERSION) is required; unable to inspect $(GO)" >&2; \
		exit 1; \
	}; \
	test "$$actual" = "$(REQUIRED_GO_VERSION)" || { \
		echo "Go $(REQUIRED_GO_VERSION) is required; found $$actual at $(GO)" >&2; \
		exit 1; \
	}

test: check-go ## Run the Go test suite with the exact reviewed compiler
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" test ./...

vet: check-go ## Run Go static analysis with the exact reviewed compiler
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" vet ./...

panel: check-go ## Build the panel binary
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/panel ./cmd/panel

agent: check-go ## Build the agent binary
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

schema17-bridge: check-go ## Build the audited legacy schema transition helper
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" build -trimpath -buildvcs=false -o bin/schema17-bridge ./deploy/schema17bridge

distro-matrix: check-go ## Regenerate the distro support matrix with the exact reviewed compiler
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 "$(GO)" run ./tools/gen-distro-matrix

freebsd-cross: check-go ## Prove the documented FreeBSD targets with the exact reviewed compiler
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 "$(GO)" build ./cmd/panel ./cmd/agent
	env -i HOME="$$HOME" PATH="$$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 GOOS=freebsd GOARCH=arm64 "$(GO)" build ./cmd/panel

web: ## Build the frontend (web/dist)
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) ci --no-audit --no-fund
	cd web && PATH="$(NODEDIR):$$PATH" $(NPM) run build

dist: build ## Assemble an offline initial-install tarball (no target toolchain needed)
	# Mirror the operational release layout so initial installation has all
	# reviewed helpers. Privileged updates still enter through bootstrap-update.sh,
	# which publishes and verifies an immutable release before changing /opt.
	# İlk kurulum için incelenmiş yardımcıların tamamını taşı. Yetkili güncellemeler
	# yine değişmez sürümü yayımlayıp doğrulayan bootstrap-update.sh yolunu kullanır.
	rm -rf dist/$(DIST)
	mkdir -p dist/$(DIST)/bin dist/$(DIST)/web/dist dist/$(DIST)/deploy
	cp bin/panel bin/agent bin/schema17-bridge dist/$(DIST)/bin/
	cp -r web/dist/. dist/$(DIST)/web/dist/
	cp -r deploy/. dist/$(DIST)/deploy/
	cp install.sh bootstrap-update.sh update.sh rollback.sh Makefile README.md SECURITY.md NOTICE dist/$(DIST)/
	tar --sort=name --mtime="@$(SOURCE_DATE_EPOCH)" --owner=0 --group=0 --numeric-owner --format=gnu -cf dist/$(DIST).tar -C dist $(DIST)
	gzip -n -f dist/$(DIST).tar
	rm -rf dist/$(DIST)
	cd dist && sha256sum "$(DIST).tar.gz" > "$(DIST).tar.gz.sha256"
	@echo "→ dist/$(DIST).tar.gz"
	@echo "→ dist/$(DIST).tar.gz.sha256"

dist-sign: dist ## Create an optional detached signature with an operator-owned key
	@test -n "$(SIGNING_KEY)" || { echo "SIGNING_KEY is required (GPG key ID or fingerprint)" >&2; exit 1; }
	@command -v gpg >/dev/null || { echo "gpg is required for dist-sign" >&2; exit 1; }
	gpg --batch --yes --armor --local-user "$(SIGNING_KEY)" --detach-sign \
		--output "dist/$(DIST).tar.gz.asc" "dist/$(DIST).tar.gz"
	@echo "→ dist/$(DIST).tar.gz.asc"

clean: ## Remove build outputs
	rm -rf bin web/dist dist

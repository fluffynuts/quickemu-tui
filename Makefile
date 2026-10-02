BINARY  := quickemu-tui
PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
GO      ?= go
GOFLAGS ?=
LDFLAGS ?= -s -w

# VERSION holds major.minor; BUILD (the CI run number) is the third part.
VERSION      := $(shell tr -d '[:space:]' < VERSION)
BUILD        ?= 0
FULL_VERSION := $(VERSION).$(BUILD)

# Stamped into the binary for `quickemu-tui -version`: the short commit (with
# "-dirty" if there are uncommitted or untracked-but-not-ignored files) and the
# UTC build time. Evaluated once per make run. Override COMMIT when building
# from a source tarball with no .git.
GIT_SHA      := $(shell git rev-parse --short=12 HEAD 2>/dev/null)
GIT_DIRTY    := $(shell [ -n "$$(git status --porcelain 2>/dev/null)" ] && echo -dirty)
COMMIT       ?= $(if $(GIT_SHA),$(GIT_SHA)$(GIT_DIRTY))
BUILD_DATE   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_LDFLAGS = $(LDFLAGS) -X main.version=$(FULL_VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# `make dist` cross-compiles for GOOS/GOARCH (default: this machine) and zips
# the result. Zip names say "macos" rather than "darwin".
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)
PLATFORM    := $(if $(filter darwin,$(GOOS)),macos,$(GOOS))
DIST_DIR    := dist
DIST_NAME   := $(BINARY)-$(FULL_VERSION)-$(PLATFORM)-$(GOARCH)

SOURCES := $(shell find . -name '*.go' -not -path './vendor/*') go.mod

.PHONY: all build test vet fmt tidy run install uninstall clean dist

all: build

build: $(BINARY)

go.sum: go.mod
	$(GO) mod tidy

$(BINARY): $(SOURCES) go.sum
	$(GO) build $(GOFLAGS) -ldflags '$(BUILD_LDFLAGS)' -o $@ .

test:
	$(GO) test $(GOFLAGS) ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

run: build
	./$(BINARY) $(ARGS)

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY)

# Prints the zip's path as its last line; run as `make -s dist` to keep
# everything else quiet. The binary is built without cgo, so any target
# cross-compiles from any host.
dist:
	@rm -rf $(DIST_DIR)/$(DIST_NAME) $(DIST_DIR)/$(DIST_NAME).zip
	@mkdir -p $(DIST_DIR)/$(DIST_NAME)
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags '$(BUILD_LDFLAGS)' -o $(DIST_DIR)/$(DIST_NAME)/$(BINARY) .
	@cp README.md $(DIST_DIR)/$(DIST_NAME)/
	@cd $(DIST_DIR) && zip -qr $(DIST_NAME).zip $(DIST_NAME)
	@echo $(DIST_DIR)/$(DIST_NAME).zip

clean:
	rm -rf $(BINARY) $(DIST_DIR)
	$(GO) clean

BINARY  := quickemu-tui
PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
GO      ?= go
GOFLAGS ?=
LDFLAGS ?= -s -w

SOURCES := $(shell find . -name '*.go' -not -path './vendor/*') go.mod

.PHONY: all build test vet fmt tidy run install uninstall clean

all: build

build: $(BINARY)

go.sum: go.mod
	$(GO) mod tidy

$(BINARY): $(SOURCES) go.sum
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ .

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

clean:
	rm -f $(BINARY)
	$(GO) clean

# oosfs Makefile — build, run, and install the filesystem MCP server.

VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BINARY  := oosfs
PREFIX  ?= $(HOME)/bin

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run install clean tidy test

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY) /Users/frank/repro/onisin

# Install to $PREFIX (default: ~/bin). Add that to $PATH or point the
# Claude Desktop config directly at the binary in this directory.
install: build
	install -m 0755 $(BINARY) $(PREFIX)/$(BINARY)

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -f $(BINARY)

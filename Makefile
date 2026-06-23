# sizzletracker build
#
# The rakyll/portmidi cgo bindings hardcode /usr/local include/lib paths
# (Intel Homebrew). On Apple Silicon (and generally) we point cgo at the
# Homebrew prefix detected via `brew --prefix`.

BIN := sizzletracker
PREFIX := $(shell brew --prefix 2>/dev/null || echo /usr/local)

export CGO_ENABLED := 1
export CGO_CFLAGS := -I$(PREFIX)/include
export CGO_LDFLAGS := -L$(PREFIX)/lib

.PHONY: all build run vet clean deps

all: build

deps:
	@echo "Install native libraries (macOS):"
	@echo "  brew install portmidi ncurses"

build:
	go build -o $(BIN) .

run: build
	./$(BIN)

vet:
	go vet ./...

clean:
	rm -f $(BIN)

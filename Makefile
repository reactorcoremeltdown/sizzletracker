# sizzletracker build
#
# Plain `go build` works out of the box on macOS (Apple Silicon + Intel) and
# Linux (amd64 + arm64): the cgo include/lib directives in
# internal/portmidi/portmidi.go already list every standard prefix.
#
# If your portmidi/ncurses live somewhere non-standard, export CGO_CFLAGS /
# CGO_LDFLAGS to point at them and they will be merged in.

BIN := sizzletracker

.PHONY: all build run vet clean deps deps-mac deps-debian

all: build

build:
	CGO_ENABLED=1 go build -o $(BIN) .

run: build
	./$(BIN)

vet:
	CGO_ENABLED=1 go vet ./...

clean:
	rm -f $(BIN)

deps: ## print platform install hints
	@echo "macOS:        make deps-mac"
	@echo "Debian/Ubuntu: make deps-debian"

deps-mac:
	brew install portmidi

deps-debian:
	sudo apt-get update && sudo apt-get install -y \
		build-essential libportmidi-dev pkg-config

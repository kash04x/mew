BINARY  := mew
OUT     := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
LDFLAGS := -s -w -X mew/internal/version.Version=$(VERSION)

.PHONY: build build-all install test vet fmt tidy clean

## build: compile for the current platform
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY) ./cmd/mew

## build-all: cross-compile release binaries for every supported platform
build-all:
	@mkdir -p $(OUT)
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-darwin-amd64 ./cmd/mew
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-darwin-arm64 ./cmd/mew
	GOOS=linux  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-linux-amd64  ./cmd/mew
	GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-linux-arm64  ./cmd/mew
	@ls -lh $(OUT)/

## install: build for the current platform and copy to /usr/local/bin
install: build
	install -m 755 $(OUT)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed: /usr/local/bin/$(BINARY)"

## test: run unit tests
test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf $(OUT)

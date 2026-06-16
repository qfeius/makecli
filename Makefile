MODULE  := github.com/qfeius/makecli
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "DEV")
DATE    := $(shell date -u +%Y-%m-%d)
LOCAL_BIN ?= $(HOME)/.local/bin

LDFLAGS := -s -w \
	-X $(MODULE)/internal/build.Version=$(VERSION) \
	-X $(MODULE)/internal/build.Date=$(DATE)

.PHONY: build local test vet lint geb clean

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/makecli .

local: build
	mkdir -p $(LOCAL_BIN)
	install -m 0755 bin/makecli $(LOCAL_BIN)/makecli

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# GEB 分形文档一致性检查；排除 vendored 的 deps/（geb 不读 .gitignore，须显式 --exclude）
geb:
	geb lint . --exclude deps

clean:
	rm -rf bin/

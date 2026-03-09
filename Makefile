MODULE  := github.com/MakeHQ/makecli
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "DEV")
DATE    := $(shell date -u +%Y-%m-%d)
LOCAL_BIN ?= $(HOME)/.local/bin

LDFLAGS := -s -w \
	-X $(MODULE)/internal/build.Version=$(VERSION) \
	-X $(MODULE)/internal/build.Date=$(DATE)

.PHONY: build local test vet clean

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

clean:
	rm -rf bin/

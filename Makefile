.PHONY: lint build nfpm

ARCH ?= arm64
PLATFORM ?= linux
PKG_FORMAT ?= deb

lint:
	docker run --rm -v $(CURDIR):/app -w /app golangci/golangci-lint:latest golangci-lint run --fix ./...

build:
	go build -o bin/srpls .

nfpm: build
	ARCH=$(ARCH) PLATFORM=$(PLATFORM) nfpm pkg -p $(PKG_FORMAT) -f nfpm.yaml

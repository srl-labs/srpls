.PHONY: lint build

lint:
	docker run --rm -v $(CURDIR):/app -w /app golangci/golangci-lint:latest golangci-lint run --fix ./...

build:
	go build -o srpls .

.PHONY: build test lint vet tidy docker

all: tidy vet lint test build

build:
	go build -o bin/vnode ./cmd/vnode

test:
	go test ./... -v

lint:
	golangci-lint run ./...

vet:
	go vet ./...

tidy:
	go mod tidy

docker:
	docker build -t kroderdev/vnode:dev .

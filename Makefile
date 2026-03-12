.PHONY: build test lint vet tidy docker generate manifests install run

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

generate:
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

manifests:
	controller-gen crd paths="./api/..." output:crd:artifacts:config=config/crd

install: manifests
	kubectl apply -f config/crd/

run: build
	./bin/vnode

docker:
	docker build -t kroderdev/vnode:dev .

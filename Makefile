.PHONY: build test test-e2e lint vet tidy docker generate manifests install run

all: tidy vet lint test build

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X github.com/kroderdev/vnode/internal/version.Version=$(VERSION)" -o bin/ ./cmd/vnode

test:
	go test ./... -v

test-e2e:
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path)" go test ./e2e/... -count=1 -v

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

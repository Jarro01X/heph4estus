.PHONY: all build test lint docker-build tf-validate clean

all: build test lint

build:
	@mkdir -p bin
	go build -o bin/heph ./cmd/heph
	go build -o bin/nmap-worker ./cmd/workers/nmap
	go build -o bin/generic-worker ./cmd/workers/generic
	go build -o bin/heph4estus ./cmd/heph4estus

test:
	go test ./...

lint:
	golangci-lint run ./...

docker-build:
	docker build -t nmap-scanner -f containers/nmap/Dockerfile .

docker-build-generic:
	docker build -t heph-$(TOOL)-worker \
		--build-arg GO_INSTALL_CMD="$(GO_INSTALL_CMD)" \
		--build-arg RUNTIME_INSTALL_CMD="$(RUNTIME_INSTALL_CMD)" \
		-f containers/generic/Dockerfile .

tf-validate:
	cd deployments/aws/nmap/environments/dev && terraform init -backend=false && terraform validate

tf-validate-generic:
	cd deployments/aws/generic/environments/dev && terraform init -backend=false && terraform validate

clean:
	rm -rf bin/

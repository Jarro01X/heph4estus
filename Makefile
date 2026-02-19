.PHONY: all build test lint docker-build tf-validate clean

all: build test lint

build:
	@mkdir -p bin
	go build -o bin/heph-cli ./cmd/heph-cli
	go build -o bin/nmap-worker ./cmd/workers/nmap

test:
	go test ./...

lint:
	golangci-lint run ./...

docker-build:
	docker build -t nmap-scanner -f containers/nmap/Dockerfile .

tf-validate:
	cd deployments/aws/nmap/environments/dev && terraform init -backend=false && terraform validate

clean:
	rm -rf bin/

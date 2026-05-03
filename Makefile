DOCKER ?= docker
CONTAINER_DOCKERFILE ?= containers/generic/Dockerfile
CONTAINER_CONTEXT ?= .
TOOL_IMAGE_PREFIX ?= heph
TOOL_IMAGE_SUFFIX ?= worker
TOOL_IMAGE_TAG ?= latest

TOOL_IMAGES := nmap nuclei subfinder httpx masscan

NMAP_INSTALL_CMD := apk add --no-cache nmap nmap-scripts
NUCLEI_INSTALL_CMD := go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@v3.7.1
SUBFINDER_INSTALL_CMD := go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@v2.13.0
HTTPX_INSTALL_CMD := go install github.com/projectdiscovery/httpx/cmd/httpx@v1.9.0
MASSCAN_INSTALL_CMD := apk add --no-cache masscan

NMAP_SMOKE_ARGS := --version
NUCLEI_SMOKE_ARGS := --help
SUBFINDER_SMOKE_ARGS := --help
HTTPX_SMOKE_ARGS := --help
MASSCAN_SMOKE_ARGS := --version

tool_image = $(TOOL_IMAGE_PREFIX)-$(1)-$(TOOL_IMAGE_SUFFIX):$(TOOL_IMAGE_TAG)

.PHONY: all build test lint docker-build docker-build-all docker-build-nmap docker-build-nmap-generic docker-build-nuclei docker-build-subfinder docker-build-httpx docker-build-masscan docker-smoke-all docker-smoke-nmap docker-smoke-nuclei docker-smoke-subfinder docker-smoke-httpx docker-smoke-masscan container-smoke tf-validate clean

all: build test lint

build:
	@mkdir -p bin
	go build -o bin/heph ./cmd/heph
	go build -o bin/generic-worker ./cmd/workers/generic
	go build -o bin/heph4estus ./cmd/heph4estus

test:
	go test ./...

lint:
	golangci-lint run ./...

docker-build:
ifndef TOOL
	$(error TOOL is required, e.g. make docker-build TOOL=httpx GO_INSTALL_CMD='go install ...')
endif
	$(DOCKER) build -t $(call tool_image,$(TOOL)) \
		--build-arg TOOL_INSTALL_CMD="$(TOOL_INSTALL_CMD)" \
		--build-arg GO_INSTALL_CMD="$(GO_INSTALL_CMD)" \
		--build-arg RUNTIME_INSTALL_CMD="$(RUNTIME_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-nmap:
	$(DOCKER) build -t $(call tool_image,nmap) \
		--build-arg TOOL_INSTALL_CMD="$(NMAP_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-nmap-generic: docker-build-nmap

docker-build-nuclei:
	$(DOCKER) build -t $(call tool_image,nuclei) \
		--build-arg TOOL_INSTALL_CMD="$(NUCLEI_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-subfinder:
	$(DOCKER) build -t $(call tool_image,subfinder) \
		--build-arg TOOL_INSTALL_CMD="$(SUBFINDER_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-httpx:
	$(DOCKER) build -t $(call tool_image,httpx) \
		--build-arg TOOL_INSTALL_CMD="$(HTTPX_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-masscan:
	$(DOCKER) build -t $(call tool_image,masscan) \
		--build-arg TOOL_INSTALL_CMD="$(MASSCAN_INSTALL_CMD)" \
		-f $(CONTAINER_DOCKERFILE) $(CONTAINER_CONTEXT)

docker-build-all: $(addprefix docker-build-,$(TOOL_IMAGES))

docker-smoke-nmap:
	containers/smoke-tool.sh "$(call tool_image,nmap)" nmap $(NMAP_SMOKE_ARGS)

docker-smoke-nuclei:
	containers/smoke-tool.sh "$(call tool_image,nuclei)" nuclei $(NUCLEI_SMOKE_ARGS)

docker-smoke-subfinder:
	containers/smoke-tool.sh "$(call tool_image,subfinder)" subfinder $(SUBFINDER_SMOKE_ARGS)

docker-smoke-httpx:
	containers/smoke-tool.sh "$(call tool_image,httpx)" httpx $(HTTPX_SMOKE_ARGS)

docker-smoke-masscan:
	SMOKE_ALLOW_NONZERO=1 containers/smoke-tool.sh "$(call tool_image,masscan)" masscan $(MASSCAN_SMOKE_ARGS)

docker-smoke-all: $(addprefix docker-smoke-,$(TOOL_IMAGES))

container-smoke: docker-build-all
	$(MAKE) docker-smoke-all

tf-validate:
	cd deployments/aws/generic/environments/dev && terraform init -backend=false && terraform validate

clean:
	rm -rf bin/

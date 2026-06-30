GO ?= go
PROTO_DIR := proto
PROTO_OUT := pkg/api
PROTO_FILES := $(wildcard $(PROTO_DIR)/*.proto)
TOOLS_DIR := .tools
PROTOC_VERSION ?= 27.2
PROTOC_ARCHIVE := protoc-$(PROTOC_VERSION)-linux-x86_64.zip
PROTOC_LOCAL_DIR := $(TOOLS_DIR)/protoc
PROTOC_LOCAL_BIN := $(PROTOC_LOCAL_DIR)/bin/protoc
GOBIN := $(shell $(GO) env GOPATH)/bin
PROTOC_GEN_GO := $(GOBIN)/protoc-gen-go
PROTOC_GEN_GO_GRPC := $(GOBIN)/protoc-gen-go-grpc

ifeq ($(shell command -v protoc >/dev/null 2>&1 && echo yes),yes)
PROTOC_CMD := protoc
else ifneq ($(wildcard $(PROTOC_LOCAL_BIN)),)
PROTOC_CMD := $(PROTOC_LOCAL_BIN)
else
PROTOC_CMD :=
endif

.PHONY: tools protoc-local proto tidy build test run-registry run-service-cli docker-service-cli compose-up compose-up-d compose-logs compose-down trace-up trace-list-all trace-register trace-heartbeat trace-deregister trace-crash trace-recover trace-cover lint

TRACE_TARGETS ?= registry-node-1:50051,registry-node-2:50051,registry-node-3:50051
TRACE_SERVICE_NAME ?= users-api
TRACE_SERVICE_ID ?= users-1
TRACE_SERVICE_ENDPOINT ?= users-1:8080
TRACE_SERVICE_VERSION ?= v1.0.0

tools:
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.1
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

protoc-local:
	mkdir -p $(TOOLS_DIR)
	curl -fsSL -o $(TOOLS_DIR)/$(PROTOC_ARCHIVE) https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/$(PROTOC_ARCHIVE)
	rm -rf $(PROTOC_LOCAL_DIR)
	unzip -q -o $(TOOLS_DIR)/$(PROTOC_ARCHIVE) -d $(PROTOC_LOCAL_DIR)

proto:
	@test -n "$(PROTOC_CMD)" || { echo "protoc not found. Install it system-wide or run 'make protoc-local'."; exit 1; }
	@test -x "$(PROTOC_GEN_GO)" || { echo "protoc-gen-go not found. Run 'make tools'."; exit 1; }
	@test -x "$(PROTOC_GEN_GO_GRPC)" || { echo "protoc-gen-go-grpc not found. Run 'make tools'."; exit 1; }
	mkdir -p $(PROTO_OUT)
	PATH="$(GOBIN):$$PATH" $(PROTOC_CMD) -I=$(PROTO_DIR) \
		--go_out=$(PROTO_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

tidy:
	$(GO) mod tidy

build:
	$(GO) build ./...

test:
	$(GO) test ./...

run-registry:
	$(GO) run ./cmd/registry -config config/registry/example.yaml

run-service-cli:
	$(GO) run ./cmd/service-cli $(ARGS)

docker-service-cli:
	docker compose run --rm service-cli $(ARGS)

compose-up:
	docker compose up --build

compose-up-d:
	docker compose up -d --build

compose-logs:
	docker compose logs -f

compose-down:
	docker compose down --remove-orphans

trace-up:
	docker compose up -d --build registry-node-1 registry-node-2 registry-node-3

trace-list-all:
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051

trace-register:
	docker compose run --rm service-cli register -targets $(TRACE_TARGETS) -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID) -endpoint $(TRACE_SERVICE_ENDPOINT) -version $(TRACE_SERVICE_VERSION)

trace-heartbeat:
	docker compose run --rm service-cli heartbeat -targets registry-node-2:50051 -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID)

trace-deregister:
	docker compose run --rm service-cli deregister -targets registry-node-1:50051 -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID)

trace-crash:
	docker compose stop registry-node-2

trace-recover:
	docker compose start registry-node-2

trace-cover: trace-up
	@echo "=== TRACE COVERAGE START ==="
	@echo "[1/7] register service"
	docker compose run --rm service-cli register -targets $(TRACE_TARGETS) -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID) -endpoint $(TRACE_SERVICE_ENDPOINT) -version $(TRACE_SERVICE_VERSION)
	sleep 3
	@echo "[2/7] verify convergence on all registry nodes"
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051
	@echo "[3/7] send heartbeat"
	docker compose run --rm service-cli heartbeat -targets registry-node-2:50051 -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID)
	sleep 3
	@echo "[4/7] simulate node crash"
	docker compose stop registry-node-2
	@echo "[5/7] verify remaining nodes still answer"
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051
	@echo "[6/7] recover crashed node and verify it rejoins"
	docker compose start registry-node-2
	sleep 6
	docker compose run --rm service-cli list -targets registry-node-2:50051
	@echo "[7/7] deregister service and observe final state"
	docker compose run --rm service-cli deregister -targets registry-node-1:50051 -name $(TRACE_SERVICE_NAME) -id $(TRACE_SERVICE_ID)
	sleep 3
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051
	@echo "=== TRACE COVERAGE END ==="
	@echo "Use 'make compose-down' to stop and clean containers."

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install from https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...


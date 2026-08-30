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

.PHONY: tools protoc-local proto tidy build test test-unit test-integration run-registry run-service-cli docker-service-cli docker-service-cli-fast compose-up compose-up-d compose-logs compose-down trace-up trace-node-status trace-list-all-fast trace-list-all trace-register trace-select-service trace-heartbeat trace-deregister trace-crash trace-recover trace-cover make-discovery lint

TRACE_TARGETS ?= registry-node-1:50051,registry-node-2:50051,registry-node-3:50051
TRACE_SERVICE_NAME_USERS ?= users-api
TRACE_SERVICE_ID_USERS ?= users-1
TRACE_SERVICE_ENDPOINT_USERS ?= 203.0.113.10:8080
TRACE_SERVICE_VERSION_USERS ?= v1.0.0
TRACE_SERVICE_NAME_ORDERS ?= orders-api
TRACE_SERVICE_ID_ORDERS ?= orders-1
TRACE_SERVICE_ENDPOINT_ORDERS ?= 203.0.113.20:8080
TRACE_SERVICE_VERSION_ORDERS ?= v1.0.0
TRACE_SERVICE_NAME ?= $(TRACE_SERVICE_NAME_USERS)
TRACE_SERVICE_ID ?= $(TRACE_SERVICE_ID_USERS)
TRACE_SERVICE_ENDPOINT ?= $(TRACE_SERVICE_ENDPOINT_USERS)
TRACE_SERVICE_VERSION ?= $(TRACE_SERVICE_VERSION_USERS)
TRACE_HEARTBEAT_INTERVAL ?= 4
TRACE_HEARTBEAT_PID_FILE ?= .trace-heartbeat.pid
TRACE_HEARTBEAT_LOG_FILE ?= .trace-heartbeat.log
TRACE_STATE_FILE ?= .trace-up.state
TRACE_NODE_TARGET ?=
TRACE_SERVICE_STATE_FILE ?= .trace-service.state
TRACE_REGISTER_TRACE ?= true

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

test: test-unit

test-unit:
	$(GO) test ./...

test-integration:
	$(GO) test -tags=integration ./internal/integration

run-registry:
	$(GO) run ./cmd/registry -config config/registry/example.yaml

run-service-cli:
	$(GO) run ./cmd/service-cli $(ARGS)

docker-service-cli:
	docker compose run --rm --build service-cli $(ARGS)

docker-service-cli-fast:
	docker compose run --rm service-cli $(ARGS)

compose-up:
	docker compose up --build

compose-up-d:
	docker compose up -d --build

compose-logs:
	docker compose logs -f

compose-down:
	@if [ -f $(TRACE_HEARTBEAT_PID_FILE) ]; then kill $$(cat $(TRACE_HEARTBEAT_PID_FILE)) && rm -f $(TRACE_HEARTBEAT_PID_FILE); fi
	@docker ps -aq --filter name=service-cli-run- | xargs -r docker rm -f >/dev/null 2>&1 || true
	docker compose down --remove-orphans
	@rm -f $(TRACE_STATE_FILE)
	@rm -f $(TRACE_SERVICE_STATE_FILE)

trace-up:
	@docker compose down -v --remove-orphans
	@docker compose up -d --build registry-node-1 registry-node-2 registry-node-3
	@docker compose build service-cli
	@touch $(TRACE_STATE_FILE)
	@echo "trace-up completato. Stato salvato in $(TRACE_STATE_FILE)"
	@printf "=== TRACE COVERAGE START ===\n"
	@printf "Register service on a single node\n"
	@printf "Start continuous heartbeat\n"
	@$(MAKE) --no-print-directory trace-heartbeat

trace-heartbeat:
	@if [ -f $(TRACE_HEARTBEAT_PID_FILE) ]; then \
		old_pid=$$(cat $(TRACE_HEARTBEAT_PID_FILE)); \
		kill $$old_pid >/dev/null 2>&1 || true; \
		rm -f $(TRACE_HEARTBEAT_PID_FILE); \
	fi
	@nohup sh -c '\
		service_name="$(TRACE_SERVICE_NAME)"; \
		service_id="$(TRACE_SERVICE_ID)"; \
		if [ -f $(TRACE_SERVICE_STATE_FILE) ]; then \
			. ./$(TRACE_SERVICE_STATE_FILE); \
			service_name="$${TRACE_SERVICE_NAME:-$$service_name}"; \
			service_id="$${TRACE_SERVICE_ID:-$$service_id}"; \
		fi; \
		while :; do \
			docker compose run --rm --build service-cli heartbeat -targets "$(TRACE_TARGETS)" -name "$$service_name" -id "$$service_id" >/dev/null 2>&1; \
			sleep $(TRACE_HEARTBEAT_INTERVAL); \
		done' > $(TRACE_HEARTBEAT_LOG_FILE) 2>&1 < /dev/null & echo $$! > $(TRACE_HEARTBEAT_PID_FILE)


trace-node-status:
	@if [ ! -f $(TRACE_STATE_FILE) ]; then echo "trace-up non risulta eseguito. Esegui: make trace-up"; exit 1; fi
	@docker compose ps registry-node-1 registry-node-2 registry-node-3 >/dev/null 2>&1 || { echo "nodi registry non disponibili. Esegui: make trace-up"; exit 1; }
	@sh -c '\
		target="$(TRACE_NODE_TARGET)"; \
		if [ -z "$$target" ]; then \
			printf "Seleziona il nodo da verificare:\n" >&2; \
			printf "  1) registry-node-1:50051\n" >&2; \
			printf "  2) registry-node-2:50051\n" >&2; \
			printf "  3) registry-node-3:50051\n" >&2; \
			printf "Scelta [1-3]: " >&2; \
			read -r choice; \
			case "$$choice" in \
				2) target=registry-node-2:50051 ;; \
				3) target=registry-node-3:50051 ;; \
				*) target=registry-node-1:50051 ;; \
			esac; \
		fi; \
		docker compose run --rm service-cli list -targets "$$target"'

trace-list-all-fast:
	@if [ ! -f $(TRACE_STATE_FILE) ]; then echo "trace-up non risulta eseguito. Esegui: make trace-up"; exit 1; fi
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051

trace-list-all:
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051

trace-register:
	@if [ ! -f $(TRACE_STATE_FILE) ]; then echo "trace-up non risulta eseguito. Esegui: make trace-up"; exit 1; fi
	@if [ ! -f $(TRACE_SERVICE_STATE_FILE) ]; then $(MAKE) --no-print-directory trace-select-service; fi
	@set -e; \
		. ./$(TRACE_SERVICE_STATE_FILE); \
		printf "Registrazione robusta su: $(TRACE_TARGETS)\n"; \
		docker compose run --rm service-cli register -targets "$(TRACE_TARGETS)" -name "$$TRACE_SERVICE_NAME" -id "$$TRACE_SERVICE_ID" -endpoint "$$TRACE_SERVICE_ENDPOINT" -version "$$TRACE_SERVICE_VERSION" -trace-register=$(TRACE_REGISTER_TRACE); \
		printf "TRACE_SERVICE_NAME=%s\n" "$$TRACE_SERVICE_NAME" > $(TRACE_SERVICE_STATE_FILE); \
		printf "TRACE_SERVICE_ID=%s\n" "$$TRACE_SERVICE_ID" >> $(TRACE_SERVICE_STATE_FILE); \
		printf "TRACE_SERVICE_ENDPOINT=%s\n" "$$TRACE_SERVICE_ENDPOINT" >> $(TRACE_SERVICE_STATE_FILE); \
		printf "TRACE_SERVICE_VERSION=%s\n" "$$TRACE_SERVICE_VERSION" >> $(TRACE_SERVICE_STATE_FILE); \
		printf "TRACE_LAST_REGISTER_TARGETS=%s\n" "$(TRACE_TARGETS)" >> $(TRACE_SERVICE_STATE_FILE); \
		echo "servizio registrato su: $(TRACE_TARGETS)"
	@printf "Enabling gossip on all registry nodes\n"
	@docker compose exec -T registry-node-1 sh -c 'touch /app/data/.gossip-enabled'
	@docker compose exec -T registry-node-2 sh -c 'touch /app/data/.gossip-enabled'
	@docker compose exec -T registry-node-3 sh -c 'touch /app/data/.gossip-enabled'
	@sleep 3
	@printf "Verifying convergence after gossip/reconcile propagation across all registry nodes\n"
	@for node in registry-node-1:50051 registry-node-2:50051 registry-node-3:50051; do \
		docker compose run --rm service-cli list -targets "$$node"; \
	done

make-discovery:
	@if [ ! -f $(TRACE_STATE_FILE) ]; then echo "trace-up non risulta eseguito. Esegui: make trace-up"; exit 1; fi
	@printf "Seleziona il tipo di discovery:\n" >&2; \
	printf "  1) list: mostra tutti i servizi conosciuti\n" >&2; \
	printf "  2) get: cerca un servizio per nome (opzionale id)\n" >&2; \
	printf "Scelta [1-2]: " >&2; \
	read -r choice; \
	case "$$choice" in \
		2) \
			printf "Nome servizio: " >&2; \
			read -r service_name; \
			if [ -z "$$service_name" ]; then \
				service_name="$(TRACE_SERVICE_NAME)"; \
			fi; \
			printf "ID servizio (opzionale): " >&2; \
			read -r service_id; \
			docker compose run --rm service-cli get -targets "$(TRACE_TARGETS)" -name "$$service_name" -id "$$service_id"; \
			;; \
		*) \
			docker compose run --rm service-cli list -targets "$(TRACE_TARGETS)"; \
			;; \
		esac

trace-select-service:
	@set -e; \
	service_profile="$${TRACE_SERVICE_PROFILE:-}"; \
	if [ -z "$$service_profile" ]; then \
		printf "Seleziona il servizio da registrare prima della fase 1:\n" >&2; \
		printf "  1) users-api\n" >&2; \
		printf "  2) orders-api\n" >&2; \
		printf "Scelta [1-2]: " >&2; \
		read -r choice; \
		case "$$choice" in \
			2) service_profile=orders ;; \
			*) service_profile=users ;; \
		esac; \
	fi; \
	case "$$service_profile" in \
		orders) \
			service_name="$(TRACE_SERVICE_NAME_ORDERS)"; \
			service_id="$(TRACE_SERVICE_ID_ORDERS)"; \
			service_endpoint="$(TRACE_SERVICE_ENDPOINT_ORDERS)"; \
			service_version="$(TRACE_SERVICE_VERSION_ORDERS)" ;; \
		*) \
			service_name="$(TRACE_SERVICE_NAME_USERS)"; \
			service_id="$(TRACE_SERVICE_ID_USERS)"; \
			service_endpoint="$(TRACE_SERVICE_ENDPOINT_USERS)"; \
			service_version="$(TRACE_SERVICE_VERSION_USERS)" ;; \
	esac; \
	printf "TRACE_SERVICE_NAME=%s\n" "$$service_name" > $(TRACE_SERVICE_STATE_FILE); \
	printf "TRACE_SERVICE_ID=%s\n" "$$service_id" >> $(TRACE_SERVICE_STATE_FILE); \
	printf "TRACE_SERVICE_ENDPOINT=%s\n" "$$service_endpoint" >> $(TRACE_SERVICE_STATE_FILE); \
	printf "TRACE_SERVICE_VERSION=%s\n" "$$service_version" >> $(TRACE_SERVICE_STATE_FILE); \
	echo "servizio selezionato: $$service_name/$$service_id (stato salvato in $(TRACE_SERVICE_STATE_FILE))"

trace-phase-4-crash:
	docker compose stop registry-node-2

trace-phase-5-verify:
	docker compose run --rm service-cli list -targets registry-node-1:50051
	docker compose run --rm service-cli list -targets registry-node-2:50051
	docker compose run --rm service-cli list -targets registry-node-3:50051

trace-phase-6-recover:
	docker compose start registry-node-2

trace-deregister:
	@if [ -f $(TRACE_HEARTBEAT_PID_FILE) ]; then kill $$(cat $(TRACE_HEARTBEAT_PID_FILE)) && rm -f $(TRACE_HEARTBEAT_PID_FILE); fi
	@if [ ! -f $(TRACE_SERVICE_STATE_FILE) ]; then $(MAKE) --no-print-directory trace-select-service; fi
	@set -e; \
		. ./$(TRACE_SERVICE_STATE_FILE); \
		output=$$(docker compose run --rm service-cli deregister -targets "$(TRACE_TARGETS)" -name "$$TRACE_SERVICE_NAME" -id "$$TRACE_SERVICE_ID" 2>&1); \
		printf '%s\n' "$$output"; \
		if printf '%s' "$$output" | grep -q "servizio non presente:"; then \
			exit 0; \
		fi; \
		echo "servizio rimosso da: $(TRACE_TARGETS)"; \
		true
	@sleep 3
	@for node in registry-node-1:50051 registry-node-2:50051 registry-node-3:50051; do \
		docker compose run --rm service-cli list -targets "$$node"; \
	done

trace-crash:
	docker compose stop registry-node-2

trace-recover:
	docker compose start registry-node-2

trace-cover:
	@if [ ! -f $(TRACE_STATE_FILE) ]; then $(MAKE) --no-print-directory trace-up; fi
	@printf "=== TRACE COVERAGE START ===\n"
	@printf "Fase 1: registrazione del servizio sul cluster\n"
	@$(MAKE) --no-print-directory trace-register
	@printf "Fase 2: verifica convergenza su tutti i nodi\n"
	@$(MAKE) --no-print-directory trace-list-all
	@printf "Fase 3: attivazione heartbeat continuo\n"
	@$(MAKE) --no-print-directory trace-heartbeat
	@printf "Fase 4: fault injection - crash del nodo registry-node-2\n"
	@$(MAKE) --no-print-directory trace-phase-4-crash
	@printf "Fase 5: verifica resilienza dei nodi rimanenti\n"
	@$(MAKE) --no-print-directory trace-phase-5-verify
	@printf "Fase 6: recovery del nodo crashato\n"
	@$(MAKE) --no-print-directory trace-phase-6-recover
	@printf "Fase 7: deregistrazione finale e cleanup del servizio\n"
	@$(MAKE) --no-print-directory trace-deregister
	@printf "=== TRACE COVERAGE END ===\n"
	@printf "Use 'make compose-down' to stop and clean containers.\n"

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install from https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...


SHELL := /bin/bash
GOBIN := $(shell go env GOPATH)/bin

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build all Go binaries
	go build ./...

.PHONY: test
test: ## Run unit tests with race detector
	go test -race ./...

.PHONY: lint
lint: ## Run golangci-lint and buf lint
	golangci-lint run ./...
	cd api/proto && buf lint

.PHONY: generate
generate: ## Regenerate code from protobuf definitions (installs buf + plugins on first run; requires network access)
	@command -v buf >/dev/null 2>&1 || { echo "installing buf..."; go install github.com/bufbuild/buf/cmd/buf@v1.47.2; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "installing protoc-gen-go..."; go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.3; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "installing protoc-gen-go-grpc..."; go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1; }
	cd api/proto && PATH="$(GOBIN):$$PATH" buf generate

.PHONY: run-control-plane
run-control-plane: ## Run the control-plane service locally
	go run ./cmd/control-plane

.PHONY: migrate
migrate: ## Apply database migrations
	AGENTMESH_DATABASE_URL="$${AGENTMESH_DATABASE_URL:-postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable}" \
		go run ./cmd/migrate

.PHONY: seed
seed: ## Load demo tenant/agent seed data (implemented in Phase 1)
	@echo "not implemented yet: seed data arrives in Phase 1"

.PHONY: dev
dev: ## Start the full local stack via Docker Compose (implemented in Phase 8)
	@echo "not implemented yet: docker-compose stack arrives in Phase 8"

.PHONY: run-router
run-router: ## Run the data-plane router locally (implemented in Phase 3)
	@echo "not implemented yet: router service arrives in Phase 3"

.PHONY: run-agents
run-agents: ## Run the example agents locally (implemented in Phase 2)
	@echo "not implemented yet: example agents arrive in Phase 2"

.PHONY: load-test
load-test: ## Run k6/ghz load tests against the router (implemented in Phase 12)
	@echo "not implemented yet: load tests arrive in Phase 12"

.PHONY: profile
profile: ## Capture CPU/heap/mutex profiles (implemented in Phase 12)
	@echo "not implemented yet: profiling workflow arrives in Phase 12"

.PHONY: compose-down
compose-down: ## Tear down the Docker Compose stack (implemented in Phase 8)
	@echo "not implemented yet: docker-compose stack arrives in Phase 8"

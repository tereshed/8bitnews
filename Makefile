GOLANGCI_LINT_VERSION := v2.15.0
GOBIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT := $(GOBIN)/golangci-lint
COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: lint test validate-profiles up down

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

$(GOLANGCI_LINT):
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

test:
	go test ./...

validate-profiles:
	go run ./cmd/profiles validate

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

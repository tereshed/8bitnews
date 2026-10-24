GOLANGCI_LINT_VERSION := v2.15.0
GOBIN := $(CURDIR)/bin
GOLANGCI_LINT := $(GOBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
COMPOSE := docker compose --project-directory . -f deploy/docker-compose.yml

.PHONY: lint test validate-profiles up down

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

$(GOLANGCI_LINT):
	GOBIN=$(GOBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(GOBIN)/golangci-lint $(GOLANGCI_LINT)

# -p 1: integration tests in internal/service and internal/api share one
# Postgres/S3 (deploy/docker-compose.yml) and each truncates every table on
# setup; running package test binaries concurrently races one package's
# truncate against another's in-flight test.
test:
	go test -p 1 ./...

validate-profiles:
	go run ./cmd/profiles validate

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

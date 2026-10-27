GOLANGCI_LINT_VERSION := v2.15.0
GOBIN := $(CURDIR)/bin
GOLANGCI_LINT := $(GOBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
COMPOSE := docker compose --project-directory . -f deploy/docker-compose.yml

# make deploy-hub-vm / deploy-hub / deploy-hub-backup read deploy secrets
# from this file (see deploy/ansible/deploy.env.example) instead of from
# -e flags, so nothing sensitive shows up in shell history or `ps`.
DEPLOY_ENV ?= $(HOME)/.config/8bitnews/deploy.env
REF ?= main

.PHONY: lint test validate-profiles up down deploy-hub-vm deploy-hub deploy-hub-backup deploy-check

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

# E1d: deploy hub onto the shared Hetzner host (docs/deploy.md). Each
# target only shells out to ansible-playbook after sourcing DEPLOY_ENV —
# it never touches the real server from CI/this repo's own checks.
deploy-hub-vm:
	set -a; . $(DEPLOY_ENV); set +a; cd deploy/ansible && ansible-playbook hub_vm.yml

deploy-hub:
	set -a; . $(DEPLOY_ENV); set +a; cd deploy/ansible && ansible-playbook hub_guest.yml -e hub_ref=$(REF)

deploy-hub-backup:
	set -a; . $(DEPLOY_ENV); set +a; cd deploy/ansible && ansible-playbook hub_backup.yml

# Fully offline: ansible-playbook --syntax-check + ansible-lint on all three
# playbooks, `caddy validate`/`nft -c -f` on rendered templates, and
# `docker compose config` on the guest compose file — all containerized,
# no real server access. See deploy/ansible/deploy-check.sh.
deploy-check:
	bash deploy/ansible/deploy-check.sh

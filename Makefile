GOLANGCI_LINT_VERSION := v2.15.0
GOBIN := $(CURDIR)/bin
GOLANGCI_LINT := $(GOBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
COMPOSE := docker compose --project-directory . -f deploy/docker-compose.yml

# make deploy-hub-vm / deploy-hub / deploy-hub-backup read deploy secrets
# from this file (see deploy/ansible/deploy.env.example) instead of from
# -e flags, so nothing sensitive shows up in shell history or `ps`.
DEPLOY_ENV ?= $(HOME)/.config/8bitnews/deploy.env
REF ?= main

STUDIO_IMAGE := 8bitnews-studio

.PHONY: lint test validate-profiles up down deploy-hub-vm deploy-hub deploy-hub-backup deploy-check studio-image studio-test studio-preview studio-build-sample studio-demo

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

# E2a-1: studio frame engine and template packages (studio/README.md).
studio-image:
	docker build -t $(STUDIO_IMAGE) ./studio

studio-test: studio-image
	docker run --rm -v $(CURDIR):/repo:ro -e STUDIO_REPO_ROOT=/repo --entrypoint python $(STUDIO_IMAGE) -m pytest -q

studio-preview: studio-image
	mkdir -p out/studio-preview
	docker run --rm -v $(CURDIR)/out/studio-preview:/app/out $(STUDIO_IMAGE) \
		template preview templates/nes-desk --out out

# E2a-2a: studio build (studio/README.md) of the demo script with the stub
# voice and the synthesized test loop as music: quick, no model run.
studio-build-sample: studio-image
	mkdir -p out/studio-sample
	docker run --rm -v $(CURDIR):/repo:ro -e STUDIO_REPO_ROOT=/repo -v $(CURDIR)/out/studio-sample:/app/out \
		--entrypoint sh $(STUDIO_IMAGE) -c 'python -m studio music test-track --out /tmp/track.wav \
		&& python -m studio build --script tests/fixtures/scripts/demo.json --channel 8bitnews-us --tts stub \
			--music-file /tmp/track.wav --out out'

# E2a-3: the demo reel with the real voice (Kokoro, the anchor's own), music and
# transition sounds, then studio validate. STUDIO_MUSIC_DIR=<folder holding
# music/tracks/...> picks the track from music/catalog.yaml; without it the
# music is the synthesized test loop.
STUDIO_MUSIC_DIR ?=
studio-demo: studio-image
	mkdir -p out/studio-demo
	docker run --rm -v $(CURDIR):/repo:ro -e STUDIO_REPO_ROOT=/repo -v $(CURDIR)/out/studio-demo:/app/out \
		$(if $(STUDIO_MUSIC_DIR),-v $(abspath $(STUDIO_MUSIC_DIR)):/music:ro -e STUDIO_MUSIC_DIR=/music) \
		--entrypoint sh $(STUDIO_IMAGE) -c 'set -e; \
		if [ -n "$$STUDIO_MUSIC_DIR" ]; then music=""; else \
			python -m studio music test-track --out /tmp/track.wav; music="--music-file /tmp/track.wav"; fi; \
		python -m studio build --script tests/fixtures/scripts/demo.json --channel 8bitnews-us $$music --out out; \
		python -m studio validate --package out/package.json'

# Fully offline: ansible-playbook --syntax-check + ansible-lint on all three
# playbooks, `caddy validate`/`nft -c -f` on rendered templates, and
# `docker compose config` on the guest compose file — all containerized,
# no real server access. See deploy/ansible/deploy-check.sh.
deploy-check:
	bash deploy/ansible/deploy-check.sh

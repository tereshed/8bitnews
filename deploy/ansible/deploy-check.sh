#!/usr/bin/env bash
# `make deploy-check` — fully offline/containerized validation of the E1d
# deploy playbooks. Touches no real server: everything here either runs
# ansible-core's own checks or validates a *rendered* template inside a
# disposable container.
set -euo pipefail

ANSIBLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ANSIBLE_IMAGE="ghcr.io/ansible/community-ansible-dev-tools:latest"
CHECK_DIR="${ANSIBLE_DIR}/.deploy-check"

cleanup() { rm -rf "${CHECK_DIR}"; }
trap cleanup EXIT
rm -rf "${CHECK_DIR}"
mkdir -p "${CHECK_DIR}/rendered" "${CHECK_DIR}/caddy/sites"

run_ansible_image() {
	docker run --rm -v "${ANSIBLE_DIR}:/work" -w /work "${ANSIBLE_IMAGE}" "$@"
}

echo "==> ansible-playbook --syntax-check"
run_ansible_image sh -c '
	set -e
	for f in hub_vm.yml hub_guest.yml hub_backup.yml; do
		echo "-- $f --"
		ansible-playbook --syntax-check "$f"
	done
'

echo "==> ansible-lint"
run_ansible_image ansible-lint hub_vm.yml hub_guest.yml hub_backup.yml tasks/caddy_site.yml caddy_site_test.yml

echo "==> rendering bitnews_hub.nft and 8bitnews.caddy with representative sample values"
run_ansible_image ansible localhost -i "localhost," -c local -m template \
	-a "src=templates/bitnews_hub.nft.j2 dest=/work/.deploy-check/rendered/bitnews_hub.nft" \
	-e "hub_nft_table=bitnews_hub hub_vm_user=8bitnews-vm hub_host_public_ip=135.181.4.110"

run_ansible_image ansible localhost -i "localhost," -c local -m template \
	-a "src=templates/8bitnews.caddy.j2 dest=/work/.deploy-check/caddy/sites/8bitnews.caddy" \
	-e "hub_domain=hub.example.com hub_caddy_max_body_size=20MB hub_vm_http_port=9201"

echo "==> nft -c -f (rendered bitnews_hub.nft, inside a container with NET_ADMIN)"
docker run --rm --cap-add=NET_ADMIN \
	-v "${CHECK_DIR}/rendered/bitnews_hub.nft:/tmp/bitnews_hub.nft:ro" \
	alpine:3.20 sh -c '
		apk add --no-cache -q nftables shadow >/dev/null
		adduser -D -H 8bitnews-vm
		nft -c -f /tmp/bitnews_hub.nft
	'

echo "==> extracting a real caddy binary from the caddy:2 image (functional test below needs more than --syntax-check)"
CADDY_BIN_CID="$(docker create caddy:2)"
docker cp "${CADDY_BIN_CID}:/usr/bin/caddy" "${CHECK_DIR}/bin.caddy"
docker rm "${CADDY_BIN_CID}" >/dev/null
mkdir -p "${CHECK_DIR}/bin"
mv "${CHECK_DIR}/bin.caddy" "${CHECK_DIR}/bin/caddy"
chmod +x "${CHECK_DIR}/bin/caddy"

echo "==> running caddy_site_test.yml (executes the real render/validate/install task logic end to end)"
run_ansible_image sh -c '
	export PATH=/work/.deploy-check/bin:$PATH
	ansible-playbook -i "localhost," -c local caddy_site_test.yml
'

echo "==> caddy validate (rendered 8bitnews.caddy, imported by a minimal Caddyfile)"
cat >"${CHECK_DIR}/caddy/Caddyfile" <<'CADDYFILE'
{
	admin off
}

import /etc/caddy/sites/*.caddy
CADDYFILE

docker run --rm \
	-v "${CHECK_DIR}/caddy/Caddyfile:/etc/caddy/Caddyfile:ro" \
	-v "${CHECK_DIR}/caddy/sites:/etc/caddy/sites:ro" \
	caddy:2 caddy validate --config /etc/caddy/Caddyfile

echo "==> docker compose config (guest compose file, sample env)"
cat >"${CHECK_DIR}/sample.env" <<'ENVFILE'
POSTGRES_USER=8bitnews
POSTGRES_PASSWORD=sample-password
POSTGRES_DB=8bitnews
HUB_POSTGRES_IMAGE=postgres:16-alpine
HUB_POSTGRES_MEM_LIMIT=1g
HUB_POSTGRES_CPUS=1.0
HUB_REPO_DIR=/opt/8bitnews/repo
HUB_API_KEYS=sample-key
HUB_S3_ENDPOINT=https://example.r2.cloudflarestorage.com
HUB_S3_REGION=auto
HUB_S3_BUCKET=example-bucket
HUB_S3_ACCESS_KEY=sample
HUB_S3_SECRET_KEY=sample
HUB_S3_USE_PATH_STYLE=true
ENVFILE

docker compose -f "${ANSIBLE_DIR}/files/guest/docker-compose.yml" --env-file "${CHECK_DIR}/sample.env" config >/dev/null

echo "==> deploy-check OK"

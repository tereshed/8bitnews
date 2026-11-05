#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
test_project="8bitnews-test-$(date +%s)-$$"
test_compose=(docker compose -p "$test_project" -f deploy/compose.test.yml)
trap '"${test_compose[@]}" down -v --remove-orphans' EXIT
"${test_compose[@]}" up -d --wait --wait-timeout 180
test_pg_addr=$("${test_compose[@]}" port postgres 5432)
test_s3_addr=$("${test_compose[@]}" port s3 8000)
export HUB_TEST_DATABASE_URL="postgres://automation_test:automation_test@$test_pg_addr/automation_test?sslmode=disable"
export HUB_TEST_S3_ENDPOINT="http://$test_s3_addr"
export HUB_TEST_S3_ACCESS_KEY=automation_test
export HUB_TEST_S3_SECRET_KEY=automation_test_secret
export HUB_TEST_S3_BUCKET=automation-test-media
export HUB_TEST_REQUIRE_SERVICES=true
# Creating the bucket uses the same SDK/settings as production; no extra CLI image.
go run ./cmd/testservices
go test -p 1 -count=1 "$@" ./...

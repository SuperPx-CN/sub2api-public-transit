#!/bin/sh
# Starts only disposable local containers. No shared source database is used.
set -eu
name="ai-transit-test-$$"
image="ai-transit:verification"
cleanup() {
  docker rm -f "$name-http" "$name-db" >/dev/null 2>&1 || true
  docker network rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
docker network create "$name" >/dev/null
docker run -d --name "$name-db" --network "$name" -p 127.0.0.1::5432 -e POSTGRES_DB=transit_test -e POSTGRES_PASSWORD=fixture-admin postgres:17-alpine >/dev/null
attempt=0
until docker exec "$name-db" pg_isready -U postgres >/dev/null 2>&1; do
  attempt=$((attempt+1)); test "$attempt" -lt 30; sleep 1
done
port=$(docker port "$name-db" 5432/tcp | cut -d: -f2)
(cd backend && TEST_DATABASE_URL="postgres://postgres:fixture-admin@127.0.0.1:$port/transit_test?sslmode=disable" go test -race -count=1 ./...)
docker build -t "$image" .
docker run -d --name "$name-http" --network "$name" -p 127.0.0.1::8080 --read-only --cap-drop ALL --security-opt no-new-privileges -e PUBLIC_BASE_URL=https://transit.example -e DATABASE_HOST="$name-db" -e DATABASE_DBNAME=transit_test -e DATABASE_USER=transit_fixture_reader -e DATABASE_PASSWORD=fixture-reader -e REDIS_HOST=unused.invalid "$image" >/dev/null
port=$(docker port "$name-http" 8080/tcp | cut -d: -f2)
python3 deploy/tests/smoke.py "http://127.0.0.1:$port"
test "$(docker inspect --format '{{.Config.User}}' "$name-http")" = '10001:10001'
echo 'Read-only PostgreSQL, image build and container smoke passed.'

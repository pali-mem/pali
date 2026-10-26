#!/usr/bin/env bash
# test_backends.sh - run the env-gated live backend tests against throwaway
# docker containers (Qdrant, pgvector, Neo4j) on non-default host ports.
set -euo pipefail

cd "$(dirname "$0")/.."

QDRANT_PORT="${PALI_TEST_QDRANT_PORT:-16333}"
PG_PORT="${PALI_TEST_PGVECTOR_PORT:-15432}"
NEO4J_BOLT_PORT="${PALI_TEST_NEO4J_BOLT_PORT:-17687}"
NEO4J_PASSWORD="pali-test-password"
PG_PASSWORD="pali"

suffix="$$"
qdrant_name="pali-test-qdrant-$suffix"
pg_name="pali-test-pgvector-$suffix"
neo4j_name="pali-test-neo4j-$suffix"

cleanup() {
  docker rm -f "$qdrant_name" "$pg_name" "$neo4j_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_for() {
  local desc="$1" tries="$2"
  shift 2
  for ((i = 0; i < tries; i++)); do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "ERROR: $desc did not become ready" >&2
  return 1
}

echo "==> Starting containers"
docker run -d --name "$qdrant_name" -p "127.0.0.1:${QDRANT_PORT}:6333" qdrant/qdrant >/dev/null
docker run -d --name "$pg_name" -p "127.0.0.1:${PG_PORT}:5432" \
  -e POSTGRES_USER=pali -e POSTGRES_PASSWORD="$PG_PASSWORD" -e POSTGRES_DB=pali \
  pgvector/pgvector:pg16 >/dev/null
docker run -d --name "$neo4j_name" -p "127.0.0.1:${NEO4J_BOLT_PORT}:7687" \
  -e NEO4J_AUTH="neo4j/${NEO4J_PASSWORD}" neo4j:5 >/dev/null

wait_for "qdrant" 30 curl -fsS "http://127.0.0.1:${QDRANT_PORT}/collections"
wait_for "pgvector" 30 docker exec "$pg_name" pg_isready -U pali -d pali
# pg_isready can pass during the init-time restart; confirm a real query works.
wait_for "pgvector query" 30 docker exec "$pg_name" psql -U pali -d pali -c 'SELECT 1'
wait_for "neo4j" 60 docker exec "$neo4j_name" cypher-shell -u neo4j -p "$NEO4J_PASSWORD" 'RETURN 1;'

export PALI_TEST_QDRANT_URL="http://127.0.0.1:${QDRANT_PORT}"
export PALI_TEST_PGVECTOR_DSN="postgres://pali:${PG_PASSWORD}@127.0.0.1:${PG_PORT}/pali?sslmode=disable"
export PALI_TEST_NEO4J_URI="bolt://127.0.0.1:${NEO4J_BOLT_PORT}"
export PALI_TEST_NEO4J_USERNAME="neo4j"
export PALI_TEST_NEO4J_PASSWORD="$NEO4J_PASSWORD"

echo "==> Running live backend tests"
go test -count=1 -v -tags integration -run 'TestLive' ./test/integration/...

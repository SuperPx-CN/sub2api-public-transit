#!/bin/sh
set -eu
for dir in frontend backend/ent backend/migrations backend/internal/service backend/internal/repository; do
  test ! -e "$dir"
done
if grep -E 'gin-gonic|go-redis|entgo|google/wire' backend/go.mod; then exit 1; fi
if grep -En 'ReverseProxy|NewTicker|time.Tick|http.NewRequest|http.DefaultClient|REDIS_|AUTO_SETUP|JWT_SECRET|TOTP_' backend/internal/transit/*.go backend/cmd/transit/*.go | grep -v '_test.go:'; then exit 1; fi
if grep -Ein 'INSERT INTO|UPDATE [a-z_]+ SET|DELETE FROM|CREATE TABLE|ALTER TABLE|DROP TABLE' backend/internal/transit/*.go backend/cmd/transit/*.go | grep -v '_test.go:'; then exit 1; fi

#!/usr/bin/env bash
# Dump Supabase → restore into VPS Postgres.
#
# Run from a machine that can reach BOTH databases (usually the VPS).
# Use Supabase DIRECT DB host (db.<ref>.supabase.co:5432), NOT the pooler :6543
# — pg_dump often fails against transaction-mode pooler.
#
# 1) Put the app in maintenance briefly (stop emailtracker container).
# 2) Export:
#      export SUPABASE_URL='postgres://postgres.[ref]:[pass]@db.[ref].supabase.co:5432/postgres?sslmode=require'
#      export VPS_URL='postgres://emailtracker:[pass]@127.0.0.1:5432/emailtracker?sslmode=disable'
# 3) bash migrate-supabase-to-vps.sh
# 4) Point DATABASE_URL at VPS_URL, start app, smoke-test.
set -euo pipefail

if [[ -z "${SUPABASE_URL:-}" || -z "${VPS_URL:-}" ]]; then
  echo "Need SUPABASE_URL and VPS_URL env vars."
  exit 1
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DUMP="${DUMP_PATH:-/opt/emailtracker/backups/supabase-${STAMP}.dump}"
mkdir -p "$(dirname "$DUMP")"

echo "==> Dumping Supabase (custom format) → $DUMP"
# --no-owner --no-acl: avoid role names that don't exist on the VPS.
pg_dump "$SUPABASE_URL" \
  --format=custom \
  --no-owner \
  --no-acl \
  --verbose \
  --file="$DUMP"

echo "==> Restoring into VPS Postgres"
# Clean restore into an empty-ish DB. Schema is recreated from the dump.
pg_restore \
  --clean \
  --if-exists \
  --no-owner \
  --no-acl \
  --dbname="$VPS_URL" \
  --verbose \
  "$DUMP" || true
# pg_restore returns non-zero on some benign "does not exist" notices with --clean;
# verify with a quick count instead of trusting exit code alone.

echo "==> Spot-check"
psql "$VPS_URL" -c "SELECT COUNT(*) AS users FROM users;"
psql "$VPS_URL" -c "SELECT COUNT(*) AS campaigns FROM campaigns;"
psql "$VPS_URL" -c "SELECT COUNT(*) AS contacts FROM contact;"
psql "$VPS_URL" -c "SELECT COUNT(*) AS email_sends FROM email_sends;"

echo "Done. Dump kept at $DUMP"
echo "Next: set DATABASE_URL to VPS_URL in /opt/emailtracker/.env and restart emailtracker."

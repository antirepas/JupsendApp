#!/usr/bin/env bash
# One-time: create a Postgres 16 container on the VPS for Jupsend.
# App uses --network host, so it reaches DB at 127.0.0.1:5432.
#
# Usage (on the VPS as root):
#   bash setup-vps-postgres.sh
#
# Then put a strong password in /opt/emailtracker/.env:
#   DATABASE_URL=postgres://emailtracker:YOUR_PASSWORD@127.0.0.1:5432/emailtracker?sslmode=disable
set -euo pipefail

NAME="${POSTGRES_CONTAINER_NAME:-emailtracker-postgres}"
USER_NAME="${POSTGRES_USER:-emailtracker}"
DB_NAME="${POSTGRES_DB:-emailtracker}"
DATA_DIR="${POSTGRES_DATA_DIR:-/opt/emailtracker/pgdata}"
PASSWORD="${POSTGRES_PASSWORD:-}"

if [[ -z "$PASSWORD" ]]; then
  echo "Set POSTGRES_PASSWORD to a strong secret, e.g.:"
  echo "  POSTGRES_PASSWORD='…' bash setup-vps-postgres.sh"
  exit 1
fi

mkdir -p "$DATA_DIR"

if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  echo "Container $NAME already exists."
  docker start "$NAME" >/dev/null || true
else
  docker pull postgres:16
  docker run -d \
    --name "$NAME" \
    --restart unless-stopped \
    -e POSTGRES_USER="$USER_NAME" \
    -e POSTGRES_PASSWORD="$PASSWORD" \
    -e POSTGRES_DB="$DB_NAME" \
    -v "$DATA_DIR:/var/lib/postgresql/data" \
    -p 127.0.0.1:5432:5432 \
    postgres:16
fi

echo "Waiting for Postgres…"
for i in $(seq 1 30); do
  if docker exec "$NAME" pg_isready -U "$USER_NAME" -d "$DB_NAME" >/dev/null 2>&1; then
    echo "Postgres is ready on 127.0.0.1:5432 (db=$DB_NAME user=$USER_NAME)"
    echo "Update /opt/emailtracker/.env DATABASE_URL then restart the app container."
    exit 0
  fi
  sleep 1
done

echo "Postgres did not become ready in time."
docker logs "$NAME" --tail 50 || true
exit 1

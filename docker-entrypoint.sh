#!/bin/sh
set -eu

DATA_DIR="${DATA_DIR:-/data}"
DATA_DIR="${DATA_DIR%/}"
: "${DATA_DIR:=/}"

META_DIR="${DATA_DIR%/}/meta"
SQLITE_DIR="${SQLITE_DIR:-${DATA_DIR%/}/sqlite}"
SQLITE_DIR="${SQLITE_DIR%/}"
: "${SQLITE_DIR:=/}"

if [ "$(id -u)" = "0" ]; then
  mkdir -p "$DATA_DIR" "$META_DIR" "$SQLITE_DIR"
  case "$SQLITE_DIR" in
    "$DATA_DIR"|"$DATA_DIR"/*)
      find "$DATA_DIR" \( ! -user webdb -o ! -group webdb \) -exec chown webdb:webdb {} +
      ;;
    *)
      find "$DATA_DIR" "$SQLITE_DIR" \( ! -user webdb -o ! -group webdb \) -exec chown webdb:webdb {} +
      ;;
  esac
  chmod 700 "$META_DIR"
  chmod 750 "$SQLITE_DIR"
  exec su-exec webdb:webdb /app/webdb-studio "$@"
fi

exec /app/webdb-studio "$@"

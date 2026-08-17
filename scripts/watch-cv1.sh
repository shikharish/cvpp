#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
JSON=${ERP_CV_JSON:-"$ROOT/data/resume.json"}
DEBOUNCE_SECONDS=${ERP_CV_WATCH_DEBOUNCE_SECONDS:-1}
POLL_SECONDS=${ERP_CV_WATCH_POLL_SECONDS:-1}
BINARY=${ERP_CV_BINARY:-"$ROOT/erp-cv-portal"}

log() {
  printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

checksum() {
  if [ ! -f "$JSON" ]; then
    printf 'missing\n'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$JSON" | awk '{print $1}'
    return
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$JSON" | awk '{print $1}'
    return
  fi

  cksum "$JSON" | awk '{print $1 ":" $2}'
}

stable_checksum() {
  current=$(checksum)

  while :; do
    sleep "$DEBOUNCE_SECONDS"
    next=$(checksum)
    if [ "$next" = "$current" ]; then
      printf '%s\n' "$current"
      return
    fi
    current=$next
  done
}

last=$(checksum)
log "Watching $JSON"
log "On change: ./erp-cv-portal erp --cv 1"
log "Stop with Ctrl-C"

if [ ! -x "$BINARY" ]; then
  log "Binary not found; building ./erp-cv-portal"
  (cd "$ROOT" && go build -o erp-cv-portal ./cmd/erp-cv-portal)
fi

while :; do
  sleep "$POLL_SECONDS"
  current=$(checksum)

  if [ "$current" = "$last" ]; then
    continue
  fi

  log "Detected resume.json change; waiting for a stable write"
  last=$(stable_checksum)

  log "Running ./erp-cv-portal erp --cv 1"
  if "$BINARY" erp --cv 1; then
    log "ERP CV1 sync/download complete"
  else
    status=$?
    log "ERP CV1 sync/download failed with exit status $status"
  fi

  last=$(checksum)
done

#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

mkdir -p "$ROOT/data" "$ROOT/.erp-cv-secrets" "$ROOT/pdf"

if [ ! -f "$ROOT/data/resume.json" ]; then
  cp "$ROOT/data/resume.example.json" "$ROOT/data/resume.json"
  echo "Created data/resume.json"
else
  echo "Keeping existing data/resume.json"
fi

if [ ! -f "$ROOT/.erp-cv-secrets/erpcreds.json" ]; then
  cp "$ROOT/config/erpcreds.example.json" "$ROOT/.erp-cv-secrets/erpcreds.json"
  chmod 600 "$ROOT/.erp-cv-secrets/erpcreds.json"
  echo "Created .erp-cv-secrets/erpcreds.json"
else
  echo "Keeping existing .erp-cv-secrets/erpcreds.json"
fi

cd "$ROOT"
go build -o erp-cv-portal ./cmd/erp-cv-portal

echo
echo "Setup complete."
echo "Next:"
echo "  1. Edit .erp-cv-secrets/erpcreds.json"
echo "  2. Edit data/resume.json or run ./erp-cv-portal editor"
echo "  3. Run ./erp-cv-portal erp --cv 1"

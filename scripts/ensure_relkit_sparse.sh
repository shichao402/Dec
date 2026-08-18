#!/usr/bin/env bash
# ensure_relkit_sparse entry (macOS/Linux). Prefer .venv, else system Python.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -x "$SCRIPT_DIR/../.venv/bin/python" ]; then
  PYTHON="$SCRIPT_DIR/../.venv/bin/python"
else
  PYTHON=python3
fi
exec "$PYTHON" "$SCRIPT_DIR/ensure_relkit_sparse.py" "$@"

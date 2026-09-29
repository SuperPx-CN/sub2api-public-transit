#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
command -v helm >/dev/null 2>&1 || { printf 'helm is required\n' >&2; exit 1; }
exec python3 "$repo_root/deploy/tests/helm_chart_test.py"

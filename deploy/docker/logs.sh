#!/usr/bin/env bash
# Argus Docker 日志跟踪
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${DIR}"
exec docker compose logs -f

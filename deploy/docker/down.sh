#!/usr/bin/env bash
# Argus Docker 停止脚本
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${DIR}"
docker compose down
echo "已停止（数据保留在数据卷）"

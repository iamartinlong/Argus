#!/usr/bin/env bash
# Argus 管理后台 Docker 一键部署/停止脚本
# 用法：
#   ./up.sh          # 首次自动生成 .env（含随机密钥），构建镜像并启动
#   ./down.sh        # 停止并移除容器（保留数据卷）
#   ./logs.sh        # 跟踪日志
#
# 访问 http://localhost:8443

set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${DIR}"

if [[ ! -f ".env" ]]; then
    echo "生成 .env（随机密钥）..."
    cat > .env <<EOF
ARGUS_JWT_SECRET=$(openssl rand -hex 32)
ARGUS_MASTER_KEY=$(openssl rand -hex 32)
EOF
    chmod 600 .env
fi

echo "构建并启动（首次构建较慢）..."
docker compose up -d --build
docker compose ps

echo
echo "Argus 已启动：http://localhost:8443"
echo "停止:  ./down.sh | 日志: ./logs.sh"

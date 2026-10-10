#!/usr/bin/env bash
# Argus Agent Linux systemd 一键安装/卸载脚本（需 root）
# 用法：
#   ./install-systemd.sh            # 安装并启动（二进制/配置复制到标准路径）
#   ./install-systemd.sh --uninstall # 卸载
#
# 默认从脚本所在目录读取 argus-agent 与 agent.json，
# 复制到 /usr/local/bin/ 与 /etc/argus-agent/ 后注册 systemd 服务。

set -euo pipefail

SERVICE_NAME="argus-agent"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${SRC_DIR}/argus-agent"
CFG_SRC="${SRC_DIR}/agent.json"
BIN_DST="/usr/local/bin/argus-agent"
CFG_DIR="/etc/argus-agent"
CFG_DST="${CFG_DIR}/agent.json"

if [[ "${1:-}" == "--uninstall" ]]; then
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    rm -f "${UNIT_FILE}"
    systemctl daemon-reload
    echo "Argus Agent 服务已卸载"
    exit 0
fi

if [[ "$(id -u)" -ne 0 ]]; then
    echo "需要 root 权限，请使用 sudo 运行。" >&2
    exit 1
fi

if [[ ! -f "${BIN_SRC}" ]]; then
    echo "未找到 Agent 二进制: ${BIN_SRC}" >&2
    exit 1
fi
if [[ ! -f "${CFG_SRC}" ]]; then
    echo "未找到配置文件: ${CFG_SRC}" >&2
    exit 1
fi

install -m 0755 "${BIN_SRC}" "${BIN_DST}"
mkdir -p "${CFG_DIR}"
install -m 0600 "${CFG_SRC}" "${CFG_DST}"
install -m 0644 "${SRC_DIR}/argus-agent.service" "${UNIT_FILE}"

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
systemctl status "${SERVICE_NAME}" --no-pager || true

echo
echo "Argus Agent 服务已安装并启动（开机自启、崩溃自动重启）"
echo "二进制: ${BIN_DST}"
echo "配置:   ${CFG_DST}"

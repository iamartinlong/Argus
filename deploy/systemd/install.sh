#!/usr/bin/env bash
# Argus 管理后台 Linux systemd 一键安装/卸载脚本（需 root）
# 用法：
#   ./install.sh [BIN_PATH]  # 安装并启动；BIN_PATH 默认 build/argus；配置不存在时生成模板
#   ./install.sh --uninstall # 卸载
#
# 完成：创建 argus 用户/目录 → 复制二进制到 /usr/local/bin → 生成默认配置 →
#       注册 systemd 服务（deploy/systemd/argus.service）→ 启动。
#
# 依赖环境变量（生成配置时）：ARGUS_JWT_SECRET、ARGUS_MASTER_KEY
# 可写入 /etc/argus.env（600）供服务注入。

set -euo pipefail

SERVICE_NAME="argus"
UNIT_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/argus.service"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
BIN_SRC="${1:-build/argus}"
BIN_DST="/usr/local/bin/argus"
CFG_DIR="/etc/argus"
CFG_FILE="${CFG_DIR}/config.yaml"
ENV_FILE="/etc/argus.env"
DATA_DIR="/opt/argus/data"

if [[ "${1:-}" == "--uninstall" ]]; then
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    rm -f "${UNIT_FILE}"
    systemctl daemon-reload
    echo "Argus 管理后台服务已卸载"
    exit 0
fi

if [[ "$(id -u)" -ne 0 ]]; then
    echo "需要 root 权限，请使用 sudo 运行。" >&2
    exit 1
fi

if [[ ! -f "${BIN_SRC}" ]]; then
    echo "未找到二进制: ${BIN_SRC}" >&2
    echo "请先构建（make build）或指定路径：./install.sh /path/to/argus" >&2
    exit 1
fi

# 创建用户与目录
id -u argus &>/dev/null || useradd -r -s /usr/sbin/nologin argus
mkdir -p "${CFG_DIR}" "${DATA_DIR}"
chown -R argus:argus "${DATA_DIR}"

# 安装二进制
install -m 0755 "${BIN_SRC}" "${BIN_DST}"

# 配置：不存在则生成模板（含密钥提示）
if [[ ! -f "${CFG_FILE}" ]]; then
    echo "生成默认配置: ${CFG_FILE}"
    cat > "${CFG_FILE}" <<EOF
server:
  listen: "0.0.0.0:8443"
storage:
  driver: "sqlite"
  dsn: "${DATA_DIR}/argus.db"
EOF
    chown argus:argus "${CFG_FILE}"
    chmod 600 "${CFG_FILE}"
fi

# 环境文件（密钥）：不存在则自动生成随机密钥（可通过 ARGUS_JWT_SECRET/ARGUS_MASTER_KEY 预置）
gen_secret() {
    if command -v openssl &>/dev/null; then
        openssl rand -hex 32
    else
        head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

if [[ ! -f "${ENV_FILE}" ]]; then
    cat > "${ENV_FILE}" <<EOF
# 随机生成的密钥（预置时通过环境变量 ARGUS_JWT_SECRET / ARGUS_MASTER_KEY 传入）
ARGUS_JWT_SECRET=${ARGUS_JWT_SECRET:-$(gen_secret)}
ARGUS_MASTER_KEY=${ARGUS_MASTER_KEY:-$(gen_secret)}
EOF
    chmod 600 "${ENV_FILE}"
    echo "已生成 ${ENV_FILE}（含随机密钥，建议核对后妥善保管）。"
fi

# 密钥安全兜底：拒绝以公开占位值启动服务
if grep -q "CHANGE_ME" "${ENV_FILE}"; then
    echo "检测到 ${ENV_FILE} 中仍为 CHANGE_ME 占位密钥，拒绝启动（可伪造管理员身份）。" >&2
    echo "请替换为随机值（openssl rand -hex 32）后重新运行。" >&2
    exit 1
fi

# 安装 systemd 单元
install -m 0644 "${UNIT_SRC}" "${UNIT_FILE}"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"

echo
echo "Argus 管理后台已安装并启动（开机自启、崩溃自动重启）"
echo "二进制: ${BIN_DST}"
echo "配置:   ${CFG_FILE}"
echo "数据:   ${DATA_DIR}"
echo "访问:   http://<服务器IP>:8443"

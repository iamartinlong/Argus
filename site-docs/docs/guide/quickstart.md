# 安装与启动

## 获取二进制

```bash
# 构建完整二进制（含前端 UI）
make build

# 或 CLI-only（无 UI）
make build-noui

# 构建 Agent
make agent
```

产物位于 `build/`：`argus`（服务端，约 30MB 含 UI）、`argus-agent`（Agent，约 9MB）。

## 初始化（推荐：安装向导）

```bash
# 直接启动（无密钥时自动进入安装模式）
argus serve --config config.yaml
```

默认监听 `:8443`，浏览器访问 `http://localhost:8443`，首次打开自动进入安装向导：

1. **欢迎**：环境检查（配置文件/服务模式/ENV 接管提示）
2. **安全配置**：一键生成 JWT 密钥与主密钥（自动写回 `config.yaml` 并备份 `.bak`），可调密码策略
3. **管理员**：设置初始用户名与密码，提交即创建（无需强制改密）
4. 安装模式需点“立即重启”进入完整模式；完整模式自动登录并（需要时）重启，随后进入系统

密钥缺失时 `serve` 不再直接报错退出，而是降级为安装模式（仅开放 `/setup` 接口与前端）。

## 初始化（备选：命令行）

```bash
# 1. 生成密钥（JWT Secret 与主密钥）
argus gen-keys

# 2. 设置环境变量（或写入 config.yaml security 段）
export ARGUS_JWT_SECRET="<gen-keys 输出>"
export ARGUS_MASTER_KEY="<gen-keys 输出的主密钥>"

# 3. 创建初始管理员（首次启动，无用户时）
argus init-admin --username admin --password "你的初始密码"
# 不传 --password 将自动生成随机密码

# 4. 启动服务
argus serve --config config.yaml
```

## 首次登录

1. 使用向导（或 `init-admin`）创建的 `admin` 与密码登录
2. 进入「SSL 渠道账户」创建签发渠道

> 忘记密码：`argus reset-password --config config.yaml --username admin --password "新口令"`（本地直连数据库，无需密钥，详见[故障排查](/ops/troubleshooting)）

## 配置文件

参考仓库根目录的 `config.yaml.example`：

```yaml
server:
  listen: ":8443"
storage:
  driver: sqlite          # sqlite | postgres（预留）
  dsn: ./data/argus.db
security:
  jwt_secret_env: ARGUS_JWT_SECRET
  master_key_env: ARGUS_MASTER_KEY
auth:
  access_token_ttl: 15m
  refresh_token_ttl: 720h
  max_login_failures: 5
  lockout_minutes: 15
  min_password_len: 12
scheduler:
  enabled: true
  scan_minutes: 60
  renew_days_before: 30
acme:
  # ca_file: ./data/ca.pem   # 自建 ACME CA / 测试环境
  # http01_port: 80
notification:
  smtp: { ... }
  webhook: { url, secret }
  im: { wecom, dingtalk, feishu }
```

## 下一步

- [签发第一张证书](/guide/first-certificate)
- [部署指南](/deploy/)

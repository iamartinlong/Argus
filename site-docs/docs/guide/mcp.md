# MCP 连接指南

Argus 是一个 **MCP（Model Context Protocol）Server**：Claude Code、Cline、Zed、Claude Desktop 等 AI Agent 连接后，可以用自然语言完成证书查询、签发、续期、吊销、部署等完整生命周期管理。

MCP 是 Anthropic 发起的开放标准。Agent 通过它"发现"Argus 提供的工具（tools），按需调用。

## 快速开始

在 Argus 服务器本机执行（Claude Code）：

```bash
claude mcp add argus -- argus mcp --config /etc/argus/config.yaml
```

连接成功后，直接用自然语言提问即可：

> 列出 30 天内过期的证书
> 给 todo.example.com 签一张证并部署到 web-prod-01
> 吊销 legacy.example.com 的证书

## 两种模式

### 本地模式（推荐先用这个）

Agent 与 Argus 装在同一台机器，`argus mcp` 以子进程被 Agent 拉起，**直连本机数据库**：

```bash
argus mcp --config /etc/argus/config.yaml
```

- 无需网络端口、无需 token
- 进程权限 = 运行用户权限（与 CLI 同等信任模型）
- 可用 `--scopes` 收敛工具集（见下文）

### 远程模式

Agent 在运维电脑上，管理 Argus 服务器。经服务端 REST API 代理：

```bash
argus mcp --config /etc/argus/config.yaml \
  --server https://cert.example.com \
  --token st_xxxxxxxx
```

token 也可用环境变量 `ARGUS_API_TOKEN` 提供（避免出现在 shell 历史里）：

```bash
export ARGUS_API_TOKEN=st_xxxxxxxx
argus mcp --server https://cert.example.com
```

远程模式下**每个工具调用都由服务端强制校验 token 的 scope 与角色**，客户端无法绕过。

## 客户端配置示例

### Claude Code

```bash
# 本地
claude mcp add argus -- argus mcp --config /etc/argus/config.yaml

# 远程（token 从环境变量读取，见上）
claude mcp add argus-remote -- argus mcp --server https://cert.example.com
```

### Cline（VS Code）

在 Cline 的 MCP 设置中添加：

```json
{
  "mcpServers": {
    "argus": {
      "command": "argus",
      "args": ["mcp", "--config", "/etc/argus/config.yaml"]
    }
  }
}
```

### Zed

`settings.json`：

```json
{
  "context_servers": {
    "argus": {
      "command": {
        "path": "argus",
        "args": ["mcp", "--config", "/etc/argus/config.yaml"]
      }
    }
  }
}
```

### Claude Desktop

`claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "argus": {
      "command": "C:\\argus\\argus.exe",
      "args": ["mcp", "--config", "C:\\argus\\config.yaml"]
    }
  }
}
```

## 工具清单

### 查询（只读）

| 工具 | 说明 | 所需 scope |
|---|---|---|
| `list_certificates` | 证书列表，支持状态/临期天数/域名关键字过滤 | `certificates:read` |
| `get_certificate` | 证书详情（SAN/有效期/渠道/状态），按 id 或域名 | `certificates:read` |
| `list_deploy_targets` | 部署目标列表（通道 × 场景 × 状态） | `deploy:read` |
| `list_accounts` | 渠道账户列表（SSL 渠道 + DNS 验证账户，凭据脱敏） | `accounts:read` |
| `list_agents` | Agent 在线状态 | `admin:read` |
| `get_task_logs` | 最近任务日志（扫描/续期/提醒） | `certificates:read` |

### 操作（写入）

| 工具 | 说明 | 所需 scope |
|---|---|---|
| `issue_certificate` | 签发证书（受 CA 速率限制，可能数分钟） | `certificates:write` |
| `renew_certificate` | 续期证书 | `certificates:write` |
| `revoke_certificate` | **吊销证书（不可逆）** | `certificates:write` |
| `import_certificate` | 导入既有证书（PEM） | `certificates:write` |
| `deploy_certificate` | 推送部署到目标 | `deploy:write` |
| `test_deploy_target` | 测试部署目标连通性 | `deploy:write` |

### 敏感操作（默认关闭）

| 工具 | 说明 |
|---|---|
| `download_certificate` | 下载证书打包文件（**含私钥**）。默认不注册；需在 `config.yaml` 配置 `mcp.allow_key_download: true`，且每次调用都会审计 |

工具入参均带 JSON Schema 校验，**未知字段会被拒绝**。

## scope 申请与最小权限

远程模式使用的 API Token 在 **系统 → API Token** 页面创建。按用途授予最小 scope：

| 用途 | 建议 scope |
|---|---|
| 只读巡检 / 到期报告 | `certificates:read`、`deploy:read` |
| 签发 + 部署自动化 | `certificates:read`、`certificates:write`、`deploy:read`、`deploy:write` |
| 需要下载私钥 | 上述 + `certificates:write`，并开启 `allow_key_download` |

本地模式可在启动时收敛工具集（不出现写工具，Agent 也无法调用）：

```bash
argus mcp --config /etc/argus/config.yaml --scopes certificates:read
```

## 安全注意事项

- **吊销不可逆**：`revoke_certificate` 会让证书立即失效，依赖它的服务会中断。工具的 description 中已内置警示，Agent 调用前通常会向你确认
- **私钥不出对话**：`download_certificate` 只返回文件名与大小，私钥内容不会进入对话文本
- **token 保管**：远程模式的 token 等同账号凭证。建议用环境变量传入、定期轮换、可随时在 API Token 页吊销
- **审计**：所有写操作走既有审计体系（`certificate.issue/renew/revoke/deploy` 等 action），可在审计日志页追溯；MCP 调用不产生新 action 类型
- **本地模式进程权限** = 运行用户权限，请以适当的系统用户运行

## 排障

| 现象 | 排查 |
|---|---|
| 连接失败 / Agent 看不到工具 | `argus mcp --config <path>` 在本机是否能启动；stdout 是否被其他日志污染（MCP 要求 stdout 只有协议帧，Argus 的日志一律走 stderr） |
| 工具调用返回权限不足 | 远程模式检查 token scope（`certificates:write` 等）与用户角色；本地模式检查 `--scopes` |
| `download_certificate` 不存在 | 默认不注册；在 `config.yaml` 设置 `mcp.allow_key_download: true` 后重启 |
| 签发长时间无响应 | ACME 签发可能耗时数分钟（DNS 传播验证）；客户端超时请调大，或先用 `get_task_logs` 看进度 |
| 提示"证书未绑定部署目标" | `deploy_certificate` 未指定 `target_ids` 且证书没有绑定任何目标；显式传 `target_ids` |

## 相关文档

- [API Token 与用户](/manual/system)
- [部署目标](/manual/deploy-targets)
- [运维：备份](/ops/backup)

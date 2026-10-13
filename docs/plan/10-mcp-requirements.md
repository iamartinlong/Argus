# 需求文档：MCP 服务（三方 Agent 连接能力）

> 版本：v1.0（规划稿） · 状态：待评审 · 关联实施计划：[11-mcp-implementation.md](11-mcp-implementation.md)

## 1. 背景与目标

Argus 已有 Web / REST API / CLI 三个人机界面。AI Agent（Claude Code、Cline、Zed、Claude Desktop 等）已成为新的操作入口，但它们需要标准协议才能调用系统能力。

**MCP（Model Context Protocol）** 是 Anthropic 发起的开放标准：MCP Server 以 JSON-RPC 2.0 向 Agent 暴露 **tools**（可执行操作）、**resources**（可读数据）、**prompts**（提示模板），传输支持 **stdio**（客户端拉起子进程）与 **streamable HTTP**（远程连接）。

本需求目标：让 Argus 成为一个 **MCP Server**，使三方 Agent 能用自然语言完成证书查询、签发、续期、吊销、部署等完整生命周期管理。

### 目标（Goals）

- G1：Argus 以 `argus mcp` 子命令提供 stdio MCP Server，本地/远程两种数据通路
- G2：v1 开放 12 个工具，覆盖查询 + 签发 + 部署完整闭环
- G3：复用既有 API Token + scope 体系，不建平行认证
- G4：敏感操作（私钥下载）默认关闭，可配置开启并审计
- G5：单二进制交付，零新增部署组件

### 非目标（Non-Goals，v1 明确不做）

- NG1：HTTP streamable HTTP 传输（M2）
- NG2：OAuth 2.1 授权服务器（M2 评估）
- NG3：resources / prompts（M2）
- NG4：用户管理、通知规则、API Token 等管理面工具（v1 只开放证书生命周期）
- NG5：改写现有 REST API 或 Web

## 2. 用户与场景

| 场景 | 客户端 | 模式 | 典型指令示例 |
|---|---|---|---|
| S1 本机运维 | Claude Code / Claude Desktop 装在 Argus 服务器 | stdio 本地 | "列出 30 天内过期的证书" |
| S2 远程管理 | Claude Code / Cline 在运维电脑 | stdio 远程（REST 代理） | "给 todo.example.com 签一张证并部署到 web-prod-01" |
| S3 巡检报告 | 任意支持 MCP 的 Agent | stdio 本地/远程 | "生成证书到期报告"（list + get 组合） |
| S4 应急吊销 | 任意 | stdio 远程 | "吊销 legacy.example.com 的证书" |

## 3. 名词定义

| 术语 | 含义 |
|---|---|
| MCP Server | 实现 MCP 协议的服务端；本文档中指 Argus 自身 |
| Tool | Agent 可调用的函数，带 JSON Schema 入参 |
| stdio 传输 | 客户端以子进程启动 Server，经 stdin/stdout 收发 JSON-RPC |
| streamable HTTP | 经 HTTP POST 收发 JSON-RPC（可选 SSE 流式），MCP 2025-03 规范 |
| scope | Argus API Token 权限粒度：`certificates:read/write`、`deploy:*`、`accounts:*`、`admin:*`、`read`、`all` |

## 4. 功能需求

### FR-1 传输与运行模式

- FR-1.1 `argus mcp` 启动 stdio Server（本地模式）：经 `newBootstrap` 直连数据库，无需网络端口
- FR-1.2 `argus mcp --server <url> --token <st_xxx>`（或环境变量 `ARGUS_API_TOKEN`）启动 stdio Server（远程模式）：经既有 REST API 代理，服务端强制 token scope
- FR-1.3 `--scopes` 旗标（本地模式）：收敛实际注册的工具集（如 `--scopes certificates:read` 只读）；远程模式由 token 自身 scope 决定（服务端强制，客户端仅作预过滤）
- FR-1.4 M2：`POST /api/v1/mcp` streamable HTTP（本需求只占位于配置与架构，不在 v1 实现）

### FR-2 工具集（v1 = 12 个）

参数均为 JSON Schema 描述；破坏性操作在工具 description 中明示风险。

| # | 工具 | 所需 scope | 入参（要点） | 行为 |
|---|---|---|---|---|
| T1 | `list_certificates` | certificates:read | `status?`、`expiring_within_days?`、`keyword?` | 证书列表（域名/状态/渠道/到期） |
| T2 | `get_certificate` | certificates:read | `id?`、`domain?`（二选一） | 证书详情 + SAN + 最近订单 |
| T3 | `list_deploy_targets` | deploy:read | — | 部署目标（通道×场景） |
| T4 | `list_accounts` | accounts:read | `type?`（acme/dns） | 渠道账户列表（脱敏） |
| T5 | `list_agents` | admin:read | — | Agent 在线状态 |
| T6 | `get_task_logs` | certificates:read | `limit?` | 最近任务日志/扫描摘要 |
| T7 | `issue_certificate` | certificates:write | `domains`、`account_id`、`verification`、`dns_provider_account_id?`、`deploy_target_ids?`、`notify_events?` | 签发（同步等待，可能达分钟级） |
| T8 | `renew_certificate` | certificates:write | `id` 或 `domain` | 续期 |
| T9 | `revoke_certificate` | certificates:write | `id` 或 `domain` | **不可逆**吊销 |
| T10 | `import_certificate` | certificates:write | `domains`、`certificate`(PEM)、`private_key`(PEM) | 导入既有证书 |
| T11 | `deploy_certificate` | deploy:write | `id` 或 `domain`、`target_ids?`（空=全部绑定目标） | 推送部署 |
| T12 | `test_deploy_target` | deploy:write | `id` | 目标连通性测试 |
| T13 | `download_certificate` | certificates:write | `id`、`format`（nginx/apache/iis/tomcat） | **默认不注册**；`mcp.allow_key_download: true` 时注册；含私钥，审计留痕 |

### FR-3 认证与授权

- FR-3.1 本地模式：进程文件访问即信任（与 CLI 同等模型），无 token 环节；`--scopes` 提供收敛手段
- FR-3.2 远程模式：每个工具调用经服务端既有认证（`X-API-Token`）与 scope 强制；客户端不缓存、不绕过
- FR-3.3 每个工具注册时声明所需 scope；本地模式启动时按 `--scopes` 过滤注册表；远程模式预过滤 + 服务端强制双保险
- FR-3.4 私钥下载（T13）三重门槛：配置开关 + `certificates:write` + 审计日志

### FR-4 安全与审计

- FR-4.1 所有写操作经既有审计体系（`certificate.issue/renew/revoke/deploy` 等 action 不变；MCP 调用不产生新 action 类型，保持审计语义稳定）
- FR-4.2 T13 下载私钥沿用既有 `authorizeKeyAccess` 语义（操作权限 + 审计）
- FR-4.3 工具 description 内置风险提示：T9 不可逆；T7/T8 受 ACME 速率限制约束；T13 输出含私钥
- FR-4.4 本地模式进程权限 = 运行用户权限（与 CLI 一致，文档明示）

### FR-5 配置

```yaml
mcp:
  enabled: false            # HTTP 传输开关（M2 生效）；stdio 子命令不受此限（显式本地调用即启用）
  allow_key_download: false # T13 注册开关（本地/远程均生效）
```

- FR-5.1 默认值：`enabled: false`、`allow_key_download: false`
- FR-5.2 `Validate()` 校验：布尔字段无需强校验；`enabled` 仅在 M2 挂载时读取
- FR-5.3 安装向导（`/setup`）不暴露 MCP 配置（保持最小可用集），高级用户编辑 `config.yaml`

### FR-6 可观测性

- FR-6.1 stdio 模式：JSON-RPC 错误按 MCP 规范返回 `{code, message}`；不向 stdout 打印任何日志（stdout 是协议通道），诊断日志走 stderr
- FR-6.2 远程模式：服务端既有请求日志（trace_id）+ 审计不受影响

## 5. 非功能需求

| # | 要求 |
|---|---|
| NFR-1 | 不引入平行认证/授权体系；scope 语义与 REST 完全一致 |
| NFR-2 | 单二进制：MCP 能力内嵌 `argus.exe`，无新部署组件 |
| NFR-3 | 分层不变：MCP 作为新 delivery（`internal/mcp`），只依赖 application 层接口，不反向依赖 httpapi |
| NFR-4 | Go 官方 SDK `github.com/modelcontextprotocol/go-sdk`（v1.8.x），不手写 JSON-RPC |
| NFR-5 | 核心层单测覆盖（工具参数校验、scope 过滤、错误映射）；stdio e2e 覆盖 initialize/tools/list/tools/call |
| NFR-6 | CI 全绿（vet/test/build）；文档站新增 MCP 连接指南 |
| NFR-7 | 工具入参禁用未知字段（与 REST `readJSON` 同策略），请求体上限 1MB |

## 6. 决策记录

| # | 决策 | 理由 | 备选（否） |
|---|---|---|---|
| D1 | 使用官方 Go SDK | 一等公民维护，stdio/HTTP/tools/resources 全覆盖，避免手写协议 | mark3labs/mcp-go（社区版，API 类似但非官方） |
| D2 | M1 = stdio 本地 + stdio 远程 | 覆盖 S1/S2 主场景且零新增网络暴露；HTTP 留 M2 | 直接上 HTTP（暴露面大，OAuth 未就绪） |
| D3 | 远程模式经 REST 代理而非直连库 | 复用服务端认证/scope/审计；客户端零信任库文件 | stdio 远程直连库（需同步 DB 文件，违背集中式模型） |
| D4 | Bearer API Token（M2 HTTP） | 复用既有 token 体系；Claude Code/Cline/Zed 均支持自定义 header | OAuth 2.1（Claude Desktop 远程连接器要求，工作量大，M2 评估） |
| D5 | 私钥下载默认关闭 | 私钥经 AI 通道外泄面最大；双门槛+审计 | 默认开启（否决：违背最小暴露） |
| D6 | `mcp.enabled` 默认 false | 门禁 HTTP 端点（唯一网络面）；stdio 为显式本地命令，等同 CLI 信任 | 同时门禁 stdio（会让 `argus mcp` 必须改配置才能用，与向导化方向相悖） |
| D7 | v1 工具 = 12+1（T13 默认关） | 覆盖完整生命周期闭环，Agent 可独立完成运维任务 | 只读起步（价值不足，需二次迭代） |
| D8 | MCP 不产生新审计 action | 审计语义稳定，报表/筛选不变 | 新增 `mcp.*` action（信息增量低，破坏既有筛选） |

## 7. 验收标准

- AC1：`claude mcp add argus -- argus mcp --config <path>` 连接成功后 `tools/list` 返回 12 个工具（T13 默认不在列）
- AC2：e2e 脚本走通 initialize → tools/list → tools/call（list_certificates / issue_certificate / deploy_certificate）
- AC3：`--scopes certificates:read` 模式下写工具不出现在 tools/list，直接调用被拒
- AC4：远程模式使用无 `certificates:write` scope 的 token 调用 T7，服务端返回 403 且不产生任何副作用
- AC5：`allow_key_download: true` 后 T13 出现且每次调用产生审计；关闭则不出现
- AC6：`go vet ./...`、`go test ./...`、`npm run build`、CI 全绿
- AC7：文档站 MCP 指南含 ≥3 个客户端配置示例，且按文档步骤可复现 AC1

## 8. 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| SDK API 变动（pre-1.0 历史） | 升级成本 | 锁定 v1.8.x；SDK 调用集中在 `internal/mcp/server.go` 单点 |
| ACME 签发分钟级阻塞 stdio | Agent 等待超时 | 工具 description 明示；客户端侧超时由用户配置；后续可加异步任务工具（M2） |
| Agent 误操作破坏性工具（吊销） | 证书失效 | description 不可逆警示；scope 收敛（只读 token）；T9 可被 `--scopes` 排除 |
| 私钥经 AI 通道外泄 | 安全事故 | D5 双门槛 + 审计；文档明示风险 |
| 远程模式 token 明文出现在客户端配置 | 凭证扩散 | 文档引导用环境变量传 token；token 可随时吊销重建 |

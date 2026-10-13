# 实施计划：MCP 服务（v1 / M1）

> 关联需求：[10-mcp-requirements.md](10-mcp-requirements.md) · 状态：待评审 · 范围：**仅 stdio（本地+远程）+ 12+1 工具**，HTTP 传输与 resources/prompts 属 M2（本文档占位说明，不在本次实施）

## 1. 里程碑拆分

### M1a 骨架（协议跑通，零业务工具）

目标：`argus mcp` 能被 MCP 客户端连接，`initialize` / `tools/list`（空列表）通过。

| # | 任务 | 产出 |
|---|---|---|
| a1 | 引入依赖 `github.com/modelcontextprotocol/go-sdk v1.8.x`，`go mod tidy` | go.mod/go.sum |
| a2 | 新建 `internal/mcp/` 包：`server.go`（SDK Server 装配、stderr 日志、CertOps 接口定义） | server.go |
| a3 | `cmd/argus/cmd/mcp.go`：`argus mcp` 子命令，本地模式走 `newBootstrap(configPath, false)` | mcp.go + root.go 注册 |
| a4 | 本地 smoke：`echo` 式 JSON-RPC 手工验证 initialize 往返（或临时 test） | 验证记录 |

### M1b 工具实现（12+1）

| # | 任务 | 产出 |
|---|---|---|
| b1 | `ops_local.go`：`CertOps` 本地实现（包 application services；`List/Get/Issue/Renew/Revoke/Import/Deploy/TestTarget/Download/ListTargets/ListAccounts/ListAgents/TaskLogs`） | ops_local.go |
| b2 | `ops_remote.go`：远程实现（REST 代理；请求头 `X-API-Token`；信封解包 `{code,data}`；错误码→Go error 映射） | ops_remote.go |
| b3 | `tools.go`：13 个工具定义（name/description/JSON Schema/scope 声明）+ handler 薄封装（参数绑定→ops 调用→结果结构化） | tools.go |
| b4 | scope 过滤：启动时按 `--scopes`（本地）或 token scope（远程预过滤）裁剪注册表；写工具 description 含风险提示 | server.go/tools.go |
| b5 | T13 条件注册：`allow_key_download` 为 true 才注册；配置读取自 `cfg`（本地）或 status 端点（远程，关闭时直接不注册） | tools.go |
| b6 | 错误映射：`domain.ErrNotFound/ErrConflict/ErrInvalidArgument/ErrUnauthorized` → MCP `InvalidParams/InvalidRequest/InternalError`；5xx 不透细节 | server.go |

### M1c 测试、配置与文档

| # | 任务 | 产出 |
|---|---|---|
| c1 | 单测：参数校验（缺字段/未知字段/二选一互斥）、scope 过滤矩阵、错误映射表驱动 | internal/mcp/*_test.go |
| c2 | stdio e2e（`-tags integration` 或 e2e 包）：spawn `argus mcp` 子进程，走 initialize→tools/list→tools/call（list/issue/deploy 各一） | internal/tests/e2e/mcp_test.go |
| c3 | `config.go` 增 `MCPConfig{Enabled, AllowKeyDownload}` + `Default()` + `Validate()`；`config.yaml.example` 同步 | config.go/example |
| c4 | 文档：`site-docs/docs/guide/mcp.md`（Claude Code/Cline/Zed/Claude Desktop 配置示例、scope 申请、安全注意事项、排障） | mcp.md + sidebar/nav |
| c5 | README「特性」与「快速开始」增 MCP 入口；CHANGELOG `[未发布]` 条目 | README/CHANGELOG |
| c6 | 全量验证：`go vet` / `go test ./...` / `npm run build` / CI；推送后按 AC 逐条复核 | 验证记录 |

## 2. 文件级改动清单

**新增**

```
internal/mcp/server.go        # SDK 装配、CertOps 接口、scope 过滤、错误映射
internal/mcp/tools.go         # 13 工具定义与 handler
internal/mcp/ops_local.go     # 本地实现（application services）
internal/mcp/ops_remote.go    # 远程实现（REST 代理）
internal/mcp/tools_test.go    # 参数/scope/错误映射单测
cmd/argus/cmd/mcp.go          # argus mcp 子命令
internal/tests/e2e/mcp_test.go# stdio e2e
site-docs/docs/guide/mcp.md   # 连接指南
```

**修改**

```
go.mod / go.sum                    # + go-sdk
cmd/argus/cmd/root.go              # 注册 mcpCmd
internal/config/config.go          # + MCPConfig 段（Default/Validate）
config.yaml.example                # + mcp 示例（注释默认关闭）
site-docs/docs/.vitepress/config.mjs# guide 下加入口
README.md / CHANGELOG.md           # 功能说明与变更记录
docs/plan/README.md                # 索引登记（本文档两篇）
```

**明确不改**：`internal/delivery/httpapi/*`（MCP 不经 httpapi；M2 挂载时再动）、`web/*`、数据库 schema。

## 3. 接口设计草案

```go
// internal/mcp/server.go（草案，实施时按 SDK API 微调）
type CertOps interface {
    ListCertificates(ctx context.Context, f ListFilter) ([]*domain.Certificate, error)
    GetCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error)
    IssueCertificate(ctx context.Context, in IssueParams) (*domain.Certificate, error)
    RenewCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error)
    RevokeCertificate(ctx context.Context, id, domainName string) error
    ImportCertificate(ctx context.Context, in ImportParams) (*domain.Certificate, error)
    ListTargets(ctx context.Context) ([]*domain.DeployTarget, error)
    DeployCertificate(ctx context.Context, id, domainName string, targetIDs []string) error
    TestTarget(ctx context.Context, id string) error
    ListAccounts(ctx context.Context, typ string) ([]AccountBrief, error)
    ListAgents(ctx context.Context) ([]*domain.Agent, error)
    TaskLogs(ctx context.Context, limit int) ([]*domain.TaskLog, error)
    DownloadCertificate(ctx context.Context, id, format string) ([]byte, string, error) // data, filename
}
```

- `ListFilter{Status string; ExpiringWithinDays int; Keyword string}`
- `IssueParams` 对齐 `application.IssueInput`（Domains/AccountID/Verification/DNSProviderAccountID/DeployTargetIDs/NotifyEvents）
- 本地实现直接调 `application.CertificateService` 等；远程实现调 REST（路径与 `certificate_handler.go` 路由一一对应）

**工具 Schema 示例（T1）**：

```json
{
  "name": "list_certificates",
  "description": "列出证书。可按状态或临期天数过滤，用于巡检与到期预警。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "status": {"type": "string", "enum": ["valid", "expiring", "expired"]},
      "expiring_within_days": {"type": "integer", "minimum": 1, "maximum": 365},
      "keyword": {"type": "string", "description": "域名模糊匹配"}
    },
    "additionalProperties": false
  }
}
```

## 4. 关键技术点

1. **SDK 用法**（v1.8.x）：`mcp.NewServer(&mcp.Implementation{Name:"argus",Version:version}, nil)` → `server.AddTool(tool, handler)` → `mcp.NewStdioServer`/`server.ServeStdio()`（以 SDK 实际 API 为准，封装在 `server.go` 单点）
2. **stdout 纯净**：SDK 接管 stdout 收发 JSON-RPC；Argus 既有 `log.Printf` 全部默认 stderr（Go log 默认 stderr，需自查无 `fmt.Println` 泄漏到 stdout——`cmd/argus/cmd` 各命令的 Println 在 mcp 模式不可达，启动路径独立）
3. **长耗时**：T7/T8 同步等待 ACME（分钟级）——SDK stdio 无默认超时，客户端自行决定；description 明示
4. **远程模式信封**：复用 `remoteClient` 同款解包（`{code:0,data}` / 错误 `{code,message}`）；HTTP 状态→error 映射与 `remoteClient.do` 保持一致
5. **配置读取**（远程模式的 T13 开关）：远程无法读服务端配置——采用「保守注册」：远程模式**不注册** T13（私钥下载仅本地模式可用），需求 FR-5 不变、文档明示。避免新增 status 字段的协议面
6. **scope 预过滤**：远程模式启动时用 token 调一次 `GET /api/v1/stats` 探测权限不可靠——改为「注册全部、由服务端强制」+ description 标注所需 scope；本地模式按 `--scopes` 精确过滤（无服务端兜底，必须精确）

## 5. 测试计划

| 层 | 内容 |
|---|---|
| 单元 | 参数校验表驱动（必填/互斥/枚举/未知字段拒绝）；scope 过滤矩阵（每种 scope 组合的 tools/list 结果）；错误映射（domain error → MCP code） |
| 集成 | stdio e2e：spawn 子进程 → initialize → tools/list（断言 12 个、T13 缺席）→ tools/call list_certificates（临时 sqlite + 种子证书）→ issue/deploy 走 fake provider |
| 回归 | `go test ./...` 全绿；既有 REST/CLI 测试不回归（零改动 httpapi，预期无影响） |
| 手工 | 按文档用 Claude Code 实连一次（AC1/AC7） |

## 6. 文档计划

`site-docs/docs/guide/mcp.md` 结构：是什么 → 快速开始（Claude Code 一行命令）→ 客户端配置示例（Claude Code / Cline / Zed / Claude Desktop）×（本地/远程）→ 工具清单表 → scope 申请与最小权限建议 → 安全注意事项（私钥/不可逆操作/token 保管）→ 排障（stdout 日志冲突、权限不足、连接失败）。

## 7. Definition of Done

- [ ] 需求文档 8 项验收标准（AC1–AC7）逐条通过并记录
- [ ] `go vet ./...` / `go test ./...` / `npm run build` / CI 全绿
- [ ] 文档站 MCP 指南上线（Pages 自动部署后可访问）
- [ ] CHANGELOG `[未发布]` 记录；`docs/plan/README.md` 索引登记
- [ ] M2 占位说明（HTTP/OAuth/resources）写入里程碑文档

## 8. 风险与缓解（实施视角）

| 风险 | 缓解 |
|---|---|
| SDK v1.8 API 与草案签名不符 | SDK 调用隔离在 server.go；b1 前先写 20 行 spike 验证 AddTool/ServeStdio 签名 |
| e2e spawn 子进程在 Windows 的路径/引号问题 | 测试用 `os.Executable()` + `-tags` 控制跳过；CI 用 ubuntu |
| `cmd/argus/cmd` 既有 Println 污染 stdio | mcp 启动路径不复用任何带 Println 的代码路径；e2e 断言 stdout 仅含 JSON-RPC |
| 远程模式错误信息不足导致 Agent 反复重试 | 错误 message 带可操作建议（如「token 缺少 certificates:write scope」） |

## 9. 工时预估（单人）

| 阶段 | 预估 |
|---|---|
| M1a 骨架 | 0.5 天 |
| M1b 工具 | 2 天（13 工具 × 本地/远程双实现） |
| M1c 测试+文档 | 1.5 天 |
| 合计 | **约 4 天** |

M2（HTTP + resources/prompts + OAuth 评估）另行排期，预估 2–3 天。

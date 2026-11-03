# 14 交接文档（Handover）

> 最后更新：2026-11-03（v1.4.0 发布后）
> 适用范围：Argus 单仓库全栈（Go 后端 + React 前端 + 文档站 + CI/CD）

## 1. 当前状态

| 项 | 状态 |
|---|---|
| 当前版本 | **v1.4.0**（已发布，2026-11-03） |
| 最近发布内容 | MCP Server（AI Agent 接入）：stdio 本地/远程双模式、12+1 工具 |
| 仓库同步 | GitHub `iamartinlong/Argus`（主）+ 内网 gitea（`origin`，main + tag 同步） |
| CI | 4 job（Go vet+test / Web typecheck+build / Cross-build / Docs Pages）+ Release（tag 触发，6 平台 zip + SHA256SUMS） |
| 未发布改动 | 无（CHANGELOG 无 `[未发布]` 段） |
| 本地开发实例 | **已关闭**（2026-11-03）：8443 服务已停、端口释放、浏览器标签页已关；临时配置 `%TEMP%\opencode\argus-web\` 保留备用。数据 `data/argus.db` 原样保留（含 `admin` 账号） |

## 2. 本次工作日志

### 2.1 MCP M1 三批（`74e28a3` → `576f8b5` → `635d6d0`）

| 批次 | 内容 | 关键决策 |
|---|---|---|
| M1a 骨架 | 引入官方 SDK `modelcontextprotocol/go-sdk v1.8.0`；`internal/mcp/server.go`（SDK 装配 + `CertOps` 接口 + stderr 日志）；`argus mcp` 子命令 | stdout 是协议通道，日志一律走 stderr；零工具阶段先验证握手 |
| M1b 工具 | 13 工具 + 本地/远程双 ops + scope 过滤 + 错误映射；`internal/infrastructure/certbundle` 从 httpapi 抽出（下载共用）；`MCPConfig` 配置段 | 用 SDK 泛型 `AddTool` + struct tag 自动推断 schema（自动 `additionalProperties:false`，NFR-7 零成本）；T13 只返回文件名与大小，**私钥文本不进对话通道**；远程模式不注册 T13（无法确认服务端开关，最小暴露） |
| M1c 测试文档 | stdio e2e（spawn 真实二进制）；文档站 `guide/mcp.md`；Web「系统 → MCP」静态指南页；README/CHANGELOG | 文档站为唯一事实源，Web 页只做摘要 + 一键复制 |

### 2.2 AC4 远程联调与两个真 bug（`3d510b5`）

自建联调环境（临时服务端 127.0.0.1:18600 + 只读/全权双 API Token + 自签证书），实测发现：

1. **`mcp.go` 漏接远程分支**：传 `--server` 仍走本地 bootstrap（要求本地 config 与库），连接直接 EOF。→ 按 `remoteEnabled()` 分流
2. **远程 ops 直 unmarshal domain 实体丢字段**：domain 实体无 json tag，REST 的 `not_after`/`primary_domain` 等下划线字段匹配不上，导入证书显示零值有效期。→ 引入 DTO 层（`certDTO`/`targetDTO`/`agentDTO`/`taskLogDTO`/`accountDTO` + `toDomain()`）

实测结论：只读 token 写操作被服务端 403 拒绝且无副作用；全权 token 导入/按域名查询/列表正常。新增 `ops_remote_test.go` 锁定 DTO 转换与错误映射。

### 2.3 `--scopes` flag 缺失（`fddbbce`）

本地验证时发现：文档与 CHANGELOG 承诺 `argus mcp --scopes certificates:read`，但 CLI **从未定义该 flag**（cobra 直接 unknown flag 退出，表现为 MCP 进程秒退、initialize 超时）。`Config.Scopes` 与过滤逻辑早有且有单测，只差接线。已补。

### 2.4 Web 指南页增强（`7e3de63`）

按用户要求：新增「客户端配置模板（JSON，一键复制）」（本地/远程两个 `mcpServers` 模板，token 用 `st_xxxxxxxx` 占位符）；去掉跳外部文档站的按钮（页面自此自包含）。

### 2.5 文档站发版前审计（`29d52a2`）

审计方法：**Web 侧边栏实际功能 × CHANGELOG 关键词 × 真实 CLI/API 路由**三方交叉。发现 7 处遗漏并全部补齐：

| # | 遗漏 | 处理 |
|---|---|---|
| 1 | 域名管理（域名台账）整页缺失（v1.3.0 主打功能） | 新增 `manual/domains.md` + 侧边栏 + API 表 + 交叉链接 + 首页 |
| 2 | `argus cert` / `argus account` 命令组零文档 | 新增 `manual/cli.md`（CLI 速查） |
| 3 | `ops/backup.md` 教 sqlite3/停机复制，未提内置命令 | 改以 `argus backup`/`restore` 为主 |
| 4 | IIS 的 SNI 主机名未文档化 | 补配置项与多站点同端口必填说明 |
| 5 | 升级指南停在 1.0→1.1 | 加「1.3.x→1.4」与「跳过版本直升 1.4」两张表 |
| 6 | 首页特性表未提域名台账/MCP | 补两项 |
| 7 | milestones 只到 v1.1.0；quickstart 缺 `mcp` 段 | 补齐 |

### 2.6 发版 v1.4.0（`10dcfc8` + tag `v1.4.0`）

CHANGELOG `[未发布]` 转正 → `root.go` version 1.3.0→1.4.0 → commit → tag → 双端推送 → CI 自动发布（6 平台 + SHA256SUMS）。CI 全绿，GitHub Release 已建，文档站 Pages 新页面全部 200。

## 3. 待办（TODO）

### P1 — 近期

| # | 事项 | 说明 | 负责方 |
|---|---|---|---|
| T1 | **WS2016 生产环境升级 v1.4.0** | 升级 = 替换二进制 + 重启，迁移自动；务必先 `argus backup`。注意：生产机密码用户侧持有，如需重置走 `argus reset-password` | 用户侧操作 |
| T2 | **M2 规划与实施** | HTTP streamable HTTP 传输 + resources/prompts + OAuth 评估，约 2–3 天。**必须先处理 SDK 的 DNS rebinding CVE 与授权模型**（stdio 无此问题，HTTP 有） | 待排期 |
| T3 | ~~清理本地开发实例~~ | **已完成（2026-11-03）**：8443 服务已停、进程无残留、端口已释放、浏览器标签页已关；临时配置 `%TEMP%\opencode\argus-web\` 保留备用（启动脚本见 §5）。数据 `data/argus.db` 原样保留 | ✅ |

### P2 — 可选增强

| # | 事项 | 说明 |
|---|---|---|
| T4 | 文档站同步 JSON 模板 | Web 页已有一键复制 JSON 模板；`guide/mcp.md` 可补同样段落保持两处一致（当前文档站是手写配置示例，内容等价） |
| T5 | MCP 远程模式 T13 | 当前远程不注册私钥下载工具。若服务端能暴露开关状态（如 `/api/v1/mcp/status`），可改为按服务端实际开关注册 |
| T6 | MCP 工具覆盖面扩展 | 当前 12+1 未含：批量续期/部署、通知规则查询、证书导出。按需增量 |
| T7 | Playwright E2E 冒烟 | 路线图 P3，M5 预留 |
| T8 | CLI 子命令补全 | `deploy-target`/`agent`/`notification` 使用频率低，已下调可选 |

### P3 — 长期（路线图）

多中心 HA（PostgreSQL 共享存储 + 会话外部化）、登录限流外部化（Redis/DB）、更多部署目标类型（FTP/IPv6）。

## 4. 关键上下文（操作手册）

### 4.1 常用命令

```powershell
# 构建
go build -o build\argus.exe .\cmd\argus

# 全量验证
go vet ./...; go test ./...
go test -tags integration ./internal/tests/e2e/ -run TestMCPStdioEndToEnd   # 需 $env:ARGUS_E2E_BIN 指向二进制

# 前端
cd web; npx tsc -b; npx vitest run; npm run build     # build 输出到 webassets/dist（go:embed 源）
cd site-docs; npm run build                            # 文档站（死链会报错）

# MCP 一键验证脚本（自建环境，跑完自动清理）
powershell -ExecutionPolicy Bypass -File $env:TEMP\opencode\argus-mcp-verify.ps1
```

### 4.2 发版流程

1. CHANGELOG 写 `## [未发布]` → 发版时转正为 `## [X.Y.Z] - 日期`
2. bump `cmd/argus/cmd/root.go` 的 `version`
3. commit → `git tag -a vX.Y.Z` → **双端推送**（`git push github main` + `git -c credential.helper=wincred push origin main`，tag 同理）
4. CI 自动：校验 CHANGELOG 有条目 → 6 平台构建 → 建 Release；文档站 Pages 同步部署

> CI guard 强制：**CHANGELOG 没有对应版本条目会直接失败**。

### 4.3 踩过的坑（重要）

| 坑 | 说明 |
|---|---|
| **jsonschema tag 不能含 `=`** | SDK 的 tag 解析器把 `acme=SSL` 当选项赋值，直接 panic。描述里用「acme 表示 SSL」 |
| **PowerShell 读子进程 stdout 要指定 UTF8** | 否则按本地代码页（GBK）解码，中文把 JSON 结构破坏（引号被吃进多字节字符）→ `$psi.StandardOutputEncoding = [Text.Encoding]::UTF8` |
| **PowerShell here-string `@"` 后不能跟内容** | 必须独占行尾，否则解析报错；复杂脚本一律落盘成 .ps1 再跑 |
| **PS 5.1 跑脚本要 CRLF + BOM** | 落盘后转换：`-replace "\r\n","\n" -replace "\n","\r\n"` + `UTF8Encoding($true)` |
| **控制台内联中文会被损坏** | 校验文件内容用落盘 + `[IO.File]::ReadAllText(path, UTF8)` 再读，别用内联中文 grep |
| **go:embed 路径是 `webassets/dist`** | 不是 `web/dist`；前端 `npm run build` 的 outDir 已指向 `../webassets/dist` |
| **domain 实体无 json tag** | 任何跨边界序列化（REST 响应 → 远程 ops）都要 DTO 层，不能直接 unmarshal 进 domain 实体 |
| **`.ac4-test/` 等联调产物** | 未 gitignore，提交前必须删；`mock-data/`、`data/` 已 gitignore |

### 4.4 测试环境变量

- `ARGUS_E2E_BIN`：指向 `build/argus.exe` 才跑 stdio e2e
- `ARGUS_JWT_SECRET` / `ARGUS_MASTER_KEY`：本机 Machine 级已存在，会影响本地实例解密（与建库时不一致则渠道凭据等加密字段解不开，登录不受影响）
- `ARGUS_API_TOKEN`：远程模式 token（也可用 `--token`）

## 5. 本地开发实例现状

| 项 | 值 |
|---|---|
| 地址 | http://127.0.0.1:8443 |
| 二进制 | `D:\opencode\SuperCert\build\argus.exe`（注意：可能是 `fddbbce` 构建，**不含 1.4.0 版本号**；要看 1.4.0 需重新 build 或用 Release 的 zip） |
| 配置 | `%TEMP%\opencode\argus-web\config.yaml`（临时目录，不污染仓库） |
| 数据 | 仓库 `data/argus.db`（gitignore；**库里基本是空的**，只有 admin 用户） |
| 管理员 | `admin` / `DevReset2026Argus`（开发环境临时值，≥12 位含大小写数字） |
| 启动脚本 | `%TEMP%\opencode\argus-web-start.ps1` |
| **当前状态** | **已停止**（2026-11-03）：进程已终止、8443 已释放、浏览器标签页已关；上述配置与数据保留，随时可重启 |

重启：`powershell -ExecutionPolicy Bypass -File %TEMP%\opencode\argus-web-start.ps1`（脚本自带健康检查等待）

停掉：`Get-CimInstance Win32_Process -Filter "Name='argus.exe'" | ? { $_.CommandLine -like "*argus-web*" } | % { Stop-Process -Id $_.ProcessId -Force }`

## 6. 风险与遗留

| 风险 | 状态 / 缓解 |
|---|---|
| M2 HTTP 传输的 DNS rebinding CVE | **未解决**，实施 M2 前必须处理（SDK 侧 + Origin/Host 校验） |
| 远程模式无法确认服务端 `allow_key_download` | 当前按最小暴露直接不注册 T13；见 TODO T5 |
| 主密钥不可轮换 | 轮换需重新加密全部私钥/凭据（规划中） |
| 生产机密码 | 用户侧持有，未记录在本仓库 |
| `data/argus.db` 本地库近乎空 | 若要有数据的验证环境，可从生产备份 restore（`argus restore`） |

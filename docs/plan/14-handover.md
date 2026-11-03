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
| 测试 | **353 个用例全绿**；函数级覆盖率 47.6%（domain 100% / certbundle 86.6% / mcp **65.1%**） |
| MCP 能力 | stdio 本地+远程（v1.4.0）+ **HTTP 传输 /mcp（未发版，见 §2.8）**；resources/prompts/OAuth 未做 |
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

### 2.7 覆盖率审计与测试补充（`d9db94b` / `5c81282` / `0ebfbc6`）

函数级覆盖率审计（44.8% 起）→ 补充 47 个用例（总 337 PASS）→ 47.5%：

| 包 | 前 | 后 | 内容 |
|---|---|---|---|
| `internal/domain` | 27.8% | **100%** | 9 个枚举 `Valid()`、**API Token scope 矩阵**（all/read/精确/多 scope/空/nil——安全关键，此前 0%）、证书状态机 `UpdateStatus`（终态保护、无有效期归 pending、30 天窗口边界） |
| `certbundle` | **0%** | **86.6%** | zip/空包、随机密码（长度+字符集+唯一性）、5 种服务器格式；iis/tomcat 用 go-pkcs12 **真解析 PFX** 校验密码/证书 CN/私钥一致 |
| `internal/mcp` | 27.8% | **60.7%** | httptest 覆盖 `do()`（信封解包、`X-API-Token`、HTTP→domain 错误映射、5xx 不透细节）、8 个远程 ops 的 method+path 映射表、`certBrief` 不泄露 PEM/私钥 |

顺带修掉两个问题：

1. **`deploy.SplitChain` 文档与实现不符**（真缺陷）：注释称"无法解析为任何证书时返回错误"，实现却静默返回空串——会导致 apache 格式下载把**空证书文件**递给用户。改为非空但解析失败才报错（空/空白输入行为不变）+ 4 个边界用例。
2. **`mcp.certBrief` 不可测**：内部读系统时钟。改为显式传 `now`（与 `domain.ComputeCoverage` 同风格），5 个调用点传 `time.Now().UTC()`。

**收敛重复实现**：`ops_local.go` 与 `ops_remote.go` 各写了一遍同样的过滤规则（刷新过渡态 + 状态/临期天数/关键字）——正是远程联调出 bug 的同类风险，且远程那份有测试、本地那份没有。抽出 `internal/mcp/filter.go` 的 `filterCertificates(certs, f, now)` 共用，两侧各减 17 行，配 26 例表驱动测试。

剩余 15 个 0% 全为 `ops_local` 薄委托方法 + `server.go Run`：前者需装配 application 服务栈而业务逻辑已在 application 层覆盖，后者由 stdio e2e 覆盖——**结论是不补**。

### 2.8 M2 第一步：HTTP 传输（`f238da3`）

**需求变更**：用户明确「客户机不会安装 argus 二进制，只会通过 MCP 连接服务器」——stdio 模式（客户端拉起本地子进程）在此场景下不可用，HTTP 传输从可选变为刚需。

| 决策 | 落地 |
|---|---|
| 挂载路径 `/mcp` | 与 Web/REST **同端口同 TLS**，不新开端口，反代配置零改动 |
| Stateless | 每 POST 自包含、无会话亲和性，反代后可水平扩容 |
| T13 | HTTP 模式下 MCP Server 即 Argus 本体，能读到服务端开关 → 改为「`allow_key_download: true` 才注册」，与 REST 下载接口暴露条件一致（补了 M1 的 T5 缺口） |
| scope 强制 | SDK 的 `getServer(req)` 钩子按请求构造 Server，用请求凭证的 scopes 过滤工具集；JWT 按角色映射（admin 全量 / operator 证书与部署读写 / readonly 只读，与 REST `requireRole` 一致） |

关键实现文件：`internal/mcp/http.go`（`NewHTTPHandler`）、`httpapi/handler.go`（挂载 + 审计钩子）、`httpapi/middleware.go`（`principal.TokenScopes` + `scopesForPrincipal`）。

**踩到并修掉的坑**：`webassets.Handler` 的 SPA 回退把 `/mcp` 当前端路由，直接返回 index.html——挂载根本不执行（不实测发现不了）。

### 2.9 本地实测（官方 SDK 客户端）

起本地服务端（`mcp.enabled: true` + 只读/全权双 token），用 **SDK 的 Streamable HTTP 客户端**连接：

| 场景 | 结果 |
|---|---|
| 只读 token | `tools/list` 6 个（写工具不出现）；`list_certificates`/`list_deploy_targets` 正常 |
| 全权 token | 12 个（5 个写工具在列）；`list_accounts`/`get_task_logs` 正常；缺参返回工具级错误 |
| 无效 token | 连接被拒（Unauthorized） |

### 2.10 审计 M2 并修复（`caff1f6`）

安全与质量审计发现 4 项，修复 2 项：

1. **【中】T13 私钥下载在 MCP 下零审计**——REST 有 `authorizeKeyAccess`（含用户/IP/UA），MCP 一个记录都不写。已加 `AuditKeyDownload` 钩子，**仅成功路径触发**（失败尝试不写，避免记成"已下载"）。**关键前提**：先查 SDK 源码确认 `connectStreamable(req.Context(), ...)` 用请求上下文建会话，principal 能穿透到工具 handler——否则审计会静默退化成 `system`。
2. **【中】每请求重建 Server：1.68ms + 764KB 分配 + 7879 allocs**。已按「scope 集合 + 开关」缓存（稳定键、条目上限 64、mutex 并发保护）。
3. **【低-中】fail-open 隐患**：principal 缺失时 `hasScope(nil)` 返回 true → 放行全部工具。当前不可达（`authenticate` 必经），但已改为 fail-closed（哨兵 `scopeDenyAll`）。
4. **【低】MCP 发起的证书操作审计记 `system`**（丢失用户归属）。REST 同样如此，属既有设计；修复需把 actor 穿透整个 service 层，影响面超出发版范围 → **记为已知缺口**。

核查确认无害：`MaxRequestBodyBytes` SDK 默认 4 MiB；stateless 无会话 ID，无 SDK 所述的会话劫持面。

新增 16 个单测 + 1 个 e2e（`TestMCPHTTPKeyDownloadAudit`：开关开→T13 注册、只读 token 看不到 T13、失败调用不产审计）；mcp 覆盖率 60.7% → **65.1%**，总用例 **353 PASS**。

## 3. 待办（TODO）

### P1 — 近期

| # | 事项 | 说明 | 负责方 |
|---|---|---|---|
| T1 | **WS2016 生产环境升级 v1.4.0 并启用 MCP HTTP** | 两步：①升级二进制（先 `argus backup`）；②`config.yaml` 加 `mcp.enabled: true` 并重启。**注意**：v1.4.0 的二进制不含 M2 的 `/mcp` 端点，需用 `f238da3` 之后的构建（或等下一个版本发布） | 用户侧操作 |
| T2 | **M2 规划与实施** | **HTTP 传输已完成并实测通过**（`f238da3` + 审计修复 `caff1f6`，见 §2.8–2.10）。剩余：**resources / prompts**（把证书清单作为上下文直喂模型、预置巡检提示模板）与 **OAuth 评估**（多用户各自登录授权，替代共享 token；当前 API Token 已够用）。DNS rebinding 已由 SDK 默认防护 + 标准库跨源防护解决，不再是阻塞项 | 待排期 |
| T3 | ~~清理本地开发实例~~ | **已完成（2026-11-03）**：8443 服务已停、进程无残留、端口已释放、浏览器标签页已关；临时配置 `%TEMP%\opencode\argus-web\` 保留备用（启动脚本见 §5）。数据 `data/argus.db` 原样保留 | ✅ |

### P2 — 可选增强

| # | 事项 | 说明 |
|---|---|---|
| T4 | 文档站同步 JSON 模板 | Web 页已有一键复制 JSON 模板；`guide/mcp.md` 可补同样段落保持两处一致（当前文档站是手写配置示例，内容等价） |
| T5 | ~~MCP 远程模式 T13~~ | **已由 M2 解决**：HTTP 模式下 MCP Server 即 Argus 本体，能直接读到 `mcp.allow_key_download`，改为按开关注册（stdio 的 REST 代理模式仍不注册，因其确实无法确认） |
| T6 | MCP 工具覆盖面扩展 | 当前 12+1 未含：批量续期/部署、通知规则查询、证书导出。按需增量 |
| T7 | Playwright E2E 冒烟 | 路线图 P3，M5 预留 |
| T8 | CLI 子命令补全 | `deploy-target`/`agent`/`notification` 使用频率低，已下调可选 |
| T9 | `internal/testutil` 拆分 | 拆为 `testcert`（纯 stdlib）+ `testlive`（重依赖），解除叶子包测试的 import cycle 陷阱；当前 certbundle 已内联绕过，属技术债清理 |
| T10 | `internal/mcp` 三个文件 gofmt 对齐 | `server.go`/`ops_remote.go`/`tools.go` 的 const 块与 map 字面量对齐为 HEAD 既有问题，刻意未顺手格式化（避免无关 diff）；可作独立提交 |
| T11 | **MCP/REST 的证书操作审计记 `system`** | `CertificateService.Issue/Revoke/...` 硬编码 `auditAction(ctx, nil, ...)`，两个通道都丢失用户归属。修复需把 actor 穿透 service 层（影响 REST），超出发版范围；MCP 有 per-user token 后问题更明显，建议优先做 |
| T12 | **M2 的 resources / prompts** | 把证书清单/临期清单作为 MCP resources 直喂模型（省去反复调工具）；预置「巡检并出报告」类 prompts。OAuth 暂不建议做（API Token 已够用） |
| T13 | **v1.4.0 之后的发版** | MCP HTTP 传输等 M2 内容累计在 `[未发布]`（新增交付能力 + 修复），按语义化版本应发 **v1.5.0** |

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

# 覆盖率（函数级；total 行是总体，按包筛 0% 函数找缺口）
go test ./... -coverprofile=coverage -covermode=atomic
go tool cover -func coverage          # 注意：文件名叫 coverage 而非 coverage.out

# MCP HTTP 传输验证（integration tag，需二进制）
$env:ARGUS_E2E_BIN = "D:\opencode\SuperCert\build\argus.exe"
go test -tags integration ./internal/tests/e2e/ -run "TestMCPHTTP" -v
# 本地手工验证：起服务端（config 里 mcp.enabled: true）→ init-admin →
# 登录改密 → 建只读/全权两个 API Token → 用 SDK StreamableClientTransport 连 /mcp

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
| **`internal/testutil` 有 import cycle 陷阱** | `live_server.go` 拖入整个 httpapi，导致叶子包（如 certbundle）测试无法复用 `GenTestCert`。**建议拆成 `testcert`（纯 stdlib）+ `testlive`（重依赖）**；当前 certbundle 测试已内联一份证书生成绕过 |
| **PowerShell here-string 匹配文件内容常失败** | 目标 Go 文件多为 LF 行尾，而 here-string 是 CRLF——匹配前先 `-replace "\r\n","\n"` 归一化；或直接用 edit 工具（ASCII 片段） |
| **PS 5.1 无 3 参数 `Replace` / `IndexOf` 后要手动拼** | `$t.Replace($a,$b,1)` 会报"找不到替换"；用 `IndexOf` + `Substring` 拼接实现单次替换 |
| **`go test -race` 本机跑不了** | 需 gcc/cgo，Windows 开发机未装；并发单测可跑，竞态检测靠 CI（Linux） |
| **SDK 客户端连不上先查响应体** | Streamable HTTP 出错时 SDK 只给状态码；用裸 HTTP 打一发看 body 才能定位（本次靠这招发现 `Bearer Bearer` 双前缀） |

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
| `internal/mcp/{server,ops_remote,tools}.go` gofmt 未对齐 | HEAD 既有，刻意未在功能提交中顺手格式化；见 TODO T10 |

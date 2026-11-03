# 06 实施里程碑

每个里程碑产出含：代码 + 对应测试 + 文档更新。完成后进入下一阶段。

## M1 骨架
- Go 项目结构（cmd/internal 分层、配置加载）
- 存储层（SQLite schema、迁移、PostgreSQL 兼容抽象）
- JWT 认证 + 用户/角色基础 + 审计日志骨架
- 测试脚手架 + CI（Linux/Windows 双平台，单元 + 集成分命令）
- 前端脚手架：Vite + shadcn/ui + 登录页 + 布局 + API 客户端封装
- 交付：可启动、可登录、健康检查通过

## M2 SSL Provider 引擎（ACME 先行）
- Provider 统一接口 + 注册表（CertificateProvider）
- ACME 实现：账户管理（含 EAB）、签发/续期/吊销（DNS-01 lego Provider、HTTP-01 内置验证服务器）
- 证书列表/详情/手动操作 API（签发请求按 provider 路由）
- Pebble 集成测试（全链路，错误注入）
- 前端：证书列表/详情/签发表单/手动操作
- 交付：ACME 真实签发可用（staging 验证）

## M2.5 云厂商 Provider
- 阿里云实现：免费/DV 证书申请 → DNS 验证 → 轮询签发 → 下载（pem）
- 腾讯云实现：申请 → DNS 验证 → 轮询签发 → 下载（pem）
- 云凭据管理（AccessKey/SecretKey 加密存储、可用性测试）
- 前端：渠道账户管理（ACME 账户 + 云凭据统一管理）
- 测试：mock 云 API（httptest stub 模拟异步签发流程）
- 交付：阿里云/腾讯云渠道可用，与 ACME 同一签发/续期/部署管线

## M3 自动化
- 调度引擎（robfig/cron）：到期扫描、自动续期、失败重试
- 通知中心：SMTP、Webhook、企业微信/钉钉/飞书机器人；多级到期提醒；失败重试
- 事件总线（certificate.issued/renewed、deploy.failed 等）
- 任务日志 + 仪表盘统计 API
- 前端：仪表盘（Recharts）、通知规则管理、任务日志实时刷新
- 测试：MailHog、Webhook stub、调度用例单测
- 交付：自动续期 + 到期通知闭环

## M4 集中部署
- Agentless SSH 通道（ed25519、凭据加密存储、连接测试）
- reload hook：Linux Nginx、Windows IIS（PowerShell 更新 https 绑定）、通用 shell/PowerShell 模板
- K8s Secret 目标、Webhook 目标
- 部署记录（校验和、结果回传）、失败重试
- 前端：部署目标管理、部署记录查看
- 测试：本地 sshd 容器、Nginx 容器 reload 验证
- 交付：集中式 SSH 自动部署可用

## M5 完善
- API Token（scope、吊销）、RBAC 完整权限矩阵
- CLI 子命令（argus serve/issue/renew/list/deploy，本地进程内 + 远程 API Token 模式）
- Windows 服务注册 / systemd unit / Docker Compose
- 打包脚本（含/不含前端产物）
- Playwright 冒烟 E2E（可选，视进度）
- 交付：可正式部署

## M6 增强（已交付，附审计说明）
- Agent 通道：Go agent 二进制（~8.8MB）出站注册、心跳、在线监控，跨平台（Linux/Windows）
- NAT/Windows 场景覆盖：agent 出站轮询任务（2-10s 间隔），执行写文件 + hook（含 IIS PowerShell 场景）
- 与 SSH 通道统一走部署目标接口（`deploy.Adapter` + `IsAsync` 异步语义，回执联动部署记录）
- 任务超时未回执自动失败（惰性清理 + 调度器扫描双保障）
- **审计说明（与计划的差距及替代方案）**：
  - mTLS：以「HTTPS 传输 + Token 认证」达成同等安全目标；mTLS 双向证书可由中心端前置反向代理提供（见 deploy/agent/README.md）
  - 自升级：以「版本上报 + 管理员下发指令 + SHA256 校验 + 原子替换」实现（见 manual/agents.md），初始规划的「远程自动升级」已落地为受控升级
  - 多中心 HA：按计划为可选，未实施（单实例 + SQLite 适用于当前规模；多实例需共享存储/Redis 会话，见路线图）

## 后续增强（已完成）

以下为原「后续规划」清单中已交付项：

| 项 | 说明 | 状态 |
|---|---|---|
| 用户管理前端页 | `/users` 完整管理页（后端 API 早已就绪） | ✅ |
| API Token scope 前端选择 | 细粒度 scope 勾选 | ✅ |
| 联系人管理前端 | 全局申请人信息，证书落快照 | ✅ |
| 任务日志保留策略 | task_logs 定期清理（保留 N 天） | ✅ |
| PostgreSQL 驱动 | D4 标注可切换，已实现 postgres 驱动 | ✅ |
| Agent 自升级 | 版本上报→管理员下发升级指令（带可执行文件 SHA256 校验） | ✅ |
| Agent 启用/禁用开关 | agents 表 `enabled`，禁用后 Token 立即失效、置离线（软白名单） | ✅ |
| Agent 心跳超时离线 | 心跳 >90s 自动置离线 | ✅ |
| Agent IIS 自动绑定 | pfx+bindings+site_name 自动生成完整脚本（appcmd） | ✅ |
| 部署目标查看/编辑 | GET/PUT 单条 + 删除级联清理 | ✅ |
| Prometheus 指标 | `/metrics` 导出 | ✅ |
| 数据备份工具 | `argus backup/restore` 子命令 | ✅ |
| 主题切换 | shadcn dark 模式切换按钮 | ✅ |
| 文档站 + README | 部署指南服务端/客户端重组、根 README | ✅ |
| DNS 验证账户持久化 | 独立表 `dns_provider_accounts` + 证书 `dns_provider_account_id`，签发/续期优先复用账户凭据 | ✅ |
| 密码重置 CLI | `argus reset-password --username X [--password Y]`：本地直连库重置（免旧密码、无需 JWT/主密钥），校验策略+吊销全部会话+清强制改密标志，随机密码时首登强制改密；审计 `password.reset`（`user_service.go` `ResetPassword`） | ✅ |
| 可视化安装向导（v1.1.0） | 首次打开自动进 `/setup`：欢迎检查→一键生成密钥（写回 config.yaml 自动备份 .bak，MASTER 落盘 `yaml:master_key`）→界面创建管理员；缺密钥时 serve 降级安装模式（仅 setup 路由），安装模式一键重启接力进完整模式（`ARGUS_RESTART_DELAY`），服务模式降级 `Restart-Service` 指引；`GET/POST /api/v1/setup/*` 公开 status/init/generate-keys，restart 走管理员鉴权 | ✅ |
| 公开发布与 CI/CD | GitHub 仓库（module 更名 `github.com/iamartinlong/Argus`，全新初始提交）；Actions 5 job（vet/test、Web 构建、跨平台产物、**tag 自动发布 Release**（6 平台 zip + SHA256SUMS + CHANGELOG 条目强制校验）、**文档站 Pages**）；内网 gitea 同步 main+tag 并切换默认分支；About/CHANGELOG/README 下载入口与功能截图 | ✅ |
| BER 编码 PFX 导入修复（v1.1.1） | 阿里云免费证书 IIS 导出为 BER（ASN.1 不定长），Go 解析器只支持 DER 导致导入失败；新增 `internal/infrastructure/asn1ber`（BER→DER 规范化：不定长/分片拍平/内嵌 ASN.1/PKCS#12 隐式变体）+ `internal/infrastructure/pkcs12fix`（规范化 + RFC 7292 MAC 重算）；`ImportPFX` 解析前自动修复；真实 PFX API 端到端验证 | ✅ |
| 列表页体验优化（v1.2.0） | ①15 个列表/详情页新增刷新按钮（共享 `RefreshButton`）+ 手动刷新遮罩动效（`TableRefreshOverlay` + `useTableRefresh`：仅包裹主动点击的刷新、最短展示 400ms、轮询页不闪、失败也收遮罩）；②签发/新建类弹窗重开不再残留上次输入（含 DNS 密钥/密码/Token，关闭即清覆盖 Esc/遮罩/X）；③签发/导入失败改为内联 Alert（常驻详细错误）+ 简短 toast；④侧边栏品牌区展示版本号（`GET /api/v1/system/version` 取二进制版本，单一事实源） | ✅ |
| 表单体验优化（v1.3.0） | ①必填项统一红星（`Label` `required` prop，补 53 处；部署目标必填规则收敛为 `isDeployFieldRequired` 单一来源驱动 zod 与红星 + 31 例防漂移测试）；②长 URL 改 Textarea（通知渠道 4 个 Webhook URL + 部署目标 Webhook URL，提交 trim）；③下拉选中显示 label 而非 value（base-ui `Select` `items`，5 处）；④空选项引导（`EmptySelectHint`：渠道账户/申请人/部署目标/Agent，点击跳转对应管理页）；⑤表单内 Select 统一 `w-full`；⑥IIS SNI 主机名支持台账选择（`DomainInput` 共享组件 + 单域名模式） | ✅ |
| 域名管理/域名台账 | 域名作为**主数据**独立管理（`domains` 表，迁移 id=19，sqlite+PG 双写）；与证书**不建映射表**——关联 = `certificates.domain_id`（引用）+ `domain_name`（副本），读取时实时计算覆盖状态五态（missing/expiring/expired/covered/monitor，通配符父域覆盖子域）；签发时经 `DomainInput`（base-ui combobox：左输入框自由输入 + 右按钮弹台账多选 + chips）从台账选择，后端按 primary domain 反查落副本；台账页含统计条/覆盖筛选/富化行；域名规范化三处重复实现收敛至 `domain.NormalizeDomain`；顺带修复 import 证书默认配置下收不到到期提醒的缺陷 | ✅ |
| MCP Server（v1.4.0） | Argus 成为 MCP（Model Context Protocol）Server：`argus mcp` 子命令以 stdio 传输启动（官方 SDK `modelcontextprotocol/go-sdk v1.8.0`），**本地模式**直连数据库（进程权限即信任，同 CLI 模型）、**远程模式** `--server/--token`（或 `ARGUS_API_TOKEN`）经 REST 代理由服务端强制 token scope；**12+1 工具**（查询 6：证书/目标/账户/Agent/任务日志；操作 6：签发/续期/吊销/导入/部署/连通性；`download_certificate` 含私钥**默认不注册**，需 `mcp.allow_key_download: true` 且每次审计，且只返回文件名与大小、私钥文本不进对话通道）；本地 `--scopes` 收敛工具集；入参 JSON Schema 校验且拒绝未知字段（SDK 泛型 `AddTool` + struct tag 自动推断 `additionalProperties:false`）；复用既有 API Token scope 与审计体系，不建平行认证；`internal/infrastructure/certbundle` 从 httpapi 抽出供下载共用；Web「系统 → MCP」静态指南页（连接命令 + 客户端 JSON 模板一键复制 + 工具清单 + Token 跳转）+ 文档站 `guide/mcp.md`；stdio e2e spawn 真实二进制；真实双 token 联调修复远程分支未接线与 REST DTO 转换两个 bug | ✅ |

## 路线图（剩余未开发）

### 近期（功能完整度）

| 优先级 | 项 | 说明 |
|---|---|---|
| P1 | M2：MCP HTTP 传输 + resources/prompts + OAuth | stdio 双模式已交付（v1.4.0）；HTTP streamable 传输、resources/prompts、OAuth 授权留 M2（约 2–3 天）。**实施前必须处理 SDK 的 DNS rebinding CVE 与授权模型**（stdio 无此问题） |
| P1 | 生产环境升级 v1.4.0 | WS2016：替换二进制 + 重启，迁移自动；先 `argus backup` |

### 中期（规模化 / 增强）

| 优先级 | 项 | 说明 |
|---|---|---|
| P2 | 多中心 HA | 单实例 + SQLite 适用当前规模；多实例需共享存储（PostgreSQL）+ 会话外部化 |
| P2 | 登录限流外部化 | 当前进程内计数，多实例需 Redis/DB 化 |
| P2 | 可扩展部署目标 | 更多目标类型（FTP、IPv6 等） |

### 可选项

| 优先级 | 项 | 说明 |
|---|---|---|
| P3 | Playwright E2E 冒烟 | M5 预留，视进度 |
| P3 | CLI 子命令补全 | 现有 `account/serve/cert/backup/root` 已覆盖核心场景；`deploy-target`/`agent`/`notification` 使用频率低，可通过 Web/API 替代，按需再补 |

### 运维建议（长期有效）

- 云厂商 AccessKey 使用最小权限子账号，并定期轮换
- 定期备份数据库文件与主密钥（两者同等保密，丢失主密钥则已加密凭据无法解密）
- 业务机 Agent 建议服务化运行（`deploy/agent/install-windows-service.ps1` 或 systemd）

### 设计备忘：DNS 验证账户持久化（已实现）

**实现**：按「独立表 + 独立页面」落地（最佳实践——机制复用、数据隔离，避免合并至 `provider_accounts` 导致语义混乱）。

- 独立表 `dns_provider_accounts`（`id/name/provider/credentials_enc/status`）+ 证书新增 `dns_provider_account_id`（`db.go:346` 迁移 `id:16`），复用 AES-GCM 加密
- `DnsProviderAccountService`（`dns_account_service.go:1`）提供 CRUD/脱敏/校验，`CertificateService`（`certificate_service.go:66`）新增 `DNSProviderAccountID` 并在签发/续期时优先解析 DNS 账户
- `POST /dns-accounts` 等独立路由（`dns_account_handler.go:1`，`handler.go:156` 挂载），签发表单 `issue-dialog.tsx:114` 改为「选择 DNS 账户（可复用）/ 手动输入」二选一
- 前端独立管理页 `dns-accounts-page.tsx:1`，侧边栏/路由同步（`app-sidebar.tsx:52`、`router.tsx:8`）

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| ACME 限额（Let's Encrypt rate limit） | staging 测试、重试退避、吊销节流 |
| IIS 证书绑定操作风险 | 部署前备份绑定、演练脚本、PowerShell 测试覆盖 |
| 私钥泄露 | 加密存储、权限加固、审计、按需下发 |
| Agent 通道复杂度 | 延后至 M6，先行 SSH 验证部署闭环 |
| 前端规模膨胀 | shadcn 组件化、页面按模块收敛、M5 后冻结 UI 范围 |

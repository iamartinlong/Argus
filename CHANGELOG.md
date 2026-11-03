# 变更记录

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

> 1.0.x 为公开仓库建立前发布的版本，此处保留变更摘要；提交历史未随公开仓库迁移。

## [1.4.0] - 2026-11-03

### 新增

- **MCP Server（AI Agent 接入）**：Argus 成为 MCP（Model Context Protocol）Server，Claude Code / Cline / Zed / Claude Desktop 等 AI Agent 可用自然语言管理证书全生命周期
  - `argus mcp` 子命令以 stdio 传输启动（官方 Go SDK `modelcontextprotocol/go-sdk` v1.8.0），零新增部署组件
  - **本地模式**直连数据库（进程权限即信任，同 CLI 模型）；**远程模式** `--server/--token`（或 `ARGUS_API_TOKEN`）经 REST 代理，服务端强制 token scope
  - **12+1 个工具**：查询 6（证书/目标/账户/Agent/任务日志）、操作 6（签发/续期/吊销/导入/部署/连通性测试）、`download_certificate`（含私钥）**默认不注册**，需 `mcp.allow_key_download: true` 且每次审计
  - 本地模式 `--scopes` 收敛工具集（如只读巡检）；工具入参 JSON Schema 校验，未知字段拒绝；吊销等破坏性操作 description 内置警示
  - 复用既有 API Token scope 与审计体系，不建平行认证、不产生新审计 action
  - Web「系统 → MCP」静态指南页（连接命令一键复制 + 工具清单 + Token 跳转）；文档站新增 MCP 连接指南
  - 配置段 `mcp.enabled`（M2 HTTP 门禁，默认 false）/ `mcp.allow_key_download`（默认 false）
  - 测试：工具注册矩阵（默认 12 / T13 条件注册 / scope 过滤）、工具调用与缺参错误、stdio e2e（spawn 真实二进制走 initialize → tools/list → tools/call）
  - HTTP streamable HTTP 传输、resources/prompts、OAuth 属 M2，另行排期
  - 远程模式经真实联调（服务端 + 只读/全权双 token）验证：只读 token 的写操作被服务端 403 拒绝且无副作用，全权 token 的导入/按域名查询/列表正常；修复了远程分支未接线与 REST 响应 DTO 转换两个问题（详见提交说明）

## [1.3.0] - 2026-11-03

### 新增

- **域名管理（域名台账）**：域名作为主数据独立管理，与证书不建映射表——关联 = 证书上的 `domain_id` 引用 + `domain_name` 副本，覆盖状态读取时实时计算
  - 覆盖五态：`missing`（缺证书）/ `expiring`（临近到期）/ `expired` / `covered` / `monitor`（仅关注）；通配符证书覆盖其子域
  - 台账页：统计条（总数/缺证书/临期/仅关注）、覆盖状态筛选、富化行（当前证书 + 到期 + 剩余天数 + 标签 + 备注）
  - 签发证书表单域名录入改为「输入或选择」组件（`DomainInput`）：左侧输入框自由输入（回车落 chip、规范化去重、Backspace 删末项），右侧按钮弹出域名台账多选面板
  - 新增 `GET /api/v1/domains`（列表富化行，所有人可读）；`POST/PUT/DELETE` 限 admin/operator；API Token 复用 certificates 读写 scope
  - 域名规范化统一收敛至 `domain.NormalizeDomain`（原三处重复实现）

### 优化

- **必填表单项统一标注红色星号**（用户反馈：只有部分字段有星号，用户不知道该填哪些）：`Label` 组件新增 `required` prop（根因修复——此前全仓仅 1 处手写星号、66 处必填字段漏标）；本轮补全 **53 处**（无条件必填 30 + 条件必填 23），覆盖签发/导入证书、部署目标（创建+编辑）、通知渠道/规则、DNS/渠道账户、用户、申请人、API Token、Agent、域名、登录/改密/安装向导等全部表单
- **部署目标必填规则收敛为单一来源**：`isDeployFieldRequired(type, scene, field)` 同时驱动 zod `superRefine` 校验与 Label 红星（原为两套独立规则，已重构 schema 改用该函数，错误文案不变）；新增 31 例**防漂移测试**（遍历 type × scene 矩阵，比对红星标记与 safeParse 报错字段必须一致）
- **长 URL 输入改用 Textarea**：通知渠道的 4 个 Webhook URL 字段（webhook/wecom/dingtalk/feishu）与部署目标的 Webhook URL 从单行 Input 改为 Textarea（随内容自动增高、可查看全文、等宽字体便于辨认参数）；提交时对 textarea 字段 trim，避免粘贴带入的换行导致请求失败
- **修复下拉框选中后显示原始 value 而非中文标签**（域名期望状态、DNS 提供商、任务日志的类型/状态筛选共 5 处）：根因是 `SelectValue` 未做 value→label 映射。改用 base-ui `Select` 的 `items` 属性（label 单一来源，触发器自动渲染 item 标签，从机制上避免此类不同步），选项数组同时驱动下拉项与触发器
- **表单下拉为空时的引导**：渠道账户 / 申请人 / 部署目标 / Agent 四类"必须先去别处创建"的下拉，选项为空时不再显示空下拉（或禁用项），改为「说明 + 跳转按钮」（共享组件 `EmptySelectHint`），点击自动关闭当前弹窗并跳到对应管理页
- **表单下拉宽度统一**：`SelectTrigger` 默认 `w-fit` 在无值时塌成一条细线；所有表单内下拉（签发证书、新建/编辑部署目标、DNS 账户、渠道、通知规则、用户、域名等）统一补 `w-full`
- **IIS 部署目标的主机名（SNI）支持"输入或选择"**：部署目标表单的 3 处 IIS 主机名字段（本机 / SSH / Agent 通道）接入域名录入组件——左侧输入框自由输入，右侧按钮从域名台账选择，与签发证书体验一致
- **域名录入组件升级为共享组件**（`components/domain-input.tsx`）并新增**单域名模式**（`single`）：不显示 chips、输入框直接展示当前值、从面板选择即替换（SNI 主机名语义）；多域名模式（签发证书）行为不变
- 组件健壮性：Esc 行为确定化（关闭面板并恢复输入显示，不再依赖 base-ui 内部 mounted 状态，也不会误清空已填主机名）；单域名模式忽略 base-ui 的 Esc 置空（用户清空走清除按钮 / Backspace）

### 修复

- **导入证书收不到到期提醒**：调度器对导入证书 `continue` 跳过续期的同时连带跳过通知分支，而 `renew_days_before`（默认 30）≥ 到期提醒最大档位（30），导致导入证书任何档位都发不出提醒。改为「跳过续期但仍走通知分支」

## [1.2.0] - 2026-11-02

### 新增

- **列表页刷新按钮**：15 个列表/详情页（证书、部署目标、DNS 账户、SSL 渠道、Agent、通知渠道/规则/发送历史、任务日志、审计日志、用户、申请人、API Token 等）工具栏新增刷新按钮（共享组件 `RefreshButton`），请求中图标旋转并禁用，无需再刷浏览器
- **刷新加载动效**：手动刷新时表格上方显示「刷新中...」半透明遮罩（旧数据保持可见、不闪空），请求结束即消失；遮罩最短展示 400ms（本地 API 过快时也不会一闪而过）；带 5s 自动轮询的页面不触发遮罩（避免周期性闪烁）。新增 `useTableRefresh` hook：只包裹用户主动点击的刷新，请求失败也保证遮罩收起
- **版本号展示**：侧边栏品牌区在「Argus」旁以小字显示当前版本（下拉菜单同步），数据源为新增接口 `GET /api/v1/system/version`（取二进制版本变量，单一事实源，避免前端 package.json 版本漂移）

### 修复

- **签发证书弹窗表单未清空**：对话框常驻挂载导致再次打开回显上次的域名/CSR/DNS 密钥（安全隐患）；打开时统一重置表单与错误态。同问题一并修复：新建用户（账号密码残留）、创建 API Token（名称/有效期残留）、添加 Agent（名称残留）
- **签发/导入失败提示易被漏看**：由单一 toast 改为「内联 Alert（常驻显示详细错误）+ 简短 toast 提醒」双通道，关闭或重试前错误不消失

## [1.1.1] - 2026-10-13

### 新增

- **公开发布**：仓库迁移至 GitHub（`github.com/iamartinlong/Argus`），module 路径同步更名；内网 gitea 同步 `main` + tag 并切换默认分支
- **CI 自动发布**：推 `v*` tag 自动交叉编译 6 平台（windows/linux/darwin × amd64/arm64）→ 打包 zip + SHA256SUMS → 创建 GitHub Release；发布前强制校验 CHANGELOG 存在对应版本条目
- **文档站 Pages**：main 推送自动构建 VitePress 并部署 GitHub Pages（`iamartinlong.github.io/Argus`）
- README 增加下载入口（Releases 平台表 + SHA256 校验）与功能展示截图（7 张，本地演示环境实拍）
- 仓库 About：Description / Website（文档站）/ 14 个 topics

### 修复

- **导入 BER 编码的 PFX 失败**（阿里云免费证书 IIS 导出为此格式）：PKCS#12 规范允许 BER（ASN.1 不定长），而 Go 解析器只支持 DER。新增 BER→DER 规范化（`internal/infrastructure/asn1ber`）+ PFX 修复管线（`internal/infrastructure/pkcs12fix`，含 RFC 7292 MAC 重算），导入前自动修复；DER 文件零成本直通
- setup handler 在传入配置缺少密钥时回退读取 `config.yaml`
- CI 干净检出 `go:embed all:dist` 失败：先构建前端再 vet/test；`webassets/dist/.gitkeep` 占位使裸 `go build/vet/test` 开箱可跑
- `vite build` 的 `emptyOutDir` 会清空占位文件：构建后自动恢复

### 文档

- 新增 `CHANGELOG.md`（Keep a Changelog 规范，含 1.0.0–1.1.1 完整版本史）
- 新增升级指南（文档站）：通用原则（备份/自动迁移/回滚）、Windows 服务、Linux systemd、Docker、Agent 五种升级路径、1.0.x→1.1 变化清单、升级后验证清单
- 用户手册新增「密码管理」章节（自助改密 + `reset-password` 全流程）
- 文档示例修正：`reset-password` 示例密码统一为满足策略的 `NewPass1234`（原 `NewPass123` 仅 10 位，低于 12 位下限）；PowerShell 代码块统一使用 `.\argus.exe` 或完整路径（PowerShell 不在当前目录查找可执行文件），并在 Windows 部署文档补充该坑位说明
- 故障排查新增「导入证书失败」小节（BER PFX / 密码错误 / PEM 不匹配）
- 清理仓库内部信息：删除陈旧重复的 `docs/openapi.yaml`（唯一事实源 `webassets/openapi.yaml`）；里程碑文档的运维遗留改写为通用建议

## [1.1.0] - 2026-10-10

### 新增

- **可视化安装向导**：首次打开自动进入 `/setup`，三步完成配置——环境检查 → 安全密钥（一键生成并写回 `config.yaml`，自动备份 `.bak`）→ 创建初始管理员。无需手动编辑配置文件或执行 `init-admin`
- `security.master_key` 支持直接写入 `config.yaml`（环境变量仍优先覆盖）；缺少密钥时 `serve` 降级为安装模式（仅开放 setup 接口）而非报错退出
- **`argus reset-password` CLI**：忘记密码时在本机直连数据库重置——免旧密码、无需 JWT/主密钥，重置后吊销该用户全部会话；不传 `--password` 则生成随机密码（首次登录强制修改）
- Windows 安装脚本：服务就绪（`/healthz` 探测，最多 30s）后自动打开浏览器；支持 `-NoBrowser`、`-Url` 参数

### 变更

- Go module 路径更名为 `github.com/iamartinlong/Argus`
- 安装模式重启需携带 `/setup/init` 下发的 `restart_token`（收敛匿名重启 DoS 面）
- 环境变量接管的密钥不再经向导写回 `config.yaml`（避免 ENV 密钥明文落盘）
- 部署/Agent 子进程终止改为进程树 kill（Windows `taskkill /T`），避免孙进程残留

### 修复

- setup handler 在传入配置缺少密钥时回退读取 `config.yaml`（密钥基线）
- `webassets/dist` 增加占位文件，修复裸 `go build/vet/test` 报 `pattern all:dist: no matching files found`
- `vite build` 清空 dist 后自动恢复 `go:embed` 占位文件（构建脚本 + 恢复脚本）

### 文档

- 用户手册新增「密码管理」（自助改密 + CLI 重置全流程）
- 故障排查新增「忘记管理员密码」；Windows 部署文档补安装向导与重置说明
- 清理仓库内网域名与内部运维信息；删除陈旧重复的 `docs/openapi.yaml`（唯一事实源为 `webassets/openapi.yaml`）

## [1.0.5] - 2026-09-02

### 新增

- Nginx 同端口多域名 SNI：`server_names` + `{domain}` 路径模板 + `nginx -t` 预检（local/ssh/agent 三通道）
- Nginx SNI 单元测试（5 用例）

## [1.0.4] - 2026-09-01

### 新增

- 任务日志摘要（`summary`，schema 迁移 18）：扫描/续期结果聚合展示，前端支持筛选、分页与详情抽屉

### 优化

- 新建表单残留重置（`CREATE_DEFAULT` + `resetCreate` 双保险）
- 删除确认由 `window.confirm`/`AlertDialog` 统一为 `Dialog`（证书/目标/吊销/部署/续期）
- 部署/续期/吊销操作增加 Loading 状态

## [1.0.3] - 2026-09-01

### 修复

- 部署记录中文乱码：远程 PowerShell 执行统一 `chcp 65001` + `OutputEncoding UTF8` + GBK 解码兼容

## [1.0.2] - 2026-09-01

### 新增

- IIS 多站点同 443 端口 SNI 绑定：`netsh hostnameport` + `SslFlags1` + `HostHeader`，支持 hostname 三通道（local/ssh/agent，留空回退主域名）

## [1.0.1] - 2026-09-01

### 修复

- Windows 服务 `1053 服务没有及时响应启动或控制请求`：原生服务化（`svc.IsWindowsService` + `svc.Run`），无需 NSSM/WinSW

## [1.0.0] - 2026-08-20

首个正式版。核心平台能力：

- 多渠道签发：ACME（lego，HTTP-01/DNS-01/EAB/自定义 CA）、阿里云、腾讯云免费证书
- 全自动续期：调度扫描、失败重试、多级到期提醒（30/14/7/1 天）
- 多通道部署：SSH（Agentless）、Agent（出站穿透 NAT）、K8s Secret、Webhook、Windows IIS（PFX 导入 + 绑定）
- 通知中心：SMTP、Webhook、企业微信/钉钉/飞书，7 类事件（含签发/部署成败、证书级订阅）
- DNS 验证账户持久化（独立表 + 独立页面，签发/续期优先复用）
- Agent 版本管理聚合与自升级（SHA256 校验 + 原子替换）、心跳超时离线、启用/禁用软白名单
- 用户/RBAC/API Token（scope 矩阵）/审计日志/任务日志保留策略
- 单二进制交付（前端 go:embed 内嵌）；Docker/systemd/Windows 服务一键部署
- 项目更名 Argus（永不眨眼的 SSL 证书守护者）+ 文档站

[1.1.0]: https://github.com/iamartinlong/Argus/releases/tag/v1.1.0

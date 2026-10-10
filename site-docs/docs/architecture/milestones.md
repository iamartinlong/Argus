# 里程碑

## 已完成（M1–M6 及增强）

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 骨架 | 分层架构、JWT 认证（强制改密/限流/RBAC）、SQLite、CI 双平台 | ✅ 完成+审计 |
| M2 SSL Provider 引擎 | ACME（lego：HTTP-01/DNS-01/EAB/自定义 CA）、证书全流程 API、Pebble 集成测试、前端证书页 | ✅ 完成+审计 |
| M2.5 云厂商渠道 | 阿里云/腾讯云免费证书（申请→验证→轮询→下载），mock 云 API 测试 | ✅ 完成+审计 |
| M3 自动化 | 调度自动续期、5 通知渠道、事件总线、任务日志、手动扫描 | ✅ 完成+审计 |
| M4 集中部署 | Agentless SSH（known_hosts）、Nginx/IIS hook、K8s/Webhook 目标、部署记录 | ✅ 完成+审计 |
| M5 完善 | API Token（scope 矩阵）、CLI（cobra 双模式）、前端内嵌单二进制、Docker/systemd/Windows 服务 | ✅ 完成+审计 |
| M6 Agent 通道 | 出站 Agent（NAT/Windows）、任务队列、回执联动部署记录、惰性超时清理 | ✅ 完成+审计 |
| IIS 支持 | 独立 iis 目标类型（PFX 合成+绑定脚本）、Agent PFX 通道（服务端合成/下载导入） | ✅ 完成+审计 |

## 已完成（后续增强项）

| 项 | 说明 | 状态 |
|---|---|---|
| Agent 启用/禁用开关 | agents.enabled 软白名单，禁用后 Token 立即失效并置离线 | ✅ |
| Agent 心跳超时离线 | 心跳 >90s 自动置离线，前端实时刷新 | ✅ |
| Agent 自升级 | 版本上报 → 管理员下发升级指令（SHA256 校验 + 原子替换） | ✅ |
| Agent IIS 自动绑定 | 配置 pfx+bindings+site_name 自动生成完整脚本（appcmd，免 WebAdministration） | ✅ |
| 部署目标查看/编辑 | GET/PUT 单条（config 解密回显）、删除级联清理记录与任务 | ✅ |
| 用户管理前端页 | `/users` 完整管理页 | ✅ |
| API Token scope 前端 | scope 勾选管理 | ✅ |
| 联系人管理前端 | 全局申请人信息（证书快照落库） | ✅ |
| 任务日志保留策略 | 每日清理（保留 N 天，可配置） | ✅ |
| PostgreSQL 驱动 | 存储抽象支持 postgres（D4 已标注可切换） | ✅ |
| Prometheus 指标 | `/metrics` 导出 | ✅ |
| 数据备份工具 | `argus backup/restore` 子命令 | ✅ |
| 主题切换 | dark 模式切换按钮 | ✅ |
| 文档站 + README | 部署指南按服务端/客户端重组、生产机清单、根 README | ✅ |
| 可视化安装向导（v1.1.0） | 首次打开自动进 `/setup`：密钥一键生成落盘 + 界面创建管理员；缺密钥降级安装模式，一键重启进完整模式 | ✅ |

## 路线图（剩余未开发）

### 近期（功能完整度）

| 优先级 | 项 | 说明 |
|---|---|---|
| P1 | CLI 子命令补全 | 现有 `account/serve/cert/backup/root`，缺 `deploy-target`、`agent`、`notification` 子命令 |

### 中期（规模化 / 增强）

| 优先级 | 项 | 说明 |
|---|---|---|
| P2 | 多中心 HA | 当前单实例 + SQLite；多实例需共享存储（PostgreSQL 已有）+ 会话外部化 |
| P2 | 登录限流外部化 | 当前进程内计数；多实例需 Redis/DB 化 |
| P2 | 可扩展部署目标 | 更多目标类型（FTP、IPv6 等） |

### 可选项

| 优先级 | 项 | 说明 |
|---|---|---|
| P3 | Playwright E2E 冒烟 | M5 预留，视进度 |

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| ACME 限额（Let's Encrypt rate limit） | staging 测试、重试退避、吊销节流 |
| IIS 证书绑定操作风险 | 部署前备份绑定、演练脚本、PowerShell 测试覆盖 |
| 私钥泄露 | 加密存储、权限加固、审计、按需下发 |
| 多中心一致性 | PostgreSQL 共享存储 + 会话/限流外部化后推进 |

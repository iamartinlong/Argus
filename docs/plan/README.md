# Argus — SSL 证书管理系统 完整计划

集中式 SSL 证书管理平台：Web 管理后台 + REST API + CLI + 后台调度服务，支持 ACME 自动签发/续期、多服务器集中部署推送、到期通知告警，跨平台（Windows / Linux）。

## 目录

| 文档 | 内容 |
|---|---|
| [00-project-structure.md](00-project-structure.md) | 完整目录结构划分与原则 |
| [01-architecture.md](01-architecture.md) | 总体架构（Clean Architecture）、部署模式、通信通道、安全设计 |
| [02-features.md](02-features.md) | 核心功能模块、数据模型、通知渠道、部署适配器 |
| [03-api.md](03-api.md) | REST API 概要、认证与权限 |
| [04-frontend.md](04-frontend.md) | 前端技术选型与页面结构 |
| [05-testing.md](05-testing.md) | 测试策略与 CI |
| [06-milestones.md](06-milestones.md) | 实施里程碑（M1–M6） |
| [07-auth.md](07-auth.md) | 用户登录认证设计（JWT 会话、API Token、RBAC、审计） |

## 关键决策记录

| # | 决策 | 理由 |
|---|---|---|
| D1 | 后端 Go 1.22+ | lego 提供最成熟 ACME 生态（150+ DNS Provider）；单二进制天然满足 Windows/Linux 双平台；调度模型贴合 |
| D2 | 前端 React + shadcn/ui + Vite | 组件源码入项目可定制，深色模式开箱即用；产物 go:embed 进二进制保持单文件分发 |
| D3 | Clean Architecture 分层 | 展示层可插拔（Web/CLI/未来 Desktop），application 层是唯一业务入口 |
| D4 | 存储默认 SQLite，可切 PostgreSQL | 单机零运维，接口抽象便于切换 |
| D5 | 集中式管理为主，兼容本地模式 | 管理服务器统管多台业务服务器；无 Agent 环境用 SSH 推送 |
| D6 | 部署通道：SSH（Agentless）先行，Agent（mTLS）后补 | SSH 零侵入覆盖主流 Linux；Agent 解决 NAT/Windows/大规模场景 |
| D7 | 集中式首选 DNS-01 验证 | 管理服务器无需公网入口，业务机无需入站 |
| D8 | 通知：SMTP + Webhook + 企微/钉钉/飞书机器人 | IM 渠道本质是带签名的 Webhook，实现成本极低；不支持个人微信（无官方 API） |
| D9 | 测试：单元 > 集成 > E2E，CI 双平台 | 依赖倒置使 domain/application 可用假实现单测，核心层覆盖率 ≥80% |
| D10 | 私钥 AES-GCM 加密存储 | 主密钥文件/环境变量保管，明文仅按需下发至业务机 |
| D11 | site-docs 文档站（已建设） | VitePress 独立工程（`site-docs/`），Markdown 驱动：快速开始/用户手册/部署指南/运维/API/架构，`make docs` 构建，产物独立发布 |
| D12 | SSL 签发渠道可扩展 | 统一 `CertificateProvider` 接口 + 注册表，内置 ACME/阿里云/腾讯云，新渠道只实现接口；渠道凭据加密存储，签发后流程与渠道无关 |

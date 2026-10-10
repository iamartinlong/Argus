# Argus — 永不眨眼的 SSL 证书守护者

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev)
[![React](https://img.shields.io/badge/React-18-61DAFB.svg)](https://react.dev)
[![Downloads](https://img.shields.io/github/downloads/iamartinlong/Argus/total.svg)](https://github.com/iamartinlong/Argus/releases)

> **Argus（阿耳戈斯）**：希腊神话中的百眼巨人，无论何时总有一双眼睛在睁着、注视着。
> SSL 证书自动化的核心正是如此——**永远盯着、到期自动续**。这就是 Argus 名字的由来。

统一的 SSL/TLS 证书管理平台：**多渠道签发、全自动续期、多通道部署、到期通知**，覆盖 Linux / Windows 跨平台。

- 签发渠道：ACME（Let's Encrypt 等）、阿里云、腾讯云免费证书，HTTP-01 / DNS-01 验证
- 自动续期：到期前自动续期、失败重试、多级到期提醒（30 / 14 / 7 / 1 天）
- 多通道部署：SSH、Agent（穿透 NAT）、K8s Secret、Webhook、Windows IIS（PFX 导入 + 绑定）
- 通知中心：SMTP、Webhook、企业微信 / 钉钉 / 飞书机器人
- 交付：单二进制（前端内嵌）、Web + CLI 双前端、Docker / systemd / Windows 服务一键部署
- 零配置上手：首次打开自动进入可视化安装向导（密钥一键生成 + 管理员创建），无需手动编辑 `config.yaml`

## 特性

| 类别 | 说明 |
|---|---|
| 🛡️ 多渠道签发 | ACME / 阿里云 / 腾讯云统一管理，渠道凭据 AES-GCM 加密存储，可扩展新渠道 |
| 🔄 全自动续期 | 调度器扫描自动续期，失败重试，到期多级提醒 |
| 🚀 多通道部署 | SSH（Agentless）、Agent（出站穿透 NAT）、K8s Secret、Webhook、Windows IIS |
| 📢 通知中心 | SMTP / Webhook / 企微 / 钉钉 / 飞书机器人，失败自动重试 |
| 💻 Web + CLI | React 管理后台 + cobra CLI（本地进程内 / 远程 API Token 模式） |
| 📦 单二进制 | 前端产物 go:embed 内嵌，Windows 服务 / systemd / Docker 一键部署 |

## 架构

```
delivery (httpapi / cli)
    ↓
application (用例编排，唯一业务入口；事件总线解耦签发/续期/部署/通知)
    ↓
domain (实体/规则/接口定义)
    ↑
infrastructure (sqlite / lego+云 SDK / 部署适配器 / 通知渠道 / 调度器)
```

- **技术栈**：Go 1.26（chi、lego、SQLite/modernc）、React 18 + TypeScript + Vite（shadcn/ui）、TanStack Query
- **核心链路**：签发 → 私钥加密入库 → 事件发布 → 自动部署（SSH/Agent/K8s/Webhook/IIS）→ 部署记录+校验和 → 通知
- **可扩展**：`CertificateProvider`（签发渠道）、`deploy.Adapter`（部署通道）、`MessageSender`（通知渠道）均可插拔

## 快速开始

### 下载预编译版本（推荐）

每次发版由 CI 自动构建并上传至 [Releases](https://github.com/iamartinlong/Argus/releases/latest)：

| 平台 | 包名 |
|---|---|
| Windows (amd64 / arm64) | `argus-v<版本>-windows-amd64.zip` / `argus-v<版本>-windows-arm64.zip` |
| Linux (amd64 / arm64) | `argus-v<版本>-linux-amd64.zip` / `argus-v<版本>-linux-arm64.zip` |
| macOS (amd64 / arm64) | `argus-v<版本>-darwin-amd64.zip` / `argus-v<版本>-darwin-arm64.zip` |

每个包内含 `argus`、`argus-agent`、`config.yaml.example`、一键部署脚本（`deploy/`）、LICENSE 与 README，另附 `SHA256SUMS.txt`：

```bash
# 校验完整性（Linux/macOS）
sha256sum -c SHA256SUMS.txt
```

### 构建

```bash
# 构建服务端（含前端产物）与 Agent
make build     # 产物 build/argus
make agent     # 产物 build/argus-agent
```

### 一键部署

| 平台 | 命令 |
|---|---|
| Linux (systemd) | `sudo make install` 或 `sudo deploy/systemd/install.sh build/argus` |
| Windows 服务 | `powershell -ExecutionPolicy Bypass -File deploy/windows/install-service.ps1` |
| Docker | `cd deploy/docker && ./up.sh` |

> 部署后访问 `http://<服务器>:8443`，**首次打开自动进入安装向导**（可视化生成密钥、创建管理员），
> 无需手动执行 `init-admin` 或编辑配置文件。忘记密码可用 `argus reset-password` 本地重置。
> 详细步骤见[文档站部署指南](#文档站)。

### 签发第一张证书

```bash
# 示例：ACME 渠道签发并自动部署
argus serve --config config.yaml
```

在 Web 后台配置签发渠道账户（ACME / 阿里云 / 腾讯云）→ 添加证书 → 签发成功后自动推送至部署目标。

## 目录结构

```
├── cmd/
│   ├── argus/            # 服务端（Web UI + API + 调度器）
│   └── argus-agent/      # 业务机 Agent（出站轮询部署任务）
├── internal/
│   ├── delivery/         # httpapi / cli
│   ├── application/      # 用例编排、事件总线
│   ├── domain/           # 实体、规则、接口定义
│   └── infrastructure/   # sqlite、lego/云 SDK、部署适配器、通知渠道、调度器
├── web/                  # React 管理后台（Vite + shadcn/ui）
├── site-docs/            # VitePress 文档站
├── deploy/               # 一键部署脚本（systemd / windows / docker / agent / iis）
├── docs/                 # 设计与计划文档（docs/plan）
└── webassets/            # 构建产物嵌入目录
```

## 部署通道

| 通道 | 适用场景 | 业务机需安装 |
|---|---|---|
| SSH | 主流 Linux，零侵入 | 无需 |
| Agent | NAT / 无入站端口 / Windows | `argus-agent` |
| Kubernetes Secret | K8s 集群 | 无需 |
| Webhook | 业务系统对接 | 无需 |
| Windows IIS | IIS 站点证书 | 可选 Agent 或 SSH |

## 文档站

完整文档位于 `site-docs/`（VitePress）：

```bash
cd site-docs && npm install && npm run dev   # 开发
cd site-docs && npm run build                 # 构建产物
```

- [快速开始](site-docs/docs/guide/quickstart.md)
- [部署指南](site-docs/docs/deploy/index.md)（服务端 / 客户端）
- [用户手册](site-docs/docs/manual/certificates.md)
- [API 参考](site-docs/docs/api/overview.md)

## 测试

```bash
make test    # 单元 + 集成测试
```

- 单元 > 集成 > E2E，核心层覆盖率 ≥ 80%
- Agent 心跳超时（90s）自动置离线；任务超时未回执自动失败

## 许可

[MIT License](LICENSE)
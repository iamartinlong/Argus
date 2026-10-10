# 总体架构

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26、chi 路由、lego（ACME）、SQLite（modernc，纯 Go） |
| 前端 | React 18 + TypeScript + Vite、shadcn/ui（Tailwind v4）、TanStack Query、Recharts |
| CLI | cobra（本地进程内 / 远程 API Token 模式） |
| Agent | 独立 Go 二进制（出站轮询任务） |
| 部署 | 单二进制（前端内嵌 go:embed）、Docker 多阶段、systemd、Windows 服务 |

## 分层

```
delivery (httpapi / cli)
    ↓
application (用例编排，唯一业务入口；事件总线解耦签发/续期/部署/通知)
    ↓
domain (实体/规则/接口定义)
    ↑
infrastructure (sqlite / lego+云 SDK / 部署适配器 / 通知渠道 / 调度器)
```

- 接口由消费者定义（application 声明，infrastructure 实现）
- 事件总线：`certificate.issued / renewed / renew.failed / revoked` → 自动部署与通知联动

## 核心链路

```
签发（ACME / 阿里云 / 腾讯云）
  → 私钥 AES-GCM 加密入库
  → 事件发布 → 自动部署（SSH / Agent / K8s / Webhook / IIS）→ 部署记录+校验和
  → 通知（SMTP / Webhook / 企微 / 钉钉 / 飞书）
调度器（扫描）→ 自动续期（<30 天）→ 到期提醒（30/14/7/1 天）→ 失败重试
```

## 可扩展性

- **SSL 渠道**：实现 `CertificateProvider` 接口 + 注册即可接入新签发渠道
- **部署通道**：实现 `deploy.Adapter`（含 `IsAsync` 异步语义）即可接入新目标
- **通知渠道**：实现 `MessageSender` 接口即可接入新通知方式
- **展示层**：Web / CLI / 未来 Desktop 共用 application 用例

# 01 总体架构

## 1.1 分层架构（Clean Architecture 的 Go 惯用落地）

```
cmd/argus/              入口：main、子命令（serve/issue/renew/list/deploy）、启动装配
internal/
├── domain/                 实体 + 领域规则（证书、订单、部署目标、通知规则）—— 零依赖，纯 Go 类型
├── application/            用例层（签发流程、续期编排、部署触发、通知触发）
│                           依赖 domain 定义的接口（CertificateStore 等），接口由消费者定义
├── infrastructure/         实现层：SQLite/PostgreSQL、lego ACME、SMTP/Webhook、SSH、K8s、调度器
├── delivery/               展示层适配，只做协议转换，不含业务
│   ├── httpapi/            REST API（Web 使用，也是远程 CLI 的通道）
│   └── cli/                cobra 子命令：本地模式进程内调用 application；远程模式走 httpapi + API Token
├── config/                 配置加载（文件 + 环境变量）
└── web/                    前端构建产物（go:embed，build tag 控制是否嵌入）
```

依赖方向：`delivery → application → domain`，`infrastructure` 实现 `application` 声明的接口。

## 1.1.1 SSL Provider 可扩展引擎

所有签发渠道统一抽象，新增渠道零业务改动：

```go
// application 层定义，infrastructure 实现
type CertificateProvider interface {
    Name() string
    Issue(ctx, req IssueRequest) (*IssuedCertificate, error)   // 含异步申请的轮询
    Renew(ctx, req RenewRequest) (*IssuedCertificate, error)   // 可选
    Revoke(ctx, req RevokeRequest) error                        // 可选
    ValidateDomain(ctx, req ValidateRequest) error              // 域名验证（DNS 等）
}

type IssuedCertificate struct {            // 统一签发结果
    CertificatePEM, PrivateKeyPEM, IssuerPEM string
    NotBefore, NotAfter time.Time
}
```

- 注册表：`RegisterProvider(name, factory)`，配置按名称选渠道
- 内置实现：`acme`（lego）、`aliyun`（阿里云数字证书管理服务 OpenAPI）、`tencent`（腾讯云 SSL 证书 API）；私有云/自建 CA 按同接口扩展
- 渠道凭据（ACME 账户密钥 / 云 AccessKey-SecretKey）统一 AES-GCM 加密存储
- 签发后的入库/部署/通知/续期流程与渠道无关，复用同一管线

## 1.2 展示层可扩展性

- Web（React SPA）→ httpapi
- CLI（argus 子命令）→ 进程内 application（本机管理）或 httpapi + API Token（远程管理）
- Desktop（未来，Wails/Tauri）→ 内嵌调用 application 或连接本地 API

新增展示层只实现协议转换，业务零改动；API Token 含 scope 限制，为程序化客户端预留。

## 1.3 部署模式

### 本地模式
Argus 装在哪台就管哪台：本机写文件 + reload hook。适合单机小规模。

### 集中式模式（核心目标）
```
管理服务器 (Argus 中心端)
  存储全部证书与私钥（密文）、任务编排、事件总线
  ├─ 通道1 Agentless SSH（M4 实现）：ed25519 密钥认证，推送证书后执行 reload hook
  ├─ 通道2 Agent（M6 实现）：业务机 5MB Go agent 出站注册，mTLS，过 NAT
  └─ 通道3 K8s API：直接写 Secret，无代理
        ├─ 业务服务器 (Linux Nginx / Windows IIS / 其他)
        ├─ 业务服务器 ...
        └─ K8s 集群
```

## 1.4 自动化链路

```
签发（DNS-01 首选）→ 证书入库（私钥加密）
→ 事件总线 certificate.issued/renewed
→ 匹配部署目标 → 加密通道推送（SSH/K8s/Webhook）→ 落盘权限加固 → reload hook（Nginx reload / IIS 绑定更新）
→ 回传部署结果 + 校验和 → 失败重试 + 通知
```

调度引擎（robfig/cron）：
- 每日扫描到期证书（<30 天自动续期）
- 到期提醒（30/14/7/1 天多级）
- 部署失败重试
- 通知失败重试

## 1.5 安全设计

| 项 | 方案 |
|---|---|
| Web 会话 | JWT（短期）+ Refresh Token |
| 程序化客户端 | API Token（scope 限制，可吊销） |
| 私钥存储 | AES-GCM 加密入库，主密钥来自文件/环境变量，可独立于数据库备份 |
| SSH 凭据 | AES-GCM 加密存储，ed25519 密钥认证 |
| Agent 通道 | mTLS（中心 CA 签发 agent 证书）、心跳、在线监控 |
| 业务机密 | 证书+私钥仅按需通过加密通道下发；明文不落管理库外 |
| 审计 | 所有敏感操作（签发/吊销/部署/权限变更）记审计日志 |

## 1.6 配置项（示例）

```yaml
server:
  listen: ":8443"
storage:
  driver: sqlite        # sqlite | postgres
  dsn: ./data/argus.db
security:
  master_key_file: ./data/master.key
  jwt_secret_env: ARGUS_JWT_SECRET
notification:
  smtp: ...
  webhook: ...
acme:
  directory_url: https://acme-v02.api.letsencrypt.org/directory
  staging_directory_url: https://acme-staging-v02.api.letsencrypt.org/directory
deploy:
  ssh: ...
agent:
  enabled: false
```

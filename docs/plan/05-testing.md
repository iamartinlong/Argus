# 05 测试策略与 CI

## 5.1 测试金字塔

单元（主体）> 集成 > E2E（冒烟）

| 层 | 类型 | 工具 | 内容 |
|---|---|---|---|
| domain | 单元 | Go testing + testify | 领域规则、有效期计算、状态机 |
| application | 单元 | 同上 | 用例编排；内存假实现注入 `CertificateStore` 等接口，不碰真实依赖 |
| delivery/httpapi | 单元 + 接口 | httptest | 协议转换、认证/RBAC、参数校验 |
| infrastructure/存储 | 集成 | testcontainers-go / docker compose | SQLite/PostgreSQL 真实存取、私钥加解密 |
| ACME 引擎 | 集成 | Pebble（Let's Encrypt 官方测试 CA，本地运行，可注入错误） | 签发/续期/吊销全链路走真实 ACME 协议，不发公网 |
| 通知 | 集成 | MailHog（SMTP 捕获）、Webhook stub | 渠道验证与失败重试 |
| 部署推送 | 集成 | 本地 SSH 容器（sshd）、Nginx 容器 reload 验证、IIS 用 PowerShell mock | 通道与 hook 验证 |
| 前端 | 组件 | Vitest + Testing Library | 表格/表单/状态组件 |
| E2E | 冒烟 | Playwright（M5 决定） | 登录 → 建证书 → 签发 → 查看日志 |

## 5.2 工程保障

- CI：GitHub Actions，Linux + Windows 双平台 `go test ./...` + 集成套件；前端 `vitest run` + `npm run build`
- 集成测试用 `//go:build integration` 隔离，`make test` / `make test-integration` 分命令
- 覆盖率目标：domain/application ≥ 80%；infrastructure 以集成测试覆盖为主，不追行数
- 测试夹具：Pebble 证书链、测试用样例私钥、内存队列假实现
- 测试脚手架在 M1 就位，随里程碑逐层补齐

## 5.3 测试数据安全

- 所有测试仅用测试专用证书/私钥/账户
- 环境变量注入测试配置，不进版本库
- CI 中敏感信息用 secrets，不在日志输出

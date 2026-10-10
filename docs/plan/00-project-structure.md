# 00 项目目录结构

```
Argus/
├── cmd/
│   └── argus/                  # 可执行入口：main.go + 子命令装配
│       ├── main.go
│       ├── serve.go                # serve 子命令（启动 HTTP + 调度器）
│       ├── install.go / uninstall.go  # Windows 服务注册 / systemd 安装
│       ├── issue.go / renew.go / list.go / deploy.go   # CLI 子命令
│       └── gen-keys.go             # 主密钥生成
│
├── internal/                       # 不被外部引用的内部代码
│   ├── domain/                     # 领域层：实体 + 规则，零外部依赖
│   │   ├── certificate.go          # Certificate 实体、状态机、有效期计算
│   │   ├── order.go                # Order 实体
│   │   ├── deploy_target.go        # DeployTarget / DeployRecord
│   │   ├── notification.go         # NotificationRule
│   │   ├── user.go                 # User / ApiToken / AuditLog
│   │   └── store.go                # 接口定义：CertificateStore 等（消费者定义）
│   │
│   ├── application/                # 用例层：业务编排唯一入口
│   │   ├── certificate_service.go  # 签发/续期/吊销/导入
│   │   ├── deploy_service.go       # 部署编排（匹配目标→推送→hook→回执）
│   │   ├── notification_service.go # 通知触发与重试
│   │   ├── scheduler_service.go    # 调度：到期扫描/自动续期/提醒
│   │   ├── auth_service.go         # 登录/JWT/Token 校验
│   │   └── events.go               # 事件总线：certificate.issued/renewed/deploy.failed
│   │
│   ├── infrastructure/             # 实现层：一切外部依赖
│   │   ├── storage/
│   │   │   ├── sqlite/             # SQLite 实现 + 迁移
│   │   │   └── postgres/           # PostgreSQL 实现（可选切换）
│   │   ├── crypto/                 # AES-GCM 私钥/凭据加解密、主密钥
│   │   ├── providers/              # SSL Provider 可扩展引擎（统一接口 + 注册表）
│   │   │   ├── provider.go         # CertificateProvider 接口与注册表
│   │   │   ├── acme/               # lego 封装：账户/签发/续期/吊销
│   │   │   │   └── dnsproviders/   # DNS Provider 配置映射
│   │   │   ├── aliyun/             # 阿里云数字证书管理服务（申请/验证/签发/下载）
│   │   │   └── tencent/            # 腾讯云 SSL 证书（申请/验证/签发/下载）
│   │   ├── deploy/
│   │   │   ├── local/              # 本机路径 + reload hook
│   │   │   ├── ssh/                # Agentless SSH 推送
│   │   │   ├── k8s/                # Secret 写入
│   │   │   ├── webhook/            # Webhook 目标
│   │   │   └── hooks/              # reload hook 执行器（nginx / iis / shell 模板）
│   │   ├── notify/
│   │   │   ├── smtp/               # 邮件
│   │   │   ├── webhook/            # 通用 webhook
│   │   │   └── im/                 # 企业微信/钉钉/飞书机器人
│   │   └── scheduler/              # robfig/cron 封装、任务记录
│   │
│   ├── delivery/                   # 展示层：只做协议转换
│   │   ├── httpapi/
│   │   │   ├── router.go           # chi 路由 + 中间件（认证/RBAC/审计）
│   │   │   ├── auth_handler.go
│   │   │   ├── certificate_handler.go
│   │   │   ├── acme_handler.go
│   │   │   ├── deploy_handler.go
│   │   │   ├── notification_handler.go
│   │   │   ├── system_handler.go
│   │   │   └── dto/                # API 请求/响应模型
│   │   └── cli/                    # cobra 命令：本地进程内 或 远程 API Token
│   │
│   ├── config/                     # 配置加载与校验
│   │   ├── config.go
│   │   └── config.yaml.example
│   │
│   └── webassets/                  # embed.FS 挂载 web/dist（build tag 控制）
│
├── web/                            # 前端项目（独立 npm 工程）
│   ├── src/
│   │   ├── app/                    # 路由、布局、Provider
│   │   ├── components/ui/          # shadcn 组件
│   │   ├── components/             # 业务组件（DataTable、状态徽标、时间线…）
│   │   ├── features/               # 按模块：auth / certificates / acme /
│   │   │                           #       deploy-targets / notifications / logs / system
│   │   ├── lib/                    # API 客户端、查询 hooks、Zod schema
│   │   └── styles/
│   ├── index.html
│   ├── vite.config.ts
│   ├── package.json
│   └── ...
│
├── site-docs/                      # 文档站（独立 npm 工程，规划预留，后续建设）
│   ├── docs/                       # Markdown 内容：快速开始 / 部署指南 /
│   │                               #       用户手册 / API 参考 / 运维 FAQ
│   ├── config/                     # 站点配置（导航/侧边栏/主题）
│   ├── package.json
│   └── ...                         # 与 web 独立构建，产物可发布独立静态站点
│
├── deploy/                         # 部署产物
│   ├── docker/                     # Dockerfile + docker-compose.yml
│   ├── systemd/argus.service
│   └── windows/                    # 服务安装脚本、卸载脚本
│
├── docs/
│   └── plan/                       # 计划文档（本目录）
│
├── scripts/                        # make / 打包 / CI 辅助脚本
├── Makefile
├── go.mod
├── .github/workflows/ci.yml        # 双平台 CI
└── .gitignore
```

## 划分原则

1. **依赖方向单向**：`delivery → application → domain`，infrastructure 只实现 domain/application 声明的接口，不反向引用
2. **application 无框架代码**：不 import gin/chi、不 import lego，纯编排逻辑，可用假实现单测
3. **infrastructure 按外部依赖分包**：存储/加密/ACME/部署/通知/调度互不耦合
4. **前端按 features 分模块**：与 API 资源一一对应，避免 components 大杂烩
5. **CLI 与 Web 共用 application**：cli 子命令直接装配 application 或走 HTTP，两种模式同一用例

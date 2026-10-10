# 目录结构

```
Argus/
├── cmd/
│   ├── argus/          # 服务端与 CLI（cobra：serve/init-admin/reset-password/gen-keys/cert/account）
│   └── argus-agent/    # Agent 二进制（出站轮询任务）
├── internal/
│   ├── domain/             # 实体/规则/接口定义
│   ├── application/        # 用例编排 + 事件总线
│   ├── infrastructure/
│   │   ├── storage/sqlite/ # SQLite（迁移 v1-v7）
│   │   ├── crypto/         # AES-GCM / JWT
│   │   ├── providers/      # SSL 渠道：acme / aliyun / tencent
│   │   ├── deploy/         # 部署适配器：local / ssh / iis / agent / k8s / webhook
│   │   ├── notify/         # 通知渠道：smtp / webhook / wecom / dingtalk / feishu
│   │   └── scheduler/      # cron 调度 + 任务日志
│   ├── delivery/
│   │   ├── httpapi/        # REST API（含 agent 出站接口、Web UI 托管）
│   │   └── cli/            # CLI 命令（本地/远程双模式）
│   └── config/             # 配置加载
├── web/                    # React 前端（Vite，产物输出 webassets/dist 供 embed）
├── webassets/              # go:embed 前端产物（noui tag 可跳过）
├── site-docs/              # 文档站（VitePress）
├── deploy/                 # docker / systemd / windows / agent / iis
├── docs/plan/              # 计划与审计文档
└── config.yaml.example
```

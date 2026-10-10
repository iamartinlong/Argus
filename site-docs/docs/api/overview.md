# API 概览与认证

基础路径：`/api/v1`。所有接口返回 JSON（下载类除外）。

## 认证

| 方式 | 头 | 适用 |
|---|---|---|
| JWT 会话 | `Authorization: Bearer <token>` | Web 后台（登录返回 access_token） |
| API Token | `X-API-Token: <token>` | CLI 远程、脚本、CI |
| Agent Token | `X-Agent-Token: <token>` | Agent 出站接口 |

强制改密：初始密码用户仅可访问 `PUT /users/me/password`，其余接口 403。

## 错误格式

```json
{ "error": "错误描述" }
```

常见状态码：400 参数错误 / 401 未认证 / 403 无权限 / 404 不存在 / 409 冲突 / 422 签发失败（含证书信息）。

## 主要端点

| 分组 | 路径 |
|---|---|
| 认证 | `/auth/login` `/auth/refresh` `/auth/logout` |
| 证书 | `/certificates`（CRUD + renew/revoke/deploy/import/download/orders/deploy-records，支持 `dns_provider_account_id`） |
| 渠道账户 | `/acme-accounts`（CRUD + test + providers）、`/dns-accounts`（CRUD + providers，ACME DNS-01 复用） |
| 部署 | `/deploy-targets`（CRUD + test）、`/task-logs/scan` |
| 通知 | `/notification-rules`、`notification-channels`、`notification-templates`、`notification-send-logs`（含导出）、`channels` 列表 |
| 系统 | `/users`、`/api-tokens`、`/agents`（含 `binaries`/`upgrade`/`enabled`/`batch-upgrade`）、`/audit-logs`、`/task-logs`、`/stats`、`/metrics` |
| Agent | `/agent/register` `/agent/tasks` `/agent/tasks/{id}/result` `/agent/upgrade` `/agent/binaries/{version}/download` |

## API Token Scope

创建 Token 时指定 scope，请求按路径+方法自动匹配：

- `all`：继承用户角色全部权限
- `read`：全部只读
- 资源级：`certificates/accounts/deploy/notify/admin` 的 `:read` / `:write`

未知路径默认要求 `admin:read/write`（安全默认）。

# 通知中心

按事件触发通知，支持多渠道组合、失败重试与模板变量。

## 事件

| 事件 | 触发时机 |
|---|---|
| 证书到期提醒 `cert_expiring` | 到期前 N 天（30/14/7/1 可配，防重复） |
| 续期成功 `renew_success` | 自动/手动续期完成 |
| 续期失败 `renew_failed` | 续期失败（含错误信息） |
| 签发成功 `issue_success` | 签发完成 |
| 签发失败 `issue_failed` | 签发失败 |
| 部署成功 `deploy_success` | 部署完成 |
| 部署失败 `deploy_failed` | 部署失败 |

## 渠道

渠道在界面“通知渠道”中创建，凭据 AES-GCM 加密存储，支持启用/停用、连接测试。

| 渠道 | 类型 | 配置字段 | 说明 |
|---|---|---|---|
| 邮件 | `smtp` | `host/port/username/password/from/use_tls` | 支持 465 隐式 TLS 或 STARTTLS |
| 通用 Webhook | `webhook` | `url/secret` | POST JSON，`X-Signature: HMAC-SHA256(secret, body)` |
| 企业微信机器人 | `wecom` | `webhook_url/key` |  |
| 钉钉机器人 | `dingtalk` | `webhook_url/key` | 钉钉加签（timestamp + sign） |
| 飞书机器人 | `feishu` | `webhook_url` |  |

旧 `config.yaml:15-21` 的 `notification.*` 仅作首次初始化种子导入，后续以界面为准。

## 通知规则

“通知规则”关联事件与渠道，支持多渠道组合与到期提前天数（仅 `cert_expiring` 生效）。

- 创建：名称、事件、渠道（`channel_ids`）、提前天数、启用
- 过滤：按 `event` 查询规则（`GET /notification-rules?event=cert_expiring`）

## 模板变量

“通知规则”中的模板支持变量（`PUT /notification-templates/{event}`）：

- `{domain}` / `{expires}` / `{days_left}` / `{status}` / `{error}`
- 标题/正文均空即删除自定义模板，回退默认文案

## 发送历史与导出

- 历史：`GET /notification-send-logs?limit&offset&status&keyword`（按规则/渠道/证书模糊搜索），分页
- 导出：`GET /notification-send-logs/export`（上限 10000 条，CSV）
- 失败自动重试 3 次，发送与防重记录可按保留天数清理

## 内容示例

- 到期提醒：域名、到期时间、剩余天数
- 续期/签发/部署结果：新有效期或错误信息、部署目标

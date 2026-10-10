# 02 功能模块与数据模型

## 2.1 功能模块

| 模块 | 功能 |
|---|---|
| 证书管理 | 导入/列表/详情/下载（pem/crt/key）、到期状态与倒计时、手动签发/续期/吊销/重推、导出 |
| SSL Provider 引擎 | 可扩展签发渠道：ACME（lego，HTTP-01/DNS-01、EAB、staging/生产切换）、阿里云、腾讯云；统一接口与注册表，新渠道只实现 Provider 接口 |
| 调度自动化 | 到期扫描、自动续期、通知触发、部署触发、失败重试 |
| 部署推送 | 本机路径、SSH 远程主机、K8s Secret、Webhook；前置/后置 hook（reload 命令） |
| 通知中心 | SMTP 邮件、通用 Webhook、企业微信/钉钉/飞书机器人；多级到期提醒；失败重试 |
| 系统管理 | 用户与角色（管理员/操作员/只读）、API Token、审计日志、任务日志、健康检查 |

## 2.2 首批 reload hook 适配器

| 目标 | reload 动作 |
|---|---|
| Linux Nginx | 写入站点证书路径 + `nginx -s reload` 或 `systemctl reload nginx`（可配置） |
| Windows IIS | 写入证书文件 + PowerShell 更新 https 绑定（netsh/WebAdministration） |
| 通用 shell / PowerShell | 用户自定义命令模板（占位符：证书路径/域名） |
| K8s | 更新 Secret + 可选触发 Deployment 滚动重启 |
| Webhook | POST 证书信息 JSON 到业务系统，由对方处理 |

## 2.3 数据模型

```
Certificate: id, domains(SAN), status(active/expiring/expired/revoked/failed),
             ca, issued_at, not_before, not_after, private_key_enc(密文),
             verification(http/dns), acme_account_id, created_at, updated_at

AcmeAccount: id, directory_url, contact, private_key_enc(密文), eab_config

Order:       id, certificate_id, type(issue/renew/revoke), status(running/success/failed),
             error, attempts, started_at, finished_at

DeployTarget: id, name, type(local/ssh/k8s/webhook), config(加密凭据),
              cert_path, cert_key_path, pre_hook, post_hook, status, last_deploy_at

DeployRecord: id, target_id, certificate_id, status, checksum, message, deployed_at

NotificationRule: id, name, event(cert_expiring/renew_success/renew_failed/deploy_failed),
                  channels(邮件/Webhook/IM), days_before(30/14/7/1), enabled

User:        id, username, password_hash, role(admin/operator/readonly), status
ApiToken:    id, user_id, name, token_hash, scopes, expires_at, last_used_at
AuditLog:    id, user_id, action, resource, detail, ip, created_at
TaskLog:     id, task_type, status, detail, created_at   # 调度任务运行记录
```

## 2.4 通知渠道实现细节

| 渠道 | 实现 |
|---|---|
| SMTP 邮件 | 内置，支持 TLS/认证/模板（标题+正文含证书信息与链接） |
| 通用 Webhook | 裸 POST JSON，可加自定义 Header/签名密钥 |
| 企业微信机器人 | Webhook + HMAC-SHA256 时间戳签名 |
| 钉钉机器人 | Webhook + HmacSHA256 签名 |
| 飞书机器人 | Webhook 直发（无签名） |

不支持：微信公众号模板消息（需认证且面向 C 端）、个人微信（无官方 API，第三方不合规）。

## 2.5 通知场景

- 到期提醒：到期前 30/14/7/1 天（可配置），邮件 + IM 双通道
- 续期成功 / 续期失败：即时通知
- 部署成功 / 部署失败：即时通知，失败重试

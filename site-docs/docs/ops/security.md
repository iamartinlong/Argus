# 安全建议

## 传输与访问

- 生产环境对外提供 **HTTPS**（反向代理），并设置 `auth.cookie_secure: true`
- 管理后台建议限制来源 IP 或启用额外认证
- Agent / 业务机 `server_url` 必须 HTTPS；如需 mTLS，在反向代理层启用客户端证书

## 密钥管理

- `ARGUS_JWT_SECRET`、`ARGUS_MASTER_KEY` 使用独立随机值，权限 0600 / 环境文件保护
- 主密钥离线备份；数据库与主密钥分开存放
- 云渠道使用**最小权限子账号**（SSL 证书 + DNS 操作），定期轮换

## 凭据与证书

- SSH 部署目标凭据 AES-GCM 加密存储；建议使用专用部署账号 + `known_hosts` 严格校验
- API Token 按 scope 最小化，长期 Token 定期轮换；Token 泄漏立即吊销重建
- 私钥下载仅 operator+，全程审计；Webhook 目标会收到含私钥明文，务必 HTTPS

## 审计与监控

- 定期查看审计日志（登录、私钥访问、权限变更）
- 配置通知规则：续期失败与到期提醒必开
- 关注任务日志中的部署失败与 agent 离线

## 已知限制

- 登录限流为进程内存态（单实例有效；多实例需外部化）
- 任务日志按保留期自动清理（`scheduler.task_log_retention_days`，默认 90 天）
- 初始管理员密码强制修改；禁用账户立即吊销其会话
- Agent 自升级二进制由 admin 上传并 SHA256 校验；上传渠道需经 HTTPS

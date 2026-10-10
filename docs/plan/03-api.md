# 03 REST API 概要

基础路径：`/api/v1`。Web 会话用 JWT（`Authorization: Bearer`），程序化客户端用 API Token（`X-API-Token`）。

## 3.1 认证与系统

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /auth/login | 登录，返回 JWT |
| POST | /auth/refresh | 刷新令牌 |
| GET | /healthz | 健康检查 |
| GET | /stats | 仪表盘统计（总数/到期分布/续期成功率） |

## 3.2 证书

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /certificates | 签发请求（域名、验证方式、ACME 账户、可选部署目标） |
| GET | /certificates | 列表（筛选：状态/到期/搜索） |
| GET | /certificates/{id} | 详情 + 订单时间线 |
| GET | /certificates/{id}/download | 格式打包下载（format=nginx/apache/iis/tomcat/other → zip；含私钥需权限+审计；兼容旧 kind） |
| POST | /certificates/{id}/renew | 手动续期 |
| POST | /certificates/{id}/revoke | 吊销 |
| POST | /certificates/{id}/deploy | 重新部署 |
| POST | /certificates/import | 导入已有证书（pem+私钥） |

## 3.3 ACME 账户

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST/PUT/DELETE | /acme/accounts | 账户 CRUD（含 EAB 配置） |

## 3.4 部署目标

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST/PUT/DELETE | /deploy-targets | CRUD（type: local/ssh/k8s/webhook） |
| POST | /deploy-targets/{id}/test | 连接测试 |
| GET | /deploy-records | 部署记录（结果/校验和） |

## 3.5 通知规则

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST/PUT/DELETE | /notification-rules | CRUD |
| POST | /notification-rules/{id}/test | 发送测试消息 |

## 3.6 系统管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST/PUT/DELETE | /users | 用户管理（仅管理员） |
| GET/POST/DELETE | /api-tokens | API Token 管理 |
| GET | /audit-logs | 审计日志 |
| GET | /task-logs | 任务日志 |

## 3.7 权限矩阵

| 角色 | 能力 |
|---|---|
| admin | 全部，含用户/Token/审计 |
| operator | 证书签发/续期/部署、目标/规则管理，不可管理用户 |
| readonly | 仅查询 |

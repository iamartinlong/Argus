# 07 用户登录认证设计

## 7.1 认证体系概览

两类身份 + 三种角色：

| 身份 | 凭证 | 用途 |
|---|---|---|
| Web 用户 | JWT 会话（用户名/密码登录） | Web 管理后台 |
| 程序化客户端 | API Token | CLI 远程模式、脚本、集成 |

角色：`admin`（全部） / `operator`（证书与部署操作） / `readonly`（只读）。

## 7.2 登录与会话流程

```
首次启动 → 无用户时引导创建初始 admin（argus init-admin，密码强制修改）
登录    POST /api/v1/auth/login → bcrypt 校验
        → 签发 Access Token (JWT, 15 分钟) + Refresh Token (随机不透明, 30 天)
刷新    POST /api/v1/auth/refresh → 旧 Refresh 作废并发新（轮换，防重放）
登出    POST /api/v1/auth/logout → 吊销 Refresh + 加入吊销列表
```

- **Access Token**：JWT HS256（对称密钥，env 注入），payload 含 `sub/user_id/role`，短时
- **Refresh Token**：随机 128bit 不透明串，库中存 hash，支持吊销与轮换
- **前端保管**：Access 存内存（Zustand），Refresh 走 **httpOnly + SameSite=Strict Cookie**，防 XSS 窃取；跨域部署时配置 Secure + 显式 Domain

## 7.3 账户安全

| 项 | 策略 |
|---|---|
| 密码存储 | bcrypt（cost 12） |
| 密码策略 | ≥12 位，含大写/小写/数字（可配置） |
| 登录限流 | IP + 用户名维度滑动窗口（默认 5 次失败锁 15 分钟），防暴力破解 |
| 初始账户 | 首次启动引导创建；初始化密码登录后强制改密 |
| 会话失效 | 登出/改密/禁用用户立即吊销该用户全部 Refresh |

## 7.4 API Token（程序化客户端）

- 创建：POST /api/v1/api-tokens，返回明文一次（之后不可见），库中存 hash
- 属性：`name`、`scopes`（如 `certificates:read`、`certificates:write`、`deploy:write`、`admin:read`…）、`expires_at`（可选）、`last_used_at`
- 认证：`X-API-Token` 头；中间件校验 hash + 有效期 + scope；吊销即刻生效（DB 标记）
- 安全：scope 最小化建议，长期 token 定期轮换

## 7.5 RBAC 与中间件链

```
认证中间件（JWT 或 API Token）→ 角色校验 → 资源级校验 → 审计记录
```

| 角色 | 登录/后台 | 证书签发/续期/吊销 | 部署操作 | 目标/规则管理 | 用户/Token/审计 |
|---|---|---|---|---|---|
| admin | 是 | 是 | 是 | 是 | 是 |
| operator | 是 | 是 | 是 | 是 | 否 |
| readonly | 是（查） | 否 | 否 | 否 | 否 |

## 7.6 审计日志

- 记录：登录成功/失败、刷新、Token 创建/吊销、改密、所有写操作（签发/续期/吊销/部署/权限变更）
- 字段：`user_id、action、resource、detail、ip、user_agent、created_at`，只追加不可改

## 7.7 前端认证流程

- 登录页 → `auth` store → 路由守卫（未登录跳 `/login`，按角色过滤菜单与按钮）
- axios 拦截器：401 → 用 Refresh 刷新 Access → 重试原请求；刷新失败 → 清除会话跳登录
- 会话过期提示与静默续期；登出清理内存态 + 服务端吊销

## 7.8 API 一览

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /api/v1/auth/login | 登录 |
| POST | /api/v1/auth/refresh | 刷新会话 |
| POST | /api/v1/auth/logout | 登出 |
| PUT | /api/v1/users/me/password | 改密（当前用户） |
| GET/POST/PUT/DELETE | /api/v1/users | 用户管理（admin） |
| GET/POST/DELETE | /api/v1/api-tokens | API Token 管理（admin/operator 自管） |
| GET | /api/v1/audit-logs | 审计查询（admin） |

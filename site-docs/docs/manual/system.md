# API Token 与系统

## API Token

程序化客户端认证（CLI 远程模式、脚本、CI 集成）：

- 请求头：`X-API-Token: <token>`（Token 以 `st_` 开头）
- 创建时仅显示一次；sha256 哈希存储
- **Scope 权限**：
  - `all`：继承创建者角色全部权限
  - `read`：全部只读
  - 资源级：`certificates:read|write`、`accounts:read|write`、`deploy:read|write`、
    `notify:read|write`、`admin:read|write`
- 有效期可设（天数）；吊销立即生效
- 仅本人可吊销（admin 可吊销任意）

## 用户与角色

| 角色 | 能力 |
|---|---|
| admin | 全部（含用户管理、审计、API Token） |
| operator | 证书签发/续期/吊销、部署、目标与规则管理 |
| readonly | 仅查询（不含私钥下载） |

## 审计日志

- 记录：登录成功/失败、改密、Token 创建/吊销、证书签发/续期/吊销/导入/删除、
  私钥下载、账户与目标变更、通知发送/失败
- 字段：操作者、动作、资源、IP、User-Agent、时间（只追加）

## 任务日志

- 调度器运行记录（扫描、自动续期、部署重试），5 秒实时刷新
- 支持手动触发扫描：`POST /api/v1/task-logs/scan`（operator+，经任务日志记录）

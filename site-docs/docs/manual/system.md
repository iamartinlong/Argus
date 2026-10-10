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

## 密码管理

### 修改自己的密码

- **强制场景**：使用初始密码 / 随机密码首次登录后，系统自动跳转「修改密码」页，修改前无法访问其他功能
- **主动修改**：登录状态下浏览器直接访问 `/change-password`，需验证当前密码
- **策略**：长度 ≥ `auth.min_password_len`（默认 12），且必须包含大写字母、小写字母和数字
- 修改成功后该用户全部登录会话被吊销，需重新登录

### 忘记密码（管理员重置）

Web 无自助找回流程（未接入邮件），由管理员在**服务器本机**执行 CLI 重置——
直连数据库，**无需 JWT 密钥 / 主密钥**，也不依赖任何已加密数据：

```bash
# 显式指定新密码（≥12 位，含大小写字母与数字）
argus reset-password --config config.yaml --username admin --password "NewPass1234"

# 不传 --password：自动生成随机密码（首次登录强制修改）
argus reset-password --config config.yaml --username admin
```

> **Windows PowerShell 注意**：PowerShell 默认不在当前目录查找可执行文件，直接输入 `argus` 会提示找不到命令。
> 需使用 `.\argus.exe`（先 `cd` 到所在目录）或完整路径，例如：
> `& "D:\argus\argus.exe" --config "D:\argus\config.yaml" reset-password --username admin --password "NewPass1234"`。
> Linux 下二进制位于 `/usr/local/bin/argus`（已在 PATH），可直接使用 `argus`。

要点：

- **前提**：能访问 `config.yaml` 与数据库文件（即本机管理员权限；这也意味着无远程攻击面）
- 服务运行中亦可执行（SQLite WAL + busy_timeout 5s）；若遇 `database is locked`，先 `Stop-Service` 再执行
- 免验旧密码；重置后该用户**全部会话立即吊销**，已签发的访问令牌最长 `auth.access_token_ttl`（默认 15 分钟）内自然过期
- 随机生成的密码会置「首次登录需改密」标志（与 `init-admin` 行为一致）
- 用户不存在时报错并提示：若系统尚未初始化，请使用安装向导（`/setup`）或 `argus init-admin`
- 操作记入审计日志（动作「重置密码」），含操作者与目标用户

Windows 服务场景完整步骤见[故障排查 — 忘记管理员密码](/ops/troubleshooting)。

## 审计日志

- 记录：登录成功/失败、改密、**密码重置**、Token 创建/吊销、证书签发/续期/吊销/导入/删除、
  私钥下载、账户与目标变更、通知发送/失败
- 字段：操作者、动作、资源、IP、User-Agent、时间（只追加）

## 任务日志

- 调度器运行记录（扫描、自动续期、部署重试），5 秒实时刷新
- 支持手动触发扫描：`POST /api/v1/task-logs/scan`（operator+，经任务日志记录）

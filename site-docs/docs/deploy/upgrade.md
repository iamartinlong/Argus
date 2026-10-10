# 升级指南

Argus 采用**单二进制 + 自动数据库迁移**，升级 = 替换二进制 + 重启服务，配置与数据全部保留。

## 通用原则

1. **升级前备份**（必须）：数据库文件 + 主密钥，两者同等重要
   - 数据库：SQLite 为 `storage.dsn` 指定路径（如 `/opt/argus/data/argus.db`、`D:\argus\data\argus.db`）；PostgreSQL 用 `pg_dump`
   - 主密钥：环境变量 `ARGUS_MASTER_KEY`（Machine 级）或 `config.yaml` 的 `security.master_key`——**丢失则已加密的渠道凭据/私钥无法解密**
2. **数据库迁移自动执行**：启动时按 `schema_migrations` 表幂等升级，无需手动操作，也无需先升中间版本
3. **停机窗口**：替换二进制 + 重启约 1 分钟内；建议避开自动续期高峰期（调度器重启后会按原周期继续）
4. **回滚** = 旧版本二进制 + 升级前的数据库备份（见下文）

## 从 1.0.x 升级到 1.1：变化清单

| 变化 | 对既有部署的影响 | 需要做什么 |
|---|---|---|
| 可视化安装向导 `/setup` | **仅全新安装（无用户）时出现**；已有用户的部署打开仍是登录页 | 无需操作 |
| `security.master_key` 可写入 `config.yaml` | 环境变量仍然优先，既有 ENV 部署行为不变 | 无需操作 |
| 新增 `argus reset-password` CLI | 纯增量功能 | 忘记密码时使用（见[用户手册](/manual/system)） |
| `argus gen-keys` 同时输出 JWT 与主密钥 | CLI 输出变化 | 无 |
| Windows 安装脚本就绪后自动打开浏览器 | 新增 `-NoBrowser` / `-Url` 参数 | 无头/远程场景加 `-NoBrowser` |
| `config.yaml.example` 增加可选注释项 | 纯文档 | 无需修改现有配置 |

> 升级后首次打开浏览器：**已有用户 → 登录页**（不会进入向导）；如需体验向导必须在空库上全新安装。

## Windows 服务升级

```powershell
# 1. 备份（升级前保险）
Copy-Item D:\argus\data\argus.db "D:\argus\data\argus.db.bak-$(Get-Date -f yyyyMMdd)"

# 2. 停服务并替换二进制（从 Release 下载的 zip 中取 argus.exe）
Stop-Service Argus
Copy-Item .\argus.exe D:\argus\argus.exe -Force

# 3. 启动并验证
Start-Service Argus
& D:\argus\argus.exe version          # 应输出版本号 1.1.0
Get-Service Argus                      # 应为 Running
```

- `config.yaml`、`data\`、Machine 级 `ARGUS_*` 环境变量**都不要动**
- 也可重新运行 `install-service.ps1` 完成升级（脚本会更新服务注册并启动；服务运行中需先 `Stop-Service`）
- 打开 `http://localhost:8443`：已有用户直接登录；**忘记管理员密码**用 `argus.exe --config D:\argus\config.yaml reset-password --username admin --password "新口令"`

## Linux（systemd）升级

安装脚本是幂等的（保留既有配置、密钥与数据），用新二进制重跑即可：

```bash
# 1. 备份
sudo cp /opt/argus/data/argus.db /opt/argus/data/argus.db.bak-$(date +%Y%m%d)

# 2. 用新二进制重跑安装脚本（自动替换 /usr/local/bin/argus 并重启服务）
sudo ./deploy/systemd/install.sh /path/to/argus

# 或手动：install -m 0755 argus /usr/local/bin/argus && sudo systemctl restart argus

# 3. 验证
argus version
systemctl status argus
```

- `/etc/argus/config.yaml`、`/etc/argus.env`（密钥）、`/opt/argus/data` 均保留
- 若曾手动修改过 `argus.service` 单元，注意脚本会用仓库版本覆盖 `/etc/systemd/system/argus.service`（重跑前备份）

## Docker 升级

Docker 镜像从源码构建，数据在命名卷中持久化：

```bash
# 1. 备份数据卷
docker run --rm -v argus_data:/data -v "$PWD:/backup" alpine tar czf /backup/argus-data.tgz -C /data .

# 2. 拉取新代码并重建
git pull
cd deploy/docker && ./up.sh        # docker compose up -d --build

# 3. 验证
docker compose logs --tail=50
```

- `.env`（密钥）与数据卷不会被重建覆盖
- 升级即重建镜像，首次构建较慢

## Agent 升级（业务机侧）

Agent 与服务端版本相互独立，可单独升级。两种方式：

**方式一：Web 版本管理下发（推荐）**

1. 「Agent 管理」→ 版本管理 → 上传新版本 `argus-agent` 二进制（SHA256 自动计算落盘）
2. 单机「升级」或勾选多台「批量升级」→ 选择目标版本下发
3. Agent 下次轮询自动下载 → **SHA256 严格校验** → 原子替换 → 重启；新版本上报后指令自动清除

**方式二：手动替换**

```powershell
# Windows（服务化运行的 Agent）
Stop-Service ArgusAgent
Copy-Item .\argus-agent.exe D:\argus-agent\argus-agent.exe -Force
Start-Service ArgusAgent
```

```bash
# Linux
sudo systemctl stop argus-agent
sudo install -m 0755 argus-agent /usr/local/bin/argus-agent
sudo systemctl start argus-agent
```

Agent 的 `agent.json` 配置（server_url、token、poll_interval）升级时保持不变。

## 回滚

```powershell
# Windows
Stop-Service Argus
Copy-Item D:\argus\argus.exe.bak-<旧版本> D:\argus\argus.exe -Force   # 事前留好的旧二进制
Copy-Item D:\argus\data\argus.db.bak-<日期> D:\argus\data\argus.db -Force  # 升级前备份
Start-Service Argus
```

```bash
# Linux
sudo systemctl stop argus
sudo install -m 0755 /path/to/argus-old /usr/local/bin/argus
sudo cp /opt/argus/data/argus.db.bak-<日期> /opt/argus/data/argus.db
sudo systemctl start argus
```

说明：

- 数据库迁移是**追加式**的（新增列/表均带默认值），旧版二进制通常也能读新库；但回滚时**连数据库一起回滚**最稳妥，避免新旧 schema 混用
- 主密钥若在升级时更换过，回滚数据库时必须匹配**备份时点的密钥**，否则已加密数据无法解密

## 升级后验证清单

- [ ] `argus version` 输出版本号正确，服务/容器状态正常
- [ ] 能正常登录；仪表盘统计有数据
- [ ] 证书列表/详情正常，到期状态刷新
- [ ] 手动触发一次「扫描」（任务日志出现成功记录）
- [ ] 对一台测试目标执行一次部署，记录为 success
- [ ] 通知渠道「测试」按钮通过
- [ ] 日志无异常报错（`journalctl -u argus` / `docker compose logs` / Windows 事件查看器）

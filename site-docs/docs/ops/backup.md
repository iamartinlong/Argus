# 数据备份与恢复

## 备份内容

| 数据 | 位置 | 说明 |
|---|---|---|
| SQLite 数据库 | `storage.dsn`（默认 ./data/argus.db） | 证书/订单/目标/用户等全部数据 |
| 主密钥 | 环境变量/文件 | 丢失则无法解密私钥与凭据 |
| JWT Secret | 环境变量/文件 | 丢失则所有会话失效 |

## 备份方法

推荐用内置命令（在线安全快照，服务运行中可执行）：

```bash
# 备份到 zip（含 manifest.json + 数据库快照）
argus backup --output /backup/argus-20261103.zip
```

```powershell
# Windows / PowerShell 同理
.\argus.exe backup --config D:\argus\config.yaml --output D:\backup\argus-20261103.zip
```

原理：`VACUUM INTO` 生成一致性快照后打包，不锁表、不影响服务。

可选 `--with-key` 把 `ARGUS_MASTER_KEY` 一并写入备份包——
**备份包含主密钥即可解密全部私钥与凭据，务必按敏感材料保管，勿随包分发**。
不加 `--with-key` 时，恢复需另行提供原主密钥。

> 手动备份（无 argus 二进制的场景）也可用数据库原生方式：
> `sqlite3 argus.db ".backup '/backup/argus.db'"` 或停机后直接复制文件。
> Agent 二进制目录（`data/agent-binaries/`）建议一并备份（含版本与哈希文件），
> 避免升级指令下发后版本缺失。

主密钥必须单独安全备份（如密码管理器）——**数据库 + 主密钥缺一不可**。

## 恢复

推荐用内置命令（自动校验 + 保留现库）：

```bash
argus restore --input /backup/argus-20261103.zip
```

1. **先停止服务**（恢复会替换数据库文件）
2. 执行 `argus restore --input <备份 zip>`（加 `--force` 可跳过交互确认）
3. 恢复前自动做两件事：校验备份包内 SQLite 完整性（失败即中止）、
   把当前数据库另存为 `<db>.bak-<时间戳>`
4. 用**原** `ARGUS_MASTER_KEY` 与 `ARGUS_JWT_SECRET` 启动服务

手动恢复（等价步骤）：停服务 → 用备份文件替换 `data/argus.db` →
删除残留的 `argus.db-wal` / `argus.db-shm` → 用原密钥启动。

> 主密钥不可轮换（当前版本）：轮换需重新加密全部私钥/凭据（规划中）。

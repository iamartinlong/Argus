# CLI 速查

Argus 除 Web 界面外还提供命令行（`argus`），适合脚本、CI 与无界面服务器。
所有命令都支持 `--config <路径>` 指定配置文件（默认 `./config.yaml`）。

> Windows 下在文件所在目录执行请加 `.\` 前缀，如 `.\argus.exe cert list`。

## 命令总览

| 命令 | 用途 |
|---|---|
| `argus serve` | 启动 Web 服务与调度器（缺配置文件时进入可视化安装模式） |
| `argus init-admin` | 创建初始管理员（系统中无用户时） |
| `argus reset-password` | 重置用户密码（忘记密码的本地补救通道） |
| `argus cert` | 证书管理子命令组（list / issue / renew / revoke / deploy） |
| `argus account` | 渠道账户管理子命令组（list / create） |
| `argus backup` | 在线备份数据库到 zip |
| `argus restore` | 从备份 zip 恢复数据库 |
| `argus gen-keys` | 生成随机密钥（JWT Secret / 主密钥） |
| `argus mcp` | 启动 MCP Server（stdio，供 AI Agent 连接） |
| `argus version` | 显示版本号 |

## 本地模式与远程模式

- **本地模式**（默认）：直接读配置文件与数据库，无需网络与 token
- **远程模式**：加 `--server https://cert.example.com --token st_xxx`
  （token 也可用环境变量 `ARGUS_API_TOKEN`），命令转为调用服务端 REST API

```powershell
# 本地
.\argus.exe cert list --config D:\argus\config.yaml

# 远程（token 走环境变量，避免出现在命令历史）
$env:ARGUS_API_TOKEN = "st_xxxxxxxx"
.\argus.exe cert list --server https://cert.example.com
```

## 服务与初始化

```powershell
# 启动服务（配置文件不存在时自动进入可视化安装模式）
.\argus.exe serve --config D:\argus\config.yaml

# 创建初始管理员（不传 --password 则随机生成，首次登录强制修改）
.\argus.exe init-admin --config D:\argus\config.yaml --username admin --password "YourPass1234"

# 忘记密码：本机直连数据库重置（服务运行中亦可执行）
.\argus.exe reset-password --config D:\argus\config.yaml --username admin --password "NewPass1234"

# 生成随机密钥（写入环境变量或 config.yaml security 段）
.\argus.exe gen-keys
```

## 证书（`argus cert`）

| 子命令 | 必填参数 | 说明 |
|---|---|---|
| `cert list` | — | 列出证书（ID / 域名 / 状态 / 渠道 / 到期日） |
| `cert issue` | `--domain` | 签发证书 |
| `cert renew` | `--id` | 续期证书 |
| `cert revoke` | `--id` | 吊销证书（不可逆） |
| `cert deploy` | `--id` | 部署到该证书绑定的全部目标 |

`cert issue` 可选参数：

| 参数 | 默认 | 说明 |
|---|---|---|
| `--account` | 第一个账户 | 渠道账户 ID（`argus account list` 查看） |
| `--verification` | `http01` | 验证方式：`http01` / `dns01` |
| `--targets` | 无 | 部署目标 ID 列表，逗号分隔（签发成功后自动部署） |

```powershell
.\argus.exe cert list
.\argus.exe cert issue --domain www.example.com --verification dns01 --targets t1,t2
.\argus.exe cert renew --id 3f2a...
.\argus.exe cert deploy --id 3f2a...
```

## 渠道账户（`argus account`）

| 子命令 | 必填参数 | 说明 |
|---|---|---|
| `account list` | — | 列出渠道账户（ID / 名称 / 渠道 / 状态） |
| `account create` | `--provider` | 创建渠道账户 |

`account create` 参数：

| 参数 | 说明 |
|---|---|
| `--provider` | `acme` / `aliyun` / `tencent` |
| `--name` | 账户名称 |
| `--credentials` | 凭据，`key=value,key2=value2` 形式 |

```powershell
.\argus.exe account list
.\argus.exe account create --provider acme --name letsencrypt --credentials "email=admin@example.com"
```

## 备份与恢复

```powershell
# 在线备份（VACUUM INTO 一致性快照，服务运行中可执行）
.\argus.exe backup --config D:\argus\config.yaml --output D:\backup\argus-20261103.zip

# 备份包含主密钥（敏感：没有它无法解密私钥与凭据，谨慎使用）
.\argus.exe backup --with-key

# 恢复（请先停止服务；当前库会先另存为 .bak-<时间戳>）
.\argus.exe restore --config D:\argus\config.yaml --input D:\backup\argus-20261103.zip
```

恢复前会自动校验备份包内数据库完整性，校验失败会中止；详见[数据备份与恢复](/ops/backup)。

## MCP（AI Agent）

```powershell
# 本地模式：直连数据库
.\argus.exe mcp --config D:\argus\config.yaml

# 收敛工具集（只保留证书只读工具）
.\argus.exe mcp --config D:\argus\config.yaml --scopes certificates:read
```

完整说明见 [MCP 连接指南](/guide/mcp)。

## 相关文档

- [快速开始](/guide/quickstart)
- [部署指南](/deploy/)
- [数据备份与恢复](/ops/backup)
- [MCP 连接指南](/guide/mcp)

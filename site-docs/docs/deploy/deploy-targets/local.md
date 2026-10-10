# 本机部署

将证书写入**服务端本机路径**，并执行自定义命令（如 reload）。

## 适用场景

- 开发测试环境
- 单机部署（服务端与证书目标为同一台机器）

## 应用场景

本机通道支持全部场景（通用/Nginx/Apache/Tomcat/Windows IIS）：
- **通用 / Nginx / Apache**：写 PEM + Key + 命令模板
- **Tomcat**：本机合成 PFX 写入 + 可选命令
- **Windows IIS**：本机合成 PFX + PowerShell 导入/绑定（需服务端为 Windows）

> **本机 IIS 场景权限要求**：证书导入 `LocalMachine\My` + netsh 绑定需管理员权限。
> **argus 服务必须以管理员运行**，否则部署失败 `0x80070005 (E_ACCESSDENIED)`。
> 以管理员 PowerShell 启动：`Start-Process build\argus.exe serve`（首次弹 UAC 确认）。

## 配置字段

| 字段 | 必填 | 说明 |
|---|---|---|
| 证书路径（通用/Nginx/Apache） | 是 | 证书写入路径（如 `/etc/ssl/certs/x.pem`） |
| 私钥路径（通用/Nginx/Apache） | 否 | 私钥写入路径（如 `/etc/ssl/private/x.key`） |
| PFX 路径（Tomcat/IIS） | 是 | PFX 落盘路径 |
| HTTPS 端口（IIS） | 是 | 逗号分隔的绑定端口 |
| 部署后命令 | 否 | 执行的 shell/PowerShell 命令 |

## 模板变量

部署后命令支持以下变量（由中心端渲染后下发）：

| 变量 | 说明 |
|---|---|
| `{cert_path}` | 实际写入的证书路径 |
| `{key_path}` | 实际写入的私钥路径 |
| `{pfx_path}` / `{pfx_password}` | Tomcat/IIS 场景的 PFX 路径与动态密码 |
| `{domain}` | 证书主域名 |

## 使用流程

1. Web 后台「部署目标」→ 新建 → 类型「本机」
2. 选择应用场景，填写对应路径 + 部署后命令
3. 签发/续期成功后自动写入并执行命令

## 安全注意

- 部署后命令以服务端进程用户权限执行
- 建议使用最小权限账户运行 Argus 服务
- 私钥写入后文件权限由命令控制（如需收紧需在 hook 中设置）

# SSH 服务器部署

通过 **Agentless SSH** 将证书推送至远程服务器，并执行 reload 命令。

## 适用场景

- Linux/Nginx/Apache 服务器（主流生产环境）
- 可直连的服务器（无需穿透 NAT）
- 希望零侵入、不安装客户端

## 应用场景

SSH 通道支持全部场景：

| 场景 | 部署产物 | 说明 |
|---|---|---|
| 通用 | PEM + Key | 写文件 + 自定义命令 |
| Nginx | PEM + Key | 预填 `/etc/nginx/certs/` + `nginx -s reload` |
| Apache | PEM + Key | 预填 `/etc/apache2/ssl/` + `systemctl reload apache2` |
| Tomcat | PFX | 服务端合成 PFX 推送 + 可选命令（替换 keystore 等） |
| Windows IIS | PFX | 服务端合成 PFX + PowerShell 导入 + netsh/appcmd 绑定（目标需 Windows） |

## 前置条件

- 目标服务器已安装并启用 OpenSSH Server
- 部署账号具备读取证书/执行 reload 的权限
- 已知服务器 SSH 主机密钥指纹（可选，建议配置防中间人攻击）
- IIS 场景目标需 Windows Server

## 配置字段

| 字段 | 必填 | 说明 |
|---|---|---|
| 主机 | 是 | SSH 主机 IP 或域名 |
| 端口 | 否 | SSH 端口，默认 22 |
| 用户 | 是 | SSH 用户名 |
| 私钥（PEM） | 否* | SSH 私钥内容（ed25519/RSA/DSA），AES-GCM 加密存储 |
| 密码 | 否* | SSH 密码（*私钥与密码至少配置一种） |
| 证书路径（通用/Nginx/Apache） | 是 | 远程证书写入路径 |
| 私钥路径（通用/Nginx/Apache） | 否 | 远程私钥写入路径 |
| PFX 路径（Tomcat/IIS） | 是 | 远程 PFX 写入路径 |
| HTTPS 端口（IIS） | 是 | 逗号分隔的绑定端口 |
| 部署后命令 | 否 | 远程执行的命令 |
| known_hosts | 否 | 主机指纹，配置后严格校验（防中间人攻击） |

## 模板变量

部署后命令支持以下变量：

| 变量 | 说明 |
|---|---|
| `{cert_path}` | 远程证书路径 |
| `{key_path}` | 远程私钥路径 |
| `{pfx_path}` / `{pfx_password}` | Tomcat/IIS 场景的 PFX 路径与动态密码 |
| `{domain}` | 证书主域名 |

## 使用流程

1. Web 后台「部署目标」→ 新建 → 类型「SSH」
2. 选择应用场景，填写主机、用户、认证凭据 + 对应路径 + 部署后命令
3. 点击「测试」验证 SSH 连接
4. 签发/续期成功后自动推送证书并执行命令

## 安全设计

- **传输加密**：SSH/SCP 协议全程加密，证书私钥不明文传输
- **凭据加密**：私钥/密码经 AES-GCM 加密存储于服务端数据库
- **指纹校验**：配置 `known_hosts` 后严格校验主机密钥，防止中间人攻击
- **审计日志**：每次部署操作记录操作人、时间、结果
- **权限控制**：仅 admin/operator 角色可配置和触发部署

## Linux vs Windows IIS 权限差异

Linux 部署（nginx/apache/tomcat）与 Windows IIS 的权限要求有本质区别：

| 层面 | Windows IIS 场景 | Linux（nginx/apache/tomcat） |
|---|---|---|
| 证书存储 | 写入**系统证书库** `Cert:\LocalMachine\My`（受保护系统资源）→ **必须管理员** | 证书是**普通文件**，写入配置路径（如 `/etc/nginx/certs/`）→ **仅需目标目录可写** |
| 绑定操作 | `netsh http add sslcert` 修改 HTTP.sys（系统级）→ 必须管理员 | 无系统级绑定，服务配置 `ssl_certificate` 引用文件路径即可 |
| 服务 reload | appcmd/netsh（管理员） | `nginx -s reload` / `apachectl graceful` 需**信号权限**（root/属主或 sudo） |

**核心结论**：
- Linux 证书部署**无需 root**——SSH 部署用户对目标目录（如 `/etc/nginx/certs/`）有写权限即可完成证书写入
- 唯一可能涉及权限的是 **hook（reload 命令）**：`nginx -s reload` 的 master 进程以 root 运行，普通用户发信号可能被拒。这是**服务操作权限**，与证书写入是两回事，可通过以下方式解决：
  - 给部署用户配置 **sudo 白名单**（仅允许 reload 命令）
  - 用 ACL 让部署用户对 reload 命令有权限
  - 部署账号本身即服务属主

> 测试环境说明：Docker 容器默认以 root 运行，因此写文件 + reload 均无权限问题，可顺畅验证部署链路。

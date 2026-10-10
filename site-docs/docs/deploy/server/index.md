# 服务端部署

服务端即**管理后台**：Web UI + 证书签发/续期调度器 + REST API，运行于中心服务器。

## 部署方式对比

| 方式 | 一键脚本 | 适用场景 |
|---|---|---|
| [Linux / systemd](/deploy/server/linux) | `deploy/systemd/install.sh` | 生产推荐：开机自启、崩溃重启、安全加固 |
| [Windows 服务](/deploy/server/windows) | `deploy/windows/install-service.ps1` | Windows Server 生产部署 |
| [Docker](/deploy/server/docker) | `deploy/docker/up.sh` | 快速试用、容器化编排 |

## 如何选择

- **生产 Linux**：优先 systemd（独立用户、`NoNewPrivileges` 加固、`journalctl` 日志）
- **Windows 服务器**：Windows 服务（LocalSystem 运行，开机自启 + 失败重启）
- **快速试用 / 测试**：Docker 一条命令，数据卷持久化
- **K8s / 编排环境**：Docker 镜像可入容器平台

## 部署后

- 访问 `http://<服务器>:8443`
- 初始化管理员：`argus init-admin --username admin --password <强密码>`
- 配置签发渠道账户（ACME / 阿里云 / 腾讯云）后即可签发

> 生产环境安全建议见 [运维指南 · 安全](/ops/security)。

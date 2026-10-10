# 部署指南

Argus 分**服务端**与**部署目标**两部分部署：

## 服务端部署（管理后台）

运行集中式证书管理后台（Web UI + 调度器 + API），支持三种方式：

| 方式 | 适用场景 | 快速开始 |
|---|---|---|
| [Linux / systemd](/deploy/server/linux) | 生产推荐，开机自启、崩溃重启 | 一键脚本 `install.sh` |
| [Windows 服务](/deploy/server/windows) | Windows 服务器生产部署 | 一键脚本 `install-service.ps1` |
| [Docker](/deploy/server/docker) | 快速试用 / 容器化环境 | 一键脚本 `up.sh` |

> 已在旧版本上运行？升级见[升级指南](/deploy/upgrade)（替换二进制 + 重启，数据自动迁移）。

## 部署目标（证书推送通道）

证书签发后自动推送到目标服务器。采用 **连接通道 × 应用场景** 正交模型
（通道：本机 / SSH / Agent；场景：通用 / Nginx / Apache / Tomcat / Windows IIS），
另有 K8s、Webhook 独立通道：

| 通道 | 适用场景 | 业务机安装 |
|---|---|---|
| [SSH](/deploy/deploy-targets/ssh) | Linux/Windows 可直连（主流生产） | 否 |
| [Agent](/deploy/deploy-targets/agent) | NAT / Windows | 是 |
| [本机](/deploy/deploy-targets/local) | 开发测试 / 单机 | 否 |
| [Kubernetes](/deploy/deploy-targets/k8s) | K8s 集群 TLS Secret | 否 |
| [Webhook](/deploy/deploy-targets/webhook) | 配置中心 / 自定义 | 否 |

各通道均支持全部应用场景（通用/Nginx/Apache/Tomcat/Windows IIS），详见
[部署目标总览](/deploy/deploy-targets/)。

## 安装脚本

一键部署脚本位于仓库 `deploy/` 目录：

- 服务端：`deploy/systemd/install.sh`、`deploy/windows/install-service.ps1`、`deploy/docker/up.sh`
- Agent 客户端：`deploy/agent/install-systemd.sh`、`deploy/agent/install-windows-service.ps1`

## 下一步

- [升级指南](/deploy/upgrade)
- [服务端部署总览](/deploy/server/)
- [部署目标总览](/deploy/deploy-targets/)

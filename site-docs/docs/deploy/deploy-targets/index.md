# 部署目标

证书签发/续期成功后，自动推送到目标服务器。Argus 采用 **连接通道 × 应用场景** 正交模型：
- **连接通道**（怎么连）：本机 / SSH / Agent
- **应用场景**（部署到什么）：通用 / Nginx / Apache / Tomcat / Windows IIS

## 通道总览

| 通道 | 适用场景 | 业务机需安装 | 说明 |
|---|---|---|---|
| [SSH](/deploy/deploy-targets/ssh) | Linux/Windows 可直连（主流生产） | 否 | Agentless 直连推送，支持 ed25519/RSA |
| [Agent](/deploy/deploy-targets/agent) | NAT / 无入站端口 / Windows | **是** | 出站轮询，穿透 NAT |
| [本机](/deploy/deploy-targets/local) | 开发测试 / 单机 | 否 | 写入服务端本地路径 + hook |
| [Kubernetes](/deploy/deploy-targets/k8s) | K8s 集群 TLS Secret | 否 | kubectl apply + 可选 rollout restart |
| [Webhook](/deploy/deploy-targets/webhook) | 配置中心 / 反向代理 / 自定义 | 否 | POST 证书 JSON + HMAC 签名 |

## 场景总览

场景决定部署产物格式与动作模板，**三种连接通道（本机/SSH/Agent）均支持全部场景**（正交）：

| 场景 | 部署产物 | 动作 |
|---|---|---|
| 通用 | PEM + Key | 写文件 + 自定义命令 |
| Nginx | PEM + Key | 写文件 + `nginx -s reload` 模板 |
| Apache | PEM + Key | 写文件 + `apachectl graceful` 模板 |
| Tomcat | PFX（服务端合成） | 推送 PFX + 可选命令（替换 keystore 等） |
| Windows IIS | PFX（服务端合成） | PowerShell 导入 + netsh/appcmd 绑定 |

> **场景属性 vs 通道属性**：IIS 场景需目标为 Windows 是「场景」的平台属性，不是「通道」的限制——无论走哪个通道，IIS 目标机都必须是 Windows。

## 如何选择

```
需要穿透 NAT？
├─ 是 → Agent
└─ 否
   ├─ 目标是 K8s 集群？ → Kubernetes Secret
   ├─ 仅需通知外部系统？ → Webhook
   ├─ 本地开发/单机？ → 本机
   ├─ 目标是 IIS？ → SSH/Agent + Windows IIS 场景
   └─ 可直连服务器？ → SSH + 通用/Nginx/Apache/Tomcat 场景
```

## 部署记录

每次部署自动生成记录：目标、状态（进行中/成功/失败）、SHA256 校验和、消息、重试次数。
Agent 异步场景：部署排队后由 Agent 回执联动更新最终结果。

## 失败重试

部署失败记录由调度器自动重试（≤3 次）；连续失败建议检查目标连通性与凭据。

## 部署目标列表

创建/管理部署目标：Web 后台「部署目标」页面。

> 部署目标配置详情见各类型文档，也可参考 [用户手册 · 部署目标](/manual/deploy-targets)。

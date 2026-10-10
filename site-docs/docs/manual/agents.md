# Agent 管理

Agent 用于无法 SSH 直连的业务机（NAT、无入站端口、Windows 服务器）。

## 添加 Agent

1. 「Agent 管理」→ 添加 Agent → 复制 Token（仅显示一次）
2. 业务机配置 `agent.json`：

```json
{
  "server_url": "https://cert.example.com",
  "token": "ag_xxxxxxxxxxxxxxxxxxxxxxxx",
  "poll_interval": 10
}
```

3. 启动：`argus-agent --config agent.json`（Linux 建议 systemd，见部署指南）

## 在线状态

- 每次轮询更新心跳；页面 5 秒实时刷新
- 离线检测：心跳超过 **90 秒**未更新即自动置离线（Agent 停止/网络断开后约 90s 内显示离线）

## 启用 / 禁用（软白名单）

- 禁用后 Agent **Token 立即失效**（注册/拉任务/回执全部 401）、状态置离线
- 重新启用即恢复（Token 无需更换）
- 与删除的区别：保留登记记录与历史任务，适合临时下线/维护场景
- 仅 admin 可操作

## 部署流程

证书签发/续期 → 部署目标（Agent）排队任务 → Agent 轮询领取
→ 写证书/私钥/PFX 文件 → 执行 hook → 回传校验和与结果 → 部署记录联动更新。

### IIS 自动绑定

部署目标类型「Agent」配置 **PFX 路径 + HTTPS 端口 + IIS 站点名** 后，
中心端自动生成完整部署脚本（证书导入 + netsh 端口绑定 + appcmd 站点绑定），
Agent 以管理员权限运行即可完成 IIS https 自动部署，无需手写 hook。
详见 [部署目标](/deploy/deploy-targets/agent)。

## 安全

- `agent.json` 权限 600（Token 明文）
- 生产环境 server_url 必须 HTTPS（mTLS 可经反向代理提供）
- Token 泄漏：删除 Agent 重新创建，旧 Token 立即失效
- 任务超时未回执（10 分钟）自动标记失败

## 任务状态

| 状态 | 含义 |
|---|---|
| pending | 已排队，等待 Agent 领取 |
| running | 已领取，执行中 |
| success / failed | 回执结果；超时未回执自动 failed |

## 版本管理

「Agent 管理」右上角「版本管理」为聚合窗口，含两 Tab：

- **上传二进制**：填版本号 + 选择 `argus-agent` 可执行文件（含目标平台），上传后 SHA256 自动落盘（`version.sha256`）
- **版本历史**：表格展示已上传版本、大小、SHA256 前 16 位、上传时间（`GET /agents/binaries`，过滤 `.sha256` 伴生文件）

## 自升级

管理员（admin）可对 Agent 下发升级：

1. 「Agent 管理」→ 版本管理 → 上传二进制（版本号 + argus-agent 可执行文件，SHA256 自动计算）
2. 单机「升级」或勾选多台「批量升级」→ 选择目标版本下发（同版本/已待升级版本自动禁用，幂等）
3. Agent 下次轮询（poll_interval）检测到指令 → 下载 → **SHA256 严格校验**（防篡改）→ 原子替换 → 重启
4. 新版本上报后升级指令自动清除（`GET /agents/{id}` 的 `upgrade_version` 回空），明细页“待升级版本”消失

- 下载端点仅允许 Agent 拉取**下发给自己的版本**（越权 403）
- 二进制存储于中心端数据目录 `agent-binaries/`（版本号白名单校验，防路径遍历）
- SHA256 在**上传时固定并落盘**：Agent 下载校验与上传时一致，上传后文件被篡改即校验失败
- Linux：原地替换后 `exec` 重启；Windows：新副本替换主文件后重启
- 校验失败拒绝替换并记录日志（不中断运行）；升级指令保留，Agent 将持续重试
  直到成功或管理员重新上传一致版本

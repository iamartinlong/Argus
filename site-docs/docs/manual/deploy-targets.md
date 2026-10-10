# 部署目标

证书签发/续期成功后自动推送的目标通道。采用 **连接通道 × 应用场景** 正交模型：

## 部署触发时机

证书**签发成功**或**续期成功**后，系统自动对绑定到该证书的部署目标执行部署：
- 签发成功 → 立即自动部署（无需手动操作）
- 续期成功 → 立即自动部署（复用已存私钥）
- 签发失败 / 吊销 → 不触发自动部署

部署在**签发接口返回后异步执行**，不阻塞签发流程；每个目标生成一条部署记录，
成功写「成功」、失败写「失败」（失败由调度器自动重试，见下方「失败重试」）。

> 自动部署只认「签发/续期成功」这一时机。也可以随时在证书详情手动「部署」或改绑目标。
- **连接通道**（怎么连）：本机 / SSH / Agent
- **应用场景**（部署到什么）：通用 / Nginx / Apache / Tomcat / Windows IIS

## 连接通道

### 本机
- 写入**服务端本机路径** + 部署后命令（Windows 自动用 PowerShell）
- 支持全部场景：通用/Nginx/Apache 写 PEM+Key；Tomcat/IIS 本机合成 PFX

### SSH 服务器
- 主机/端口/用户、私钥（推荐）或密码认证
- `known_hosts`：建议配置防中间人（格式同 ssh known_hosts）
- 支持全部场景：通用/Nginx/Apache 推 PEM+Key + 部署后命令；
  Tomcat 服务端合成 PFX 推送；IIS 走 `deployIISOverSSH`（PFX + PowerShell 导入 + 绑定）
- 模板变量：`{cert_path} {key_path} {pfx_path} {pfx_password} {domain}`

### Agent
- 业务机运行 `argus-agent` 出站连接中心端（穿透 NAT）
- 支持全部场景：通用/Nginx/Apache 写 PEM+Key + 命令；
  Tomcat 服务端合成 PFX 附带；IIS 自动绑定（见下）
- **IIS 自动绑定**：配置 PFX 路径 + HTTPS 端口（+可选 IIS 站点名）后，
  中心端自动生成完整部署脚本（证书导入 + netsh 端口绑定 + appcmd 站点绑定），无需手写 hook
- hook 支持模板变量：`{pfx_path} {pfx_password} {cert_path} {key_path} {domain}`

## 应用场景

| 场景 | 部署产物 | 动作 |
|---|---|---|
| 通用 | PEM + Key | 写文件 + 自定义命令 |
| Nginx | PEM + Key | 写文件 + `nginx -s reload` 模板 |
| Apache | PEM + Key | 写文件 + `apachectl graceful` 模板 |
| Tomcat | PFX（服务端合成） | 推送 PFX + 可选命令（替换 keystore 等） |
| Windows IIS | PFX（服务端合成） | PowerShell 导入 + netsh/appcmd 绑定 |

> 场景属性 vs 通道属性：IIS 需目标为 Windows 是「场景」的平台属性，非「通道」限制。

## 独立通道

### Kubernetes Secret
- `kubectl apply` TLS Secret（kubeconfig 可指定），可选 rollout restart

### Webhook
- POST 证书+私钥 JSON（HMAC-SHA256 签名），注意传输需 HTTPS

## 部署记录

每次部署生成记录：目标、状态（进行中/成功/失败）、内容 SHA256 校验和、消息、重试次数。
Agent 异步场景：部署排队后由 agent 回执联动更新最终结果。

## 失败重试

部署失败记录由调度器自动重试（≤3 次）；连续失败建议检查目标连通性与凭据。

## 实测提示（Windows IIS）

- 本机通道 IIS 场景：argus 服务须以**管理员**运行，否则 `Import-PfxCertificate` 报 `0x80070005`
- Agent 通道 IIS：agent 进程以管理员运行即可
- 详见 [Windows IIS 场景](/deploy/deploy-targets/iis) 的「实测验证经验」

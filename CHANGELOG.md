# 变更记录

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

> 1.0.x 为公开仓库建立前发布的版本，此处保留变更摘要；提交历史未随公开仓库迁移。

## [未发布]

## [1.1.1] - 2026-10-13

### 新增

- **公开发布**：仓库迁移至 GitHub（`github.com/iamartinlong/Argus`），module 路径同步更名；内网 gitea 同步 `main` + tag 并切换默认分支
- **CI 自动发布**：推 `v*` tag 自动交叉编译 6 平台（windows/linux/darwin × amd64/arm64）→ 打包 zip + SHA256SUMS → 创建 GitHub Release；发布前强制校验 CHANGELOG 存在对应版本条目
- **文档站 Pages**：main 推送自动构建 VitePress 并部署 GitHub Pages（`iamartinlong.github.io/Argus`）
- README 增加下载入口（Releases 平台表 + SHA256 校验）与功能展示截图（7 张，本地演示环境实拍）
- 仓库 About：Description / Website（文档站）/ 14 个 topics

### 修复

- **导入 BER 编码的 PFX 失败**（阿里云免费证书 IIS 导出为此格式）：PKCS#12 规范允许 BER（ASN.1 不定长），而 Go 解析器只支持 DER。新增 BER→DER 规范化（`internal/infrastructure/asn1ber`）+ PFX 修复管线（`internal/infrastructure/pkcs12fix`，含 RFC 7292 MAC 重算），导入前自动修复；DER 文件零成本直通
- setup handler 在传入配置缺少密钥时回退读取 `config.yaml`
- CI 干净检出 `go:embed all:dist` 失败：先构建前端再 vet/test；`webassets/dist/.gitkeep` 占位使裸 `go build/vet/test` 开箱可跑
- `vite build` 的 `emptyOutDir` 会清空占位文件：构建后自动恢复

### 文档

- 新增 `CHANGELOG.md`（Keep a Changelog 规范，含 1.0.0–1.1.1 完整版本史）
- 新增升级指南（文档站）：通用原则（备份/自动迁移/回滚）、Windows 服务、Linux systemd、Docker、Agent 五种升级路径、1.0.x→1.1 变化清单、升级后验证清单
- 用户手册新增「密码管理」章节（自助改密 + `reset-password` 全流程）
- 文档示例修正：`reset-password` 示例密码统一为满足策略的 `NewPass1234`（原 `NewPass123` 仅 10 位，低于 12 位下限）；PowerShell 代码块统一使用 `.\argus.exe` 或完整路径（PowerShell 不在当前目录查找可执行文件），并在 Windows 部署文档补充该坑位说明
- 故障排查新增「导入证书失败」小节（BER PFX / 密码错误 / PEM 不匹配）
- 清理仓库内部信息：删除陈旧重复的 `docs/openapi.yaml`（唯一事实源 `webassets/openapi.yaml`）；里程碑文档的运维遗留改写为通用建议

## [1.1.0] - 2026-10-10

### 新增

- **可视化安装向导**：首次打开自动进入 `/setup`，三步完成配置——环境检查 → 安全密钥（一键生成并写回 `config.yaml`，自动备份 `.bak`）→ 创建初始管理员。无需手动编辑配置文件或执行 `init-admin`
- `security.master_key` 支持直接写入 `config.yaml`（环境变量仍优先覆盖）；缺少密钥时 `serve` 降级为安装模式（仅开放 setup 接口）而非报错退出
- **`argus reset-password` CLI**：忘记密码时在本机直连数据库重置——免旧密码、无需 JWT/主密钥，重置后吊销该用户全部会话；不传 `--password` 则生成随机密码（首次登录强制修改）
- Windows 安装脚本：服务就绪（`/healthz` 探测，最多 30s）后自动打开浏览器；支持 `-NoBrowser`、`-Url` 参数

### 变更

- Go module 路径更名为 `github.com/iamartinlong/Argus`
- 安装模式重启需携带 `/setup/init` 下发的 `restart_token`（收敛匿名重启 DoS 面）
- 环境变量接管的密钥不再经向导写回 `config.yaml`（避免 ENV 密钥明文落盘）
- 部署/Agent 子进程终止改为进程树 kill（Windows `taskkill /T`），避免孙进程残留

### 修复

- setup handler 在传入配置缺少密钥时回退读取 `config.yaml`（密钥基线）
- `webassets/dist` 增加占位文件，修复裸 `go build/vet/test` 报 `pattern all:dist: no matching files found`
- `vite build` 清空 dist 后自动恢复 `go:embed` 占位文件（构建脚本 + 恢复脚本）

### 文档

- 用户手册新增「密码管理」（自助改密 + CLI 重置全流程）
- 故障排查新增「忘记管理员密码」；Windows 部署文档补安装向导与重置说明
- 清理仓库内网域名与内部运维信息；删除陈旧重复的 `docs/openapi.yaml`（唯一事实源为 `webassets/openapi.yaml`）

## [1.0.5] - 2026-09-02

### 新增

- Nginx 同端口多域名 SNI：`server_names` + `{domain}` 路径模板 + `nginx -t` 预检（local/ssh/agent 三通道）
- Nginx SNI 单元测试（5 用例）

## [1.0.4] - 2026-09-01

### 新增

- 任务日志摘要（`summary`，schema 迁移 18）：扫描/续期结果聚合展示，前端支持筛选、分页与详情抽屉

### 优化

- 新建表单残留重置（`CREATE_DEFAULT` + `resetCreate` 双保险）
- 删除确认由 `window.confirm`/`AlertDialog` 统一为 `Dialog`（证书/目标/吊销/部署/续期）
- 部署/续期/吊销操作增加 Loading 状态

## [1.0.3] - 2026-09-01

### 修复

- 部署记录中文乱码：远程 PowerShell 执行统一 `chcp 65001` + `OutputEncoding UTF8` + GBK 解码兼容

## [1.0.2] - 2026-09-01

### 新增

- IIS 多站点同 443 端口 SNI 绑定：`netsh hostnameport` + `SslFlags1` + `HostHeader`，支持 hostname 三通道（local/ssh/agent，留空回退主域名）

## [1.0.1] - 2026-09-01

### 修复

- Windows 服务 `1053 服务没有及时响应启动或控制请求`：原生服务化（`svc.IsWindowsService` + `svc.Run`），无需 NSSM/WinSW

## [1.0.0] - 2026-08-20

首个正式版。核心平台能力：

- 多渠道签发：ACME（lego，HTTP-01/DNS-01/EAB/自定义 CA）、阿里云、腾讯云免费证书
- 全自动续期：调度扫描、失败重试、多级到期提醒（30/14/7/1 天）
- 多通道部署：SSH（Agentless）、Agent（出站穿透 NAT）、K8s Secret、Webhook、Windows IIS（PFX 导入 + 绑定）
- 通知中心：SMTP、Webhook、企业微信/钉钉/飞书，7 类事件（含签发/部署成败、证书级订阅）
- DNS 验证账户持久化（独立表 + 独立页面，签发/续期优先复用）
- Agent 版本管理聚合与自升级（SHA256 校验 + 原子替换）、心跳超时离线、启用/禁用软白名单
- 用户/RBAC/API Token（scope 矩阵）/审计日志/任务日志保留策略
- 单二进制交付（前端 go:embed 内嵌）；Docker/systemd/Windows 服务一键部署
- 项目更名 Argus（永不眨眼的 SSL 证书守护者）+ 文档站

[1.1.0]: https://github.com/iamartinlong/Argus/releases/tag/v1.1.0

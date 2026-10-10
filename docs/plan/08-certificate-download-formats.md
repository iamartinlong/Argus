# 08 证书多格式打包下载

## 8.1 需求

证书明细页的下载功能升级为"服务器格式打包下载"，参考阿里云证书下载的格式划分：

| 服务器类型 | 证书格式 | 包内文件 | 状态 |
|---|---|---|---|
| Nginx | pem/key | fullchain.pem + privkey.pem | 可下载 |
| Apache | crt/key | cert.crt + privkey.key | 可下载 |
| IIS | pfx | cert.pfx + password.txt（随机密码） | 可下载 |
| Tomcat | pfx | cert.pfx + password.txt（随机密码） | 可下载 |
| 其他 | pem/key | cert.pem + privkey.key | 可下载 |
| JKS | jks | — | 即将支持（前端禁用） |
| 根证书 | crt/cer | — | 即将支持（前端禁用） |

交互：详情页「下载」按钮 → Dialog 表格展示格式，每行尾部「下载」按钮 → 下载 zip 压缩包。

## 8.2 设计

### 8.2.1 后端

`GET /api/v1/certificates/{id}/download` 新增 `format` 查询参数，返回 `application/zip`：

| format | 说明 |
|---|---|
| `nginx` | fullchain.pem（=`CertificatePEM`，已含叶子+链）+ privkey.pem |
| `apache` | cert.crt（叶子证书）+ privkey.key |
| `iis` / `tomcat` | cert.pfx（`deploy.BuildPFX` 合成）+ password.txt |
| `other` | cert.pem + privkey.key |
| `jks` / `root` | 501 Not Implemented（前端不请求） |

- zip 用标准库 `archive/zip`，文件名 `域名-{format}.zip`
- IIS/Tomcat 密码：`crypto/rand` 生成 16 位（字母数字混合），仅写入包内 `password.txt`，不落日志
- 含私钥的 format（nginx/apache/iis/tomcat/other）沿用既有 sensitive 逻辑：readonly 角色 403 + 审计 `certificate.key.access`
- 兼容旧 `?kind=` 参数：未传 `format` 时回退现有单文件逻辑（crt/pem/key/fullchain），保证既有 API 客户端与测试不破坏

新增 deploy 包导出函数 `SplitChain(certPEM) (leaf, rest string, err error)`：解析多个 CERTIFICATE block，首张为叶子、其余为链。

### 8.2.2 前端（certificate-detail-page.tsx）

- 「证书」「私钥」两个按钮整合为单个「下载」按钮 → 打开 DownloadDialog
- Dialog 表格列：服务器类型 | 证书格式 | 说明 | 操作
- 行：Nginx/Apache/IIS/Tomcat/其他 可下载；JKS/根证书 禁用（「即将支持」）
- 下载：axios blob → 临时链接点击保存 zip
- readonly 对含私钥行点击后由后端 403 → toast 提示

### 8.2.3 测试

- 新 `certificate_download_test.go`：各 format zip 内容与正确性；iis/tomcat 的 pfx 可用 `pkcs12.Decode` 校验且密码匹配；jks/root 501
- `security_test.go` 扩展：readonly 下载 nginx/iis → 403；operator → 200
- 既有 `kind` 下载测试保持通过

## 8.3 实施步骤

1. deploy 包导出 `SplitChain` + 单测
2. 后端 download 端点 `format` 分支 + zip 组装 + 兼容 kind
3. 后端测试（download + security）
4. 前端 DownloadDialog + 整合按钮
5. `docs/openapi.yaml` 同步 `format` 参数 + webassets
6. `npm run build` + `go build` + 重启 + 端到端验证

## 8.4 验收

- 详情页「下载」弹出表格，5 行可下载、2 行禁用
- nginx zip 含 fullchain.pem + privkey.pem；apache 含叶子 cert.crt；iis/tomcat 含 pfx + password.txt
- readonly 无法下载含私钥包；审计记录格式
- 旧 kind 下载不受影响

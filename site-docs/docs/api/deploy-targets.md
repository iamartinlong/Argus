# 部署目标 API

## 创建

```
POST /api/v1/deploy-targets
```

```json
{ "name": "web-01", "type": "ssh",
  "config": { "host": "10.0.0.5", "port": 22, "user": "root",
              "private_key": "-----BEGIN ...-----", "cert_path": "/etc/nginx/certs/x.pem",
              "key_path": "/etc/nginx/certs/x.key", "hook": "nginx -s reload" } }
```

类型 `type`：`ssh` / `agent` / `iis` / `k8s` / `webhook` / `local`。

### iis 示例

```json
{ "name": "iis-01", "type": "iis",
  "config": { "host": "10.0.0.6", "user": "Administrator",
              "private_key": "...", "pfx_path": "C:\\certs\\x.pfx",
              "bindings": [443], "site_name": "Default Web Site" } }
```

### agent 示例（IIS 场景）

```json
{ "name": "agent-iis", "type": "agent",
  "config": { "agent_id": "...", "pfx_path": "C:\\certs\\x.pfx",
              "hook": "Import-PfxCertificate -FilePath '{pfx_path}' -Password (ConvertTo-SecureString '{pfx_password}' -AsPlainText -Force) -Exportable" } }
```

hook 模板变量：`{pfx_path} {pfx_password} {cert_path} {key_path} {domain}`。

## 测试 / 删除

```
POST   /api/v1/deploy-targets/{id}/test
DELETE /api/v1/deploy-targets/{id}
```

## 部署触发

- 自动：签发/续期成功后推送绑定目标
- 手动：`POST /api/v1/certificates/{id}/deploy`
- 失败重试：调度器自动（≤3 次）

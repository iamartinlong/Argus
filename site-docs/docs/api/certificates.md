# 证书 API

## 签发

```
POST /api/v1/certificates
```

```json
{
  "domains": ["example.com", "www.example.com"],
  "account_id": "渠道账户 ID",
  "verification": "http01",
  "dns_config": { "provider": "alidns", "ALICLOUD_ACCESS_KEY": "..." },
  "deploy_target_ids": ["目标 ID"]
}
```

- 云厂商渠道：`verification` 忽略，域名验证由平台完成
- 成功 201；失败 422（含 `certificate` 字段与错误）

### CSR 模式（仅 ACME 渠道）

`csr_pem` 非空时以 CSR 模式签发：私钥由客户端持有，服务端只提交 CSR 完成 ACME 签发，
不落盘私钥、签发返回的证书无私钥字段。非 ACME 渠道传入 `csr_pem` 返回 400。

```json
{
  "account_id": "ACME 渠道账户 ID",
  "verification": "http01",
  "csr_pem": "-----BEGIN CERTIFICATE REQUEST-----..."
}
```

- 域名从 CSR 的 CN / SAN 自动解析，`domains` 可省略
- CSR 证书无法自动续期（私钥不在服务端），到期需重新生成 CSR 手动签发

> 适用场景与使用步骤见 [证书管理 - CSR 模式签发](/manual/certificates)。

## 列表 / 详情

```
GET /api/v1/certificates
GET /api/v1/certificates/{id}
```

列表字段：id、domains、primary_domain、provider、status、not_before/not_after、verification、
deploy_target_ids、days_to_expire 等。

## 续期 / 吊销 / 部署

```
POST /api/v1/certificates/{id}/renew      # body 可选 {dns_config}
POST /api/v1/certificates/{id}/revoke
POST /api/v1/certificates/{id}/deploy     # body 可选 {target_ids}
```

## 更新部署目标绑定

签发后可随时增删改证书的部署目标绑定（整组替换），仅 admin/operator：

```
PUT /api/v1/certificates/{id}/targets
```

```json
{ "deploy_target_ids": ["目标 ID"] }
```

- 传空数组即清空绑定
- 更新后该证书后续自动部署（签发/续期）将使用最新绑定

## 导入

```
POST /api/v1/certificates/import
```

PEM 模式：

```json
{
  "domains": ["example.com"],
  "certificate": "-----BEGIN CERTIFICATE-----...",
  "private_key": "-----BEGIN ...-----...",
  "deploy_target_ids": []
}
```

PFX/P12 模式（多域名自动从证书 SAN 提取，省略 domains）：

```json
{
  "pfx": "<base64 编码的 PFX 文件>",
  "pfx_password": "PFX 密码",
  "deploy_target_ids": []
}
```

> 导入的证书（provider=import）无签发渠道，**不支持续期、吊销、自动续期**，只能删除或重新导入。

## 下载

```
GET /api/v1/certificates/{id}/download?kind=crt|pem|key|fullchain
```

私钥下载需 operator+ 角色，记录审计。

## 订单与部署记录

```
GET /api/v1/certificates/{id}/orders
GET /api/v1/certificates/{id}/deploy-records
```

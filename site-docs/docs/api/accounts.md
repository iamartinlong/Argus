# 渠道账户 API

## 创建

```
POST /api/v1/acme-accounts
```

```json
{ "provider": "acme", "name": "letsencrypt",
  "directory_url": "https://acme-v02.api.letsencrypt.org/directory",
  "contact": "admin@example.com", "eab_kid": "", "eab_hmac_key": "" }
```

云厂商渠道传入 `credentials`（加密存储）；申请人信息不再存放在账户凭据中，改由全局「申请人信息」（`/api/v1/settings/contacts`）提供，签发时注入：

```json
{ "provider": "aliyun", "name": "ali-prod",
  "credentials": { "access_key_id": "...", "access_key_secret": "...",
                   "region": "cn-hangzhou" } }

{ "provider": "tencent", "name": "tc-prod",
  "credentials": { "secret_id": "...", "secret_key": "...",
                   "dv_auth_method": "DNS_AUTO" } }
```

## 申请人信息（联系人）

全局共享，供云渠道签发时选择使用（ACME 渠道无需）：

```
GET    /api/v1/settings/contacts            # 列表
POST   /api/v1/settings/contacts            # 创建 {name,email,phone}
PUT    /api/v1/settings/contacts/{id}       # 更新
DELETE /api/v1/settings/contacts/{id}       # 删除
```

签发请求通过 `contact_id` 指定申请人；留空使用全局第一个。未配置申请人时云渠道签发返回 400。

## 连接测试

```
POST /api/v1/acme-accounts/{id}/test
```

ACME 渠道在测试时完成账户注册并持久化凭据。

## DNS 验证账户

独立于签发渠道的 DNS 服务商凭据（`alidns`/`cloudflare`/`dnspod`/`godaddy`），供 ACME DNS-01 复用：

```
POST   /api/v1/dns-accounts            # 创建 {name, provider, credentials}
GET    /api/v1/dns-accounts            # 列表
GET    /api/v1/dns-accounts/providers  # ["alidns","cloudflare","dnspod","godaddy"]
GET    /api/v1/dns-accounts/{id}       # 详情（凭据脱敏回显）
PUT    /api/v1/dns-accounts/{id}       # 更新（credentials 留空=保持）
DELETE /api/v1/dns-accounts/{id}       # 有证书引用时返回 409
```

签发时通过 `dns_provider_account_id` 引用；续期优先复用该账户，兼容旧证书的 `dns_config`。

## 其他

```
GET  /api/v1/acme-accounts             # 列表（不回显凭据）
GET  /api/v1/acme-accounts/providers   # 已注册渠道：["acme","aliyun","tencent"]
DELETE /api/v1/acme-accounts/{id}      # 有证书引用时返回 409
```

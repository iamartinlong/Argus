# SSL 渠道账户

签发渠道统一管理（ACME / 阿里云 / 腾讯云），凭据 AES-GCM 加密存储。

## ACME

- 目录 URL：默认 Let's Encrypt 生产（`https://acme-v02.api.letsencrypt.org/directory`）；staging 或企业 ACME CA 可自填
- 联系邮箱：证书申请通知
- EAB（External Account Binding）：企业 CA / 预注册账户需要时配置
- 「测试」触发账户注册并持久化账户密钥（后续签发/续期自动登录）

## 阿里云

- AccessKey ID/Secret（建议使用子账号，仅授权 SSL 证书与 DNS 权限）
- 地域（默认 cn-hangzhou）
- 申请人姓名/邮箱/电话（免费证书申请必填）
- 域名需托管阿里云 DNS（自动验证）；单域名签发（symantec-free-1-free）

## 腾讯云

- SecretId/SecretKey
- 联系邮箱/手机
- 验证方式：DNS_AUTO（域名托管腾讯云 DNS 且同账号，自动验证，推荐）/ DNS（手动添加 TXT）
- 免费证书 TrustAsia C1 DV，3 个月有效期，单域名

## DNS 验证账户

用于 ACME DNS-01 验证的 DNS 服务商凭据独立管理（`alidns`/`cloudflare`/`dnspod`/`godaddy`），避免每张证书重复录入。

- 独立表 `dns_provider_accounts`，凭据 AES-GCM 加密
- 签发时选择 DNS 账户（可复用），亦支持手动输入（兼容旧证书）
- 删除前校验是否被证书引用（仍有引用返回 409）

## 安全

- 凭据密文存储，列表与详情不回显
- 删除账户前需先删除/转移其下证书
- 建议使用最小权限子账号，定期轮换

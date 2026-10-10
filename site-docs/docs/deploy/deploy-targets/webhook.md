# Webhook 部署

通过 HTTP POST 将证书内容推送到目标 URL，由外部系统处理（配置中心、反向代理重载等）。

## 适用场景

- 对接配置中心（如 Apollo、Nacos）
- 触发反向代理 reload（HAProxy、Traefik 等）
- 自定义业务逻辑（证书变更通知、审计上报）

## 配置字段

| 字段 | 必填 | 说明 |
|---|---|---|
| URL | 是 | 目标地址（**必须 HTTPS**，证书私钥明文传输） |
| 签名密钥 | 否 | HMAC-SHA256 签名密钥，设置后请求头附带 `X-Signature` |

## POST 请求格式

Content-Type: `application/json`

```json
{
  "certificate_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "private_key_pem": "-----BEGIN EC PRIVATE KEY-----\n...\n-----END EC PRIVATE KEY-----",
  "domain": "todo.example.com",
  "domains": ["todo.example.com", "*.example.com"],
  "not_before": "2026-08-17T00:00:00Z",
  "not_after": "2026-11-15T00:00:00Z"
}
```

### 签名验证

如果配置了签名密钥，请求头包含：

```
X-Signature: sha256=<HMAC-SHA256 HexDigest>
```

验证方式：
```python
import hmac, hashlib

def verify_signature(payload, signature, secret):
    expected = hmac.new(secret.encode(), payload.encode(), hashlib.sha256).hexdigest()
    return hmac.compare_digest(f"sha256={expected}", signature)
```

## 使用流程

1. Web 后台「部署目标」→ 新建 → 类型「Webhook」
2. 填写 HTTPS URL + 签名密钥（可选）
3. 签发/续期成功后自动 POST 证书内容

## 安全设计

- **HTTPS 强制**：URL 必须以 `https://` 开头，防止私钥明文传输被窃听
- **HMAC 签名**：X-Signature 头防伪造请求（签名密钥 AES-GCM 加密存储）
- **超时控制**：30 秒超时，避免阻塞签发流程
- **重试机制**：部署失败自动重试（最多 3 次）

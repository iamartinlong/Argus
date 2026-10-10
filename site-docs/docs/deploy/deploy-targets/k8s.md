# Kubernetes Secret 部署

通过 `kubectl` 将证书作为 **TLS Secret** 写入 Kubernetes 集群。

## 适用场景

- Kubernetes 集群中的 Ingress / TLS 终止
- 云厂商托管 K8s（阿里云 ACK、腾讯云 TKE 等）

## 前置条件

- 服务端已安装 `kubectl` 并配置 kubeconfig（或 ServiceAccount）
- 目标 namespace 存在（或由 Secret 创建自动创建）
- 签发账号具备创建/更新 Secret 的 RBAC 权限

## 配置字段

| 字段 | 必填 | 说明 |
|---|---|---|
| Secret 名称 | 是 | TLS Secret 名称（如 `todo-example-com-tls`） |
| 命名空间 | 否 | K8s 命名空间，默认 `default` |
| kubeconfig | 否 | kubeconfig 文件路径（留空使用默认 `~/.kube/config`） |
| 证书字段名 | 否 | Secret 中证书的 key，默认 `tls.crt` |
| 私钥字段名 | 否 | Secret 中私钥的 key，默认 `tls.key` |
| 滚动重启 Deployment | 否 | 部署后自动 restart 的 Deployment 名称（可选） |

## 使用流程

1. Web 后台「部署目标」→ 新建 → 类型「Kubernetes Secret」
2. 填写 Secret 名称 + 命名空间（+ 可选 kubeconfig 路径）
3. 签发/续期成功后自动执行 `kubectl apply`

## 示例

Nginx Ingress TLS Secret：

```
Secret 名称：todo-example-com-tls
命名空间：default
证书字段：tls.crt
私钥字段：tls.key
滚动重启：todo-example-com-deployment
```

执行效果：
```bash
kubectl apply -f - --namespace=default <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: todo-example-com-tls
type: kubernetes.io/tls
data:
  tls.crt: <base64 证书链>
  tls.key: <base64 私钥>
EOF
kubectl rollout restart deployment/todo-example-com-deployment --namespace=default
```

## 安全设计

- kubeconfig 存储于服务端，经 AES-GCM 加密
- 推荐使用 ServiceAccount + RoleBinding 最小权限（仅 `secrets` 资源 `create/update`）
- 证书私钥通过 kubectl 命令行传入，不明文落盘

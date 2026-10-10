package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// 部署内容：证书链 + 私钥（解密后）
type Payload struct {
	Domains        []string
	PrimaryDomain  string
	CertificatePEM string // 完整链
	PrivateKeyPEM  string
}

// Result 部署结果
type Result struct {
	Checksum string // 内容 SHA256
	Message  string
}

// Adapter 部署通道适配器
type Adapter interface {
	Type() string
	// Deploy 将证书推送到目标并执行 hook
	Deploy(ctx context.Context, payload Payload, config map[string]any) (*Result, error)
	// Test 连接测试
	Test(ctx context.Context, config map[string]any) error
	// IsAsync 是否异步执行（agent 通道：Deploy 仅排队，结果由 agent 回执后联动更新）
	IsAsync() bool
}

// Registry 适配器注册表
type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

func (r *Registry) Register(a Adapter) {
	r.adapters[a.Type()] = a
}

func (r *Registry) Get(t string) (Adapter, bool) {
	a, ok := r.adapters[t]
	return a, ok
}

// HookEnv 模板变量
const (
	VarCertPath      = "{cert_path}"
	VarKeyPath       = "{key_path}"
	VarFullchainPath = "{fullchain_path}"
	VarDomain        = "{domain}"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
func jsonMarshal(v any) ([]byte, error)      { return json.Marshal(v) }

// ChecksumOf 计算部署内容 SHA256（供各适配器与 agent 任务使用）
func ChecksumOf(payload Payload) string {
	h := sha256.New()
	h.Write([]byte(payload.CertificatePEM))
	h.Write([]byte(payload.PrivateKeyPEM))
	return hex.EncodeToString(h.Sum(nil))
}

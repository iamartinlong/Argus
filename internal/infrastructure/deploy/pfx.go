package deploy

import (
	"crypto/rand"
	"fmt"
	"sync/atomic"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// PFXProfile 控制合成 PKCS#12（.pfx）时使用的加密算法。
//
// 背景：go-pkcs12 的 Modern2023（PBES2 + PBKDF2-HMAC-SHA-256 + AES-256-CBC）
// 官方文档注明「可被 Windows Server 2019 及更高版本读取」——Server 2016 及更早
// （含 2008 R2 / 2012 / 2012 R2 / 2016）的 CryptoAPI 解密不了，导入时表现为
// 误导性的「指定的网络密码不正确」。故默认用 LegacyDES（3DES + SHA-1），
// 该格式各版本 Windows 均可读。
type PFXProfile string

const (
	// PFXProfileLegacy 3DES + SHA-1 MAC（等价 OpenSSL -descert），兼容全部 Windows。
	PFXProfileLegacy PFXProfile = "legacy"
	// PFXProfileModern AES-256 + PBKDF2-SHA256，仅 Windows Server 2019+ 可读。
	PFXProfileModern PFXProfile = "modern"
)

// Valid 是否是受支持的 profile 取值。
func (p PFXProfile) Valid() bool {
	return p == PFXProfileLegacy || p == PFXProfileModern
}

// pfxProfile 全局策略（部署目标是全局配置，不按目标区分）。
// 用 atomic.Value 保证并发读安全；由启动时 SetPFXProfile 写入一次。
var pfxProfile atomic.Value

func init() { pfxProfile.Store(PFXProfileLegacy) }

// SetPFXProfile 设置全局 PFX 加密策略。应在启动装配阶段调用一次；
// 传入空值或非法值时保持默认（legacy）。
func SetPFXProfile(p PFXProfile) {
	if !p.Valid() {
		return
	}
	pfxProfile.Store(p)
}

// CurrentPFXProfile 返回当前的 PFX 加密策略（供诊断/日志用）。
func CurrentPFXProfile() PFXProfile {
	p, _ := pfxProfile.Load().(PFXProfile)
	if !p.Valid() {
		return PFXProfileLegacy
	}
	return p
}

// BuildPFX 将证书链 + 私钥合成为 PKCS#12（.pfx），使用当前全局 profile。
func BuildPFX(certPEM, keyPEM, password string) ([]byte, error) {
	return BuildPFXWithProfile(certPEM, keyPEM, password, CurrentPFXProfile())
}

// BuildPFXWithProfile 指定 profile 合成 PKCS#12（主要供测试与诊断使用）。
func BuildPFXWithProfile(certPEM, keyPEM, password string, profile PFXProfile) ([]byte, error) {
	certs, err := parseCertChain(certPEM)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("证书链为空")
	}
	key, err := parseAnyPrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	caCerts := certs[1:]

	encoder := pkcs12.LegacyDES
	if profile == PFXProfileModern {
		encoder = pkcs12.Modern
	}
	pfx, err := encoder.WithRand(rand.Reader).Encode(key, certs[0], caCerts, password)
	if err != nil {
		return nil, fmt.Errorf("合成 PFX: %w", err)
	}
	return pfx, nil
}

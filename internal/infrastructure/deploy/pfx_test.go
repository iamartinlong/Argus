package deploy

import (
	"bytes"
	"testing"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// DER 编码的算法 OID（用于断言实际使用的加密算法）
var (
	// 1.2.840.113549.1.12.1.3 pbeWithSHAAnd3KeyTripleDESCBC（legacy 用）
	oidPBE3DES = []byte{0x06, 0x0A, 0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x0C, 0x01, 0x03}
	// 1.2.840.113549.1.5.13 PBES2（modern 用）
	oidPBES2 = []byte{0x06, 0x09, 0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x05, 0x0D}
	// 2.16.840.1.101.3.4.1.42 aes256-CBC（modern 用）
	oidAES256CBC = []byte{0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x01, 0x2A}
)

// T11-相邻修复：默认必须是 legacy（3DES+SHA1），否则 2016 及更早的 Windows
// 解不开，导入时报误导性的「指定的网络密码不正确」。
func TestPFXDefaultProfileIsLegacy(t *testing.T) {
	// 不显式设置时，init() 已置为 legacy
	if got := CurrentPFXProfile(); got != PFXProfileLegacy {
		t.Fatalf("默认 profile 应为 legacy，got %q", got)
	}
}

// legacy profile 必须产出 3DES 加密的 PFX，且不含 PBES2/AES
func TestBuildPFXLegacyUses3DES(t *testing.T) {
	certPEM, keyPEM := generateTestPair(t, "legacy.example.com")
	pfx, err := BuildPFXWithProfile(certPEM, keyPEM, "secret-pw-1234", PFXProfileLegacy)
	if err != nil {
		t.Fatalf("BuildPFX legacy: %v", err)
	}
	if !bytes.Contains(pfx, oidPBE3DES) {
		t.Error("legacy PFX 应使用 pbeWithSHAAnd3KeyTripleDESCBC")
	}
	if bytes.Contains(pfx, oidPBES2) || bytes.Contains(pfx, oidAES256CBC) {
		t.Error("legacy PFX 不应含 PBES2/AES-256（旧 Windows 解不开）")
	}
	// 仍能被标准解析器读回（证明格式有效）
	if _, _, _, err := pkcs12.DecodeChain(pfx, "secret-pw-1234"); err != nil {
		t.Fatalf("legacy PFX 解析失败: %v", err)
	}
}

// modern profile 使用 PBES2 + AES-256
func TestBuildPFXModernUsesAES(t *testing.T) {
	certPEM, keyPEM := generateTestPair(t, "modern.example.com")
	pfx, err := BuildPFXWithProfile(certPEM, keyPEM, "secret-pw-1234", PFXProfileModern)
	if err != nil {
		t.Fatalf("BuildPFX modern: %v", err)
	}
	if !bytes.Contains(pfx, oidPBES2) {
		t.Error("modern PFX 应使用 PBES2")
	}
	if !bytes.Contains(pfx, oidAES256CBC) {
		t.Error("modern PFX 应使用 aes256-CBC")
	}
	if _, _, _, err := pkcs12.DecodeChain(pfx, "secret-pw-1234"); err != nil {
		t.Fatalf("modern PFX 解析失败: %v", err)
	}
}

// 两种 profile 都必须是有效 PFX（含正确私钥与证书）
func TestBuildPFXBothProfilesValid(t *testing.T) {
	certPEM, keyPEM := generateTestPair(t, "both.example.com")
	for _, profile := range []PFXProfile{PFXProfileLegacy, PFXProfileModern} {
		pfx, err := BuildPFXWithProfile(certPEM, keyPEM, "pw-1234-secret", profile)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		priv, leaf, _, err := pkcs12.DecodeChain(pfx, "pw-1234-secret")
		if err != nil {
			t.Fatalf("%s decode: %v", profile, err)
		}
		if priv == nil || leaf == nil {
			t.Fatalf("%s: 私钥或证书为空", profile)
		}
		if leaf.Subject.CommonName != "both.example.com" {
			t.Fatalf("%s: CN 不符 %q", profile, leaf.Subject.CommonName)
		}
	}
}

// SetPFXProfile 只接受合法值，非法值不改变当前策略
func TestSetPFXProfileIgnoresInvalid(t *testing.T) {
	orig := CurrentPFXProfile()
	defer SetPFXProfile(orig)

	SetPFXProfile(PFXProfileModern)
	if CurrentPFXProfile() != PFXProfileModern {
		t.Fatal("应切到 modern")
	}
	for _, bad := range []PFXProfile{"", "MODERN", "aes", "legacy "} {
		SetPFXProfile(bad)
		if CurrentPFXProfile() != PFXProfileModern {
			t.Fatalf("非法值 %q 不应改变策略", bad)
		}
	}
}

// PFXProfile.Valid
func TestPFXProfileValid(t *testing.T) {
	for _, p := range []PFXProfile{PFXProfileLegacy, PFXProfileModern} {
		if !p.Valid() {
			t.Errorf("%q 应合法", p)
		}
	}
	for _, p := range []PFXProfile{"", "Modern", "modern2023", "rc2"} {
		if p.Valid() {
			t.Errorf("%q 不应合法", p)
		}
	}
}

// BuildPFX（无 profile 参数）跟随全局策略
func TestBuildPFXFollowsGlobalProfile(t *testing.T) {
	orig := CurrentPFXProfile()
	defer SetPFXProfile(orig)

	certPEM, keyPEM := generateTestPair(t, "global.example.com")

	SetPFXProfile(PFXProfileLegacy)
	legacy, err := BuildPFX(certPEM, keyPEM, "pw-1234-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(legacy, oidPBE3DES) {
		t.Error("全局 legacy 时应产出 3DES PFX")
	}

	SetPFXProfile(PFXProfileModern)
	modern, err := BuildPFX(certPEM, keyPEM, "pw-1234-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(modern, oidAES256CBC) {
		t.Error("全局 modern 时应产出 AES PFX")
	}
}

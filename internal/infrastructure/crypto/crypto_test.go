package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestAESGCMRoundtrip(t *testing.T) {
	for _, key := range []string{
		"0123456789abcdef0123456789abcdef",                // hex
		base64.StdEncoding.EncodeToString(make([]byte, 32)), // base64
		strings.Repeat("k", 32),                             // raw
	} {
		c, err := NewAESGCM(key)
		if err != nil {
			t.Fatalf("NewAESGCM(%q): %v", key, err)
		}
		plain := "敏感数据-私钥-PEM"
		enc, err := c.EncryptString(plain)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.DecryptString(enc)
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Fatalf("roundtrip mismatch: %q != %q", got, plain)
		}
	}
}

func TestAESGCMTamperDetected(t *testing.T) {
	c, _ := NewAESGCM(strings.Repeat("k", 32))
	enc, _ := c.EncryptString("secret")
	// 篡改一个字节 → 认证失败
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := c.DecryptString(tampered); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	// 短密文拒绝
	if _, err := c.DecryptString("short"); err == nil {
		t.Fatal("short ciphertext accepted")
	}
	// 空密钥拒绝
	if _, err := NewAESGCM(""); err == nil {
		t.Fatal("empty key accepted")
	}
	// 短密钥拒绝
	if _, err := NewAESGCM("short"); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestJWTIssueVerify(t *testing.T) {
	svc, err := NewJWTService("test-secret-0123456789abcdef0123456789ab", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	user := &domain.User{ID: "u1", Role: domain.RoleAdmin}
	tok, err := svc.IssueAccessToken(user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.VerifyAccessToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "u1" || got.Role != domain.RoleAdmin {
		t.Fatalf("unexpected claims: %+v", got)
	}
}

func TestJWTExpiredRejected(t *testing.T) {
	svc, _ := NewJWTService("test-secret-0123456789abcdef0123456789ab", 1*time.Second)
	tok, _ := svc.IssueAccessToken(&domain.User{ID: "u1", Role: domain.RoleAdmin})
	time.Sleep(2 * time.Second)
	if _, err := svc.VerifyAccessToken(tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestJWTTamperedRejected(t *testing.T) {
	svc, _ := NewJWTService("test-secret-0123456789abcdef0123456789ab", time.Minute)
	tok, _ := svc.IssueAccessToken(&domain.User{ID: "u1", Role: domain.RoleAdmin})
	parts := strings.Split(tok, ".")
	// 篡改 payload 后重签需要密钥，这里直接翻转 payload 字符使其签名失效
	bad := parts[0] + "." + strings.TrimRight(parts[1], "=") + "x" + "." + parts[2]
	if _, err := svc.VerifyAccessToken(bad); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestJWTWrongKeyRejected(t *testing.T) {
	svc1, _ := NewJWTService("test-secret-0123456789abcdef0123456789ab", time.Minute)
	svc2, _ := NewJWTService("other-secret-0123456789abcdef0123456789xy", time.Minute)
	tok, _ := svc1.IssueAccessToken(&domain.User{ID: "u1", Role: domain.RoleAdmin})
	if _, err := svc2.VerifyAccessToken(tok); err == nil {
		t.Fatal("token signed with different key accepted")
	}
}

func TestJWTInvalidRoleRejected(t *testing.T) {
	svc, _ := NewJWTService("test-secret-0123456789abcdef0123456789ab", time.Minute)
	// 手工构造非法 role 的 token
	now := time.Now()
	c := claims{
		Role: "superuser",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(svc.secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyAccessToken(tok); err == nil {
		t.Fatal("invalid role token accepted")
	}
}

func TestJWTShortSecretRejected(t *testing.T) {
	if _, err := NewJWTService("short", time.Minute); err == nil {
		t.Fatal("short secret accepted")
	}
}

func TestIssueRefreshTokenHashed(t *testing.T) {
	svc, _ := NewJWTService("test-secret-0123456789abcdef0123456789ab", time.Minute)
	raw, hash, err := svc.IssueRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 || len(hash) != 64 {
		t.Fatalf("unexpected lengths: raw=%d hash=%d", len(raw), len(hash))
	}
	if raw == hash {
		t.Fatal("raw token equals hash")
	}
}

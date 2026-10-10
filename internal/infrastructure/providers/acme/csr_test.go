package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
)

// TestJWKThumbprintDeterministic 同一公钥两次计算一致，且输出为 RFC7638 base64url(SHA-256)
func TestJWKThumbprintDeterministic(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a, err := jwkThumbprint(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	b, err := jwkThumbprint(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("thumbprint not deterministic: %q vs %q", a, b)
	}
	// RFC7638 输出：base64url 编码的 32 字节 SHA-256（43 字符，无 padding）
	if len(a) != 43 {
		t.Fatalf("unexpected thumbprint length %d: %q", len(a), a)
	}

	// 不同公钥 → 不同 thumbprint
	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := jwkThumbprint(&key2.PublicKey)
	if c == a {
		t.Fatal("different keys produced identical thumbprint")
	}
}

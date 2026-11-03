package certbundle

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// readZip 解压并返回 文件名 → 内容
func readZip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("不是合法 zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = b
	}
	return out
}

// testPair 生成自签证书与私钥 PEM（ECDSA P256）。
// 内联而非复用 testutil.GenTestCert：testutil 的 live_server.go 拖入 httpapi，
// 会与 certbundle 形成 import cycle（testutil 需按依赖拆分后才能在此复用）。
func testPair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "bundle.example.com"},
		DNSNames:     []string{"bundle.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// ecKeyDER 取 EC 私钥 PEM 的 DER（与 x509.MarshalECPrivateKey 输出可比）
func ecKeyDER(t *testing.T, keyPEM string) []byte {
	t.Helper()
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		t.Fatalf("私钥 PEM 解析失败")
	}
	return block.Bytes
}

func TestBuildZip(t *testing.T) {
	data, err := BuildZip([]File{
		{Name: "a.txt", Data: []byte("alpha")},
		{Name: "dir/b.txt", Data: []byte("beta")},
	})
	if err != nil {
		t.Fatalf("BuildZip: %v", err)
	}
	files := readZip(t, data)
	if len(files) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(files), files)
	}
	if string(files["a.txt"]) != "alpha" || string(files["dir/b.txt"]) != "beta" {
		t.Fatalf("content mismatch: %v", files)
	}
}

func TestBuildZipEmpty(t *testing.T) {
	data, err := BuildZip(nil)
	if err != nil {
		t.Fatalf("BuildZip(nil): %v", err)
	}
	if files := readZip(t, data); len(files) != 0 {
		t.Fatalf("expected empty zip, got %v", files)
	}
}

func TestRandomPassword(t *testing.T) {
	const n = 16
	p, err := RandomPassword(n)
	if err != nil {
		t.Fatalf("RandomPassword: %v", err)
	}
	if len(p) != n {
		t.Fatalf("length = %d, want %d", len(p), n)
	}
	// 仅字母数字（避免 shell/配置文件转义问题）
	for _, c := range p {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Fatalf("unexpected char %q in password %q", c, p)
		}
	}
	// 两次调用不应相同（crypto/rand）
	p2, _ := RandomPassword(n)
	if p == p2 {
		t.Fatal("two passwords identical — randomness broken")
	}
}

func TestFormatSupported(t *testing.T) {
	for _, f := range SupportedFormats {
		if !FormatSupported(f) {
			t.Errorf("%q should be supported", f)
		}
	}
	for _, f := range []string{"", "jks", "root", "NGINX", "pem"} {
		if FormatSupported(f) {
			t.Errorf("%q should NOT be supported", f)
		}
	}
}

// PEM 类格式：nginx / apache / other 应含证书与私钥文件，内容原样落入
func TestBuildFormatZipPEMFormats(t *testing.T) {
	certPEM, keyPEM := testPair(t)
	cases := map[string]map[string]string{
		"nginx":  {"fullchain.pem": certPEM, "privkey.pem": keyPEM},
		"other":  {"cert.pem": certPEM, "privkey.pem": keyPEM},
		"apache": {"cert.crt": "", "privkey.key": keyPEM},
	}
	for format, want := range cases {
		data, name, err := BuildFormatZip(certPEM, keyPEM, format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if name != format+".zip" {
			t.Errorf("%s: filename = %q", format, name)
		}
		files := readZip(t, data)
		if len(files) != len(want) {
			t.Fatalf("%s: expected %d entries, got %v", format, len(want), files)
		}
		for f, content := range want {
			got, ok := files[f]
			if !ok {
				t.Errorf("%s: missing %s (got %v)", format, f, files)
				continue
			}
			if content != "" && string(got) != content {
				t.Errorf("%s: %s content mismatch", format, f)
			}
		}
	}
}

// PFX 类格式：iis / tomcat 生成的 PFX 必须能用 password.txt 里的密码解开，
// 且解出的证书 CN 与私钥均与输入一致（强校验，防"合成成功但打不开"）。
func TestBuildFormatZipPFXFormats(t *testing.T) {
	certPEM, keyPEM := testPair(t)
	wantKeyDER := ecKeyDER(t, keyPEM)
	for _, format := range []string{"iis", "tomcat"} {
		data, name, err := BuildFormatZip(certPEM, keyPEM, format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if name != format+".zip" {
			t.Errorf("%s: filename = %q", format, name)
		}
		files := readZip(t, data)
		pfx, ok := files["cert.pfx"]
		if !ok {
			t.Fatalf("%s: missing cert.pfx (got %v)", format, files)
		}
		pw, ok := files["password.txt"]
		if !ok {
			t.Fatalf("%s: missing password.txt", format)
		}
		password := strings.TrimSpace(string(pw))
		if len(password) != 16 {
			t.Errorf("%s: password length = %d, want 16", format, len(password))
		}

		priv, leaf, _, err := pkcs12.DecodeChain(pfx, password)
		if err != nil {
			t.Fatalf("%s: PFX 无法用 password.txt 解开: %v", format, err)
		}
		if leaf == nil || leaf.Subject.CommonName != "bundle.example.com" {
			t.Errorf("%s: PFX 内证书 CN 不符: %+v", format, leaf)
		}
		ecKey, ok := priv.(*ecdsa.PrivateKey)
		if !ok {
			t.Fatalf("%s: PFX 内私钥类型 %T，期望 *ecdsa.PrivateKey", format, priv)
		}
		gotKeyDER, err := x509.MarshalECPrivateKey(ecKey)
		if err != nil {
			t.Fatalf("%s: MarshalECPrivateKey: %v", format, err)
		}
		if !bytes.Equal(wantKeyDER, gotKeyDER) {
			t.Errorf("%s: PFX 内私钥与输入不一致", format)
		}
	}
}

// 不支持的格式必须报错且不返回数据
func TestBuildFormatZipUnsupported(t *testing.T) {
	certPEM, keyPEM := testPair(t)
	for _, format := range []string{"", "jks", "root", "bogus"} {
		data, name, err := BuildFormatZip(certPEM, keyPEM, format)
		if err == nil {
			t.Fatalf("%q: expected error, got zip %q (%d bytes)", format, name, len(data))
		}
		if data != nil || name != "" {
			t.Fatalf("%q: expected nil data/name on error", format)
		}
	}
}

// 证书解析失败时 apache 应报错（SplitChain 失败路径）
func TestBuildFormatZipInvalidCert(t *testing.T) {
	data, _, err := BuildFormatZip("not a pem", "also not", "apache")
	if err == nil {
		t.Fatalf("expected error for invalid cert, got %d bytes", len(data))
	}
	if !strings.Contains(err.Error(), "证书解析失败") {
		t.Errorf("unexpected error message: %v", err)
	}
}

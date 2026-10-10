package httpapi

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func generateRealCertPair(t *testing.T, cn string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	derKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: derKey}))
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return certPEM, keyPEM
}

func importRealCert(t *testing.T, s *testServer, token, domain string) string {
	t.Helper()
	certPEM, keyPEM := generateRealCertPair(t, domain)
	rec := s.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
		"domains":      []string{domain},
		"certificate":  certPEM,
		"private_key":  keyPEM,
		"verification": "dns01",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import %s: %d %s", domain, rec.Code, rec.Body.String())
	}
	var cert struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &cert)
	return cert.ID
}

func readZip(t *testing.T, rec *httptest.ResponseRecorder) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = data
	}
	return files
}

func downloadFormat(t *testing.T, s *testServer, certID, format, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?format="+format, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("download %s: %d %s", format, rec.Code, rec.Body.String())
	}
	return rec
}

func TestDownloadNginxZip(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	token := s.login("admin", "Adminpass123456")
	certID := importCert(t, s, token, "nginx.example.com")

	rec := downloadFormat(t, s, certID, "nginx", token)
	files := readZip(t, rec)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if _, ok := files["fullchain.pem"]; !ok {
		t.Fatal("missing fullchain.pem")
	}
	if _, ok := files["privkey.pem"]; !ok {
		t.Fatal("missing privkey.pem")
	}
	if !strings.Contains(string(files["fullchain.pem"]), "BEGIN CERTIFICATE") {
		t.Fatal("fullchain.pem not a certificate")
	}
	if !strings.Contains(string(files["privkey.pem"]), "PRIVATE KEY") {
		t.Fatal("privkey.pem not a key")
	}
}

func TestDownloadApacheZip(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	token := s.login("admin", "Adminpass123456")
	certID := importCert(t, s, token, "apache.example.com")

	rec := downloadFormat(t, s, certID, "apache", token)
	files := readZip(t, rec)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if _, ok := files["cert.crt"]; !ok {
		t.Fatal("missing cert.crt")
	}
	if _, ok := files["privkey.key"]; !ok {
		t.Fatal("missing privkey.key")
	}
	if strings.Count(string(files["cert.crt"]), "BEGIN CERTIFICATE") != 1 {
		t.Fatal("cert.crt must contain exactly the leaf certificate")
	}
}

func TestDownloadIISZip(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	token := s.login("admin", "Adminpass123456")
	certID := importRealCert(t, s, token, "iis.example.com")

	rec := downloadFormat(t, s, certID, "iis", token)
	files := readZip(t, rec)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	pfx, ok := files["cert.pfx"]
	if !ok {
		t.Fatal("missing cert.pfx")
	}
	pwData, ok := files["password.txt"]
	if !ok {
		t.Fatal("missing password.txt")
	}
	if len(pfx) == 0 {
		t.Fatal("empty pfx")
	}
	if len(pwData) < 8 {
		t.Fatalf("password too short: %q", string(pwData))
	}
}

func TestDownloadOtherZip(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	token := s.login("admin", "Adminpass123456")
	certID := importCert(t, s, token, "other.example.com")

	rec := downloadFormat(t, s, certID, "other", token)
	files := readZip(t, rec)
	if _, ok := files["cert.pem"]; !ok {
		t.Fatal("missing cert.pem")
	}
	if _, ok := files["privkey.pem"]; !ok {
		t.Fatal("missing privkey.pem")
	}
}

func TestDownloadUnsupportedFormats(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	token := s.login("admin", "Adminpass123456")
	certID := importCert(t, s, token, "jks.example.com")

	for _, format := range []string{"jks", "root"} {
		rec := s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?format="+format, nil, token)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s: expected 501, got %d", format, rec.Code)
		}
	}
	rec := s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?format=bogus", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus format: expected 400, got %d", rec.Code)
	}
}
package application

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

type fakeProviderAccountStore struct {
	mu       sync.Mutex
	accounts map[string]*domain.ProviderAccount
}

func newFakeProviderAccountStore() *fakeProviderAccountStore {
	return &fakeProviderAccountStore{accounts: map[string]*domain.ProviderAccount{}}
}

func (f *fakeProviderAccountStore) Create(_ context.Context, a *domain.ProviderAccount) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts[a.ID] = a
	return nil
}

func (f *fakeProviderAccountStore) GetByID(_ context.Context, id string) (*domain.ProviderAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accounts[id]; ok {
		return a, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeProviderAccountStore) List(context.Context) ([]*domain.ProviderAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.ProviderAccount
	for _, a := range f.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeProviderAccountStore) Update(_ context.Context, a *domain.ProviderAccount) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts[a.ID] = a
	return nil
}

func (f *fakeProviderAccountStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.accounts, id)
	return nil
}

var testCertPEM string

func init() {
	testCertPEM = generateTestCert("test.example.com")
}

func generateTestCert(cn string) string {
	cert, _ := generateTestCertWithKey(cn)
	return cert
}

// generateTestCertWithKey 生成自签证书 + 私钥 PEM
func generateTestCertWithKey(cn string) (string, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

// fakeProvider 最小渠道实现：仅用于账户创建等不触发真实签发的测试
type fakeProvider struct{}

func (fakeProvider) Name() string { return "acme" }

func (fakeProvider) Issue(context.Context, providers.IssueRequest) (*providers.IssuedCertificate, error) {
	return nil, fmt.Errorf("fake provider 不支持签发")
}

func (fakeProvider) Renew(context.Context, providers.RenewRequest) (*providers.IssuedCertificate, error) {
	return nil, fmt.Errorf("fake provider 不支持续期")
}

func (fakeProvider) Revoke(context.Context, providers.RevokeRequest) error {
	return fmt.Errorf("fake provider 不支持吊销")
}

func (fakeProvider) ValidateDomain(context.Context, providers.ValidateRequest) error { return nil }

func (fakeProvider) TestConnection(context.Context, *domain.ProviderAccount) error { return nil }

// testRegistry 含 fakeProvider 的注册表
type testRegistry struct {
	providers map[string]providers.CertificateProvider
}

func newTestRegistry() *testRegistry {
	return &testRegistry{providers: map[string]providers.CertificateProvider{
		"acme":    fakeProvider{},
		"aliyun":  fakeProvider{},
		"tencent": fakeProvider{},
	}}
}

func (r *testRegistry) Get(name string) (providers.CertificateProvider, error) {
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("未注册的证书渠道: %s", name)
}

func (r *testRegistry) Names() []string { return []string{"acme"} }

var _ = time.Now

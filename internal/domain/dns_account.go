package domain

import "time"

type DnsProvider string

const (
	DnsAliDNS     DnsProvider = "alidns"
	DnsCloudflare DnsProvider = "cloudflare"
	DnsDnspod     DnsProvider = "dnspod"
	DnsGodaddy    DnsProvider = "godaddy"
)

func (p DnsProvider) Valid() bool {
	return p == DnsAliDNS || p == DnsCloudflare || p == DnsDnspod || p == DnsGodaddy
}

type DnsProviderAccount struct {
	ID             string
	Name           string
	Provider       DnsProvider
	CredentialsEnc string // 加密后的 JSON 凭据
	Status         string // active | error
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

package asn1ber

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// berify 将 DER 输入递归改写为 BER：constructed 类型改不定长 + EOC 终结。
// 用于测试“BER 编码的 PFX”场景。
func berify(der []byte) []byte {
	tlvs, rest, err := parseAll(der)
	if err != nil || len(rest) != 0 {
		panic("berify: input not DER")
	}
	out := make([]byte, 0, len(der)*2)
	for _, t := range tlvs {
		out = t.berifyInto(out)
	}
	return out
}

func (t tlv) berifyInto(out []byte) []byte {
	if !isConstructed(t.tag) {
		return append(out, t.raw...)
	}
	inner, _, err := parseAll(t.content)
	if err != nil {
		panic("berify: inner not DER")
	}
	out = append(out, t.tag, lengthIndefinite)
	for _, c := range inner {
		out = c.berifyInto(out)
	}
	return append(out, tagEOC, 0x00)
}

// makeTestPFX 生成一张自签证书并编码为 DER PFX（测试夹具）
func makeTestPFX(t *testing.T, password string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "ber-test.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"ber-test.example.com"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pfxData, err := pkcs12.Modern2023.Encode(key, leaf, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	return pfxData
}

func TestToDERPassthroughForDERInput(t *testing.T) {
	der := makeTestPFX(t, "pass1234")
	out, err := ToDER(der)
	if err != nil {
		t.Fatalf("ToDER(DER): %v", err)
	}
	if !bytes.Equal(out, der) {
		t.Fatal("DER input should pass through unchanged")
	}
	if !IsDER(der) {
		t.Fatal("IsDER should be true for DER input")
	}
}

func TestToDERNormalizesIndefinitePFX(t *testing.T) {
	der := makeTestPFX(t, "lzh5n74s")
	ber := berify(der)
	if bytes.Equal(ber, der) {
		t.Fatal("berify should produce different bytes")
	}
	if IsDER(ber) {
		t.Fatal("IsDER should be false for BER input")
	}

	out, err := ToDER(ber)
	if err != nil {
		t.Fatalf("ToDER(BER): %v", err)
	}
	if !bytes.Equal(out, der) {
		t.Fatal("normalized output should equal original DER")
	}

	// 规范化后 pkcs12 应能解析
	priv, leaf, _, err := pkcs12.DecodeChain(out, "lzh5n74s")
	if err != nil {
		t.Fatalf("DecodeChain after normalize: %v", err)
	}
	if leaf.Subject.CommonName != "ber-test.example.com" {
		t.Fatalf("unexpected CN: %q", leaf.Subject.CommonName)
	}
	if priv == nil {
		t.Fatal("private key missing")
	}

	// 未规范化的 BER 必须失败（复现线上问题）
	if _, _, _, err := pkcs12.DecodeChain(ber, "lzh5n74s"); err == nil {
		t.Fatal("expected DecodeChain to fail on raw BER (regression guard)")
	}
}

func TestToDERFlattensConstructedOctetString(t *testing.T) {
	// SEQUENCE { constructed OCTET STRING { "AB", "CD" } } → SEQUENCE { OCTET STRING "ABCD" }
	ber := []byte{
		0x30, 0x80, // SEQUENCE indefinite
		0x24, 0x80, // constructed OCTET STRING indefinite
		0x04, 0x02, 'A', 'B',
		0x04, 0x02, 'C', 'D',
		0x00, 0x00, // EOC (octet string)
		0x00, 0x00, // EOC (sequence)
	}
	out, err := ToDER(ber)
	if err != nil {
		t.Fatal(err)
	}
	// SEQUENCE(6) { OCTET STRING(4) "ABCD" }
	want := []byte{0x30, 0x06, 0x04, 0x04, 'A', 'B', 'C', 'D'}
	if !bytes.Equal(out, want) {
		t.Fatalf("got % x, want % x", out, want)
	}
}

func TestToDERNestedIndefinite(t *testing.T) {
	// SEQUENCE { SET { INTEGER 1 } }，两层均不定长
	ber := []byte{
		0x30, 0x80,
		0x31, 0x80,
		0x02, 0x01, 0x01,
		0x00, 0x00,
		0x00, 0x00,
	}
	out, err := ToDER(ber)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x30, 0x05, 0x31, 0x03, 0x02, 0x01, 0x01}
	if !bytes.Equal(out, want) {
		t.Fatalf("got % x, want % x", out, want)
	}
}

func TestToDERErrors(t *testing.T) {
	cases := map[string][]byte{
		"empty":              {},
		"truncated":          {0x30, 0x05, 0x01},
		"indefinite-no-eoc":  {0x30, 0x80, 0x02, 0x01, 0x01},
		"trailing-after-tlv": {0x02, 0x01, 0x01, 0xff},
	}
	for name, in := range cases {
		if _, err := ToDER(in); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

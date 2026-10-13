package pkcs12fix

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------- 测试夹具：构造带畸形特征的 BER PFX ----------

var (
	oidDataContentTypeQuirk = asn1.ObjectIdentifier([]int{1, 2, 840, 113549, 1, 7, 1})
)

// quirkify 把标准 DER PFX 改写成阿里云风格的 BER PFX：
//  1. 所有 constructed 元素改不定长（0x80 ... 00 00）
//  2. authSafe 的 Data OCTET STRING 改 constructed(0x24) 多分片
//  3. EncryptedData 的 encryptedContent 改 [0] 显式挂多个 OCTET STRING 分片
func quirkify(t *testing.T, der []byte) []byte {
	t.Helper()
	var top asn1.RawValue
	if _, err := asn1.Unmarshal(der, &top); err != nil {
		t.Fatalf("quirkify: unmarshal: %v", err)
	}
	out := encodeQuirk(t, top, false, false)
	if len(out) == 0 {
		t.Fatal("quirkify: empty output")
	}
	return out
}

// encodeQuirk 重编码单个 RawValue；isDataContent/isEncryptedContent 标记特殊处理点
func encodeQuirk(t *testing.T, v asn1.RawValue, inDataContentInfo, inEncryptedContentInfo bool) []byte {
	if !v.IsCompound {
		return v.FullBytes
	}
	children := parseChildren(t, v.Bytes)

	// ContentInfo { OID(data), [0] } → Data OCTET STRING 改 constructed 分片
	if inDataContentInfo && len(children) == 2 && children[1].Class == asn1.ClassContextSpecific && children[1].Tag == 0 {
		var octet asn1.RawValue
		if _, err := asn1.Unmarshal(children[1].Bytes, &octet); err == nil && !octet.IsCompound && octet.Tag == asn1.TagOctetString {
			// children[1].Bytes = "04 <len> <authSafe>"；取 authSafe 内容分 3 段
			content := octet.Bytes
			frags := splitN(content, 3)
			inner := []byte{0x24, 0x80}
			for _, f := range frags {
				inner = append(inner, encodeOctet(f)...)
			}
			inner = append(inner, 0x00, 0x00)
			out := append([]byte{0xa0, 0x80}, inner...)
			out = append(out, 0x00, 0x00)
			// 第一个子元素（OID）原样
			head := encodeQuirk(t, children[0], false, false)
			return append(head, out...)
		}
	}

	// EncryptedContentInfo { OID(data), SEQUENCE(alg), [0] ct } → [0] 显式多分片
	if inEncryptedContentInfo && len(children) == 3 && children[2].Class == asn1.ClassContextSpecific && children[2].Tag == 0 {
		head := append(encodeQuirk(t, children[0], false, false), encodeQuirk(t, children[1], false, false)...)
		ct := children[2].Bytes // [0] IMPLICIT：内容即密文
		frags := splitN(ct, 2)
		inner := []byte{0xa0, 0x80}
		for _, f := range frags {
			inner = append(inner, encodeOctet(f)...)
		}
		inner = append(inner, 0x00, 0x00)
		return append(head, inner...)
	}

	// 通用：constructed 改不定长，递归处理子元素
	var buf []byte
	buf = append(buf, v.FullBytes[0], 0x80)
	isDataCI := isContentInfoWithData(children)
	isECI := isEncryptedContentInfoShape(children)
	for i, c := range children {
		buf = append(buf, encodeQuirk(t, c, isDataCI && i == 1, isECI && i == 2)...)
	}
	buf = append(buf, 0x00, 0x00)
	return buf
}

func encodeOctet(b []byte) []byte {
	out := []byte{0x04}
	if len(b) < 0x80 {
		out = append(out, byte(len(b)))
	} else {
		out = append(out, 0x82, byte(len(b)>>8), byte(len(b)))
	}
	return append(out, b...)
}

func splitN(b []byte, n int) [][]byte {
	if len(b) < n*2 {
		n = 1
	}
	size := (len(b) + n - 1) / n
	var out [][]byte
	for i := 0; i < len(b); i += size {
		end := i + size
		if end > len(b) {
			end = len(b)
		}
		out = append(out, b[i:end])
	}
	return out
}

func parseChildren(t *testing.T, b []byte) []asn1.RawValue {
	t.Helper()
	var out []asn1.RawValue
	rest := b
	for len(rest) > 0 {
		var v asn1.RawValue
		remaining, err := asn1.Unmarshal(rest, &v)
		if err != nil {
			t.Fatalf("parseChildren: %v", err)
		}
		out = append(out, v)
		rest = remaining
	}
	return out
}

func isContentInfoWithData(children []asn1.RawValue) bool {
	if len(children) != 2 {
		return false
	}
	if children[0].Tag != asn1.TagOID {
		return false
	}
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(children[0].FullBytes, &oid); err != nil {
		return false
	}
	return oid.Equal(oidDataContentTypeQuirk) && children[1].Class == asn1.ClassContextSpecific
}

func isEncryptedContentInfoShape(children []asn1.RawValue) bool {
	if len(children) != 3 {
		return false
	}
	if children[0].Tag != asn1.TagOID || !children[1].IsCompound {
		return false
	}
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(children[0].FullBytes, &oid); err != nil {
		return false
	}
	return oid.Equal(oidDataContentTypeQuirk) && children[2].Class == asn1.ClassContextSpecific
}

// makeMacPFX 生成带 MAC 的标准 DER PFX（Modern2023 编码器）
func makeMacPFX(t *testing.T, password string) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(20260101),
		Subject:      pkix.Name{CommonName: "quirk.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(48 * time.Hour),
		DNSNames:     []string{"quirk.example.com"},
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
	return pfxData, leaf
}

// ---------- 测试 ----------

func TestRepairQuirkyBERPFX(t *testing.T) {
	const password = "lzh5n74s"
	der, leaf := makeMacPFX(t, password)

	// 原始 DER 必须可直接解析
	if _, _, _, err := pkcs12.DecodeChain(der, password); err != nil {
		t.Fatalf("baseline DER decode: %v", err)
	}

	ber := quirkify(t, der)
	if bytes.Equal(ber, der) {
		t.Fatal("quirkify should change bytes")
	}
	// 畸形 BER 必须无法直接解析（复现线上问题）
	if _, _, _, err := pkcs12.DecodeChain(ber, password); err == nil {
		t.Fatal("expected raw BER decode to fail (regression guard)")
	}

	fixed, err := Repair(ber, password)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	priv, gotLeaf, _, err := pkcs12.DecodeChain(fixed, password)
	if err != nil {
		t.Fatalf("DecodeChain after repair: %v", err)
	}
	if gotLeaf.Subject.CommonName != leaf.Subject.CommonName {
		t.Fatalf("CN mismatch: %q", gotLeaf.Subject.CommonName)
	}
	if priv == nil {
		t.Fatal("private key missing after repair")
	}

	// 错误密码必须被拒绝（MAC 重算后不会放行错密码）
	if _, _, _, err := pkcs12.DecodeChain(fixed, "wrong-pass-1234"); err == nil {
		t.Fatal("expected wrong password to be rejected")
	}
}

func TestRepairDERPassthrough(t *testing.T) {
	const password = "lzh5n74s"
	der, _ := makeMacPFX(t, password)
	out, err := Repair(der, password)
	if err != nil {
		t.Fatalf("Repair(DER): %v", err)
	}
	if !bytes.Equal(out, der) {
		t.Fatal("DER input should pass through unchanged")
	}
}

func TestRepairWrongPasswordStillFailsDecryption(t *testing.T) {
	const password = "lzh5n74s"
	der, _ := makeMacPFX(t, password)
	ber := quirkify(t, der)
	fixed, err := Repair(ber, password)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	// 修复本身成功，但用错误密码解不开
	if _, _, _, err := pkcs12.DecodeChain(fixed, "not-the-password"); err == nil {
		t.Fatal("expected decode failure with wrong password")
	}
}

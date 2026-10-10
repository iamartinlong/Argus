package application

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"
)

// certValidity 解析首个 PEM 证书的有效期
func certValidity(certPEM string) (time.Time, time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("无效的证书 PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return cert.NotBefore, cert.NotAfter, nil
}

func extractCN(certPEM string) string {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.Subject.CommonName == "" {
		return ""
	}
	return cert.Subject.CommonName
}

// keyMatchesCert 校验私钥与证书公钥是否匹配（通过 PKIX 公钥 DER 比较）
func keyMatchesCert(cert *x509.Certificate, privKey any) bool {
	var pub any
	switch k := privKey.(type) {
	case *ecdsa.PrivateKey:
		pub = &k.PublicKey
	case *rsa.PrivateKey:
		pub = &k.PublicKey
	case ed25519.PrivateKey:
		pub = k.Public()
	default:
		return false
	}
	certPub, err1 := x509.MarshalPKIXPublicKey(cert.PublicKey)
	keyPub, err2 := x509.MarshalPKIXPublicKey(pub)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(certPub, keyPub)
}

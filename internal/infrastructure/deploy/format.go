package deploy

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// SplitChain 将证书 PEM（叶子 + 链）拆为叶子与其余链。
// certPEM 为空时返回空串；无法解析为任何证书时返回错误。
func SplitChain(certPEM string) (leaf, rest string, err error) {
	var blocks []*pem.Block
	restData := []byte(certPEM)
	for len(restData) > 0 {
		var block *pem.Block
		block, restData = pem.Decode(restData)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, perr := x509.ParseCertificate(block.Bytes); perr != nil {
			return "", "", fmt.Errorf("解析证书失败: %w", perr)
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return "", "", nil
	}
	leaf = string(pem.EncodeToMemory(blocks[0]))
	for _, b := range blocks[1:] {
		rest += string(pem.EncodeToMemory(b))
	}
	return leaf, rest, nil
}

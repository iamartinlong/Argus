// Package pkcs12fix 修复 BER 编码的 PKCS#12（PFX）文件，使其可被标准 DER 解析器读取。
//
// 背景：PKCS#12 规范（RFC 7292）允许 BER 编码，阿里云等平台导出的 IIS 证书 PFX
// 使用 ASN.1 不定长（indefinite length）编码。Go 的 encoding/asn1（以及基于它的
// software.sslmate.com/src/go-pkcs12）只支持严格 DER，会直接拒绝此类文件。
//
// 修复分两步：
//  1. BER → DER 结构化规范化（internal/infrastructure/asn1ber）
//  2. 重算 MAC：authSafe 内容字节被规范化改写后，原 MAC 即失效，
//     需按 RFC 7292 Appendix B.2 的 PKCS#12 KDF 重新派生 MAC 密钥并计算 HMAC，
//     再写回 MacData.Digest
//
// 已是 DER 的输入原样返回（零成本快路径）。
package pkcs12fix

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/asn1"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"unicode/utf16"

	"github.com/iamartinlong/Argus/internal/infrastructure/asn1ber"
)

var (
	oidDataContentType = asn1.ObjectIdentifier([]int{1, 2, 840, 113549, 1, 7, 1})
	oidSHA1           = asn1.ObjectIdentifier([]int{1, 3, 14, 3, 2, 26})
	oidSHA256         = asn1.ObjectIdentifier([]int{2, 16, 840, 1, 101, 3, 4, 2, 1})
	oidSHA512         = asn1.ObjectIdentifier([]int{2, 16, 840, 1, 101, 3, 4, 2, 3})
)

// pfxPduMirror 与 pkcs12 库内部结构一致的镜像（该库未导出这些类型）
type pfxPduMirror struct {
	Version  int
	AuthSafe contentInfoMirror
	MacData  macDataMirror `asn1:"optional"`
}

type contentInfoMirror struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"tag:0,explicit,optional"`
}

type macDataMirror struct {
	Mac        digestInfoMirror
	MacSalt    []byte
	Iterations int `asn1:"optional,default:1"`
}

type digestInfoMirror struct {
	Algorithm pkixAlgorithmIdentifier
	Digest    []byte
}

type pkixAlgorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

// Repair 返回可被 DER 解析器（encoding/asn1 / go-pkcs12）读取的 PFX 字节。
// password 用于重算 MAC；已是 DER 或无需 MAC 时尽量保持原字节。
func Repair(data []byte, password string) ([]byte, error) {
	if asn1ber.IsDER(data) {
		return data, nil
	}
	der, err := repairNormalize(data)
	if err != nil {
		return nil, err
	}
	return recomputeMac(der, password)
}

// repairNormalize PKCS#12 专用规范化：
// authSafe 内的 AuthenticatedSafe 同样是 BER，必须一并转 DER 才能被解析；
// [0] 下多 OCTET STRING 分片需拼接（部分面板产出该变体）。
func repairNormalize(data []byte) ([]byte, error) {
	return asn1ber.ToDERPFX(data)
}

// recomputeMac 按 RFC 7292 重算 MacData.Digest 并重编码 PFX PDU
func recomputeMac(der []byte, password string) ([]byte, error) {
	var pdu pfxPduMirror
	if _, err := asn1.Unmarshal(der, &pdu); err != nil {
		return nil, fmt.Errorf("规范化后仍无法解析 PFX 结构: %w", err)
	}
	// 无 MAC（MacData 缺省）：直接返回规范化结果，由调用方/库决定是否可接受
	if len(pdu.MacData.Mac.Digest) == 0 {
		return der, nil
	}
	if !pdu.AuthSafe.ContentType.Equal(oidDataContentType) {
		return nil, errors.New("仅支持 password-protected（data 类型）PFX")
	}

	// Content.Bytes 为 [0] EXPLICIT 内的 Data OCTET STRING TLV；取其内容即 authSafe 字节
	var dataTLV asn1.RawValue
	if _, err := asn1.Unmarshal(pdu.AuthSafe.Content.Bytes, &dataTLV); err != nil {
		return nil, fmt.Errorf("解析 authSafe Data 失败: %w", err)
	}
	authSafe := dataTLV.Bytes
	if len(authSafe) == 0 {
		return nil, errors.New("authSafe 内容为空")
	}

	pw, err := bmpStringZeroTerminated(password)
	if err != nil {
		return nil, err
	}
	newDigest, err := computeMac(&pdu.MacData, authSafe, pw)
	if err != nil {
		return nil, fmt.Errorf("重算 MAC 失败: %w", err)
	}
	pdu.MacData.Mac.Digest = newDigest

	out, err := asn1.Marshal(pdu)
	if err != nil {
		return nil, fmt.Errorf("重编码 PFX 失败: %w", err)
	}
	return out, nil
}

// computeMac 与 go-pkcs12 的 verifyMac 完全同构（HMAC + PKCS#12 KDF ID=3）
func computeMac(macData *macDataMirror, message, password []byte) ([]byte, error) {
	var hFn func() hash.Hash
	var key []byte
	switch {
	case macData.Mac.Algorithm.Algorithm.Equal(oidSHA1):
		hFn = sha1.New
		key = pkcs12kdf(sha1.New, 20, 64, macData.MacSalt, password, macData.Iterations, 3, 20)
	case macData.Mac.Algorithm.Algorithm.Equal(oidSHA256):
		hFn = sha256.New
		key = pkcs12kdf(sha256.New, 32, 64, macData.MacSalt, password, macData.Iterations, 3, 32)
	case macData.Mac.Algorithm.Algorithm.Equal(oidSHA512):
		hFn = sha512.New
		key = pkcs12kdf(sha512.New, 64, 128, macData.MacSalt, password, macData.Iterations, 3, 64)
	default:
		return nil, fmt.Errorf("不支持的 MAC 摘要算法: %v", macData.Mac.Algorithm.Algorithm)
	}
	mac := hmac.New(hFn, key)
	mac.Write(message)
	return mac.Sum(nil), nil
}

// pkcs12kdf 实现 RFC 7292 Appendix B.2 的密钥派生函数（PKCS#12 KDF）。
// 与 go-pkcs12 内部 pbkdf 等价（BSD 许可实现的等价重写）。
func pkcs12kdf(h func() hash.Hash, u, v int, salt, password []byte, r int, id byte, size int) []byte {
	// 1. D = v/8 个 ID 字节（注意：RFC 文本为 v 个 ID 比特即 v/8 字节，
	//    但 go-pkcs12 及主流实现按 v 字节填充，此处保持一致以互操作）
	d := make([]byte, v)
	for i := range d {
		d[i] = id
	}
	// 2. S：salt 重复填充至 v 字节整数倍
	s := fillWithRepeats(salt, v)
	// 3. P：password 重复填充至 v 字节整数倍
	p := fillWithRepeats(password, v)
	// 4. I = S || P
	i := append(s, p...)
	// 5. c = ceil(size/u)
	c := (size + u - 1) / u

	a := make([]byte, c*u)
	var ijBuf []byte
	for blk := 0; blk < c; blk++ {
		// A. Ai = H^r(D || I)
		hh := h()
		hh.Write(d)
		hh.Write(i)
		ai := hh.Sum(nil)
		for j := 1; j < r; j++ {
			hh := h()
			hh.Write(ai)
			ai = hh.Sum(nil)
		}
		copy(a[blk*u:], ai)

		if blk < c-1 {
			// B. B = Ai 重复填充至 v 字节
			b := make([]byte, 0, v)
			for len(b) < v {
				b = append(b, ai...)
			}
			b = b[:v]
			// C. I_j = (I_j + B + 1) mod 2^v（按 v 字节块递增）
			bInt := new(big.Int).SetBytes(b)
			one := big.NewInt(1)
			ij := new(big.Int)
			for j := 0; j < len(i)/v; j++ {
				ij.SetBytes(i[j*v : (j+1)*v])
				ij.Add(ij, bInt)
				ij.Add(ij, one)
				ijb := ij.Bytes()
				if len(ijb) > v {
					ijb = ijb[len(ijb)-v:]
				}
				if len(ijb) < v {
					if ijBuf == nil {
						ijBuf = make([]byte, v)
					}
					short := v - len(ijb)
					for k := 0; k < short; k++ {
						ijBuf[k] = 0
					}
					copy(ijBuf[short:], ijb)
					ijb = ijBuf
				}
				copy(i[j*v:(j+1)*v], ijb)
			}
		}
	}
	return a[:size]
}

func fillWithRepeats(pattern []byte, v int) []byte {
	if len(pattern) == 0 {
		return nil
	}
	outputLen := v * ((len(pattern) + v - 1) / v)
	out := make([]byte, 0, outputLen)
	for len(out) < outputLen {
		out = append(out, pattern...)
	}
	return out[:outputLen]
}

// bmpStringZeroTerminated BMPString（UCS-2BE）编码并以 0x0000 结尾（RFC 7292 B.1）
func bmpStringZeroTerminated(s string) ([]byte, error) {
	ret := make([]byte, 0, 2*len(s)+2)
	for _, r := range s {
		t := utf16.Encode([]rune{r})
		if len(t) != 1 || t[0] == 0xfffd {
			return nil, errors.New("密码包含无法以 UCS-2 编码的字符")
		}
		ret = append(ret, byte(t[0]>>8), byte(t[0]&0xff))
	}
	return append(ret, 0, 0), nil
}

// 确保 subtle 被使用（MAC 常量时间比较的工具函数，防御性保留）
var _ = subtle.ConstantTimeCompare

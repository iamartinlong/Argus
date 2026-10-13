// Package asn1ber 提供 BER → DER 规范化能力。
//
// 背景：PKCS#12（PFX）规范允许 BER 编码，部分证书面板/工具（非 Windows 标准导出）
// 产出的 PFX 使用 ASN.1 不定长（indefinite length）编码。Go 的 encoding/asn1 只支持
// 严格 DER，会直接报 "indefinite length found (not DER)"，导致此类 PFX 无法导入。
//
// 本包将 BER 输入转换为等价 DER（定长）输出：递归重写不定长 TLV 为定长，
// 并将 BER 允许的 constructed OCTET STRING / BIT STRING 拍平为 primitive。
package asn1ber

import (
	"errors"
	"fmt"
)

var (
	errTruncated  = errors.New("asn1: truncated input")
	errBadLength  = errors.New("asn1: invalid length encoding")
	errUnexpected = errors.New("asn1: unexpected data after end-of-contents")
)

// tag class / constructed 标志位
const (
	classMask   = 0xC0
	constructed = 0x20
	tagNumber   = 0x1F

	// 特殊 tag
	tagEOC          = 0x00 // end-of-contents（与 length 0 组成 00 00 终结符）
	tagHighNumber   = 0x1F // tag number >= 31 的 High-tag-number form
	lengthIndefinite = 0x80
)

// tlv 表示一个解析出的 BER TLV（原始字节 + 结构信息）
type tlv struct {
	tag        byte // 首字节（class+constructed+number，多字节 tag 仅保留首字节）
	content    []byte
	indefinite bool
	raw        []byte // 完整原始字节（含 tag+length）
}

// ToDER 将 BER 编码的 ASN.1 数据转换为等价 DER。
// 仅做结构转换（不定长→定长、constructed string 拍平），
// 所有 primitive 内容字节逐字节保留——PKCS#12 的 MAC 覆盖 authSafe 内容，
// 因此本函数对含 MAC 的 PFX 是安全的。
// 输入本身已是 DER 时原样返回（零拷贝语义上返回新 slice，内容不变）。
func ToDER(ber []byte) ([]byte, error) {
	return toDER(ber, false, false)
}

// ToDERWithEmbedded 在 ToDER 基础上，额外把 OCTET STRING 内容中内嵌的 ASN.1
// （如 PKCS#12 的 AuthenticatedSafe）一并规范化。
// 注意：这会改写 OCTET STRING 内容字节，对含 MAC 的 PFX 会破坏 MAC 校验，
// 调用方必须配合 MAC 重算（见 internal/infrastructure/pkcs12fix）。
func ToDERWithEmbedded(ber []byte) ([]byte, error) {
	return toDER(ber, true, false)
}

// ToDERPFX PKCS#12（PFX）专用规范化：在 ToDERWithEmbedded 基础上，
// 额外处理 PFX BER 变体——[0] EXPLICIT 下直接挂多个 OCTET STRING 分片
// （`A0 { 04 f1, 04 f2, ... }`，部分面板/工具产出）时拼接为单个 OCTET STRING。
// 会改写 authSafe 内容字节，调用方必须配合 MAC 重算（见 pkcs12fix.Repair）。
func ToDERPFX(ber []byte) ([]byte, error) {
	return toDER(ber, true, true)
}

func toDER(ber []byte, embedded, pfxMode bool) ([]byte, error) {
	if len(ber) == 0 {
		return nil, errTruncated
	}
	tlvs, rest, err := parseAll(ber)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", errUnexpected, len(rest))
	}
	// 顶层只应有一个 TLV（PKCS#12 为单个 SEQUENCE）；多个时按 DER 规范同样不允许，
	// 但为兼容宽松输入，逐个输出。
	out := make([]byte, 0, len(ber))
	for _, t := range tlvs {
		out = t.encode(out, embedded, pfxMode)
	}
	return out, nil
}

// IsDER 快速判断输入是否已是严格 DER（无不定长、无 constructed string）。
// 用于决定是否需要转换，避免对正常文件做无谓重写。
func IsDER(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	tlvs, rest, err := parseAll(data)
	if err != nil || len(rest) != 0 {
		return false
	}
	for _, t := range tlvs {
		if t.indefinite {
			return false
		}
		if isConstructedString(t.tag) {
			return false
		}
		if !t.childrenAreDER() {
			return false
		}
	}
	return true
}

func (t tlv) childrenAreDER() bool {
	if t.indefinite || !isConstructed(t.tag) {
		return true
	}
	inner, rest, err := parseAll(t.content)
	if err != nil || len(rest) != 0 {
		return false
	}
	for _, c := range inner {
		if c.indefinite || isConstructedString(c.tag) {
			return false
		}
		if !c.childrenAreDER() {
			return false
		}
	}
	return true
}

// parseAll 解析缓冲区中的连续 TLV 序列。
// 返回：TLV 列表、剩余未解析字节（含 indefinite 场景下的 EOC 已由调用方消费）。
func parseAll(b []byte) ([]tlv, []byte, error) {
	var out []tlv
	i := 0
	for i < len(b) {
		t, n, err := parseTLV(b, i)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, t)
		i += n
	}
	return out, nil, nil
}

// parseTLV 从 off 处解析一个 TLV，返回 TLV 与消费的总字节数。
// indefinite 情形下 EOC（00 00）已被消费，content 为 EOC 之前的所有子 TLV 原始字节。
func parseTLV(b []byte, off int) (tlv, int, error) {
	start := off
	if off >= len(b) {
		return tlv{}, 0, errTruncated
	}
	tag := b[off]
	off++
	if tag&tagNumber == tagHighNumber {
		// High-tag-number form：跳过多字节 tag number（PKCS#12 不会出现，保守处理）
		for off < len(b) {
			c := b[off]
			off++
			if c&0x80 == 0 {
				break
			}
		}
	}
	if off >= len(b) {
		return tlv{}, 0, errTruncated
	}
	l := b[off]
	off++
	var (
		contentStart, contentEnd int
		indefinite               bool
	)
	switch {
	case l < lengthIndefinite:
		contentStart = off
		contentEnd = off + int(l)
	case l == lengthIndefinite:
		// 不定长：解析子 TLV 直到 EOC（00 00）
		indefinite = true
		contentStart = off
		for {
			if off+2 > len(b) {
				return tlv{}, 0, errTruncated
			}
			if b[off] == tagEOC && b[off+1] == 0x00 {
				off += 2
				break
			}
			_, n, err := parseTLV(b, off)
			if err != nil {
				return tlv{}, 0, err
			}
			off += n
		}
		contentEnd = off - 2
	case l == 0xFF:
		return tlv{}, 0, errBadLength
	default:
		nb := int(l & 0x7f)
		if off+nb > len(b) {
			return tlv{}, 0, errTruncated
		}
		var n int
		for j := 0; j < nb; j++ {
			n = n<<8 | int(b[off+j])
		}
		off += nb
		contentStart = off
		contentEnd = off + n
	}
	if contentEnd > len(b) || contentStart > contentEnd {
		return tlv{}, 0, errTruncated
	}
	// 注意：不定长时 EOC（00 00）已被消费，consumed 必须含 EOC；
	// 否则 parseAll 会把 EOC 当作尾部残留字节，导致规范化退化为原样透传。
	consumed := contentEnd
	if indefinite {
		consumed = off
	}
	return tlv{
		tag:        tag,
		content:    b[contentStart:contentEnd],
		indefinite: indefinite,
		raw:        b[start:contentEnd],
	}, consumed - start, nil
}

// encode 将 TLV（递归规范化后）写入 out，返回追加后的切片。
func (t tlv) encode(out []byte, embedded, pfxMode bool) []byte {
	if !t.indefinite && !containsIndefinite(t.content) && !isConstructedString(t.tag) {
		// 已是 DER 形态且子树干净：原样透传（最快路径）
		return append(out, t.raw...)
	}
	content := t.normalizedContent(embedded, pfxMode)
	out = append(out, t.effectiveTag())
	out = appendDefiniteLength(out, len(content))
	return append(out, content...)
}

// effectiveTag 规范化后应输出的 tag：constructed string 拍平时改为 primitive
//（0x24→0x04、0x23→0x03；constructed OCTET/BIT STRING 是 BER 专有，DER 不允许）。
func (t tlv) effectiveTag() byte {
	if isConstructedString(t.tag) {
		return t.tag &^ constructed
	}
	return t.tag
}

// normalizedContent 产出规范化后的 content 字节。
// embedded=true 时额外规范化 OCTET STRING 内层 ASN.1（会改写内容字节，破坏 MAC）。
// pfxMode=true 时启用 PKCS#12 变体规则（context 标签下多 OCTET STRING 分片拼接）。
func (t tlv) normalizedContent(embedded, pfxMode bool) []byte {
	if isConstructedString(t.tag) {
		// constructed OCTET STRING(0x24)/BIT STRING(0x23)：拍平所有子段的 content
		// （内容字节为各段拼接，逐字节保留，MAC 安全）
		var flat []byte
		inner, _, err := parseAll(t.content)
		if err != nil {
			return t.content // 解析失败保守返回原内容
		}
		for _, c := range inner {
			flat = append(flat, c.content...)
		}
		if embedded {
			return normalizeEmbeddedASN1(flat)
		}
		return flat
	}
	if !isConstructed(t.tag) {
		// primitive OCTET STRING：embedded 模式下内容可能是内嵌 ASN.1
		// （如 PKCS#12 AuthenticatedSafe），尝试规范化；加密内容无法按 ASN.1
		// 解析时原样返回
		if embedded && t.tag == 0x04 {
			return normalizeEmbeddedASN1(t.content)
		}
		return t.content // primitive：内容不解释
	}
	// constructed（SEQUENCE/SET/context tag）：递归规范化子 TLV
	inner, _, err := parseAll(t.content)
	if err != nil {
		return t.content
	}
	// PKCS#12 变体：EncryptedContentInfo 的 encryptedContent 是 [0] IMPLICIT
	// OCTET STRING（Go encoding/asn1 对 []byte tag:0 只认隐式 80 编码；
	// 显式 A0{04} 会被静默跳过导致 empty encrypted data）。
	// 部分 BER 产出方把它写成 A0 下挂多个 OCTET STRING 分片，此处统一为 80 <拼接>。
	if pfxMode && t.tag == 0x30 && isEncryptedContentInfo(inner) {
		return encodeEncryptedContentInfo(inner)
	}
	out := make([]byte, 0, len(t.content))
	for _, c := range inner {
		out = c.encode(out, embedded, pfxMode)
	}
	return out
}

// oidDataContentType PKCS#12 data 内容类型 OID（1.2.840.113549.1.7.1）
var oidDataContentType = []byte{0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x07, 0x01}

// isEncryptedContentInfo 判断 SEQUENCE 是否为 EncryptedContentInfo：
// { OID(data), AlgorithmIdentifier SEQUENCE, [0] encryptedContent }
// （ContentInfo 为 { OID, [0] }，第二元素是 context 标签而非 SEQUENCE，可区分）
func isEncryptedContentInfo(children []tlv) bool {
	if len(children) < 3 {
		return false
	}
	if children[0].tag != 0x06 || !bytesEqual(children[0].raw, oidDataContentType) {
		return false
	}
	return children[1].tag == 0x30 && isContextTag(children[2].tag)
}

// encodeEncryptedContentInfo 规范化 EncryptedContentInfo：前两个元素原样递归，
// encryptedContent（[0]）按 IMPLICIT 输出（80 <各 OCTET STRING 分片拼接>）。
func encodeEncryptedContentInfo(children []tlv) []byte {
	out := make([]byte, 0, 64)
	for i, c := range children {
		if i < 2 {
			out = c.encode(out, true, true)
			continue
		}
		if i > 2 {
			out = c.encode(out, true, true)
			continue
		}
		// encryptedContent：拼接其下所有 primitive OCTET STRING 分片内容
		var flat []byte
		if c.tag&0x20 != 0 {
			if frags, _, err := parseAll(c.content); err == nil {
				for _, f := range frags {
					flat = append(flat, f.content...)
				}
			} else {
				flat = c.content
			}
		} else {
			flat = c.content
		}
		out = append(out, 0x80) // [0] IMPLICIT context primitive
		out = appendDefiniteLength(out, len(flat))
		out = append(out, flat...)
	}
	return out
}

func isContextTag(tag byte) bool { return tag&classMask == 0x80 }

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// allPrimitiveOctetStrings 所有子 TLV 均为 primitive OCTET STRING（0x04）
func allPrimitiveOctetStrings(tlvs []tlv) bool {
	for _, t := range tlvs {
		if t.tag != 0x04 || t.indefinite {
			return false
		}
	}
	return true
}

// normalizeEmbeddedASN1 尝试把 OCTET STRING 的内容按 ASN.1 规范化。
// 仅当内容完整解析为 TLV 序列且以 SEQUENCE(0x30) 开头时才重写——
// 该启发式覆盖 PKCS#12 的 Data OCTET STRING（AuthenticatedSafe 为 SEQUENCE OF），
// 同时避免把加密段（随机字节）误判为 ASN.1 而损坏。
func normalizeEmbeddedASN1(content []byte) []byte {
	if len(content) < 2 || content[0] != 0x30 {
		return content
	}
	tlvs, rest, err := parseAll(content)
	if err != nil || len(rest) != 0 || len(tlvs) == 0 {
		return content
	}
	out := make([]byte, 0, len(content))
	for _, c := range tlvs {
		out = c.encode(out, true, true)
	}
	return out
}

func containsIndefinite(b []byte) bool {
	tlvs, _, err := parseAll(b)
	if err != nil {
		return true // 保守视为需要处理
	}
	for _, t := range tlvs {
		if t.indefinite || isConstructedString(t.tag) || containsIndefinite(t.content) {
			return true
		}
	}
	return false
}

func isConstructed(tag byte) bool { return tag&constructed != 0 }

// isConstructedString BER 允许的 constructed string 类型（需拍平为 primitive）
func isConstructedString(tag byte) bool {
	if !isConstructed(tag) {
		return false
	}
	return tag&classMask == 0 && (tag&tagNumber == 4 || tag&tagNumber == 3) // 0x24 / 0x23
}

// appendDefiniteLength 以 DER 最小字节数写入长度
func appendDefiniteLength(out []byte, n int) []byte {
	switch {
	case n < 0x80:
		return append(out, byte(n))
	case n < 0x100:
		return append(out, 0x81, byte(n))
	case n < 0x10000:
		return append(out, 0x82, byte(n>>8), byte(n))
	case n < 0x1000000:
		return append(out, 0x83, byte(n>>16), byte(n>>8), byte(n))
	default:
		return append(out, 0x84, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// AESGCM 使用 32 字节主密钥（hex 或 base64 或原文）加密敏感数据。
type AESGCM struct {
	aead cipher.AEAD
}

func NewAESGCM(masterKey string) (*AESGCM, error) {
	key, err := decodeKey(masterKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func decodeKey(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("master key is empty")
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(s) >= 32 {
		// 原文形式：取前 32 字节
		return []byte(s)[:32], nil
	}
	return nil, errors.New("master key must be >= 32 bytes (hex/base64/raw)")
}

// Encrypt 输出 base64(nonce || ciphertext)
func (c *AESGCM) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (c *AESGCM) Decrypt(enc string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if len(raw) < c.aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, data := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	return c.aead.Open(nil, nonce, data, nil)
}

func (c *AESGCM) EncryptString(s string) (string, error) {
	return c.Encrypt([]byte(s))
}

func (c *AESGCM) DecryptString(enc string) (string, error) {
	b, err := c.Decrypt(enc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

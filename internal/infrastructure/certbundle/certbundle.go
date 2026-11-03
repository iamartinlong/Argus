// Package certbundle 按服务器类型打包证书文件（zip），供 HTTP 下载与 MCP
// 下载工具共用（单一实现，避免两处逻辑漂移）。
package certbundle

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// File zip 包内单文件
type File struct {
	Name string
	Data []byte
}

// BuildZip 打包为 zip 字节
func BuildZip(files []File) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RandomPassword 生成 length 位字母数字随机密码（crypto/rand）
func RandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		out[i] = charset[n.Int64()]
	}
	return string(out), nil
}

// SupportedFormats 支持的服务器格式
var SupportedFormats = []string{"nginx", "apache", "iis", "tomcat", "other"}

// BuildFormatZip 按服务器格式打包证书 + 私钥，返回 (zip 字节, 文件名)。
// format 取值见 SupportedFormats；iis/tomcat 会生成 PFX 与随机密码文件。
func BuildFormatZip(certPEM, privKeyPEM, format string) ([]byte, string, error) {
	var files []File
	switch format {
	case "nginx", "other":
		leafName := "fullchain.pem"
		if format == "other" {
			leafName = "cert.pem"
		}
		files = []File{
			{Name: leafName, Data: []byte(certPEM)},
			{Name: "privkey.pem", Data: []byte(privKeyPEM)},
		}
	case "apache":
		leaf, _, err := deploy.SplitChain(certPEM)
		if err != nil {
			return nil, "", fmt.Errorf("证书解析失败: %w", err)
		}
		files = []File{
			{Name: "cert.crt", Data: []byte(leaf)},
			{Name: "privkey.key", Data: []byte(privKeyPEM)},
		}
	case "iis", "tomcat":
		password, err := RandomPassword(16)
		if err != nil {
			return nil, "", fmt.Errorf("生成密码失败: %w", err)
		}
		pfx, err := deploy.BuildPFX(certPEM, privKeyPEM, password)
		if err != nil {
			return nil, "", fmt.Errorf("合成 PFX 失败: %w", err)
		}
		files = []File{
			{Name: "cert.pfx", Data: pfx},
			{Name: "password.txt", Data: []byte(password)},
		}
	default:
		return nil, "", fmt.Errorf("不支持的格式 %q", format)
	}
	data, err := BuildZip(files)
	if err != nil {
		return nil, "", fmt.Errorf("打包失败: %w", err)
	}
	return data, format + ".zip", nil
}

// FormatSupported 格式是否支持
func FormatSupported(format string) bool {
	for _, f := range SupportedFormats {
		if f == format {
			return true
		}
	}
	return false
}

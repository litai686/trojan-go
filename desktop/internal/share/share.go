// Package share encrypts profile configurations for copy/paste between clients.
package share

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

// Prefix identifies a desktop configuration token.
const Prefix = "TGD1:"

const (
	payloadVersion = 1
	maxTokenSize   = 32 * 1024
)

var key = sha256.Sum256([]byte("trojan-go-desktop:profile-share:v1"))

type payload struct {
	Version int                 `json:"v"`
	Name    string              `json:"name"`
	Config  store.ProfileConfig `json:"config"`
}

// Encode encrypts a profile name and config into a single-line token.
func Encode(name string, cfg store.ProfileConfig) (string, error) {
	body, err := json.Marshal(payload{
		Version: payloadVersion,
		Name:    name,
		Config:  cfg,
	})
	if err != nil {
		return "", fmt.Errorf("序列化配置失败: %w", err)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("初始化加密失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化加密失败: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, body, []byte(Prefix))
	return Prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decode decrypts a token produced by Encode.
func Decode(token string) (string, store.ProfileConfig, error) {
	compact := compactToken(token)
	if compact == "" {
		return "", store.ProfileConfig{}, errors.New("配置信息为空")
	}
	if len(compact) > maxTokenSize {
		return "", store.ProfileConfig{}, errors.New("配置信息过长")
	}
	if !strings.HasPrefix(compact, Prefix) {
		return "", store.ProfileConfig{}, errors.New("配置信息格式不正确")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(compact, Prefix))
	if err != nil {
		return "", store.ProfileConfig{}, errors.New("配置信息格式不正确")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", store.ProfileConfig{}, errors.New("解密配置信息失败")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", store.ProfileConfig{}, errors.New("解密配置信息失败")
	}
	if len(raw) <= gcm.NonceSize() {
		return "", store.ProfileConfig{}, errors.New("配置信息已损坏")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(Prefix))
	if err != nil {
		return "", store.ProfileConfig{}, errors.New("配置信息已损坏或不是本客户端生成的密文")
	}
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil {
		return "", store.ProfileConfig{}, errors.New("配置信息内容无效")
	}
	if p.Version != payloadVersion {
		return "", store.ProfileConfig{}, errors.New("配置信息版本不受支持")
	}
	p.Name = strings.TrimSpace(p.Name)
	if utf8.RuneCountInString(p.Name) > 64 {
		p.Name = string([]rune(p.Name)[:64])
	}
	return p.Name, p.Config, nil
}

func compactToken(token string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(token))
}

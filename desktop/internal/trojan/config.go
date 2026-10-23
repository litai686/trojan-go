package trojan

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

// DefaultProfileConfig returns the defaults used for a new profile.
func DefaultProfileConfig() store.ProfileConfig {
	return store.ProfileConfig{
		ServerPort: 443,
		LocalAddr:  "127.0.0.1",
		LocalPort:  1080,
		Mux:        false,
		VerifyTLS:  false,
		LogLevel:   1,
	}
}

// NormalizeProfileConfig trims fields and fills safe defaults.
func NormalizeProfileConfig(c store.ProfileConfig) store.ProfileConfig {
	c.ServerAddr = strings.TrimSpace(c.ServerAddr)
	c.Password = strings.TrimSpace(c.Password)
	c.SNI = strings.TrimSpace(c.SNI)
	c.LocalAddr = strings.TrimSpace(c.LocalAddr)

	if c.ServerPort == 0 {
		c.ServerPort = 443
	}
	if c.LocalAddr == "" {
		c.LocalAddr = "127.0.0.1"
	}
	if c.LocalPort == 0 {
		c.LocalPort = 1080
	}
	if c.SNI == "" {
		c.SNI = c.ServerAddr
	}
	return c
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateProfile checks one profile and enforces unique names.
func ValidateProfile(p *store.Profile, existing []store.Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New("配置名称不能为空")
	}
	if utf8.RuneCountInString(p.Name) > 64 {
		return errors.New("配置名称不能超过 64 个字符")
	}
	for _, other := range existing {
		if other.ID != p.ID && strings.EqualFold(strings.TrimSpace(other.Name), p.Name) {
			return errors.New("配置名称已存在")
		}
	}

	p.Config = NormalizeProfileConfig(p.Config)
	c := p.Config

	if c.ServerAddr == "" {
		return errors.New("服务器地址不能为空")
	}
	if strings.Contains(c.ServerAddr, "://") || strings.ContainsAny(c.ServerAddr, "/ ") {
		return errors.New("服务器地址只填写域名或 IP，不要包含协议或路径")
	}
	host := strings.Trim(c.ServerAddr, "[]")
	if net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
		return errors.New("服务器地址格式不正确")
	}
	if c.ServerPort < 1 || c.ServerPort > 65535 {
		return errors.New("服务器端口必须在 1-65535 之间")
	}
	if c.Password == "" {
		return errors.New("密码不能为空")
	}
	if c.LocalPort < 1 || c.LocalPort > 65535 {
		return errors.New("本地端口必须在 1-65535 之间")
	}
	if strings.ContainsAny(c.LocalAddr, " /") {
		return errors.New("本地监听地址格式不正确")
	}
	if c.LogLevel < 0 || c.LogLevel > 5 {
		return errors.New("日志级别必须在 0-5 之间")
	}
	return nil
}

type wireConfig struct {
	RunType    string     `json:"run_type"`
	LocalAddr  string     `json:"local_addr"`
	LocalPort  int        `json:"local_port"`
	RemoteAddr string     `json:"remote_addr"`
	RemotePort int        `json:"remote_port"`
	Password   []string   `json:"password"`
	LogLevel   int        `json:"log_level"`
	SSL        wireSSL    `json:"ssl"`
	Mux        wireToggle `json:"mux"`
	Router     wireToggle `json:"router"`
}

type wireSSL struct {
	Verify         bool     `json:"verify"`
	VerifyHostname bool     `json:"verify_hostname"`
	SNI            string   `json:"sni"`
	ALPN           []string `json:"alpn"`
}

type wireToggle struct {
	Enabled bool `json:"enabled"`
}

// RenderConfig converts a profile config to the JSON consumed by trojan-go.
func RenderConfig(c store.ProfileConfig) ([]byte, error) {
	c = NormalizeProfileConfig(c)
	cfg := wireConfig{
		RunType:    "client",
		LocalAddr:  c.LocalAddr,
		LocalPort:  c.LocalPort,
		RemoteAddr: c.ServerAddr,
		RemotePort: c.ServerPort,
		Password:   []string{c.Password},
		LogLevel:   c.LogLevel,
		SSL: wireSSL{
			Verify:         c.VerifyTLS,
			VerifyHostname: c.VerifyTLS,
			SNI:            c.SNI,
			ALPN:           []string{"http/1.1"},
		},
		Mux:    wireToggle{Enabled: c.Mux},
		Router: wireToggle{Enabled: c.Router},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// WriteRuntimeConfig writes the active config to <dataDir>/runtime/active.json.
func WriteRuntimeConfig(dataDir string, c store.ProfileConfig) (string, error) {
	dir := filepath.Join(dataDir, "runtime")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建运行时目录失败: %w", err)
	}
	data, err := RenderConfig(c)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "active.json")

	if old, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", old, 0o600)
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

var (
	ansiPattern  = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	levelPattern = regexp.MustCompile(`^\s*\[(TRACE|DEBUG|INFO|WARN|ERROR|FATAL)\]\s*(?:\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}\s*)?(.*)$`)
)

// ParseLogLine extracts the level and message from a trojan-go log line.
func ParseLogLine(raw string) (level, text string) {
	raw = strings.TrimSpace(strings.TrimSuffix(ansiPattern.ReplaceAllString(raw, ""), "\r"))
	if m := levelPattern.FindStringSubmatch(raw); m != nil {
		return m[1], strings.TrimSpace(m[2])
	}
	return "INFO", raw
}

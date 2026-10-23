package trojan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

func validProfile() store.Profile {
	return store.Profile{
		ID:   "p1",
		Name: "测试",
		Config: store.ProfileConfig{
			ServerAddr: "example.com",
			ServerPort: 443,
			Password:   "secret",
			LocalAddr:  "127.0.0.1",
			LocalPort:  1080,
			VerifyTLS:  true,
			LogLevel:   1,
		},
	}
}

func TestDefaultProfileConfig(t *testing.T) {
	c := DefaultProfileConfig()
	if c.Mux || c.VerifyTLS {
		t.Fatalf("expected mux and tls verification off by default: %+v", c)
	}
}

func TestValidateProfile(t *testing.T) {
	p := validProfile()
	if err := ValidateProfile(&p, nil); err != nil {
		t.Fatal(err)
	}
	if p.Config.SNI != "example.com" {
		t.Fatalf("SNI not defaulted: %q", p.Config.SNI)
	}

	cases := []struct {
		name   string
		mutate func(*store.Profile)
	}{
		{"empty name", func(p *store.Profile) { p.Name = "" }},
		{"bad server", func(p *store.Profile) { p.Config.ServerAddr = "https://example.com/x" }},
		{"bad port", func(p *store.Profile) { p.Config.ServerPort = 70000 }},
		{"empty password", func(p *store.Profile) { p.Config.Password = "" }},
		{"bad local port", func(p *store.Profile) { p.Config.LocalPort = 70000 }},
		{"bad log level", func(p *store.Profile) { p.Config.LogLevel = 9 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfile()
			tc.mutate(&p)
			if err := ValidateProfile(&p, nil); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateProfileUniqueName(t *testing.T) {
	p := validProfile()
	other := validProfile()
	other.ID = "p2"
	other.Name = strings.ToUpper(p.Name)
	if err := ValidateProfile(&p, []store.Profile{other}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	other.ID = p.ID
	if err := ValidateProfile(&p, []store.Profile{other}); err != nil {
		t.Fatalf("same id should not conflict: %v", err)
	}
}

func TestRenderConfig(t *testing.T) {
	p := validProfile()
	p.Config.SNI = ""
	data, err := RenderConfig(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["run_type"] != "client" || got["remote_addr"] != "example.com" {
		t.Fatalf("unexpected config: %#v", got)
	}
	passwords, ok := got["password"].([]any)
	if !ok || len(passwords) != 1 || passwords[0] != "secret" {
		t.Fatalf("unexpected password: %#v", got["password"])
	}
	ssl := got["ssl"].(map[string]any)
	if ssl["sni"] != "example.com" || ssl["verify"] != true {
		t.Fatalf("unexpected ssl: %#v", ssl)
	}
	if _, ok := got["websocket"]; ok {
		t.Fatal("websocket config should be omitted")
	}
}

func TestWriteRuntimeConfigCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	cfg := validProfile().Config
	path, err := WriteRuntimeConfig(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	cfg.LocalPort = 2080
	if _, err := WriteRuntimeConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("expected backup file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime", "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"local_port": 2080`) {
		t.Fatalf("new config not written: %s", data)
	}
}

func TestParseLogLine(t *testing.T) {
	cases := []struct {
		raw   string
		level string
		text  string
	}{
		{"[INFO]  2026/10/23 11:22:33 hello", "INFO", "hello"},
		{"[WARN]  something", "WARN", "something"},
		{"\x1b[32m[ERROR] 2026/10/23 11:22:33 boom\x1b[0m", "ERROR", "boom"},
		{"plain text", "INFO", "plain text"},
	}
	for _, tc := range cases {
		level, text := ParseLogLine(tc.raw)
		if level != tc.level || text != tc.text {
			t.Fatalf("ParseLogLine(%q) = %q, %q; want %q, %q", tc.raw, level, text, tc.level, tc.text)
		}
	}
}

func TestResolveBinaryEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trojan-go")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TROJAN_GO_BINARY", path)
	got, err := ResolveBinary(store.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

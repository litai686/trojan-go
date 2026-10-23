package share

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

func sampleConfig() store.ProfileConfig {
	return store.ProfileConfig{
		ServerAddr: "example.com",
		ServerPort: 443,
		Password:   "s3cret-密码",
		SNI:        "example.com",
		LocalAddr:  "127.0.0.1",
		LocalPort:  1080,
		Mux:        true,
		Router:     false,
		VerifyTLS:  true,
		LogLevel:   1,
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	token, err := Encode("香港节点", sampleConfig())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasPrefix(token, Prefix) {
		t.Fatalf("token missing prefix: %s", token)
	}
	if strings.Contains(token, "example.com") || strings.Contains(token, "s3cret") {
		t.Fatal("token leaks plaintext configuration")
	}
	name, cfg, err := Decode(token)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if name != "香港节点" {
		t.Fatalf("name = %q", name)
	}
	if cfg != sampleConfig() {
		t.Fatalf("config mismatch: %+v", cfg)
	}
}

func TestDecodeAcceptsWhitespace(t *testing.T) {
	token, err := Encode("节点", sampleConfig())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	wrapped := " \n" + token[:10] + "\r\n\t" + token[10:] + " \n"
	if _, _, err := Decode(wrapped); err != nil {
		t.Fatalf("Decode with whitespace: %v", err)
	}
}

func TestDecodeRejectsTampering(t *testing.T) {
	token, err := Encode("节点", sampleConfig())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tampered := token
	if raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, Prefix)); err == nil {
		raw[len(raw)-1] ^= 0xff
		tampered = Prefix + base64.RawURLEncoding.EncodeToString(raw)
	}
	if _, _, err := Decode(tampered); err == nil {
		t.Fatal("expected tampered token to fail")
	}
}

func TestDecodeRejectsInvalidInput(t *testing.T) {
	cases := []string{
		"",
		"not-a-token",
		"TGD1:",
		"TGD1:not-base64!",
		strings.Repeat("x", maxTokenSize+1),
	}
	for _, input := range cases {
		if _, _, err := Decode(input); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

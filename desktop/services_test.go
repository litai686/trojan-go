package main

import (
	"strings"
	"testing"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

func testConfig() store.ProfileConfig {
	return store.ProfileConfig{
		ServerAddr: "example.com",
		ServerPort: 443,
		Password:   "s3cret",
		SNI:        "example.com",
		LocalAddr:  "127.0.0.1",
		LocalPort:  1080,
		Mux:        true,
		VerifyTLS:  true,
		LogLevel:   1,
	}
}

func TestProfileExportDecodeRoundTrip(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	profile := store.Profile{
		ID:     "p1",
		Name:   "香港节点",
		Config: testConfig(),
	}
	if err := st.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	svc := &ProfileService{store: st}

	token, err := svc.ExportConfig(profile.ID)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if strings.Contains(token, "s3cret") || strings.Contains(token, "example.com") {
		t.Fatal("exported token contains plaintext configuration")
	}
	if err := st.DeleteProfile(profile.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	draft, err := svc.DecodeConfig(token)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if draft.Name != profile.Name {
		t.Fatalf("name = %q, want %q", draft.Name, profile.Name)
	}
	if draft.Config.Password != profile.Config.Password {
		t.Fatalf("password = %q", draft.Config.Password)
	}
	if draft.ID != "" {
		t.Fatalf("draft ID = %q, want empty", draft.ID)
	}
}

func TestDecodeConfigMakesNameUnique(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	profile := store.Profile{ID: "p1", Name: "香港节点", Config: testConfig()}
	if err := st.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	if err := st.SaveProfile(store.Profile{ID: "p2", Name: "香港节点 导入", Config: testConfig()}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	svc := &ProfileService{store: st}
	token, err := svc.ExportConfig(profile.ID)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	draft, err := svc.DecodeConfig(token)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if draft.Name != "香港节点 导入 2" {
		t.Fatalf("name = %q, want %q", draft.Name, "香港节点 导入 2")
	}
}

func TestDecodeConfigRejectsInvalidToken(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	svc := &ProfileService{store: st}
	if _, err := svc.DecodeConfig("not-a-token"); err == nil {
		t.Fatal("expected invalid token error")
	}
}

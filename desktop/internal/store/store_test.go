package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStorePersistsProfilesAndSettings(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	profile := Profile{
		ID:   "p1",
		Name: "测试节点",
		Config: ProfileConfig{
			ServerAddr: "example.com",
			ServerPort: 443,
			Password:   "secret",
			LocalAddr:  "127.0.0.1",
			LocalPort:  1080,
			VerifyTLS:  true,
			LogLevel:   1,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found after reload")
	}
	if got.Name != profile.Name || got.Config.ServerAddr != profile.Config.ServerAddr {
		t.Fatalf("unexpected profile: %#v", got)
	}

	settings := DefaultSettings()
	settings.MaxLogLines = 5
	settings.Theme = "neon"
	saved, err := reopened.SaveSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MaxLogLines != 2000 {
		t.Fatalf("MaxLogLines not normalized: %d", saved.MaxLogLines)
	}
	if saved.Theme != "system" {
		t.Fatalf("Theme not normalized: %s", saved.Theme)
	}
	if !reflect.DeepEqual(reopened.GetSettings(), saved) {
		t.Fatal("settings mismatch after save")
	}

	if err := reopened.DeleteProfile("p1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.GetProfile("p1"); ok {
		t.Fatal("profile still present after delete")
	}
}

func TestStoreRecoversFromCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, profilesFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Warnings()) == 0 {
		t.Fatal("expected a warning for the corrupt file")
	}
	if len(s.ListProfiles()) != 0 {
		t.Fatal("expected an empty profile list")
	}
	matches, err := filepath.Glob(filepath.Join(dir, profilesFileName+".bad-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("expected corrupt file backup, matches=%v err=%v", matches, err)
	}
}

func TestNormalizeSettings(t *testing.T) {
	s := NormalizeSettings(Settings{})
	if s.MaxLogLines != 2000 || s.Theme != "system" || s.UpdateChannel != "stable" || s.UpdateRepository == "" {
		t.Fatalf("unexpected defaults: %#v", s)
	}
	s = NormalizeSettings(Settings{MaxLogLines: 99999, Theme: "dark", UpdateChannel: "prerelease", UpdateRepository: "a/b"})
	if s.MaxLogLines != 2000 || s.Theme != "dark" || s.UpdateChannel != "prerelease" || s.UpdateRepository != "a/b" {
		t.Fatalf("unexpected normalization: %#v", s)
	}
}

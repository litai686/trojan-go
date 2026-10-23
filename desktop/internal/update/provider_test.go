package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

func TestCompareAndNormalizeVersion(t *testing.T) {
	if compareVersions("0.2.0", "0.1.0") <= 0 {
		t.Fatal("0.2.0 should be newer than 0.1.0")
	}
	if compareVersions("desktop-v0.2.0", "0.2.0") != 0 {
		t.Fatal("prefix normalization failed")
	}
	if normalizeVersion("desktop-v1.2.3") != "1.2.3" {
		t.Fatal("normalizeVersion failed")
	}
	if compareVersions("", "0.1.0") >= 0 {
		t.Fatal("empty version should be treated as older")
	}
}

func TestParseChecksum(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	body := "ignore me\n" + digest + "  trojan-go-desktop-windows-amd64.exe\n"
	got, err := parseChecksum(body, "trojan-go-desktop-windows-amd64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("unexpected digest length: %d", len(got))
	}
	if _, err := parseChecksum("zzzz  file.exe\n", "file.exe"); err == nil {
		t.Fatal("expected invalid hex error")
	}
}

func TestPickAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "trojan-go-desktop-windows-amd64-installer.exe"},
		{Name: "trojan-go-desktop-windows-amd64.exe.sha256"},
		{Name: "SHA256SUMS-desktop.txt"},
		{Name: "trojan-go-desktop-windows-amd64.exe"},
	}
	if idx := pickAsset(assets, "windows", "amd64"); idx != 3 {
		t.Fatalf("expected portable exe at index 3, got %d", idx)
	}
	if idx := pickAsset([]releaseAsset{{Name: "trojan-go-desktop-windows-amd64-installer.exe"}}, "windows", "amd64"); idx != -1 {
		t.Fatalf("installer must not be used for atomic updates, got %d", idx)
	}
	mac := []releaseAsset{
		{Name: "trojan-go-desktop-darwin-arm64.zip"},
		{Name: "trojan-go-desktop-darwin-arm64.dmg"},
	}
	if idx := pickAsset(mac, "darwin", "arm64"); idx != 0 {
		t.Fatalf("expected zip at index 0, got %d", idx)
	}
	linux := []releaseAsset{
		{Name: "trojan-go-desktop-linux-amd64.tar.gz"},
		{Name: "trojan-go-desktop-linux-amd64.AppImage"},
	}
	if idx := pickAsset(linux, "linux", "amd64"); idx != 1 {
		t.Fatalf("expected AppImage at index 1, got %d", idx)
	}
	unified := []releaseAsset{
		{Name: "trojan-go-darwin-arm64.zip"},
		{Name: "trojan-go-desktop-darwin-arm64.zip"},
	}
	if idx := pickAsset(unified, "darwin", "arm64"); idx != 1 {
		t.Fatalf("expected desktop zip at index 1, got %d", idx)
	}
	if idx := pickAsset([]releaseAsset{{Name: "trojan-go-linux-amd64.zip"}}, "linux", "amd64"); idx != -1 {
		t.Fatalf("trojan-go archives must not be used for updates, got %d", idx)
	}
}

func TestResolveProxy(t *testing.T) {
	cases := []struct {
		name     string
		resolver func() string
		want     string
	}{
		{name: "disabled"},
		{name: "empty", resolver: func() string { return "" }},
		{name: "socks5", resolver: func() string { return " socks5://127.0.0.1:1080 " }, want: "socks5://127.0.0.1:1080"},
		{name: "http", resolver: func() string { return "http://127.0.0.1:8080" }, want: "http://127.0.0.1:8080"},
		{name: "invalid", resolver: func() string { return "://bad" }},
		{name: "unsupported", resolver: func() string { return "ftp://127.0.0.1:21" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProvider(Config{ProxyURL: tc.resolver})
			got := p.resolveProxy()
			if tc.want == "" {
				if got != nil {
					t.Fatalf("expected a direct connection, got %v", got)
				}
				return
			}
			if got == nil || got.String() != tc.want {
				t.Fatalf("resolveProxy() = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestAPIRequestUsesProxy(t *testing.T) {
	hits := 0
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("via proxy"))
	}))
	defer proxySrv.Close()

	p := NewProvider(Config{ProxyURL: func() string { return proxySrv.URL }})
	req, err := http.NewRequest(http.MethodGet, "http://updates.invalid/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.doAPI(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()
	if hits != 1 {
		t.Fatalf("expected one proxy request, got %d", hits)
	}
}

func TestAPIRequestFallsBackToDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := NewProvider(Config{
		Repository: "acme/app",
		BaseURL:    srv.URL,
		ProxyURL:   func() string { return "socks5://127.0.0.1:1" },
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.doAPI(req)
	if err != nil {
		t.Fatalf("expected a direct fallback, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestProviderCheck(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/releases":
			base := "http://" + r.Host
			releases := []map[string]any{
				{"tag_name": "v0.10.0", "draft": false, "prerelease": false, "assets": []any{}},
				{
					"tag_name":   "desktop-v0.2.0",
					"name":       "Desktop 0.2.0",
					"body":       "notes",
					"draft":      false,
					"prerelease": false,
					"html_url":   "https://example.test/release",
					"assets": []map[string]any{
						{"name": "trojan-go-desktop-windows-amd64-installer.exe", "browser_download_url": base + "/installer.exe", "size": 1},
						{"name": "trojan-go-desktop-windows-amd64.exe", "browser_download_url": base + "/app.exe", "size": 123},
						{"name": "SHA256SUMS-desktop.txt", "browser_download_url": base + "/checksums", "size": 100},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(releases)
		case "/checksums":
			_, _ = w.Write([]byte(digest + "  trojan-go-desktop-windows-amd64.exe\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewProvider(Config{
		Repository: "acme/app",
		TagPrefix:  "desktop-v",
		BaseURL:    srv.URL,
	})
	rel, err := p.Check(context.Background(), updater.CheckRequest{
		CurrentVersion: "0.1.0",
		Platform:       "windows",
		Arch:           "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil {
		t.Fatal("expected an update")
	}
	if rel.Version != "0.2.0" || rel.Artifact.Filename != "trojan-go-desktop-windows-amd64.exe" {
		t.Fatalf("unexpected release: %#v", rel)
	}
	if rel.Verification == nil || len(rel.Verification.Digest) != 32 {
		t.Fatalf("missing verification: %#v", rel.Verification)
	}
	if got, _ := rel.Metadata["github.asset.url"].(string); got != srv.URL+"/app.exe" {
		t.Fatalf("unexpected asset url: %q", got)
	}

	rel, err = p.Check(context.Background(), updater.CheckRequest{
		CurrentVersion: "0.2.0",
		Platform:       "windows",
		Arch:           "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel != nil {
		t.Fatal("expected no update for the same version")
	}
}

func TestProviderRejectsMissingChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		releases := []map[string]any{
			{
				"tag_name":   "desktop-v0.2.0",
				"draft":      false,
				"prerelease": false,
				"assets": []map[string]any{
					{"name": "trojan-go-desktop-windows-amd64.exe", "browser_download_url": base + "/app.exe", "size": 123},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	p := NewProvider(Config{Repository: "acme/app", TagPrefix: "desktop-v", BaseURL: srv.URL})
	_, err := p.Check(context.Background(), updater.CheckRequest{
		CurrentVersion: "0.1.0",
		Platform:       "windows",
		Arch:           "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "缺少校验文件") {
		t.Fatalf("expected missing checksum error, got %v", err)
	}
}

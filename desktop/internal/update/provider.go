// Package update implements a Wails updater provider backed by GitHub
// Releases. Unlike the built-in provider it filters releases by tag prefix so
// desktop releases can live next to trojan-go releases in the same repository.
package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"golang.org/x/mod/semver"
)

const (
	apiBase = "https://api.github.com"
	// ChecksumAsset is the release asset name holding sha256sum-style lines.
	ChecksumAsset = "SHA256SUMS-desktop.txt"
	// assetPrefix scopes updates to desktop artifacts: the same release also
	// carries trojan-go server/client archives with similar names.
	assetPrefix = "trojan-go-desktop-"
)

// Config configures the provider.
type Config struct {
	Repository string
	TagPrefix  string
	Prerelease bool
	// BaseURL overrides the GitHub API root (useful for tests and GitHub
	// Enterprise).
	BaseURL string
	// ProxyURL returns a proxy URL (for example socks5://127.0.0.1:1080)
	// when update traffic should use the running trojan-go client, or an
	// empty string to connect directly.
	ProxyURL func() string
}

type releaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	ContentType        string `json:"content_type"`
}

type apiRelease struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	PublishedAt time.Time      `json:"published_at"`
	HTMLURL     string         `json:"html_url"`
	Assets      []releaseAsset `json:"assets"`
}

// Provider implements updater.Provider.
type Provider struct {
	mu     sync.RWMutex
	cfg    Config
	base   string
	direct *http.Transport
}

const (
	apiTimeout            = 30 * time.Second
	responseHeaderTimeout = 30 * time.Second
)

// NewProvider creates a provider. The tag prefix defaults to "desktop-v".
func NewProvider(cfg Config) *Provider {
	if cfg.TagPrefix == "" {
		cfg.TagPrefix = "desktop-v"
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = apiBase
	}
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	return &Provider{
		cfg:    cfg,
		base:   base,
		direct: direct,
	}
}

// resolveProxy returns the proxy to use for a request, or nil for a direct
// connection. Invalid or unknown proxy URLs are treated as direct too.
func (p *Provider) resolveProxy() *url.URL {
	p.mu.RLock()
	resolver := p.cfg.ProxyURL
	p.mu.RUnlock()
	if resolver == nil {
		return nil
	}
	raw := strings.TrimSpace(resolver())
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5":
		return u
	default:
		return nil
	}
}

func (p *Provider) client(proxy *url.URL, timeout time.Duration) *http.Client {
	transport := p.direct.Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// doAPI performs a small request, retrying without a proxy when the proxy
// connection failed before a response was received.
func (p *Provider) doAPI(req *http.Request) (*http.Response, error) {
	proxy := p.resolveProxy()
	resp, err := p.client(proxy, apiTimeout).Do(req)
	if err == nil || proxy == nil {
		return resp, err
	}
	return p.client(nil, apiTimeout).Do(req.Clone(req.Context()))
}

// Name implements updater.Provider.
func (p *Provider) Name() string { return "github-desktop" }

// UpdateConfig allows the settings page to change the repository/channel at
// runtime without re-initialising the updater.
func (p *Provider) UpdateConfig(repo string, prerelease bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if repo != "" {
		p.cfg.Repository = repo
	}
	p.cfg.Prerelease = prerelease
}

// Check implements updater.Provider.
func (p *Provider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()

	repo := strings.TrimSpace(cfg.Repository)
	if !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("更新仓库配置无效: %q", repo)
	}

	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=50", p.base, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := p.doAPI(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("GitHub API 访问受限或限流，请稍后重试")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var releases []apiRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}

	var picked *apiRelease
	var pickedVersion string
	for i := range releases {
		rel := &releases[i]
		if rel.Draft || (rel.Prerelease && !cfg.Prerelease) {
			continue
		}
		if !strings.HasPrefix(rel.TagName, cfg.TagPrefix) {
			continue
		}
		version := strings.TrimPrefix(rel.TagName, cfg.TagPrefix)
		if !semver.IsValid("v" + strings.TrimPrefix(version, "v")) {
			continue
		}
		if picked == nil || compareVersions(version, pickedVersion) > 0 {
			picked = rel
			pickedVersion = version
		}
	}
	if picked == nil {
		return nil, nil
	}

	current := normalizeVersion(req.CurrentVersion)
	if current != "" && semver.IsValid("v"+current) && compareVersions(pickedVersion, current) <= 0 {
		return nil, nil
	}

	idx := pickAsset(picked.Assets, req.Platform, req.Arch)
	if idx < 0 {
		return nil, fmt.Errorf("版本 %s 没有适用于 %s/%s 的更新包", pickedVersion, req.Platform, req.Arch)
	}
	asset := picked.Assets[idx]

	digest, err := p.loadChecksum(ctx, picked.Assets, asset.Name)
	if err != nil {
		return nil, err
	}
	if digest == nil {
		return nil, fmt.Errorf("版本 %s 缺少校验文件 %s，已拒绝不安全的更新", pickedVersion, ChecksumAsset)
	}

	return &updater.Release{
		Version:     normalizeVersion(pickedVersion),
		Name:        picked.Name,
		Notes:       picked.Body,
		PublishedAt: picked.PublishedAt,
		Artifact: updater.Artifact{
			Filename: asset.Name,
			Filetype: fileType(asset.Name),
			Size:     asset.Size,
			Platform: req.Platform,
			Arch:     req.Arch,
		},
		Verification: &updater.Verification{
			DigestAlgo: "sha256",
			Digest:     digest,
		},
		Metadata: map[string]any{
			"github.asset.url":       asset.BrowserDownloadURL,
			"github.release.htmlURL": picked.HTMLURL,
			"github.release.tag":     picked.TagName,
		},
	}, nil
}

// Download implements updater.Provider.
func (p *Provider) Download(ctx context.Context, rel *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	if rel == nil || rel.Metadata == nil {
		return errors.New("更新信息缺少下载地址")
	}
	url, _ := rel.Metadata["github.asset.url"].(string)
	if url == "" {
		return errors.New("更新信息缺少下载地址")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	proxy := p.resolveProxy()
	resp, err := p.client(proxy, 0).Do(req)
	if err != nil && proxy != nil {
		resp, err = p.client(nil, 0).Do(req.Clone(req.Context()))
	}
	if err != nil {
		return fmt.Errorf("下载更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("下载更新失败: HTTP %d", resp.StatusCode)
	}

	total := rel.Artifact.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(written, total)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (p *Provider) loadChecksum(ctx context.Context, assets []releaseAsset, target string) ([]byte, error) {
	var url string
	for _, a := range assets {
		if strings.EqualFold(a.Name, ChecksumAsset) {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := p.doAPI(req)
	if err != nil {
		return nil, fmt.Errorf("下载校验文件失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载校验文件失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseChecksum(string(body), target)
}

func parseChecksum(body, target string) ([]byte, error) {
	target = filepath.Base(target)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) != target {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != 32 {
			return nil, fmt.Errorf("校验文件中的 SHA256 无效: %s", fields[0])
		}
		return digest, nil
	}
	return nil, nil
}

func pickAsset(assets []releaseAsset, goos, goarch string) int {
	best := -1
	bestScore := -1
	for i, a := range assets {
		name := strings.ToLower(a.Name)
		if isSidecar(name) {
			continue
		}
		if !strings.HasPrefix(name, assetPrefix) {
			continue
		}
		// The updater swaps a single executable/bundle; installers and OS
		// packages cannot be applied by the helper.
		if strings.Contains(name, "installer") ||
			strings.HasSuffix(name, ".msi") ||
			strings.HasSuffix(name, ".dmg") ||
			strings.HasSuffix(name, ".pkg") ||
			strings.HasSuffix(name, ".deb") ||
			strings.HasSuffix(name, ".rpm") {
			continue
		}
		if !matchesPlatform(name, goos) || !matchesArch(name, goarch) {
			continue
		}
		score := 10
		switch strings.ToLower(goos) {
		case "windows":
			if strings.HasSuffix(name, ".exe") {
				score += 20
			}
		case "darwin":
			if strings.HasSuffix(name, ".zip") {
				score += 20
			}
		default:
			if strings.HasSuffix(name, ".appimage") {
				score += 20
			} else if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") {
				score += 10
			}
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	return best
}

func isSidecar(name string) bool {
	for _, suffix := range []string{".sig", ".asc", ".sha256", ".sha512", ".sha256sum", ".sha512sum", ".txt"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.Contains(name, "checksum") || strings.Contains(name, "sha256sums")
}

func matchesPlatform(name, goos string) bool {
	switch goos {
	case "windows":
		return strings.Contains(name, "windows")
	case "darwin":
		return strings.Contains(name, "darwin") || strings.Contains(name, "macos") || strings.Contains(name, "osx")
	case "linux":
		return strings.Contains(name, "linux")
	default:
		return strings.Contains(name, goos)
	}
}

func matchesArch(name, goarch string) bool {
	switch goarch {
	case "amd64":
		return strings.Contains(name, "amd64") || strings.Contains(name, "x86_64") || strings.Contains(name, "x64")
	case "arm64":
		return strings.Contains(name, "arm64") || strings.Contains(name, "aarch64")
	case "386":
		if strings.Contains(name, "amd64") || strings.Contains(name, "x86_64") || strings.Contains(name, "x64") {
			return false
		}
		return strings.Contains(name, "386") || strings.Contains(name, "i386") || strings.Contains(name, "x86")
	default:
		return strings.Contains(name, goarch)
	}
}

func compareVersions(a, b string) int {
	a = normalizeVersion(a)
	b = normalizeVersion(b)
	if a == "" || !semver.IsValid("v"+a) {
		return -1
	}
	if b == "" || !semver.IsValid("v"+b) {
		return 1
	}
	return semver.Compare("v"+a, "v"+b)
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "desktop-")
	v = strings.TrimPrefix(v, "v")
	return v
}

func fileType(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i+1:])
	}
	return ""
}

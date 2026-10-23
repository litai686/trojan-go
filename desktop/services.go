package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/litai686/trojan-go/desktop/internal/share"
	"github.com/litai686/trojan-go/desktop/internal/store"
	"github.com/litai686/trojan-go/desktop/internal/trojan"
	"github.com/litai686/trojan-go/desktop/internal/update"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ProfileService exposes profile CRUD to the frontend.
type ProfileService struct {
	store   *store.Store
	manager *trojan.Manager
	app     *application.App
}

// List returns all profiles, newest first.
func (s *ProfileService) List() ([]store.Profile, error) {
	out := s.store.ListProfiles()
	if out == nil {
		out = []store.Profile{}
	}
	return out, nil
}

// Get returns one profile.
func (s *ProfileService) Get(id string) (store.Profile, error) {
	p, ok := s.store.GetProfile(id)
	if !ok {
		return store.Profile{}, fmt.Errorf("配置不存在: %s", id)
	}
	return p, nil
}

// Save creates or updates a profile.
func (s *ProfileService) Save(input store.Profile) (store.Profile, error) {
	now := time.Now()
	existing, found := s.store.GetProfile(input.ID)

	if input.ID == "" {
		input.ID = newID()
		input.CreatedAt = now
	} else {
		if !found {
			return store.Profile{}, fmt.Errorf("配置不存在: %s", input.ID)
		}
		input.CreatedAt = existing.CreatedAt
	}
	input.UpdatedAt = now

	if err := trojan.ValidateProfile(&input, s.store.ListProfiles()); err != nil {
		return store.Profile{}, err
	}
	if err := s.store.SaveProfile(input); err != nil {
		return store.Profile{}, err
	}
	emit(s.app, "profiles:changed", nil)
	return input, nil
}

// Delete removes a profile, stopping it first when it is active.
func (s *ProfileService) Delete(id string) error {
	if s.manager.IsActive(id) {
		if _, err := s.manager.Stop(); err != nil {
			return fmt.Errorf("停止当前代理失败: %w", err)
		}
	}
	if err := s.store.DeleteProfile(id); err != nil {
		return err
	}
	settings := s.store.GetSettings()
	if settings.LastProfileID == id {
		settings.LastProfileID = ""
		_, _ = s.store.SaveSettings(settings)
		emit(s.app, "settings:changed", settings)
	}
	emit(s.app, "profiles:changed", nil)
	return nil
}

// ExportConfig returns an encrypted token for copy/paste.
func (s *ProfileService) ExportConfig(id string) (string, error) {
	p, ok := s.store.GetProfile(id)
	if !ok {
		return "", fmt.Errorf("配置不存在: %s", id)
	}
	return share.Encode(p.Name, p.Config)
}

// DecodeConfig decrypts a token into an unsaved profile draft.
func (s *ProfileService) DecodeConfig(token string) (store.Profile, error) {
	name, cfg, err := share.Decode(token)
	if err != nil {
		return store.Profile{}, err
	}
	return store.Profile{
		Name:   uniqueImportName(name, s.store.ListProfiles()),
		Config: trojan.NormalizeProfileConfig(cfg),
	}, nil
}

// Duplicate copies a profile under a new name.
func (s *ProfileService) Duplicate(id string) (store.Profile, error) {
	src, ok := s.store.GetProfile(id)
	if !ok {
		return store.Profile{}, fmt.Errorf("配置不存在: %s", id)
	}
	profiles := s.store.ListProfiles()
	name := uniqueCopyName(src.Name, profiles)
	now := time.Now()
	dup := store.Profile{
		ID:        newID(),
		Name:      name,
		Config:    src.Config,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := trojan.ValidateProfile(&dup, profiles); err != nil {
		return store.Profile{}, err
	}
	if err := s.store.SaveProfile(dup); err != nil {
		return store.Profile{}, err
	}
	emit(s.app, "profiles:changed", nil)
	return dup, nil
}

// RuntimeService controls the trojan-go child process.
type RuntimeService struct {
	store   *store.Store
	manager *trojan.Manager
	hub     *trojan.LogHub
	app     *application.App
}

// GetState returns the current runtime state.
func (s *RuntimeService) GetState() store.RuntimeState {
	return s.manager.State()
}

// MeasureLatency measures the TCP connect time to the active server.
func (s *RuntimeService) MeasureLatency() (int64, error) {
	state := s.manager.State()
	if state.Status != "running" || state.ServerAddr == "" {
		return 0, fmt.Errorf("代理未运行")
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", state.ServerAddr, 3*time.Second)
	if err != nil {
		return 0, fmt.Errorf("连接服务器失败: %w", err)
	}
	_ = conn.Close()
	return time.Since(start).Milliseconds(), nil
}

// Activate starts the selected profile, stopping any previous process.
func (s *RuntimeService) Activate(profileID string) (store.RuntimeState, error) {
	p, ok := s.store.GetProfile(profileID)
	if !ok {
		return s.manager.State(), fmt.Errorf("配置不存在: %s", profileID)
	}
	state, err := s.manager.Start(p)
	if err != nil {
		return state, err
	}
	settings := s.store.GetSettings()
	if settings.LastProfileID != profileID {
		settings.LastProfileID = profileID
		if _, err := s.store.SaveSettings(settings); err == nil {
			emit(s.app, "settings:changed", settings)
		}
	}
	return state, nil
}

// Stop stops the current process.
func (s *RuntimeService) Stop() (store.RuntimeState, error) {
	return s.manager.Stop()
}

// Restart restarts the current profile.
func (s *RuntimeService) Restart() (store.RuntimeState, error) {
	return s.manager.Restart()
}

// GetLogs returns buffered log lines.
func (s *RuntimeService) GetLogs(limit int) []store.LogEntry {
	out := s.hub.Snapshot(limit)
	if out == nil {
		out = []store.LogEntry{}
	}
	return out
}

// ClearLogs clears the log buffer.
func (s *RuntimeService) ClearLogs() error {
	s.hub.Clear()
	return nil
}

// ServiceStartup optionally activates the last profile.
func (s *RuntimeService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	go func() {
		time.Sleep(500 * time.Millisecond)
		settings := s.store.GetSettings()
		if !settings.AutoStart || settings.LastProfileID == "" {
			return
		}
		if s.manager.State().Status != "stopped" {
			return
		}
		p, ok := s.store.GetProfile(settings.LastProfileID)
		if !ok {
			return
		}
		if _, err := s.manager.Start(p); err != nil {
			s.hub.AddSystem("自动激活失败: " + err.Error())
		}
	}()
	return nil
}

// ServiceShutdown stops the child process and the log hub.
func (s *RuntimeService) ServiceShutdown() error {
	s.manager.Shutdown()
	s.hub.Stop()
	return nil
}

// SettingsService exposes global settings.
type SettingsService struct {
	store    *store.Store
	hub      *trojan.LogHub
	provider *update.Provider
	app      *application.App
}

// Get returns the current settings.
func (s *SettingsService) Get() (store.Settings, error) {
	return s.store.GetSettings(), nil
}

// SetTheme persists the theme selection immediately, without touching other settings.
func (s *SettingsService) SetTheme(theme string) (store.Settings, error) {
	settings := s.store.GetSettings()
	settings.Theme = theme
	saved, err := s.store.SaveSettings(settings)
	if err != nil {
		return s.store.GetSettings(), err
	}
	emit(s.app, "settings:changed", saved)
	return saved, nil
}

// Save validates and persists settings.
func (s *SettingsService) Save(input store.Settings) (store.Settings, error) {
	current := s.store.GetSettings()
	if input.LastProfileID == "" {
		input.LastProfileID = current.LastProfileID
	}
	input = store.NormalizeSettings(input)

	if input.BinaryPath != "" {
		info, err := os.Stat(input.BinaryPath)
		if err != nil || info.IsDir() {
			return current, fmt.Errorf("指定的 trojan-go 路径不存在: %s", input.BinaryPath)
		}
	}
	if !strings.Contains(input.UpdateRepository, "/") {
		return current, fmt.Errorf("更新仓库格式应为 owner/repo")
	}

	saved, err := s.store.SaveSettings(input)
	if err != nil {
		return current, err
	}
	s.hub.SetMax(saved.MaxLogLines)
	if s.provider != nil {
		s.provider.UpdateConfig(saved.UpdateRepository, saved.UpdateChannel == "prerelease")
	}
	emit(s.app, "settings:changed", saved)
	return saved, nil
}

// UpdateService exposes version information and the Wails updater.
type UpdateService struct {
	app        *application.App
	store      *store.Store
	manager    *trojan.Manager
	provider   *update.Provider
	dataDir    string
	installDir string
}

// VersionInfo returns build and runtime information.
func (s *UpdateService) VersionInfo() (store.VersionInfo, error) {
	return s.versionInfo(), nil
}

func (s *UpdateService) versionInfo() store.VersionInfo {
	bin, _ := trojan.ResolveBinary(s.store.GetSettings())
	return store.VersionInfo{
		AppVersion:    version,
		TrojanVersion: trojan.BinaryVersion(bin),
		Commit:        commit,
		BuildTime:     buildTime,
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		WailsVersion:  moduleVersion("github.com/wailsapp/wails/v3"),
		DataDir:       s.dataDir,
		InstallDir:    s.installDir,
		BinaryPath:    bin,
	}
}

// Check looks for a newer desktop release.
func (s *UpdateService) Check(ctx context.Context) (*store.UpdateInfo, error) {
	if s.app.Updater == nil {
		return nil, fmt.Errorf("更新模块未初始化")
	}
	rel, err := s.app.Updater.Check(ctx)
	if err != nil {
		return nil, err
	}
	current := s.app.Updater.CurrentVersion()
	info := &store.UpdateInfo{CurrentVersion: current}
	if rel == nil {
		return info, nil
	}
	info.Available = true
	info.LatestVersion = rel.Version
	info.Name = rel.Name
	info.Notes = rel.Notes
	info.PublishedAt = rel.PublishedAt.Format(time.RFC3339)
	info.AssetName = rel.Artifact.Filename
	info.AssetSize = rel.Artifact.Size
	if rel.Metadata != nil {
		info.HTMLURL, _ = rel.Metadata["github.release.htmlURL"].(string)
	}
	return info, nil
}

// Install downloads, verifies and stages the pending update.
func (s *UpdateService) Install(ctx context.Context) error {
	if s.app.Updater == nil {
		return fmt.Errorf("更新模块未初始化")
	}
	if err := s.app.Updater.DownloadAndInstall(ctx); err != nil {
		return err
	}
	return nil
}

// Restart applies the staged update and relaunches the application.
func (s *UpdateService) Restart(ctx context.Context) error {
	if s.app.Updater == nil {
		return fmt.Errorf("更新模块未初始化")
	}
	return s.app.Updater.Restart(ctx)
}

// OpenReleasePage opens the GitHub releases page.
func (s *UpdateService) OpenReleasePage() error {
	repo := s.store.GetSettings().UpdateRepository
	if repo == "" {
		repo = "litai686/trojan-go"
	}
	return s.app.Browser.OpenURL("https://github.com/" + repo + "/releases")
}

// SystemService exposes filesystem and browser helpers.
type SystemService struct {
	app        *application.App
	store      *store.Store
	manager    *trojan.Manager
	hub        *trojan.LogHub
	dataDir    string
	installDir string
}

// GetInfo returns the same version block shown on the about page.
func (s *SystemService) GetInfo() (store.VersionInfo, error) {
	bin, _ := trojan.ResolveBinary(s.store.GetSettings())
	return store.VersionInfo{
		AppVersion:    version,
		TrojanVersion: trojan.BinaryVersion(bin),
		Commit:        commit,
		BuildTime:     buildTime,
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		WailsVersion:  moduleVersion("github.com/wailsapp/wails/v3"),
		DataDir:       s.dataDir,
		InstallDir:    s.installDir,
		BinaryPath:    bin,
	}, nil
}

// OpenDataDir opens the per-user data directory.
func (s *SystemService) OpenDataDir() error {
	return openPath(s.dataDir)
}

// OpenInstallDir opens the directory containing the application.
func (s *SystemService) OpenInstallDir() error {
	return openPath(s.installDir)
}

// OpenURL opens an https URL in the system browser.
func (s *SystemService) OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("只允许打开 https 链接")
	}
	return s.app.Browser.OpenURL(url)
}

// FrontendReady records which frontend transport was initialised. It is used
// by the UI bootstrap and is handy when diagnosing runtime import failures.
func (s *SystemService) FrontendReady(mode string) {
	if mode != "wails" {
		mode = "mock"
	}
	if s.hub != nil {
		s.hub.AddSystem("前端已连接 (" + mode + ")")
	}
	if s.app != nil && s.app.Logger != nil {
		s.app.Logger.Info("frontend ready", "mode", mode)
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func uniqueCopyName(base string, profiles []store.Profile) string {
	return uniqueProfileName(base, "副本", profiles)
}

func uniqueImportName(base string, profiles []store.Profile) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "配置"
	}
	base = truncateRunes(base, 64)
	if !profileNameExists(base, profiles) {
		return base
	}
	return uniqueProfileName(base, "导入", profiles)
}

func uniqueProfileName(base, suffix string, profiles []store.Profile) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "配置"
	}
	candidate := truncateRunes(base+" "+suffix, 64)
	if !profileNameExists(candidate, profiles) {
		return candidate
	}
	for i := 2; i < 1000; i++ {
		candidate = truncateRunes(fmt.Sprintf("%s %s %d", base, suffix, i), 64)
		if !profileNameExists(candidate, profiles) {
			return candidate
		}
	}
	return truncateRunes(base+" "+suffix+" "+newID()[:6], 64)
}

func profileNameExists(name string, profiles []store.Profile) bool {
	for _, p := range profiles {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

func emit(app *application.App, name string, data any) {
	if app == nil {
		return
	}
	app.Event.Emit(name, data)
}

func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	return "unknown"
}

func openPath(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("路径不存在: %s", path)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", filepath.Clean(path))
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

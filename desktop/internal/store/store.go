package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	fileVersion      = 1
	profilesFileName = "profiles.json"
	settingsFileName = "settings.json"
)

type profilesFile struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type settingsFile struct {
	Version  int      `json:"version"`
	Settings Settings `json:"settings"`
}

// Store owns the on-disk profiles and settings. All exported methods are safe
// for concurrent use.
type Store struct {
	mu       sync.RWMutex
	dir      string
	profiles []Profile
	settings Settings
	warnings []string
}

// Open loads (or initialises) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	s := &Store{
		dir:      dir,
		settings: DefaultSettings(),
	}

	var pf profilesFile
	if err := loadJSON(filepath.Join(dir, profilesFileName), &pf); err != nil {
		s.warnings = append(s.warnings, err.Error())
	}
	if pf.Profiles != nil {
		s.profiles = pf.Profiles
	}

	var sf settingsFile
	if err := loadJSON(filepath.Join(dir, settingsFileName), &sf); err != nil {
		s.warnings = append(s.warnings, err.Error())
	} else if sf.Version != 0 || sf.Settings != (Settings{}) {
		s.settings = NormalizeSettings(sf.Settings)
	}

	return s, nil
}

// Warnings returns non-fatal problems encountered while loading.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warnings...)
}

// DataDir returns the directory backing this store.
func (s *Store) DataDir() string {
	return s.dir
}

// ListProfiles returns profiles sorted by last update (newest first).
func (s *Store) ListProfiles() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Profile(nil), s.profiles...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// GetProfile returns one profile by id.
func (s *Store) GetProfile(id string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// SaveProfile inserts or replaces a profile.
func (s *Store) SaveProfile(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := append([]Profile(nil), s.profiles...)
	replaced := false
	for i := range next {
		if next[i].ID == p.ID {
			next[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, p)
	}

	if err := writeJSON(filepath.Join(s.dir, profilesFileName), profilesFile{
		Version:  fileVersion,
		Profiles: next,
	}, 0o600); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	s.profiles = next
	return nil
}

// DeleteProfile removes a profile by id.
func (s *Store) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make([]Profile, 0, len(s.profiles))
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return errors.New("配置不存在")
	}

	if err := writeJSON(filepath.Join(s.dir, profilesFileName), profilesFile{
		Version:  fileVersion,
		Profiles: next,
	}, 0o600); err != nil {
		return fmt.Errorf("删除配置失败: %w", err)
	}
	s.profiles = next
	return nil
}

// GetSettings returns a copy of the current settings.
func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SaveSettings validates and persists settings.
func (s *Store) SaveSettings(v Settings) (Settings, error) {
	v = NormalizeSettings(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSON(filepath.Join(s.dir, settingsFileName), settingsFile{
		Version:  fileVersion,
		Settings: v,
	}, 0o600); err != nil {
		return s.settings, fmt.Errorf("保存设置失败: %w", err)
	}
	s.settings = v
	return v, nil
}

// DefaultSettings returns first-run settings.
func DefaultSettings() Settings {
	return Settings{
		AutoStart:        false,
		AutoCheckUpdate:  true,
		MaxLogLines:      2000,
		Theme:            "system",
		UpdateChannel:    "stable",
		UpdateRepository: "litai686/trojan-go",
	}
}

// NormalizeSettings clamps and defaults settings loaded from disk or the UI.
func NormalizeSettings(s Settings) Settings {
	if s.MaxLogLines < 100 || s.MaxLogLines > 20000 {
		s.MaxLogLines = 2000
	}
	switch s.Theme {
	case "system", "light", "dark":
	default:
		s.Theme = "system"
	}
	switch s.UpdateChannel {
	case "stable", "prerelease":
	default:
		s.UpdateChannel = "stable"
	}
	if s.UpdateRepository == "" {
		s.UpdateRepository = "litai686/trojan-go"
	}
	return s
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		backup := path + ".bad-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, backup)
		return fmt.Errorf("%s 已损坏，已备份为 %s: %w", filepath.Base(path), filepath.Base(backup), err)
	}
	return nil
}

func writeJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

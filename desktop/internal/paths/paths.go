package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "trojan-go-desktop"

// DataDir returns the per-user directory used for profiles, settings and
// runtime configuration.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// InstallDir returns the directory containing the running desktop binary.
func InstallDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// BinaryName returns the trojan-go executable name for the current platform.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "trojan-go.exe"
	}
	return "trojan-go"
}

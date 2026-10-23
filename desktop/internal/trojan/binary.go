package trojan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/litai686/trojan-go/desktop/internal/paths"
	"github.com/litai686/trojan-go/desktop/internal/store"
)

// ResolveBinary locates the trojan-go executable.
//
// Search order: TROJAN_GO_BINARY, settings override, install dir, development
// build dirs, then PATH.
func ResolveBinary(settings store.Settings) (string, error) {
	name := paths.BinaryName()
	var tried []string

	check := func(candidate string) (string, bool) {
		if candidate == "" {
			return "", false
		}
		candidate = os.ExpandEnv(candidate)
		if !filepath.IsAbs(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				candidate = abs
			}
		}
		tried = append(tried, candidate)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			return "", false
		}
		return candidate, true
	}

	if bin, ok := check(os.Getenv("TROJAN_GO_BINARY")); ok {
		return bin, nil
	}
	if bin, ok := check(settings.BinaryPath); ok {
		return bin, nil
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if bin, ok := check(filepath.Join(exeDir, name)); ok {
			return bin, nil
		}
		if bin, ok := check(filepath.Join(exeDir, "..", "build", runtime.GOOS+"-"+runtime.GOARCH, name)); ok {
			return bin, nil
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 5; i++ {
			if bin, ok := check(filepath.Join(dir, "build", runtime.GOOS+"-"+runtime.GOARCH, name)); ok {
				return bin, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if bin, err := exec.LookPath("trojan-go"); err == nil {
		if resolved, ok := check(bin); ok {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("未找到 trojan-go 二进制。请先构建 %s，或在设置中指定路径。已尝试: %s",
		name, strings.Join(tried, "; "))
}

// BinaryVersion asks the binary for its version. It returns "未知" when the
// binary cannot be executed or does not support -version.
func BinaryVersion(bin string) string {
	if bin == "" {
		return "未知"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := newCommandContext(ctx, bin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "未知"
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Trojan-Go") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Trojan-Go"))
		}
	}
	return "未知"
}

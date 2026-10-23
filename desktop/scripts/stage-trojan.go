// Command stage-trojan copies the platform trojan-go binary from the
// repository build directory next to the desktop build outputs.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	goos := flag.String("goos", runtime.GOOS, "target operating system")
	goarch := flag.String("goarch", runtime.GOARCH, "target architecture")
	flag.Parse()

	root, err := desktopRoot()
	if err != nil {
		fatal(err)
	}
	ext := ""
	if *goos == "windows" {
		ext = ".exe"
	}
	name := "trojan-go" + ext
	source := filepath.Join(filepath.Dir(root), "build", *goos+"-"+*goarch, name)
	if _, err := os.Stat(source); err != nil {
		fatal(fmt.Errorf("未找到 %s，请先在仓库根目录构建 trojan-go (%s/%s): %w", source, *goos, *goarch, err))
	}

	destinations := []string{filepath.Join(root, "bin", name)}
	switch *goos {
	case "windows":
		destinations = append(destinations, filepath.Join(root, "build", "windows", name))
	case "darwin":
		destinations = append(destinations, filepath.Join(root, "build", "darwin", name))
	case "linux":
		destinations = append(destinations, filepath.Join(root, "build", "linux", name))
	}

	for _, dst := range destinations {
		if err := copyFile(source, dst); err != nil {
			fatal(err)
		}
		rel, _ := filepath.Rel(root, dst)
		fmt.Printf("staged %s -> %s\n", name, rel)
	}
}

func desktopRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "frontend")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("无法定位 desktop 目录（当前目录 %s）", dir)
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := destination + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "stage-trojan:", err)
	os.Exit(1)
}

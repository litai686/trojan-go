// Command sync-version writes the release version from the Taskfile into the
// Wails build assets so installer, bundle and binary metadata stay in sync.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

type replacement struct {
	pattern *regexp.Regexp
	value   string
}

type target struct {
	path  string
	edits []replacement
}

func main() {
	version := flag.String("version", "", "release version, for example 0.1.0")
	root := flag.String("root", ".", "desktop project directory")
	flag.Parse()

	v := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	if !versionPattern.MatchString(v) {
		fatal(fmt.Errorf("无效的版本号 %q：请把 desktop/Taskfile.yml 中的 VERSION 写成 x.y.z", *version))
	}

	targets := []target{
		{
			path: filepath.Join("build", "config.yml"),
			edits: []replacement{
				{regexp.MustCompile(`(?m)^(  version: ")[^"]*(")$`), v},
			},
		},
		{
			path: filepath.Join("build", "windows", "info.json"),
			edits: []replacement{
				{regexp.MustCompile(`("file_version":\s*")[^"]*(")`), v},
				{regexp.MustCompile(`("ProductVersion":\s*")[^"]*(")`), v},
			},
		},
		{
			path:  filepath.Join("build", "darwin", "Info.plist"),
			edits: plistEdits(v),
		},
		{
			path:  filepath.Join("build", "darwin", "Info.dev.plist"),
			edits: plistEdits(v),
		},
		{
			path: filepath.Join("build", "windows", "nsis", "wails_tools.nsh"),
			edits: []replacement{
				{regexp.MustCompile(`(?m)(!define INFO_PRODUCTVERSION\s+")[^"]*(")`), v},
			},
		},
	}

	for _, t := range targets {
		if err := sync(*root, t); err != nil {
			fatal(err)
		}
	}
}

func plistEdits(version string) []replacement {
	return []replacement{
		{regexp.MustCompile(`(<key>CFBundleVersion</key>\s*<string>)[^<]*(</string>)`), version},
		{regexp.MustCompile(`(<key>CFBundleShortVersionString</key>\s*<string>)[^<]*(</string>)`), version},
	}
}

func sync(root string, t target) error {
	path := filepath.Join(root, t.path)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := string(data)
	for _, e := range t.edits {
		if !e.pattern.MatchString(updated) {
			return fmt.Errorf("%s: 未找到需要更新的版本号", t.path)
		}
		updated = e.pattern.ReplaceAllString(updated, "${1}"+e.value+"${2}")
	}
	if updated == string(data) {
		fmt.Printf("unchanged %s\n", t.path)
		return nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return err
	}
	fmt.Printf("synced %s -> %s\n", t.path, t.edits[0].value)
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sync-version:", err)
	os.Exit(1)
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncConfigVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		eol  string
	}{
		{name: "lf", eol: "\n"},
		{name: "crlf", eol: "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
				t.Fatal(err)
			}

			target := targetsFor("0.0.3")[0]
			path := filepath.Join(root, target.path)
			content := "version: '3'" + tc.eol + "info:" + tc.eol + "  version: \"0.0.1\"" + tc.eol
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := sync(root, target); err != nil {
				t.Fatalf("sync: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "version: '3'" + tc.eol + "info:" + tc.eol + "  version: \"0.0.3\"" + tc.eol
			if string(got) != want {
				t.Fatalf("got %q, want %q", got, want)
			}

			if err := sync(root, target); err != nil {
				t.Fatalf("second sync: %v", err)
			}
		})
	}
}

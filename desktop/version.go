package main

// These values are replaced at build time with -ldflags "-X main.version=..."
// The release version lives in desktop/Taskfile.yml (vars.VERSION).
var (
	version   string
	commit    = "none"
	buildTime = "unknown"
)

package main

// These values are replaced at build time with -ldflags "-X main.version=..."
var (
	version   = "0.0.1"
	commit    = "none"
	buildTime = "unknown"
)

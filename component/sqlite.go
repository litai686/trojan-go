//go:build sqlite || full || mini
// +build sqlite full mini

package build

import (
	_ "github.com/p4gefau1t/trojan-go/statistic/sqlite"
)

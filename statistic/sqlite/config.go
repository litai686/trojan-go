package sqlite

import (
	"github.com/p4gefau1t/trojan-go/config"
)

type SQLiteConfig struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	Database  string `json:"database" yaml:"database"`
	CheckRate int    `json:"check_rate" yaml:"check-rate"`
}

type Config struct {
	SQLite SQLiteConfig `json:"sqlite" yaml:"sqlite"`
}

func init() {
	config.RegisterConfigCreator(Name, func() interface{} {
		return &Config{
			SQLite: SQLiteConfig{
				Database:  "trojan-go.db",
				CheckRate: 30,
			},
		}
	})
}

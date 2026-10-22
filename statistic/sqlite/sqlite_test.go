package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
)

func TestSQLiteAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = config.WithConfig(ctx, Name, &Config{
		SQLite: SQLiteConfig{
			Database:  filepath.Join(t.TempDir(), "trojan-go.db"),
			CheckRate: 3600,
		},
	})
	ctx = config.WithConfig(ctx, memory.Name, &memory.Config{})
	auth, err := NewAuthenticator(ctx)
	common.Must(err)
	defer auth.Close()
	a := auth.(*Authenticator)

	hash := common.SHA224String("password")
	_, err = a.db.Exec("INSERT INTO `users` (`username`, `password`, `quota`) VALUES (?, ?, ?)", "test", hash, 100)
	common.Must(err)
	common.Must(a.reloadUsers())

	valid, user := a.AuthUser(hash)
	if !valid {
		t.Fatal("user in database is not loaded")
	}

	user.AddTraffic(10, 20)
	a.flushTraffic()
	var upload, download int64
	common.Must(a.db.QueryRow("SELECT `upload`, `download` FROM `users` WHERE `password` = ?", hash).Scan(&upload, &download))
	if upload != 20 || download != 10 {
		t.Fatalf("unexpected traffic, upload: %d, download: %d", upload, download)
	}

	_, err = a.db.Exec("UPDATE `users` SET `download` = 100 WHERE `password` = ?", hash)
	common.Must(err)
	common.Must(a.reloadUsers())
	if valid, _ := a.AuthUser(hash); valid {
		t.Fatal("user exceeding quota should be removed")
	}
}

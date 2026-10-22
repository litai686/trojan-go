package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	// SQLite Driver, pure Go implementation without cgo
	_ "modernc.org/sqlite"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/statistic"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
)

const Name = "SQLITE"

const (
	createUsersTable = `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		password TEXT NOT NULL,
		quota INTEGER NOT NULL DEFAULT 0,
		download INTEGER NOT NULL DEFAULT 0,
		upload INTEGER NOT NULL DEFAULT 0
	);`
	createPasswordIndex = `CREATE INDEX IF NOT EXISTS users_password ON users (password);`
)

type Authenticator struct {
	*memory.Authenticator
	db             *sql.DB
	updateDuration time.Duration
	ctx            context.Context
}

func (a *Authenticator) Close() error {
	return a.db.Close()
}

// flushTraffic writes the buffered traffic of each user into the database
func (a *Authenticator) flushTraffic() {
	for _, user := range a.ListUsers() {
		// swap upload and download for users
		hash := user.Hash()
		sent, recv := user.ResetTraffic()

		result, err := a.db.Exec("UPDATE `users` SET `upload`=`upload`+?, `download`=`download`+? WHERE `password`=?;", recv, sent, hash)
		if err != nil {
			log.Error(common.NewError("failed to update data to user table").Base(err))
			continue
		}
		if num, err := result.RowsAffected(); err == nil && num == 0 {
			a.DelUser(hash)
		}
	}
	log.Info("buffered data has been written into the database")
}

// reloadUsers refreshes the in-memory user list with the data in the database
func (a *Authenticator) reloadUsers() error {
	rows, err := a.db.Query("SELECT password,quota,download,upload FROM users")
	if err != nil {
		return common.NewError("failed to pull data from the database").Base(err)
	}
	defer rows.Close()

	for rows.Next() {
		var hash string
		var quota, download, upload int64
		if err := rows.Scan(&hash, &quota, &download, &upload); err != nil {
			return common.NewError("failed to obtain data from the query result").Base(err)
		}
		if download+upload < quota || quota < 0 {
			a.AddUser(hash)
		} else {
			a.DelUser(hash)
		}
	}
	if err := rows.Err(); err != nil {
		return common.NewError("failed to obtain data from the query result").Base(err)
	}
	return nil
}

func (a *Authenticator) updater() {
	for {
		a.flushTraffic()
		if err := a.reloadUsers(); err != nil {
			log.Error(err)
		}

		select {
		case <-time.After(a.updateDuration):
		case <-a.ctx.Done():
			log.Debug("SQLite daemon exiting...")
			return
		}
	}
}

func connectDatabase(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, common.NewError("failed to create database directory").Base(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, common.NewError("failed to open database").Base(err)
	}
	// SQLite allows only one writer, serializing the accesses avoids lock contention
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, common.NewError("failed to set busy timeout").Base(err)
	}
	if _, err := db.Exec(createUsersTable); err != nil {
		db.Close()
		return nil, common.NewError("failed to create users table").Base(err)
	}
	if _, err := db.Exec(createPasswordIndex); err != nil {
		db.Close()
		return nil, common.NewError("failed to create users password index").Base(err)
	}
	return db, nil
}

func NewAuthenticator(ctx context.Context) (statistic.Authenticator, error) {
	cfg := config.FromContext(ctx, Name).(*Config)
	dbPath := cfg.SQLite.Database
	if dbPath == "" {
		dbPath = "trojan-go.db"
	}
	db, err := connectDatabase(dbPath)
	if err != nil {
		return nil, common.NewError("failed to connect to database").Base(err)
	}
	memoryAuth, err := memory.NewAuthenticator(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	checkRate := cfg.SQLite.CheckRate
	if checkRate <= 0 {
		checkRate = 30
	}
	a := &Authenticator{
		db:             db,
		ctx:            ctx,
		updateDuration: time.Duration(checkRate) * time.Second,
		Authenticator:  memoryAuth.(*memory.Authenticator),
	}
	go a.updater()
	log.Debug("sqlite authenticator created")
	return a, nil
}

func init() {
	statistic.RegisterAuthenticatorCreator(Name, NewAuthenticator)
}

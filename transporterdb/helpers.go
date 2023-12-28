package transporterdb

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/lib/pq" // this comment here because of linter: a blank import should be only in a main or test package, or have a comment justifying it (golint)
)

// TODO: open source relayer utils and reuse code from there.

type config struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	DBName   string `toml:"dbname"`
	SSLMode  string `toml:"sslmode"`
}

func OpenPostgres(configPath string) (*sql.DB, error) {
	var cfg config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return nil, err
	}
	psInfo := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.DBName,
		cfg.SSLMode,
	)

	db, err := sql.Open("postgres", psInfo)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func OpenPostgresWithRetries(configPath string) *sql.DB {
	interval := time.Second * 5
	for {
		db, err := OpenPostgres(configPath)
		if err == nil {
			err := db.Ping()
			if err == nil {
				return db
			}
			log.Printf("Failed to ping Postgres: %v\n", err)
		} else {
			log.Printf("Failed to open postgres: %v\n", err)
		}
		time.Sleep(interval)
	}
}

// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Version is set at build time via -ldflags "-X .../config.Version=<sha>".
var Version = "dev"

// Config holds runtime settings. Add new fields here; read them only in Load.
type Config struct {
	Env       string // dev | test | staging | prod
	Port      int
	Version   string
	DataDir   string // SQLite and other local state; file is DataDir/arc.db
	UploadDir string // image files written by internal/upload
}

// Load reads configuration from the environment using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:       valueOr(getenv("APP_ENV"), "dev"),
		Port:      8080,
		Version:   Version,
		DataDir:   valueOr(getenv("DATA_DIR"), "./data"),
		UploadDir: valueOr(getenv("UPLOAD_DIR"), "./uploads"),
	}
	if p := getenv("PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return Config{}, fmt.Errorf("invalid PORT %q", p)
		}
		cfg.Port = n
	}
	switch cfg.Env {
	case "dev", "test", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("invalid APP_ENV %q", cfg.Env)
	}
	return cfg, nil
}

// FromOS is Load(os.Getenv).
func FromOS() (Config, error) { return Load(os.Getenv) }

// DBPath is the SQLite file $DATA_DIR/arc.db.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "arc.db")
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

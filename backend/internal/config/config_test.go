package config

import (
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		want    Config
	}{
		{name: "defaults", env: nil, want: Config{Env: "dev", Port: 8080, Version: "dev", DataDir: "./data"}},
		{name: "custom", env: map[string]string{"APP_ENV": "prod", "PORT": "9000", "DATA_DIR": "/data"}, want: Config{Env: "prod", Port: 9000, Version: "dev", DataDir: "/data"}},
		{name: "bad port", env: map[string]string{"PORT": "abc"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"PORT": "70000"}, wantErr: true},
		{name: "bad env", env: map[string]string{"APP_ENV": "qa"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(env(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDBPath(t *testing.T) {
	cfg := Config{DataDir: "./data"}
	if got, want := cfg.DBPath(), filepath.Join("./data", "arc.db"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	cfg.DataDir = "/data"
	if got, want := cfg.DBPath(), filepath.Join("/data", "arc.db"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

package config

import (
	"path/filepath"
	"reflect"
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
		{name: "defaults", env: nil, want: Config{Env: "dev", Port: 8080, Version: "dev", DataDir: "./data", UploadDir: "./uploads", SessionCookie: DefaultSessionCookie}},
		{
			name: "custom",
			env: map[string]string{
				"APP_ENV": "prod", "PORT": "9000", "DATA_DIR": "/data", "UPLOAD_DIR": "/uploads",
				"APP_BASE_URL": "https://Arc.Example/app", "SESSION_COOKIE": "my_sid",
				"ADMIN_EMAILS": " Ada@Example.com, , bob@example.com ",
			},
			want: Config{
				Env: "prod", Port: 9000, Version: "dev", DataDir: "/data", UploadDir: "/uploads",
				AppBaseURL: "https://Arc.Example/app", SessionCookie: "my_sid",
				AdminEmails: []string{"Ada@Example.com", "bob@example.com"},
			},
		},
		{name: "bad port", env: map[string]string{"PORT": "abc"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"PORT": "70000"}, wantErr: true},
		{name: "bad env", env: map[string]string{"APP_ENV": "qa"}, wantErr: true},
		{name: "bad base url", env: map[string]string{"APP_BASE_URL": "not a url"}, wantErr: true},
		{name: "bad base scheme", env: map[string]string{"APP_BASE_URL": "ftp://arc.example"}, wantErr: true},
		{name: "bad cookie", env: map[string]string{"SESSION_COOKIE": "arc sid"}, wantErr: true},
		{name: "cookie with semicolon", env: map[string]string{"SESSION_COOKIE": "a;b"}, wantErr: true},
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
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsAdmin(t *testing.T) {
	cfg := Config{AdminEmails: []string{"Ada@Example.com", "bob@example.com"}}
	if !cfg.IsAdmin("ada@example.com") || !cfg.IsAdmin("  BOB@example.com ") || !cfg.IsAdmin("Ada@Example.com") {
		t.Fatal("expected admin")
	}
	if cfg.IsAdmin("cara@example.com") || cfg.IsAdmin("") || cfg.IsAdmin("  ") {
		t.Fatal("expected non-admin")
	}
	if (Config{}).IsAdmin("ada@example.com") {
		t.Fatal("empty list")
	}
}

func TestOrigin(t *testing.T) {
	got, err := Origin(" HTTPS://Arc.Example:8443/app ")
	if err != nil || got != "https://arc.example:8443" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := Origin(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := Origin("http://"); err == nil {
		t.Fatal("missing host")
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

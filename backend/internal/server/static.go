package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/rhamdeew/dbprc/backend/internal/config"
)

// serveDevStatic serves the built frontend when STATIC_DIR is set.
// An existing file is returned as-is. Any other GET or HEAD path that is
// not an API route falls back to index.html, so the client router can load.
// GET /uploads/ serves UPLOAD_DIR in this mode only: there is no web
// container in dev or E2E. Staging and prod leave STATIC_DIR empty, and
// config.Load rejects it there. An empty STATIC_DIR returns next unchanged.
func serveDevStatic(next http.Handler, cfg config.Config) http.Handler {
	dir := strings.TrimSpace(cfg.StaticDir)
	if dir == "" {
		return next
	}
	root := http.Dir(dir)
	spa := &spaFiles{root: root, files: http.FileServer(root)}
	var uploads http.Handler
	if strings.TrimSpace(cfg.UploadDir) != "" {
		uploadRoot := http.Dir(cfg.UploadDir)
		uploads = &uploadFiles{
			root:  uploadRoot,
			files: http.StripPrefix("/uploads/", http.FileServer(uploadRoot)),
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if appPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// Same browser policy as deploy/env/Caddyfile, so dev and E2E run under it.
		// HSTS stays on Caddy: this server is plain HTTP.
		setSecurityHeaders(w, r)
		if uploads != nil && uploadPath(r.URL.Path) {
			w.Header().Set("Content-Disposition", "inline")
			uploads.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

func setSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	csp := "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'"
	if host != "" && !strings.ContainsAny(host, " ;\t\"'\\") {
		csp += " wss://" + host
	}
	csp += "; frame-ancestors 'none'"
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
}

func appPath(p string) bool {
	return p == "/healthz" || p == "/ws" || p == "/api" || strings.HasPrefix(p, "/api/") ||
		p == "/auth" || strings.HasPrefix(p, "/auth/")
}

func uploadPath(p string) bool {
	return p == "/uploads" || strings.HasPrefix(p, "/uploads/")
}

type spaFiles struct {
	root  http.Dir
	files http.Handler
}

func (s *spaFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	if clean != "/" && regularFile(s.root, clean) {
		s.files.ServeHTTP(w, r)
		return
	}
	// A missing asset must not come back as index.html. The browser would
	// try to execute the HTML as a script.
	if clean != "/" && path.Ext(clean) != "" {
		http.NotFound(w, r)
		return
	}
	s.serveIndex(w, r)
}

func (s *spaFiles) serveIndex(w http.ResponseWriter, r *http.Request) {
	// FileServer redirects any URL ending in /index.html to ./, which loops
	// when the fallback itself asks for that file. ServeContent does not.
	f, err := s.root.Open("/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), f)
}

type uploadFiles struct {
	root  http.Dir
	files http.Handler
}

func (h *uploadFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/uploads")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." || !regularFile(h.root, rel) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	h.files.ServeHTTP(w, r)
}

func regularFile(root http.Dir, name string) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

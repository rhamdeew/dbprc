// Package upload stores avatar, photo, and drawing images on disk.
//
// Handlers limit the request with http.MaxBytesReader (MaxBytes). Save also
// stops at MaxBytes so a caller that forgot the handler limit cannot buffer
// more than that. Bytes are sniffed, checked for decompression-bomb dimensions,
// oriented, and re-encoded so EXIF (including GPS) is not kept.
package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxBytes is the most Save will read from an upload.
	// HTTP handlers should wrap the body with http.MaxBytesReader(w, r.Body, upload.MaxBytes).
	MaxBytes = 10 << 20

	// MaxEdge is the largest width or height accepted before the bitmap is decoded.
	MaxEdge = 8000

	dirPerm  = 0o755
	filePerm = 0o644
)

// Kind selects how an image is resized and encoded.
type Kind string

const (
	Avatar  Kind = "avatar"
	Photo   Kind = "photo"
	Drawing Kind = "drawing"
)

var (
	ErrInvalidImage    = errors.New("invalid image")
	ErrTooLarge        = errors.New("image exceeds 10MB")
	ErrDimensions      = errors.New("image dimensions exceed 8000")
	ErrUnsupportedType = errors.New("unsupported image type")
	ErrNotPNG          = errors.New("drawing must be png")
	ErrInvalidKind     = errors.New("invalid image kind")
	ErrInvalidPath     = errors.New("invalid upload path")
	ErrEmpty           = errors.New("empty image")
)

// Store writes images under Dir, which is config.UploadDir.
type Store struct {
	Dir string
}

// New returns a store rooted at dir ($UPLOAD_DIR).
func New(dir string) *Store {
	return &Store{Dir: dir}
}

// Save reads one image, re-encodes it for kind, and stores it at
// <kind>/<yyyy>/<32 hex>.<ext> relative to Dir.
// The file is written to a temporary name in the same directory and renamed
// into place so readers never see a partial file.
func (s *Store) Save(ctx context.Context, r io.Reader, kind Kind) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || s.Dir == "" {
		return "", errors.New("upload directory is not configured")
	}
	if !validKind(kind) {
		return "", ErrInvalidKind
	}

	data, err := readLimited(r)
	if err != nil {
		return "", err
	}
	encoded, ext, err := prepare(data, kind)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	name, err := randomName(ext)
	if err != nil {
		return "", err
	}
	year := strconv.Itoa(time.Now().UTC().Year())
	dir := filepath.Join(s.Dir, string(kind), year)
	if err := writeAtomic(dir, name, encoded); err != nil {
		return "", err
	}
	return path.Join(string(kind), year, name), nil
}

// Delete removes a file previously returned by Save.
// relPath must stay inside Dir; absolute paths and ".." are rejected.
func (s *Store) Delete(relPath string) error {
	if s == nil || s.Dir == "" {
		return errors.New("upload directory is not configured")
	}
	full, err := s.resolve(relPath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return fmt.Errorf("delete upload: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrInvalidPath
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("delete upload: %w", err)
	}
	return nil
}

// URL is the public path the web server serves for a stored relative path.
func URL(relPath string) string {
	cleaned := path.Clean("/" + filepath.ToSlash(relPath))
	return "/uploads" + cleaned
}

func validKind(kind Kind) bool {
	switch kind {
	case Avatar, Photo, Drawing:
		return true
	default:
		return false
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrEmpty
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

func randomName(ext string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("random name: %w", err)
	}
	return hex.EncodeToString(buf[:]) + ext, nil
}

func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create upload directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp upload: %w", err)
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	// 0644, not 0600: the web container serves these files as a different user.
	if err := tmp.Chmod(filePerm); err != nil { //nolint:gosec // G302: uploads are world-readable images
		_ = tmp.Close()
		return fmt.Errorf("chmod upload: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close upload: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("rename upload: %w", err)
	}
	keep = true
	return nil
}

// resolve returns the absolute path of relPath inside the store.
func (s *Store) resolve(relPath string) (string, error) {
	if relPath == "" || strings.ContainsRune(relPath, 0) || strings.Contains(relPath, `\`) || filepath.IsAbs(relPath) {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean(relPath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidPath
	}
	root, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", fmt.Errorf("upload directory: %w", err)
	}
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return full, nil
}

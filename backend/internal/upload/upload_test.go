package upload

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSave(t *testing.T) {
	red := color.RGBA{R: 220, G: 10, B: 10, A: 255}
	green := color.RGBA{R: 10, G: 180, B: 10, A: 255}
	blue := color.RGBA{R: 10, G: 10, B: 220, A: 255}

	avatarSrc := image.NewNRGBA(image.Rect(0, 0, 300, 100))
	draw.Draw(avatarSrc, image.Rect(0, 0, 100, 100), image.NewUniform(red), image.Point{}, draw.Src)
	draw.Draw(avatarSrc, image.Rect(100, 0, 200, 100), image.NewUniform(green), image.Point{}, draw.Src)
	draw.Draw(avatarSrc, image.Rect(200, 0, 300, 100), image.NewUniform(blue), image.Point{}, draw.Src)

	wide := solid(2000, 1000, red)
	small := solid(100, 40, blue)
	drawingBig := solid(1600, 800, green)

	marker := "GPSLAT:37.774929"
	// Stored landscape with red on the left. EXIF orientation 6 asks for a 90°
	// clockwise turn, which puts that red band on top (the upright portrait).
	rotated := withEXIF(t, jpegOf(t, leftRight(80, 40, red, blue)), 6, marker)

	tests := []struct {
		name    string
		kind    Kind
		body    []byte
		wantErr error
		check   func(t *testing.T, rel string, data []byte)
	}{
		{
			name: "avatar center crop jpeg",
			kind: Avatar,
			body: jpegOf(t, avatarSrc),
			check: func(t *testing.T, rel string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
					t.Fatalf("bounds %v", img.Bounds())
				}
				assertNear(t, img.At(256, 256), 10, 180, 10, 50)
				if !bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
					t.Fatal("avatar is not a jpeg")
				}
				if !strings.HasPrefix(rel, "avatar/") || !strings.HasSuffix(rel, ".jpg") {
					t.Fatalf("path %s", rel)
				}
			},
		},
		{
			name: "avatar png",
			kind: Avatar,
			body: pngOf(t, small),
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
					t.Fatalf("bounds %v", img.Bounds())
				}
				if !bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
					t.Fatal("expected jpeg")
				}
			},
		},
		{
			name: "photo fits long side",
			kind: Photo,
			body: jpegOf(t, wide),
			check: func(t *testing.T, rel string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 1600 || img.Bounds().Dy() != 800 {
					t.Fatalf("bounds %v, want 1600x800", img.Bounds())
				}
				if !strings.HasSuffix(rel, ".jpg") {
					t.Fatalf("path %s", rel)
				}
			},
		},
		{
			name: "photo does not upscale",
			kind: Photo,
			body: pngOf(t, small),
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 40 {
					t.Fatalf("bounds %v", img.Bounds())
				}
			},
		},
		{
			name: "photo gif first frame",
			kind: Photo,
			body: animGIF(t),
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
					t.Fatalf("bounds %v", img.Bounds())
				}
				assertNear(t, img.At(8, 8), 220, 10, 10, 40)
			},
		},
		{
			name: "photo webp",
			kind: Photo,
			body: sampleWebP,
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 16 {
					t.Fatalf("bounds %v, want 32x16", img.Bounds())
				}
				if !bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
					t.Fatal("expected jpeg")
				}
			},
		},
		{
			name: "drawing png fit",
			kind: Drawing,
			body: pngOf(t, drawingBig),
			check: func(t *testing.T, rel string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 800 || img.Bounds().Dy() != 400 {
					t.Fatalf("bounds %v, want 800x400", img.Bounds())
				}
				if !bytes.HasPrefix(data, pngMagic) {
					t.Fatal("drawing is not a png")
				}
				if !strings.HasSuffix(rel, ".png") {
					t.Fatalf("path %s", rel)
				}
			},
		},
		{
			name: "drawing does not upscale",
			kind: Drawing,
			body: pngOf(t, small),
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 40 {
					t.Fatalf("bounds %v", img.Bounds())
				}
			},
		},
		{
			name:    "drawing rejects jpeg",
			kind:    Drawing,
			body:    jpegOf(t, small),
			wantErr: ErrNotPNG,
		},
		{
			name:    "oversized bytes",
			kind:    Photo,
			body:    make([]byte, MaxBytes+1),
			wantErr: ErrTooLarge,
		},
		{
			name:    "oversized width",
			kind:    Photo,
			body:    pngHeader(8001, 10),
			wantErr: ErrDimensions,
		},
		{
			name:    "oversized height",
			kind:    Avatar,
			body:    pngHeader(10, 9000),
			wantErr: ErrDimensions,
		},
		{
			name:    "pdf",
			kind:    Photo,
			body:    []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n"),
			wantErr: ErrUnsupportedType,
		},
		{
			name:    "svg",
			kind:    Photo,
			body:    []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`),
			wantErr: ErrUnsupportedType,
		},
		{
			name:    "svg fragment",
			kind:    Avatar,
			body:    []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
			wantErr: ErrUnsupportedType,
		},
		{
			name: "exif orientation upright and gps removed",
			kind: Photo,
			body: rotated,
			check: func(t *testing.T, _ string, data []byte) {
				img := mustDecode(t, data)
				if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 80 {
					t.Fatalf("bounds %v, want 40x80 upright", img.Bounds())
				}
				assertNear(t, img.At(20, 10), 220, 10, 10, 40)
				assertNear(t, img.At(20, 60), 10, 10, 220, 40)
				if bytes.Contains(data, []byte(marker)) || bytes.Contains(data, []byte("Exif\x00\x00")) {
					t.Fatal("output still contains EXIF or GPS")
				}
			},
		},
		{
			name:    "truncated jpeg",
			kind:    Photo,
			body:    []byte{0xff, 0xd8, 0xff, 0xd9},
			wantErr: ErrInvalidImage,
		},
		{
			name:    "truncated png header",
			kind:    Photo,
			body:    pngOf(t, small)[:24],
			wantErr: ErrInvalidImage,
		},
		{
			name:    "truncated png bitmap",
			kind:    Photo,
			body:    pngOf(t, small)[:33],
			wantErr: ErrInvalidImage,
		},
		{
			name:    "invalid kind",
			kind:    Kind("nope"),
			body:    jpegOf(t, small),
			wantErr: ErrInvalidKind,
		},
		{
			name:    "empty",
			kind:    Photo,
			body:    []byte{},
			wantErr: ErrEmpty,
		},
	}

	year := strconv.Itoa(time.Now().UTC().Year())
	relRe := regexp.MustCompile(`^(avatar|photo|drawing)/` + year + `/[0-9a-f]{32}\.(jpg|png)$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := New(t.TempDir())
			rel, err := st.Save(context.Background(), bytes.NewReader(tt.body), tt.kind)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got %v, want %v", err, tt.wantErr)
				}
				assertNoFiles(t, st.Dir)
				return
			}
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			if !relRe.MatchString(rel) {
				t.Fatalf("path %q", rel)
			}
			if got := URL(rel); got != "/uploads/"+rel {
				t.Fatalf("URL %q", got)
			}
			data := readStored(t, st, rel)
			if tt.check != nil {
				tt.check(t, rel, data)
			}
			assertNoTemp(t, st.Dir)
		})
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	st := New(t.TempDir())
	rel, err := st.Save(context.Background(), bytes.NewReader(pngOf(t, solid(20, 20, color.White))), Drawing)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(st.Dir, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(full); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still present: %v", err)
	}
	if err := st.Delete(rel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestDeleteRejectsEscape(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := New(filepath.Join(parent, "uploads"))
	rel, err := st.Save(context.Background(), bytes.NewReader(pngOf(t, solid(8, 8, color.Black))), Drawing)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(st.Dir, filepath.FromSlash(rel))

	tests := []string{
		"",
		"..",
		"../secret.txt",
		"avatar/../../secret.txt",
		filepath.Join("..", "secret.txt"),
		"/etc/passwd",
		"avatar/2026/file.jpg\x00.png",
		`avatar\2026\file.png`,
	}
	for _, relPath := range tests {
		t.Run(relPath, func(t *testing.T) {
			err := st.Delete(relPath)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat(outside); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(full); err != nil {
				t.Fatal("stored file was removed")
			}
		})
	}

	yearDir := filepath.Dir(full)
	relYear, err := filepath.Rel(st.Dir, yearDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(filepath.ToSlash(relYear)); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("delete dir: %v", err)
	}
	if _, err := os.Stat(full); err != nil {
		t.Fatal(err)
	}
}

func TestURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"avatar/2026/ab.jpg", "/uploads/avatar/2026/ab.jpg"},
		{"/photo/2026/ab.jpg", "/uploads/photo/2026/ab.jpg"},
	}
	for _, tt := range tests {
		if got := URL(tt.in); got != tt.want {
			t.Fatalf("URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSaveEdges(t *testing.T) {
	body := pngOf(t, solid(4, 4, color.White))

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := New(t.TempDir()).Save(ctx, bytes.NewReader(body), Photo)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("nil store", func(t *testing.T) {
		var st *Store
		_, err := st.Save(context.Background(), bytes.NewReader(body), Photo)
		if err == nil {
			t.Fatal("expected error")
		}
		if err := st.Delete("a"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil reader", func(t *testing.T) {
		_, err := New(t.TempDir()).Save(context.Background(), nil, Photo)
		if !errors.Is(err, ErrEmpty) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("reader error", func(t *testing.T) {
		_, err := New(t.TempDir()).Save(context.Background(), errReader{}, Photo)
		if err == nil || !strings.Contains(err.Error(), "read image") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("dir is a file", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := New(bad).Save(context.Background(), bytes.NewReader(body), Photo)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("distinct names", func(t *testing.T) {
		st := New(t.TempDir())
		a, err := st.Save(context.Background(), bytes.NewReader(body), Photo)
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.Save(context.Background(), bytes.NewReader(body), Photo)
		if err != nil {
			t.Fatal(err)
		}
		if a == b {
			t.Fatalf("same path %s", a)
		}
	})
}

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func leftRight(w, h int, left, right color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, image.Rect(0, 0, w/2, h), image.NewUniform(left), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(w/2, 0, w, h), image.NewUniform(right), image.Point{}, draw.Src)
	return img
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func animGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{
		color.RGBA{R: 220, G: 10, B: 10, A: 255},
		color.RGBA{R: 10, G: 10, B: 220, A: 255},
	}
	frame := func(idx uint8) *image.Paletted {
		img := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
		for i := range img.Pix {
			img.Pix[i] = idx
		}
		return img
	}
	g := &gif.GIF{Image: []*image.Paletted{frame(0), frame(1)}, Delay: []int{5, 5}}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngHeader(w, h uint32) []byte {
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], w)
	binary.BigEndian.PutUint32(data[4:8], h)
	data[8] = 8
	data[9] = 2
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	writeChunk(&buf, "IHDR", data)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	buf.Write(n[:])
	buf.WriteString(typ)
	buf.Write(data)
	sum := crc32.NewIEEE()
	_, _ = sum.Write([]byte(typ))
	_, _ = sum.Write(data)
	binary.BigEndian.PutUint32(n[:], sum.Sum32())
	buf.Write(n[:])
}

func withEXIF(t *testing.T, jpegRaw []byte, orientation uint16, marker string) []byte {
	t.Helper()
	if len(jpegRaw) < 2 || jpegRaw[0] != 0xff || jpegRaw[1] != 0xd8 {
		t.Fatal("not a jpeg")
	}
	payload := exifPayload(orientation, marker)
	tiff := payload[6:]
	asciiAt := 56
	if !bytes.Equal(tiff[asciiAt:asciiAt+len(marker)], []byte(marker)) {
		t.Fatalf("gps marker placed at %q", tiff[asciiAt:])
	}
	var out bytes.Buffer
	out.Write(jpegRaw[:2])
	out.Write([]byte{0xff, 0xe1})
	var sz [2]byte
	binary.BigEndian.PutUint16(sz[:], uint16(2+len(payload)))
	out.Write(sz[:])
	out.Write(payload)
	out.Write(jpegRaw[2:])
	if !bytes.Contains(out.Bytes(), []byte(marker)) {
		t.Fatal("fixture lost the gps marker")
	}
	return out.Bytes()
}

func exifPayload(orientation uint16, marker string) []byte {
	ascii := append([]byte(marker), 0)
	const (
		ifd0     = 8
		gpsIFD   = 38
		asciiOff = 56
	)
	buf := bytes.NewBuffer(make([]byte, 0, 6+asciiOff+len(ascii)))
	buf.WriteString("Exif\x00\x00")
	buf.Write([]byte{'I', 'I', 0x2a, 0x00})
	writeLE(buf, uint32(ifd0))
	writeLE(buf, uint16(2))
	writeIFDEntry(buf, 0x0112, 3, 1, uint32(orientation))
	writeIFDEntry(buf, 0x8825, 4, 1, gpsIFD)
	writeLE(buf, uint32(0))
	writeLE(buf, uint16(1))
	writeIFDEntry(buf, 0x0001, 2, uint32(len(ascii)), asciiOff)
	writeLE(buf, uint32(0))
	buf.Write(ascii)
	return buf.Bytes()
}

func writeIFDEntry(buf *bytes.Buffer, tag, typ uint16, count, value uint32) {
	writeLE(buf, tag)
	writeLE(buf, typ)
	writeLE(buf, count)
	writeLE(buf, value)
}

func writeLE(buf *bytes.Buffer, v any) {
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}

func mustDecode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return img
}

func assertNear(t *testing.T, c color.Color, r, g, b, tol int) {
	t.Helper()
	rr, gg, bb, _ := c.RGBA()
	dr, dg, db := int(rr>>8), int(gg>>8), int(bb>>8)
	if abs(dr-r) > tol || abs(dg-g) > tol || abs(db-b) > tol {
		t.Fatalf("rgb(%d,%d,%d), want about rgb(%d,%d,%d) ±%d", dr, dg, db, r, g, b, tol)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func readStored(t *testing.T, st *Store, rel string) []byte {
	t.Helper()
	full := filepath.Join(st.Dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(full) //nolint:gosec // test reads a path Save just returned
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %o, want 644", info.Mode().Perm())
	}
	return data
}

func assertNoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty dir, got %d entries", len(entries))
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// sampleWebP is a 32×16 lossless WebP used to prove the decoder accepts WebP.
var sampleWebP = []byte{
	0x52, 0x49, 0x46, 0x46, 0x1e, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50,
	0x56, 0x50, 0x38, 0x4c, 0x11, 0x00, 0x00, 0x00, 0x2f, 0x1f, 0xc0, 0x03,
	0x00, 0x07, 0x50, 0x85, 0x22, 0x57, 0xa1, 0xff, 0x81, 0x88, 0xe8, 0x7f,
	0x00, 0x00,
}

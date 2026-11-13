package upload

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register GIF for image.DecodeConfig / imaging.Decode
	_ "image/jpeg" // register JPEG
	_ "image/png"  // register PNG

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"
)

const (
	avatarEdge  = 512
	photoEdge   = 1600
	drawingMaxW = 800
	drawingMaxH = 400
	jpegQuality = 85
)

type format int

const (
	formatUnknown format = iota
	formatJPEG
	formatPNG
	formatGIF
	formatWebP
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// prepare sniffs magic bytes, rejects bombs, applies EXIF orientation, and
// re-encodes. Re-encoding is what drops EXIF and GPS.
func prepare(data []byte, kind Kind) ([]byte, string, error) {
	f := sniff(data)
	if f == formatUnknown {
		return nil, "", ErrUnsupportedType
	}
	if kind == Drawing && f != formatPNG {
		return nil, "", ErrNotPNG
	}

	cfg, err := decodeConfig(data, f)
	if err != nil {
		return nil, "", fmt.Errorf("%w: read image header: %w", ErrInvalidImage, err)
	}
	if cfg.Width > MaxEdge || cfg.Height > MaxEdge {
		return nil, "", fmt.Errorf("%w: %dx%d", ErrDimensions, cfg.Width, cfg.Height)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", fmt.Errorf("%w: invalid image dimensions %dx%d", ErrInvalidImage, cfg.Width, cfg.Height)
	}

	img, err := decodeOriented(data, f)
	if err != nil {
		return nil, "", fmt.Errorf("%w: decode image: %w", ErrInvalidImage, err)
	}
	out, ext, enc, err := transform(img, kind)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, out, enc, imaging.JPEGQuality(jpegQuality)); err != nil {
		return nil, "", fmt.Errorf("encode image: %w", err)
	}
	return buf.Bytes(), ext, nil
}

func sniff(b []byte) format {
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return formatJPEG
	case len(b) >= 8 && bytes.Equal(b[:8], pngMagic):
		return formatPNG
	case len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return formatGIF
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return formatWebP
	default:
		return formatUnknown
	}
}

// decodeConfig runs before the full decode so a huge IHDR/SOF is rejected
// without allocating the bitmap.
func decodeConfig(data []byte, f format) (image.Config, error) {
	r := bytes.NewReader(data)
	if f == formatWebP {
		return webp.DecodeConfig(r)
	}
	cfg, _, err := image.DecodeConfig(r)
	return cfg, err
}

// decodeOriented decodes the bitmap and applies JPEG EXIF orientation.
// imaging.AutoOrientation reads the orientation tag; the later re-encode
// does not write EXIF back. GIF uses the first frame only.
// WebP has no orientation support in imaging, so it is decoded as stored.
func decodeOriented(data []byte, f format) (image.Image, error) {
	r := bytes.NewReader(data)
	if f == formatWebP {
		return webp.Decode(r)
	}
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

func transform(img image.Image, kind Kind) (image.Image, string, imaging.Format, error) {
	switch kind {
	case Avatar:
		return imaging.Fill(img, avatarEdge, avatarEdge, imaging.Center, imaging.Lanczos), ".jpg", imaging.JPEG, nil
	case Photo:
		return imaging.Fit(img, photoEdge, photoEdge, imaging.Lanczos), ".jpg", imaging.JPEG, nil
	case Drawing:
		return imaging.Fit(img, drawingMaxW, drawingMaxH, imaging.Lanczos), ".png", imaging.PNG, nil
	default:
		return nil, "", 0, ErrInvalidKind
	}
}

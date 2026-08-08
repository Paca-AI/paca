package attachmentsvc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

func solidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestCropToSquare(t *testing.T) {
	tests := []struct {
		name    string
		w, h    int
		wantLen int
	}{
		{"wider than tall", 200, 100, 100},
		{"taller than wide", 100, 200, 100},
		{"already square", 150, 150, 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			square := cropToSquare(solidImage(tt.w, tt.h, color.White))
			b := square.Bounds()
			if b.Dx() != tt.wantLen || b.Dy() != tt.wantLen {
				t.Fatalf("cropToSquare(%dx%d): got %dx%d, want %dx%d", tt.w, tt.h, b.Dx(), b.Dy(), tt.wantLen, tt.wantLen)
			}
		})
	}
}

func TestResizeEncodePNG(t *testing.T) {
	square := solidImage(300, 300, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	for _, size := range []int{avatarFullSize, avatarThumbSize} {
		data, err := resizeEncodePNG(square, size)
		if err != nil {
			t.Fatalf("resizeEncodePNG(%d): %v", size, err)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode resized PNG: %v", err)
		}
		b := decoded.Bounds()
		if b.Dx() != size || b.Dy() != size {
			t.Fatalf("resizeEncodePNG(%d): output is %dx%d, want %dx%d", size, b.Dx(), b.Dy(), size, size)
		}
	}
}

func TestDecodeAvatarImage_PNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(10, 10, color.Black)); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
	img, err := decodeAvatarImage(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("decodeAvatarImage: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 10 || b.Dy() != 10 {
		t.Fatalf("decoded image is %dx%d, want 10x10", b.Dx(), b.Dy())
	}
}

func TestDecodeAvatarImage_InvalidBytes(t *testing.T) {
	if _, err := decodeAvatarImage([]byte("not an image"), "image/png"); err == nil {
		t.Fatal("expected decodeAvatarImage to reject non-image bytes, got nil error")
	}
}

// fakePNGHeader builds a syntactically valid PNG signature + IHDR chunk
// declaring width x height, with no pixel data. image.DecodeConfig only
// needs the IHDR chunk to report dimensions, so this lets the oversized-
// dimensions test below exercise the guard against an implausibly large
// declared image without actually allocating that much memory.
func fakePNGHeader(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8  // bit depth
	ihdr[9] = 6  // color type: truecolor + alpha
	ihdr[10] = 0 // compression method
	ihdr[11] = 0 // filter method
	ihdr[12] = 0 // interlace method
	writePNGChunk(&buf, "IHDR", ihdr)
	return buf.Bytes()
}

func writePNGChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])

	typeAndData := append([]byte(chunkType), data...)
	buf.Write(typeAndData)

	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(typeAndData))
	buf.Write(crcBuf[:])
}

func TestDecodeAvatarImage_RejectsOversizedDimensions(t *testing.T) {
	// Declares a 50000x50000 image (2.5 billion pixels, ~10 GiB as RGBA) —
	// well past MaxAvatarDecodeDimension. Must be rejected before the full
	// decoder ever runs.
	huge := fakePNGHeader(50000, 50000)
	_, err := decodeAvatarImage(huge, "image/png")
	if !errors.Is(err, attachmentdom.ErrAvatarDimensionsTooLarge) {
		t.Fatalf("decodeAvatarImage(50000x50000): got %v, want ErrAvatarDimensionsTooLarge", err)
	}
}

func TestDecodeAvatarImage_AllowsDimensionsWithinCap(t *testing.T) {
	square := solidImage(200, 200, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, square); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
	if _, err := decodeAvatarImage(buf.Bytes(), "image/png"); err != nil {
		t.Fatalf("decodeAvatarImage(200x200): unexpected error: %v", err)
	}
}

package attachmentsvc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
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

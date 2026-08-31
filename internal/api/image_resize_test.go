package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestProcessCategoryImage_ResizesLargePNG(t *testing.T) {
	in := makePNG(t, 1200, 800) // longest side 1200 > 400
	out := processCategoryImage(in, categoryImageMaxDim)

	if len(out) >= len(in) {
		t.Fatalf("expected smaller output: in=%d out=%d", len(in), len(out))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode resized: %v", err)
	}
	if cfg.Width > categoryImageMaxDim || cfg.Height > categoryImageMaxDim {
		t.Fatalf("longest side not capped: %dx%d", cfg.Width, cfg.Height)
	}
	// Aspect ratio ~1200:800 = 3:2; longest (400) -> other should be ~267.
	if cfg.Width != categoryImageMaxDim {
		t.Fatalf("expected width capped at %d, got %d", categoryImageMaxDim, cfg.Width)
	}
	if cfg.Height < 250 || cfg.Height > 285 {
		t.Fatalf("height not proportional: %d", cfg.Height)
	}
}

func TestProcessCategoryImage_ResizesLargeJPEG(t *testing.T) {
	in := makeJPEG(t, 1600, 400)
	out := processCategoryImage(in, categoryImageMaxDim)

	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode resized: %v", err)
	}
	if cfg.Width > categoryImageMaxDim || cfg.Height > categoryImageMaxDim {
		t.Fatalf("longest side not capped: %dx%d", cfg.Width, cfg.Height)
	}
	if cfg.Width != categoryImageMaxDim {
		t.Fatalf("expected width capped at %d, got %d", categoryImageMaxDim, cfg.Width)
	}
}

func TestProcessCategoryImage_SmallImageUnchanged(t *testing.T) {
	in := makePNG(t, 100, 100) // already <= 400
	out := processCategoryImage(in, categoryImageMaxDim)
	if !bytes.Equal(in, out) {
		t.Fatalf("small image should be returned unchanged")
	}
}

func TestProcessCategoryImage_GarbageReturnsOriginal(t *testing.T) {
	in := []byte("not an image at all")
	out := processCategoryImage(in, categoryImageMaxDim)
	if !bytes.Equal(in, out) {
		t.Fatalf("undecodable input should be returned unchanged")
	}
}

// makeTransparentPNG builds a w×h image with a fully transparent background and
// an opaque red square filling the central quarter. Used to verify that the
// resize step keeps the alpha channel (transparent areas stay transparent).
func makeTransparentPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h)) // all pixels start transparent
	x0, y0 := w/4, h/4
	x1, y1 := 3*w/4, 3*h/4
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255}) // opaque red
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestProcessCategoryImage_PreservesTransparency(t *testing.T) {
	in := makeTransparentPNG(t, 1200, 800) // longest side 1200 > 400
	out := processCategoryImage(in, categoryImageMaxDim)

	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode resized: %v", err)
	}
	b := img.Bounds()

	// A corner pixel (originally transparent) must stay fully transparent.
	if _, _, _, aCorner := img.At(b.Min.X, b.Min.Y).RGBA(); aCorner != 0 {
		t.Fatalf("corner lost transparency: alpha=%d", aCorner)
	}
	// The center (originally opaque red) must remain opaque and red.
	cr, cg, cb, aCenter := img.At(b.Max.X/2, b.Max.Y/2).RGBA()
	if aCenter == 0 || cr == 0 || cg != 0 || cb != 0 {
		t.Fatalf("center not opaque red: (%d,%d,%d,a=%d)", cr, cg, cb, aCenter)
	}
}

package api

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// categoryImageMaxDim is the maximum length (in pixels) of the longest side for
// a stored category image. Category images are rendered as squares via CSS
// object-cover; the largest on-screen usage is ~192px, so 400px keeps them crisp
// on retina displays while staying far smaller than typical full-res originals.
const categoryImageMaxDim = 400

// processCategoryImage resizes an uploaded image so its longest side is at most
// maxDim pixels (preserving aspect ratio) and re-encodes it in the same format.
//
// The alpha channel is preserved: PNGs with transparent regions keep them
// transparent after resizing (scaled into an RGBA canvas with draw.Over).
//
// It never fails: if the image cannot be decoded, is already small enough, uses
// a format we don't re-encode (e.g. webp/gif), or the re-encoded result is not
// smaller than the original, the original bytes are returned unchanged. This
// guarantees an upload always succeeds.
func processCategoryImage(data []byte, maxDim int) []byte {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || img == nil {
		return data // undecodable — keep original
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return data
	}

	longest := w
	if h > longest {
		longest = h
	}
	if longest <= maxDim {
		return data // already within target size — don't upscale
	}

	// Compute new dimensions preserving aspect ratio.
	scale := float64(maxDim) / float64(longest)
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	// High-quality downscale into a fresh RGBA canvas (starts fully transparent).
	// draw.Over composites the source over it, so any transparent regions in the
	// source remain transparent — preserving PNG transparency.
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	var encErr error
	switch format {
	case "png":
		encErr = png.Encode(&buf, dst)
	case "jpeg":
		encErr = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85})
	default:
		return data // webp/gif/etc. — avoid corrupting them; keep original
	}
	if encErr != nil {
		return data
	}

	out := buf.Bytes()
	// Only replace if the processed image is actually smaller on disk.
	if len(out) >= len(data) {
		return data
	}
	return out
}

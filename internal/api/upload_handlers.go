package api

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
)

const uploadDir = "./data/uploads/categories"

// uploadSubdirMaxDim bounds the max_dim parameter of the upload endpoint.
const uploadSubdirMaxDim = 4096

// HandleUploadImage handles POST /admin/upload-image
// Accepts multipart/form-data with field "file".
//
// Optional form fields:
//   - subdir:  storage subdirectory under ./data/uploads (default "categories",
//     e.g. "branding" for page decoration images). Must match [a-z0-9_-].
//   - max_dim: longest side after resize in pixels (default 400, max 4096).
//     Wide branding banners should use 1600–1920.
//
// Returns: { "url": "/uploads/{subdir}/{filename}" }
func (h *Handlers) HandleUploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Limit request size to 10MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "file too large or invalid format")
		return
	}

	// Optional subdir (default: categories, the historical upload target).
	subdir := r.FormValue("subdir")
	if subdir == "" {
		subdir = "categories"
	}
	if !isValidUploadSubdir(subdir) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid subdir")
		return
	}

	// Optional max_dim (default: categoryImageMaxDim = 400).
	maxDim := categoryImageMaxDim
	if v := r.FormValue("max_dim"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 100 || n > uploadSubdirMaxDim {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid max_dim (100..4096)")
			return
		}
		maxDim = n
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "no file provided")
		return
	}
	defer file.Close()

	// Validate file type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	if !allowedExts[ext] {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "unsupported file type: "+ext)
		return
	}

	// Generate unique filename
	filename := generateFilename() + ext

	// Ensure upload directory exists
	dir := filepath.Join("./data/uploads", subdir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create upload directory")
		return
	}

	// Read the uploaded bytes (request body is already capped at 10MB).
	raw, err := io.ReadAll(file)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file")
		return
	}

	// Resize/compress to roughly the on-screen layout size. Falls back to the
	// original bytes when processing isn't applicable or doesn't shrink the file.
	raw = processCategoryImage(raw, maxDim)

	// Save file
	dstPath := filepath.Join(dir, filename)
	if err := os.WriteFile(dstPath, raw, 0644); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save file")
		return
	}

	url := "/uploads/" + subdir + "/" + filename
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"url": url, "filename": filename})
}

// isValidUploadSubdir allows only safe subdirectory names under ./data/uploads.
func isValidUploadSubdir(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// HandleDeleteImage handles DELETE /admin/upload-image/{filename}
// Optional ?subdir= query param selects the storage subdirectory
// (default "categories", e.g. "branding").
func (h *Handlers) HandleDeleteImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract filename from path: /admin/upload-image/{filename}
	path := strings.TrimPrefix(r.URL.Path, "/admin/upload-image/")
	filename := filepath.Base(path)
	if filename == "" || strings.Contains(filename, "..") {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid filename")
		return
	}

	subdir := r.URL.Query().Get("subdir")
	if subdir == "" {
		subdir = "categories"
	}
	if !isValidUploadSubdir(subdir) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid subdir")
		return
	}

	filePath := filepath.Join("./data/uploads", subdir, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}

	if err := os.Remove(filePath); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete file")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleUploadsStatic serves static files from /uploads/{path}
// This should be registered as a prefix handler in main.go
func HandleUploadsStatic(w http.ResponseWriter, r *http.Request) {
	// Path: /uploads/{subdir}/{filename}
	relPath := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if relPath == r.URL.Path {
		http.NotFound(w, r)
		return
	}

	// Security: prevent directory traversal
	relPath = filepath.Clean(relPath)
	if strings.Contains(relPath, "..") || relPath == "." || relPath == "/" {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join("./data/uploads", relPath)

	// Check if file exists
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Set content type based on extension
	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year cache
	http.ServeFile(w, r, filePath)
}

func generateFilename() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// UploadImageRequest is used for programmatic image upload via JSON (alternative to multipart)
type UploadImageRequest struct {
	URL string `json:"url"` // external URL to fetch from (not implemented yet)
}

// UploadImageResponse is returned after successful upload
type UploadImageResponse struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

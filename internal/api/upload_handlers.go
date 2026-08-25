package api

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
)

const uploadDir = "./data/uploads/categories"

// HandleUploadImage handles POST /admin/upload-image
// Accepts multipart/form-data with field "file"
// Returns: { "url": "/uploads/categories/{filename}" }
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
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create upload directory")
		return
	}

	// Save file
	dstPath := filepath.Join(uploadDir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(dstPath)
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save file")
		return
	}

	url := "/uploads/categories/" + filename
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"url": url, "filename": filename})
}

// HandleDeleteImage handles DELETE /admin/upload-image/{filename}
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

	filePath := filepath.Join(uploadDir, filename)

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

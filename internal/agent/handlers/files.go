package handlers

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const (
	// Maximum file size for direct read/write (10MB)
	maxFileSize = 10 * 1024 * 1024
)

// WriteFileRequest represents a request to write a file.
type WriteFileRequest struct {
	// Path is the target file path (required).
	Path string `json:"path" binding:"required"`

	// Content is the file content as a string.
	Content string `json:"content,omitempty"`

	// Base64Content is the file content as base64-encoded bytes.
	Base64Content string `json:"base64Content,omitempty"`

	// Mode is the file permission mode (e.g., 0644). Default: 0644.
	Mode uint32 `json:"mode,omitempty"`
}

// WriteFileResponse represents the response after writing a file.
type WriteFileResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ReadFileResponse represents the response when reading a file.
type ReadFileResponse struct {
	Path          string `json:"path"`
	Content       string `json:"content,omitempty"`
	Base64Content string `json:"base64Content,omitempty"`
	Size          int64  `json:"size"`
}

// FileInfo represents file metadata.
type FileInfo struct {
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Size    int64       `json:"size"`
	Mode    fs.FileMode `json:"mode"`
	ModTime int64       `json:"modTime"` // Unix timestamp
	IsDir   bool        `json:"isDir"`
}

// StatFileResponse represents the response for file stat.
type StatFileResponse struct {
	FileInfo
}

// ListDirResponse represents the response for directory listing.
type ListDirResponse struct {
	Path    string     `json:"path"`
	Entries []FileInfo `json:"entries"`
}

// DeleteFileResponse represents the response for file deletion.
type DeleteFileResponse struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// UploadFileResponse represents the response after uploading a file.
type UploadFileResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// WriteFile handles POST /files/write requests.
func (h *Handler) WriteFile(c *gin.Context) {
	var req WriteFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.Content == "" && req.Base64Content == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "either content or base64Content is required"})
		return
	}

	// Resolve path via procfs
	fullPath, err := h.procFS.ResolvePath(req.Path)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		log.Printf("Failed to create parent directories for %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create directories"})
		return
	}

	// Determine content to write
	var content []byte
	if req.Base64Content != "" {
		content, err = base64.StdEncoding.DecodeString(req.Base64Content)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid base64 content: " + err.Error()})
			return
		}
	} else {
		content = []byte(req.Content)
	}

	// Check size limit
	if len(content) > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{
			Error: fmt.Sprintf("content size (%d bytes) exceeds limit (%d bytes)", len(content), maxFileSize),
		})
		return
	}

	// Determine file mode
	mode := fs.FileMode(0644)
	if req.Mode != 0 {
		mode = fs.FileMode(req.Mode)
	}

	// Write the file
	if err := os.WriteFile(fullPath, content, mode); err != nil { //nolint:gosec // mode is from request
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + req.Path})
			return
		}
		log.Printf("Failed to write file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	log.Printf("Wrote file %s (%d bytes)", req.Path, len(content))
	c.JSON(http.StatusOK, WriteFileResponse{
		Path: req.Path,
		Size: int64(len(content)),
	})
}

// ReadFile handles GET /files/read requests.
func (h *Handler) ReadFile(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

	// Check if client wants base64 encoding
	wantBase64 := c.Query("base64") == "true"

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "file not found: " + targetPath})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	if info.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	if info.Size() > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{
			Error: fmt.Sprintf("file size (%d bytes) exceeds limit (%d bytes)", info.Size(), maxFileSize),
		})
		return
	}

	content, err := os.ReadFile(fullPath) //nolint:gosec // path is resolved via procfs
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied reading: " + targetPath})
			return
		}
		log.Printf("Failed to read file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read file"})
		return
	}

	log.Printf("Read file %s (%d bytes)", targetPath, len(content))

	resp := ReadFileResponse{
		Path: targetPath,
		Size: int64(len(content)),
	}

	if wantBase64 {
		resp.Base64Content = base64.StdEncoding.EncodeToString(content)
	} else {
		resp.Content = string(content)
	}

	c.JSON(http.StatusOK, resp)
}

// StatFile handles GET /files/stat requests.
func (h *Handler) StatFile(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "file not found: " + targetPath})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	c.JSON(http.StatusOK, StatFileResponse{
		FileInfo: FileInfo{
			Name:    info.Name(),
			Path:    targetPath,
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
			IsDir:   info.IsDir(),
		},
	})
}

// ListDir handles GET /files/list requests.
func (h *Handler) ListDir(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		targetPath = "/"
	}

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "directory not found: " + targetPath})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat directory %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access directory"})
		return
	}

	if !info.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is not a directory"})
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied reading: " + targetPath})
			return
		}
		log.Printf("Failed to read directory %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read directory"})
		return
	}

	fileInfos := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		entryInfo, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}
		fileInfos = append(fileInfos, FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(targetPath, entry.Name()),
			Size:    entryInfo.Size(),
			Mode:    entryInfo.Mode(),
			ModTime: entryInfo.ModTime().Unix(),
			IsDir:   entry.IsDir(),
		})
	}

	c.JSON(http.StatusOK, ListDirResponse{
		Path:    targetPath,
		Entries: fileInfos,
	})
}

// DeleteFile handles DELETE /files requests.
func (h *Handler) DeleteFile(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

	// Prevent deleting root
	if targetPath == "/" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "cannot delete root directory"})
		return
	}

	recursive := c.Query("recursive") == "true"

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "file not found: " + targetPath})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	if info.IsDir() && !recursive {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, use recursive=true to delete"})
		return
	}

	if recursive {
		err = os.RemoveAll(fullPath)
	} else {
		err = os.Remove(fullPath)
	}

	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied deleting: " + targetPath})
			return
		}
		log.Printf("Failed to delete %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete file"})
		return
	}

	log.Printf("Deleted %s (recursive=%v)", targetPath, recursive)
	c.JSON(http.StatusOK, DeleteFileResponse{
		Path:    targetPath,
		Deleted: true,
	})
}

// UploadFile handles POST /files/upload requests (multipart form).
func (h *Handler) UploadFile(c *gin.Context) {
	targetPath := c.PostForm("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is required"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file is required"})
		return
	}

	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read uploaded file"})
		return
	}
	defer func() {
		if err := src.Close(); err != nil {
			log.Printf("Warning: failed to close uploaded file: %v", err)
		}
	}()

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		log.Printf("Failed to create parent directories for %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create directories"})
		return
	}

	dst, err := os.Create(fullPath) //nolint:gosec // path is resolved via procfs
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to create file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create file"})
		return
	}
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Warning: failed to close destination file: %v", err)
		}
	}()

	written, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Failed to write file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	// Ensure data is flushed
	if err := dst.Close(); err != nil {
		log.Printf("Failed to close file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to finalize file write"})
		return
	}

	log.Printf("Uploaded file to %s (%d bytes)", targetPath, written)
	c.JSON(http.StatusOK, UploadFileResponse{
		Path: targetPath,
		Size: written,
	})
}

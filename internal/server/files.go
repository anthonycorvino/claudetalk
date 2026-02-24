package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/google/uuid"
)

// FileStore manages file uploads on disk with in-memory metadata.
type FileStore struct {
	baseDir     string
	maxFileSize int64

	mu    sync.RWMutex
	files map[string]*protocol.FileInfo // id -> FileInfo
	rooms map[string][]string           // room -> list of file IDs
}

// NewFileStore creates a FileStore backed by the given directory.
func NewFileStore(baseDir string, maxFileSize int64) (*FileStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create file store dir: %w", err)
	}
	if maxFileSize <= 0 {
		maxFileSize = 50 * 1024 * 1024 // 50MB default
	}
	return &FileStore{
		baseDir:     baseDir,
		maxFileSize: maxFileSize,
		files:       make(map[string]*protocol.FileInfo),
		rooms:       make(map[string][]string),
	}, nil
}

// Store saves a file to disk and records metadata.
func (fs *FileStore) Store(room, sender, filename, contentType, description string, size int64, reader io.Reader) (*protocol.FileInfo, error) {
	if size > fs.maxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", size, fs.maxFileSize)
	}

	id := uuid.New().String()

	// Create room directory.
	roomDir := filepath.Join(fs.baseDir, room)
	if err := os.MkdirAll(roomDir, 0755); err != nil {
		return nil, fmt.Errorf("create room dir: %w", err)
	}

	// Write file to disk.
	diskName := id + "-" + filepath.Base(filename)
	diskPath := filepath.Join(roomDir, diskName)
	f, err := os.Create(diskPath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, io.LimitReader(reader, fs.maxFileSize+1))
	if err != nil {
		os.Remove(diskPath)
		return nil, fmt.Errorf("write file: %w", err)
	}
	if written > fs.maxFileSize {
		os.Remove(diskPath)
		return nil, fmt.Errorf("file too large: exceeded %d bytes", fs.maxFileSize)
	}

	info := &protocol.FileInfo{
		ID:          id,
		Room:        room,
		Sender:      sender,
		Filename:    filename,
		Size:        written,
		ContentType: contentType,
		Description: description,
		Timestamp:   time.Now().UTC(),
		URL:         fmt.Sprintf("/api/rooms/%s/files/%s", room, id),
	}

	fs.mu.Lock()
	fs.files[id] = info
	fs.rooms[room] = append(fs.rooms[room], id)
	fs.mu.Unlock()

	return info, nil
}

// Get returns metadata for a file by ID.
func (fs *FileStore) Get(id string) (*protocol.FileInfo, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	info, ok := fs.files[id]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", id)
	}
	return info, nil
}

// List returns all files in a room.
func (fs *FileStore) List(room string) []protocol.FileInfo {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	ids := fs.rooms[room]
	out := make([]protocol.FileInfo, 0, len(ids))
	for _, id := range ids {
		if info, ok := fs.files[id]; ok {
			out = append(out, *info)
		}
	}
	return out
}

// FilePath returns the on-disk path for a file by ID.
func (fs *FileStore) FilePath(id string) (string, error) {
	fs.mu.RLock()
	info, ok := fs.files[id]
	fs.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("file not found: %s", id)
	}

	diskName := id + "-" + filepath.Base(info.Filename)
	return filepath.Join(fs.baseDir, info.Room, diskName), nil
}

package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage implements Storage interface for local filesystem.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a new local storage backend.
func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &LocalStorage{baseDir: baseDir}, nil
}

// Upload uploads a file to local storage.
func (ls *LocalStorage) Upload(localPath, remotePath string) error {
	destPath := filepath.Join(ls.baseDir, remotePath)
	destDir := filepath.Dir(destPath)

	if err := os.MkdirAll(destDir, 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// Download downloads a file from local storage.
func (ls *LocalStorage) Download(remotePath, localPath string) error {
	srcPath := filepath.Join(ls.baseDir, remotePath)

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// Delete removes a file from local storage.
func (ls *LocalStorage) Delete(remotePath string) error {
	fullPath := filepath.Join(ls.baseDir, remotePath)
	return os.Remove(fullPath)
}

// List lists files in local storage.
func (ls *LocalStorage) List(prefix string) ([]string, error) {
	var files []string
	searchPath := filepath.Join(ls.baseDir, prefix)

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(ls.baseDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

// GetURL returns the URL for a file.
func (ls *LocalStorage) GetURL(remotePath string) (string, error) {
	return "file://" + filepath.Join(ls.baseDir, remotePath), nil
}

// Exists checks if a file exists.
func (ls *LocalStorage) Exists(remotePath string) (bool, error) {
	fullPath := filepath.Join(ls.baseDir, remotePath)
	_, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

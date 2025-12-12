package builder

import (
	"fmt"
	"log"

	"github.com/slchris/portage-engine/internal/storage"
)

// StorageUploader handles uploading build artifacts to storage.
type StorageUploader struct {
	storage storage.Storage
	enabled bool
}

// NewStorageUploader creates a new storage uploader.
func NewStorageUploader(storageType, _ /* localDir */, s3Bucket, s3Region, s3Prefix, httpBase string) (*StorageUploader, error) {
	if storageType == "" || storageType == "local" {
		// Local storage - no upload needed
		return &StorageUploader{
			enabled: false,
		}, nil
	}

	var st storage.Storage
	var err error

	switch storageType {
	case "s3":
		if s3Bucket == "" {
			return nil, fmt.Errorf("S3 bucket not configured")
		}
		st, err = storage.NewS3Storage(s3Bucket, s3Region, s3Prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 storage: %w", err)
		}
	case "http":
		if httpBase == "" {
			return nil, fmt.Errorf("HTTP base URL not configured")
		}
		st, err = storage.NewHTTPStorage(httpBase)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP storage: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}

	return &StorageUploader{
		storage: st,
		enabled: true,
	}, nil
}

// Upload uploads an artifact to storage.
func (u *StorageUploader) Upload(localPath, remotePath string) error {
	if !u.enabled {
		log.Printf("Storage upload disabled, keeping local file: %s", localPath)
		return nil
	}

	if u.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	log.Printf("Uploading %s to %s", localPath, remotePath)
	if err := u.storage.Upload(localPath, remotePath); err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}

	log.Printf("Upload complete: %s", remotePath)
	return nil
}

// GetURL returns the URL for an artifact.
func (u *StorageUploader) GetURL(remotePath string) (string, error) {
	if !u.enabled || u.storage == nil {
		return remotePath, nil
	}

	return u.storage.GetURL(remotePath)
}

// IsEnabled returns whether storage upload is enabled.
func (u *StorageUploader) IsEnabled() bool {
	return u.enabled
}

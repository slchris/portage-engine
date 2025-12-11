// Package binpkg manages binary package storage and queries.
package binpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Package represents a binary package.
type Package struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Arch         string            `json:"arch"`
	UseFlags     []string          `json:"use_flags"`
	Dependencies []string          `json:"dependencies"`
	Path         string            `json:"path"`
	Checksum     string            `json:"checksum"`
	Metadata     map[string]string `json:"metadata"`
}

// QueryRequest represents a package query request.
type QueryRequest struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Arch     string   `json:"arch"`
	UseFlags []string `json:"use_flags"`
}

// QueryResponse represents a package query response.
type QueryResponse struct {
	Found   bool     `json:"found"`
	Package *Package `json:"package,omitempty"`
}

// Store manages binary package storage.
type Store struct {
	basePath string
	mu       sync.RWMutex
	packages map[string]*Package
}

// NewStore creates a new package store.
func NewStore(basePath string) *Store {
	store := &Store{
		basePath: basePath,
		packages: make(map[string]*Package),
	}

	// Create base path if it doesn't exist
	if err := os.MkdirAll(basePath, 0750); err != nil {
		fmt.Printf("Warning: failed to create base path: %v\n", err)
	}

	store.loadPackages()
	return store
}

// Query searches for a package matching the request.
func (s *Store) Query(req *QueryRequest) (*Package, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.generateKey(req.Name, req.Version, req.Arch)
	pkg, found := s.packages[key]

	if !found {
		return nil, false
	}

	// Check USE flags compatibility
	if !s.useFlagsMatch(pkg.UseFlags, req.UseFlags) {
		return nil, false
	}

	return pkg, true
}

// Add adds a package to the store.
func (s *Store) Add(pkg *Package) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.generateKey(pkg.Name, pkg.Version, pkg.Arch)
	s.packages[key] = pkg

	return nil
}

// loadPackages loads existing packages from storage.
func (s *Store) loadPackages() {
	// In a production system, this would scan the binpkg directory
	// and load package metadata from index files
	fmt.Println("Loading existing packages from", s.basePath)
}

// generateKey generates a unique key for a package.
func (s *Store) generateKey(name, version, arch string) string {
	return fmt.Sprintf("%s-%s-%s", name, version, arch)
}

// useFlagsMatch checks if USE flags are compatible.
func (s *Store) useFlagsMatch(pkgFlags, reqFlags []string) bool {
	if len(reqFlags) == 0 {
		return true
	}

	flagSet := make(map[string]bool)
	for _, flag := range pkgFlags {
		flagSet[flag] = true
	}

	for _, flag := range reqFlags {
		if !flagSet[flag] {
			return false
		}
	}

	return true
}

// GetPath returns the storage path for a package.
func (s *Store) GetPath(pkg *Package) string {
	return filepath.Join(s.basePath, pkg.Arch, fmt.Sprintf("%s-%s.tbz2", pkg.Name, pkg.Version))
}

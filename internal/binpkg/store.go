// Package binpkg manages binary package storage and queries.
package binpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// Store manages an in-memory queryable view of the binary packages on disk
// (the PKGDIR served as a binhost). It is populated and kept fresh by
// RegenerateIndex, which scans the PKGDIR while (re)writing the Portage
// Packages index — one scan feeds both emerge clients and the JSON query API.
type Store struct {
	basePath string
	mu       sync.RWMutex
	packages map[string][]*Package // "name|arch" -> known versions

	// indexMu serializes on-disk index regeneration.
	indexMu sync.Mutex
}

// NewStore creates a new package store. The in-memory view starts empty and is
// filled by the first RegenerateIndex call (the server does this at startup
// and on a refresh interval).
func NewStore(basePath string) *Store {
	store := &Store{
		basePath: basePath,
		packages: make(map[string][]*Package),
	}

	// Create base path if it doesn't exist
	if err := os.MkdirAll(basePath, 0750); err != nil {
		fmt.Printf("Warning: failed to create base path: %v\n", err)
	}

	return store
}

// Query searches for a package matching the request. Version may be empty
// ("is any version of this package available?" — the natural binhost query),
// in which case the newest matching version is returned. Arch may be empty to
// match any architecture.
func (s *Store) Query(req *QueryRequest) (*Package, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *Package
	for _, pkg := range s.candidatesLocked(req.Name, req.Arch) {
		if req.Version != "" && pkg.Version != req.Version {
			continue
		}
		if !useFlagsMatch(pkg.UseFlags, req.UseFlags) {
			continue
		}
		if best == nil || versionLess(best.Version, pkg.Version) {
			best = pkg
		}
	}
	return best, best != nil
}

// candidatesLocked returns the known packages for name, restricted to arch
// when non-empty. Callers must hold s.mu.
func (s *Store) candidatesLocked(name, arch string) []*Package {
	if arch != "" {
		return s.packages[packageKey(name, arch)]
	}
	var all []*Package
	for _, pkgs := range s.packages {
		if len(pkgs) > 0 && pkgs[0].Name == name {
			all = append(all, pkgs...)
		}
	}
	return all
}

// Add adds (or replaces) a package in the store.
func (s *Store) Add(pkg *Package) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := packageKey(pkg.Name, pkg.Arch)
	list := s.packages[key]
	for i, existing := range list {
		if existing.Version == pkg.Version {
			list[i] = pkg
			return nil
		}
	}
	s.packages[key] = append(list, pkg)
	return nil
}

// packageKey builds the lookup key for a package. The separator cannot appear
// in Gentoo package names or versions, so "foo-1"/"2-3" and "foo"/"1-2-3" no
// longer collide (they did with the old name-version-arch scheme).
func packageKey(name, arch string) string {
	return name + "|" + arch
}

// useFlagsMatch checks if USE flags are compatible: every requested flag must
// be enabled in the package.
func useFlagsMatch(pkgFlags, reqFlags []string) bool {
	if len(reqFlags) == 0 {
		return true
	}

	flagSet := make(map[string]bool, len(pkgFlags))
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

// GetPath returns the on-disk path for a package. Packages ingested from the
// index carry their real relative path; the legacy layout is kept as a
// fallback for hand-constructed Package values.
func (s *Store) GetPath(pkg *Package) string {
	if pkg.Path != "" {
		return filepath.Join(s.basePath, pkg.Path)
	}
	return filepath.Join(s.basePath, pkg.Arch, fmt.Sprintf("%s-%s.tbz2", pkg.Name, pkg.Version))
}

// BasePath returns the on-disk PKGDIR this store manages. It is served to
// emerge clients as a binhost.
func (s *Store) BasePath() string {
	return s.basePath
}

// RegenerateIndex rebuilds the Portage "Packages" index from the binary
// packages currently on disk so that `emerge --getbinpkg` can consume this
// server as a binhost, and refreshes the in-memory query view from the same
// scan. arch is the default ARCH advertised in the index and assigned to
// scanned packages. Returns the number of packages indexed.
func (s *Store) RegenerateIndex(arch string) (int, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	entries, err := generateIndex(s.basePath, arch)
	if err != nil {
		return 0, err
	}

	fresh := make(map[string][]*Package, len(entries))
	for _, e := range entries {
		pkg := packageFromEntry(e, arch)
		if pkg == nil {
			continue
		}
		key := packageKey(pkg.Name, pkg.Arch)
		fresh[key] = append(fresh[key], pkg)
	}

	s.mu.Lock()
	s.packages = fresh
	s.mu.Unlock()

	return len(entries), nil
}

// packageFromEntry converts a scanned index entry into a queryable Package.
func packageFromEntry(e pkgEntry, arch string) *Package {
	name, version, ok := splitCPV(e.cpv)
	if !ok {
		return nil
	}
	var useFlags []string
	if use := e.extra["USE"]; use != "" {
		useFlags = strings.Fields(use)
	}
	checksum := ""
	if e.sha1 != "" {
		checksum = "sha1:" + e.sha1
	}
	return &Package{
		Name:     name,
		Version:  version,
		Arch:     arch,
		UseFlags: useFlags,
		Path:     e.path,
		Checksum: checksum,
		Metadata: e.extra,
	}
}

// splitCPV splits "category/name-version" into name and version. The version
// starts at the last hyphen that is directly followed by a digit (Gentoo
// package names cannot end in a version-like suffix, so this boundary is
// unambiguous; "-rN" revision suffixes stay with the version).
func splitCPV(cpv string) (name, version string, ok bool) {
	for i := len(cpv) - 2; i > 0; i-- {
		if cpv[i] == '-' && cpv[i+1] >= '0' && cpv[i+1] <= '9' {
			return cpv[:i], cpv[i+1:], true
		}
	}
	return "", "", false
}

// versionLess reports whether version a sorts before b, comparing dotted /
// dashed / underscored components numerically where possible. This is
// sufficient to pick the newest binpkg for a version-less query; it is not a
// full Portage vercmp.
func versionLess(a, b string) bool {
	sep := func(r rune) bool { return r == '.' || r == '-' || r == '_' }
	as, bs := strings.FieldsFunc(a, sep), strings.FieldsFunc(b, sep)
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				return an < bn
			}
		default:
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
		}
	}
	return len(as) < len(bs)
}

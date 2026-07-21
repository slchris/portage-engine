package builder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/slchris/portage-engine/pkg/config"
)

// TestFetchArtifactToBinhost verifies that a completed remote build's artifact
// is downloaded into the binhost PKGDIR under its category (previously only a
// path reference on the soon-destroyed VM was recorded, losing the artifact),
// and that the stored hook fires so the Packages index refreshes.
func TestFetchArtifactToBinhost(t *testing.T) {
	artifact := []byte("fake gpkg bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/artifacts/download/rjob-1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="jq-1.7-1.gpkg.tar"`)
		_, _ = w.Write(artifact)
	}))
	defer srv.Close()

	binhost := t.TempDir()
	mgr := NewManager(&config.ServerConfig{MaxWorkers: 1, BinpkgPath: binhost})
	defer mgr.Shutdown()

	var hookCalled atomic.Bool
	mgr.SetArtifactStoredHook(func() { hookCalled.Store(true) })

	dest, webPath, err := mgr.fetchArtifactToBinhost(srv.URL, "rjob-1", "app-misc/jq", "/var/tmp/portage-artifacts/jq-1.7-1.gpkg.tar")
	if err != nil {
		t.Fatalf("fetchArtifactToBinhost: %v", err)
	}

	want := filepath.Join(binhost, "app-misc", "jq-1.7-1.gpkg.tar")
	if dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if webPath != "/binpkgs/app-misc/jq-1.7-1.gpkg.tar" {
		t.Errorf("webPath = %q", webPath)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("artifact not stored: %v", err)
	}
	if string(data) != string(artifact) {
		t.Error("stored artifact content mismatch")
	}
	if !hookCalled.Load() {
		t.Error("artifact-stored hook was not called")
	}
}

// TestArtifactFilename covers header parsing and the fallback path.
func TestArtifactFilename(t *testing.T) {
	cases := []struct {
		disposition, remote, want string
	}{
		{`attachment; filename="jq-1.7-1.gpkg.tar"`, "", "jq-1.7-1.gpkg.tar"},
		{"", "/var/tmp/artifacts/vim-9.0-1.gpkg.tar", "vim-9.0-1.gpkg.tar"},
		{`attachment; filename="../../etc/passwd"`, "", "passwd"}, // path stripped
		{`attachment; filename=".hidden"`, "/x/fallback.tar", "fallback.tar"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := artifactFilename(c.disposition, c.remote); got != c.want {
			t.Errorf("artifactFilename(%q, %q) = %q, want %q", c.disposition, c.remote, got, c.want)
		}
	}
}

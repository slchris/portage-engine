package binpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTar writes a map of name->content as a tar archive to w.
func writeTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGenerateIndex_SignedGpkgWithCompressedMetadata builds a synthetic signed
// .gpkg.tar (compressed metadata + a .sig member) and verifies the index picks
// up SLOT/USE and marks the package SIGNED.
func TestGenerateIndex_SignedGpkgWithCompressedMetadata(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-lang", "python")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Inner metadata tar: metadata/<KEY> files.
	metaTar := writeTar(t, map[string]string{
		"metadata/SLOT":     "3.11",
		"metadata/USE":      "ssl threads sqlite",
		"metadata/KEYWORDS": "amd64",
		"metadata/CATEGORY": "dev-lang",
		"metadata/PF":       "python-3.11.0",
	})
	metaTarGz := gzipBytes(t, metaTar)

	// Outer gpkg tar: format id, compressed metadata, image, Manifest, and a sig.
	gpkg := writeTar(t, map[string]string{
		"gpkg-1":                                     "",
		"dev-lang/python-3.11.0/metadata.tar.gz":     string(metaTarGz),
		"dev-lang/python-3.11.0/image.tar":           "fake-image",
		"dev-lang/python-3.11.0/Manifest":            "DATA metadata.tar.gz 1 SHA512 abc",
		"dev-lang/python-3.11.0/metadata.tar.gz.sig": "-----BEGIN PGP SIGNATURE-----\nfake\n-----END PGP SIGNATURE-----\n",
	})

	if err := os.WriteFile(filepath.Join(pkgDir, "python-3.11.0.gpkg.tar"), gpkg, 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := GenerateIndex(dir, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 package, got %d", n)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	idx := string(data)

	for _, want := range []string{
		"CPV: dev-lang/python-3.11.0", // from CATEGORY/PF metadata
		"SLOT: 3.11",
		"USE: ssl threads sqlite",
		"KEYWORDS: amd64",
		"SIGNED: 1",
		"PATH: dev-lang/python/python-3.11.0.gpkg.tar",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q; got:\n%s", want, idx)
		}
	}
}

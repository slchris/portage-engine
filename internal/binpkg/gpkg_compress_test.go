package binpkg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func zstdBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func xzBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGenerateIndex_CompressedMetadata verifies the binhost index generator
// reads GPKG metadata compressed with each supported codec (regression for the
// zstd/xz metadata being dropped, which left packages with no SLOT/USE).
func TestGenerateIndex_CompressedMetadata(t *testing.T) {
	// Inner metadata tar with SLOT/USE and CATEGORY/PF for a composed CPV.
	metaTar := writeTar(t, map[string]string{
		"metadata/SLOT":     "3.11",
		"metadata/USE":      "ssl threads sqlite",
		"metadata/KEYWORDS": "amd64",
		"metadata/CATEGORY": "dev-lang",
		"metadata/PF":       "python-3.11.0",
	})

	cases := []struct {
		name   string
		member string
		encode func(*testing.T, []byte) []byte
	}{
		{"gzip", "metadata.tar.gz", gzipBytes},
		{"zstd", "metadata.tar.zst", zstdBytes},
		{"xz", "metadata.tar.xz", xzBytes},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pkgDir := filepath.Join(dir, "dev-lang", "python")
			if err := os.MkdirAll(pkgDir, 0o755); err != nil {
				t.Fatal(err)
			}

			gpkg := writeTar(t, map[string]string{
				"gpkg-1":                              "",
				"dev-lang/python-3.11.0/" + tc.member: string(tc.encode(t, metaTar)),
				"dev-lang/python-3.11.0/image.tar":    "fake-image",
				"dev-lang/python-3.11.0/Manifest":     "DATA " + tc.member + " 1 SHA512 abc",
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
			} {
				if !strings.Contains(idx, want) {
					t.Errorf("[%s] index missing %q; metadata was not decompressed. got:\n%s", tc.name, want, idx)
				}
			}
		})
	}
}

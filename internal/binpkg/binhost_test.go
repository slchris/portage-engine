package binpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIndex_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	n, err := GenerateIndex(dir, "amd64")
	if err != nil {
		t.Fatalf("GenerateIndex failed: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 packages, got %d", n)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Packages"))
	if err != nil {
		t.Fatalf("index not written: %v", err)
	}
	// A valid (empty) index still advertises the preamble emerge expects.
	for _, want := range []string{"ARCH: amd64", "VERSION: 0", "PACKAGES: 0"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("index missing %q; got:\n%s", want, data)
		}
	}
}

func TestGenerateIndex_FindsPackages(t *testing.T) {
	dir := t.TempDir()

	// Legacy layout: category/package-version.tbz2
	legacy := filepath.Join(dir, "dev-lang")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "python-3.11.0.tbz2"), []byte("fake-tbz2-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Modern layout: category/package/package-version.gpkg.tar
	modern := filepath.Join(dir, "sys-devel", "gcc")
	if err := os.MkdirAll(modern, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modern, "gcc-13.2.0.gpkg.tar"), []byte("fake-gpkg-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := GenerateIndex(dir, "amd64")
	if err != nil {
		t.Fatalf("GenerateIndex failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 packages, got %d", n)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	idx := string(data)

	for _, want := range []string{
		"PACKAGES: 2",
		"CPV: dev-lang/python-3.11.0",
		"CPV: sys-devel/gcc-13.2.0",
		"PATH: dev-lang/python-3.11.0.tbz2",
		"PATH: sys-devel/gcc/gcc-13.2.0.gpkg.tar",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q; got:\n%s", want, idx)
		}
	}

	// Every package stanza must carry a size and hashes so emerge can verify.
	if strings.Count(idx, "SIZE: ") != 2 {
		t.Errorf("expected 2 SIZE entries; got:\n%s", idx)
	}
	if strings.Count(idx, "SHA1: ") != 2 || strings.Count(idx, "MD5: ") != 2 {
		t.Errorf("expected 2 SHA1 and 2 MD5 entries; got:\n%s", idx)
	}
}

// xpakEntry appends one XPAK index+data entry (namelen+name+off+len).
// TestParseXpakBlob_MultiKey builds a spec-correct XPAK blob with two metadata
// keys and asserts BOTH are parsed (regression for the +12 stride bug that
// dropped every key after the first).
func TestParseXpakBlob_MultiKey(t *testing.T) {
	// data section holds the two values concatenated.
	data := []byte("0" + "ssl threads") // SLOT="0", USE="ssl threads"
	be := func(v uint32) []byte { b := make([]byte, 4); binaryBigEndianPut(b, v); return b }

	index := make([]byte, 0, 64)
	// entry SLOT: namelen, name, dataoffset(0), datalen(1)
	index = append(index, be(uint32(len("SLOT")))...)
	index = append(index, []byte("SLOT")...)
	index = append(index, be(0)...) // offset
	index = append(index, be(1)...) // length ("0")
	// entry USE: offset 1, length 11 ("ssl threads")
	index = append(index, be(uint32(len("USE")))...)
	index = append(index, []byte("USE")...)
	index = append(index, be(1)...)
	index = append(index, be(11)...)

	blob := make([]byte, 0, 128)
	blob = append(blob, []byte("XPAKPACK")...)
	blob = append(blob, be(uint32(len(index)))...)
	blob = append(blob, be(uint32(len(data)))...)
	blob = append(blob, index...)
	blob = append(blob, data...)

	meta := parseXpakBlob(blob)
	if meta["SLOT"] != "0" {
		t.Errorf("SLOT = %q, want 0", meta["SLOT"])
	}
	if meta["USE"] != "ssl threads" {
		t.Errorf("USE = %q, want 'ssl threads' (key after first was dropped)", meta["USE"])
	}
}

// TestParseXpakBlob_MalformedNoPanic ensures a blob with bogus length fields
// returns nil instead of panicking (binhost DoS regression).
func TestParseXpakBlob_MalformedNoPanic(t *testing.T) {
	bogus := []byte("XPAKPACK")
	bogus = append(bogus, 0xFF, 0xFF, 0xFF, 0xF0) // indexLen huge
	bogus = append(bogus, 0x00, 0x00, 0x00, 0x00) // dataLen 0
	// must not panic; returns nil
	if got := parseXpakBlob(bogus); got != nil {
		t.Errorf("expected nil for malformed blob, got %v", got)
	}
}

// binaryBigEndianPut is a tiny local helper to avoid importing encoding/binary
// in the test with a different alias.
func binaryBigEndianPut(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

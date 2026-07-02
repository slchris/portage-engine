package builder

import (
	"strings"
	"testing"

	"github.com/slchris/portage-engine/pkg/config"
)

// cfgWith builds a minimal BuilderConfig for signing-option tests.
func cfgWith(format string, gpgEnabled bool, keyID, gpgHome string) *config.BuilderConfig {
	return &config.BuilderConfig{
		BinpkgFormat: format,
		GPGEnabled:   gpgEnabled,
		GPGKeyID:     keyID,
		GPGHome:      gpgHome,
	}
}

// envMap turns a KEY=VALUE slice into a map for easy assertions.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

func TestBuildEnvironment_GpkgUnsigned(t *testing.T) {
	be := NewBuildExecutorWithOptions("/work", "/art", BuildOptions{Format: "gpkg"})
	bundle := &ConfigBundle{Config: &PortageConfig{}}
	env := envMap(be.buildEnvironment(PackageSpec{Atom: "dev-lang/python"}, bundle, "/work/packages"))

	if env["BINPKG_FORMAT"] != "gpkg" {
		t.Errorf("BINPKG_FORMAT = %q, want gpkg", env["BINPKG_FORMAT"])
	}
	if !strings.Contains(env["FEATURES"], "buildpkg") {
		t.Errorf("FEATURES missing buildpkg: %q", env["FEATURES"])
	}
	if strings.Contains(env["FEATURES"], "binpkg-signing") {
		t.Errorf("FEATURES should not enable signing when no key: %q", env["FEATURES"])
	}
	if _, ok := env["BINPKG_GPG_SIGNING_KEY"]; ok {
		t.Error("BINPKG_GPG_SIGNING_KEY set without a signing key")
	}
	if env["PKGDIR"] != "/work/packages" {
		t.Errorf("PKGDIR = %q, want /work/packages", env["PKGDIR"])
	}
}

func TestBuildEnvironment_GpkgSigned(t *testing.T) {
	be := NewBuildExecutorWithOptions("/work", "/art", BuildOptions{
		Format:        "gpkg",
		SignKeyID:     "0xDEADBEEF",
		SignGnupgHome: "/gpg-signing",
	})
	bundle := &ConfigBundle{Config: &PortageConfig{Environment: map[string]string{"FEATURES": "userfeature"}}}
	env := envMap(be.buildEnvironment(PackageSpec{Atom: "dev-lang/python"}, bundle, "/work/packages"))

	if !strings.Contains(env["FEATURES"], "binpkg-signing") {
		t.Errorf("FEATURES missing binpkg-signing: %q", env["FEATURES"])
	}
	// User-supplied FEATURES must be preserved, not clobbered.
	if !strings.Contains(env["FEATURES"], "userfeature") {
		t.Errorf("FEATURES dropped user feature: %q", env["FEATURES"])
	}
	if !strings.Contains(env["FEATURES"], "buildpkg") {
		t.Errorf("FEATURES missing buildpkg: %q", env["FEATURES"])
	}
	if env["BINPKG_GPG_SIGNING_KEY"] != "0xDEADBEEF" {
		t.Errorf("BINPKG_GPG_SIGNING_KEY = %q", env["BINPKG_GPG_SIGNING_KEY"])
	}
	if env["BINPKG_GPG_SIGNING_GPG_HOME"] != "/gpg-signing" {
		t.Errorf("BINPKG_GPG_SIGNING_GPG_HOME = %q", env["BINPKG_GPG_SIGNING_GPG_HOME"])
	}
}

func TestBuildEnvironment_XpakCannotSign(t *testing.T) {
	// Even with a key, the legacy xpak format must not enable signing.
	be := NewBuildExecutorWithOptions("/work", "/art", BuildOptions{
		Format:    "xpak",
		SignKeyID: "0xDEADBEEF",
	})
	bundle := &ConfigBundle{Config: &PortageConfig{}}
	env := envMap(be.buildEnvironment(PackageSpec{Atom: "dev-lang/python"}, bundle, "/work/packages"))

	if env["BINPKG_FORMAT"] != "xpak" {
		t.Errorf("BINPKG_FORMAT = %q, want xpak", env["BINPKG_FORMAT"])
	}
	if strings.Contains(env["FEATURES"], "binpkg-signing") {
		t.Errorf("xpak must not enable binpkg-signing: %q", env["FEATURES"])
	}
}

func TestBuildOptionsFromConfig(t *testing.T) {
	// Docker executor: signing home is the in-container mount path.
	docker := buildOptionsFromConfig(cfgWith("gpkg", true, "0xABC", "/var/lib/pe/gpg"), true)
	if !docker.signingEnabled() {
		t.Error("expected signing enabled for gpkg + key")
	}
	if docker.SignHostGnupgHome != "/var/lib/pe/gpg" || docker.SignGnupgHome != containerGnupgHome {
		t.Errorf("docker gnupg homes: host=%q container=%q", docker.SignHostGnupgHome, docker.SignGnupgHome)
	}

	// Native executor: signing home must be the real HOST path (the container
	// path does not exist on the host).
	native := buildOptionsFromConfig(cfgWith("gpkg", true, "0xABC", "/var/lib/pe/gpg"), false)
	if native.SignGnupgHome != "/var/lib/pe/gpg" {
		t.Errorf("native SignGnupgHome = %q, want the host path /var/lib/pe/gpg", native.SignGnupgHome)
	}

	// No key → no signing.
	if buildOptionsFromConfig(cfgWith("gpkg", true, "", "/x"), false).signingEnabled() {
		t.Error("signing should be disabled without a key")
	}
	// GPG disabled → no signing.
	if buildOptionsFromConfig(cfgWith("gpkg", false, "0xABC", "/x"), true).signingEnabled() {
		t.Error("signing should be disabled when GPG disabled")
	}
	// Default format is gpkg.
	if got := buildOptionsFromConfig(cfgWith("", true, "0xABC", "/x"), false).Format; got != "gpkg" {
		t.Errorf("default format = %q, want gpkg", got)
	}
}

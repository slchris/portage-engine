// Package builder provides validation for untrusted build specifications.
package builder

import (
	"fmt"
	"regexp"
)

// The build endpoint accepts a ConfigBundle from clients. Every field of a
// PackageSpec ultimately reaches a shell/emerge invocation, so it must be
// validated against a strict allowlist before use. These patterns intentionally
// reject shell metacharacters ($ ` ; & | > < ( ) newline etc.) and anything
// that could be interpreted as an emerge option (a leading dash).
var (
	// A Gentoo atom: category/package, optionally with a :slot suffix.
	// Examples: dev-lang/python, dev-lang/python:3.11, sys-devel/gcc
	atomPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+._-]*/[a-zA-Z0-9][a-zA-Z0-9+._-]*(:[a-zA-Z0-9][a-zA-Z0-9+._/-]*)?$`)

	// A package version: digits, dots, letters, and the usual suffixes.
	// Examples: 3.11, 13.2.0, 1.0.0_rc1, 2.38-r1
	versionPattern = regexp.MustCompile(`^[0-9][a-zA-Z0-9._-]*$`)

	// A single USE flag, optionally prefixed with - or +.
	useFlagPattern = regexp.MustCompile(`^[+-]?[a-zA-Z0-9][a-zA-Z0-9+_@-]*$`)

	// A keyword, e.g. amd64, ~amd64, **.
	keywordPattern = regexp.MustCompile(`^[~*]?[a-zA-Z0-9*][a-zA-Z0-9_-]*$`)

	// An environment variable name.
	envKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	// An environment value: printable, no shell metacharacters or newlines.
	envValuePattern = regexp.MustCompile(`^[a-zA-Z0-9 ,.:=@%+/_-]*$`)
)

// validatePackageSpec rejects a package specification whose fields contain
// anything outside the strict allowlists above. It is the single choke point
// that untrusted build requests must pass before any command is constructed.
func validatePackageSpec(pkg PackageSpec) error {
	if !atomPattern.MatchString(pkg.Atom) {
		return fmt.Errorf("invalid package atom %q", pkg.Atom)
	}
	if pkg.Version != "" && !versionPattern.MatchString(pkg.Version) {
		return fmt.Errorf("invalid package version %q", pkg.Version)
	}
	for _, u := range pkg.UseFlags {
		if !useFlagPattern.MatchString(u) {
			return fmt.Errorf("invalid USE flag %q", u)
		}
	}
	for _, k := range pkg.Keywords {
		if !keywordPattern.MatchString(k) {
			return fmt.Errorf("invalid keyword %q", k)
		}
	}
	for key, val := range pkg.Environment {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("invalid environment variable name %q", key)
		}
		if !envValuePattern.MatchString(val) {
			return fmt.Errorf("invalid value for environment variable %q", key)
		}
	}
	return nil
}

// validateBundleEnvironment validates the global environment map of a bundle.
func validateBundleEnvironment(env map[string]string) error {
	for key, val := range env {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("invalid environment variable name %q", key)
		}
		if !envValuePattern.MatchString(val) {
			return fmt.Errorf("invalid value for environment variable %q", key)
		}
	}
	return nil
}

// validateLocalBuildRequest validates every untrusted field of a build request
// against the strict allowlists above. It is the single choke point that ALL
// build paths (legacy Docker shell script, native emerge, and config bundle)
// must pass, so no user-controlled value can reach a shell or be misread as an
// emerge option.
func validateLocalBuildRequest(req *LocalBuildRequest) error {
	if req == nil {
		return fmt.Errorf("nil build request")
	}

	// The package name must be a valid atom (this also rejects a leading dash,
	// preventing emerge option injection on the native argv path).
	if !atomPattern.MatchString(req.PackageName) {
		return fmt.Errorf("invalid package name %q", req.PackageName)
	}
	if req.Version != "" && !versionPattern.MatchString(req.Version) {
		return fmt.Errorf("invalid package version %q", req.Version)
	}
	if req.Arch != "" && !keywordPattern.MatchString(req.Arch) {
		return fmt.Errorf("invalid arch %q", req.Arch)
	}
	// UseFlags is a map of flag name -> "enabled"/"disabled"/etc; validate the
	// flag NAMES (the keys reach the emerge --use / build script).
	for flag := range req.UseFlags {
		if !useFlagPattern.MatchString(flag) {
			return fmt.Errorf("invalid USE flag %q", flag)
		}
	}
	if err := validateBundleEnvironment(req.Environment); err != nil {
		return err
	}

	// If a config bundle is attached, it is validated on its own path too, but
	// validate it here as well so a legacy caller cannot smuggle bad specs.
	if req.ConfigBundle != nil {
		return validateBundle(req.ConfigBundle)
	}
	for _, spec := range req.PackageSpecs {
		if err := validatePackageSpec(spec); err != nil {
			return err
		}
	}
	return nil
}

// validateBundle validates every package spec and the global environment of a
// bundle. Callers must invoke this before executing any build.
func validateBundle(bundle *ConfigBundle) error {
	if bundle == nil {
		return fmt.Errorf("nil config bundle")
	}
	if bundle.Config != nil {
		if err := validateBundleEnvironment(bundle.Config.Environment); err != nil {
			return err
		}
	}
	if bundle.Packages == nil || len(bundle.Packages.Packages) == 0 {
		return fmt.Errorf("config bundle contains no packages")
	}
	for _, pkg := range bundle.Packages.Packages {
		if err := validatePackageSpec(pkg); err != nil {
			return err
		}
	}
	return nil
}

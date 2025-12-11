// Package gpg provides GPG signing functionality for binary packages.
package gpg

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// Signer handles GPG signing of packages.
type Signer struct {
	keyID   string
	keyPath string
	enabled bool
}

// NewSigner creates a new GPG signer.
func NewSigner(keyID, keyPath string, enabled bool) *Signer {
	return &Signer{
		keyID:   keyID,
		keyPath: keyPath,
		enabled: enabled,
	}
}

// IsEnabled returns whether GPG signing is enabled.
func (s *Signer) IsEnabled() bool {
	return s.enabled
} // SignPackage signs a binary package file.
// SignPackage signs a package file with GPG.
// SignPackage signs a package file with GPG.
func (s *Signer) SignPackage(packagePath string) error {
	if !s.enabled {
		log.Printf("GPG signing disabled, skipping signature for %s", packagePath)
		return nil
	}

	if s.keyID == "" {
		return fmt.Errorf("GPG key ID not configured")
	}

	signaturePath := packagePath + ".sig"

	log.Printf("Signing package: %s with key %s", packagePath, s.keyID)

	args := []string{
		"--detach-sign",
		"--armor",
		"--local-user", s.keyID,
	}

	if s.keyPath != "" {
		if _, err := os.Stat(s.keyPath); err == nil {
			args = append([]string{"--no-default-keyring", "--keyring", s.keyPath}, args...)
		}
	}

	args = append(args, "--output", signaturePath, packagePath)

	cmd := exec.Command("gpg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to sign package: %w", err)
	}

	log.Printf("Package signed successfully: %s", signaturePath)
	return nil
}

// VerifyPackage verifies a package signature.
func (s *Signer) VerifyPackage(packagePath string) error {
	signaturePath := packagePath + ".sig"

	if _, err := os.Stat(signaturePath); os.IsNotExist(err) {
		return fmt.Errorf("signature file not found: %s", signaturePath)
	}

	log.Printf("Verifying package signature: %s", packagePath)

	args := []string{
		"--verify",
		signaturePath,
		packagePath,
	}

	if s.keyPath != "" {
		if _, err := os.Stat(s.keyPath); err == nil {
			args = append([]string{"--no-default-keyring", "--keyring", s.keyPath}, args...)
		}
	}

	cmd := exec.Command("gpg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	log.Printf("Package signature verified successfully")
	return nil
}

// SignDirectory signs all packages in a directory.
func (s *Signer) SignDirectory(dir string) error {
	if !s.enabled {
		return nil
	}

	pattern := filepath.Join(dir, "*.tbz2")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to find packages: %w", err)
	}

	for _, pkg := range matches {
		if err := s.SignPackage(pkg); err != nil {
			log.Printf("Warning: failed to sign %s: %v", pkg, err)
		}
	}

	return nil
}

// CheckGPG checks if GPG is available.
func CheckGPG() error {
	cmd := exec.Command("gpg", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("GPG is not available: %w", err)
	}
	return nil
}

// ListKeys lists available GPG keys.
func ListKeys() error {
	cmd := exec.Command("gpg", "--list-secret-keys")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

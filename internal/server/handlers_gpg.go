package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/slchris/portage-engine/internal/gpg"
)

// handlers_gpg.go exposes GPG signing key management to the dashboard:
// status, runtime key generation, and (via the existing /api/v1/gpg/public-key
// endpoint) public key distribution.

// gpgRuntimeConfig persists a dashboard-triggered signing setup so it survives
// restarts even when the bootstrap conf has GPG_ENABLED=false.
type gpgRuntimeConfig struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}

func (s *Server) gpgRuntimePath() string {
	dataDir := s.config.DataDir
	if dataDir == "" {
		dataDir = "/var/lib/portage-engine/server"
	}
	return filepath.Join(dataDir, "gpg-runtime.json")
}

// loadGPGRuntime applies a previously saved dashboard-managed signing setup.
// Called at startup after the conf-based signer is initialized.
func (s *Server) loadGPGRuntime() {
	if s.gpgSigner.IsEnabled() {
		return // conf already enables signing
	}
	data, err := os.ReadFile(s.gpgRuntimePath())
	if err != nil {
		return
	}
	var rc gpgRuntimeConfig
	if json.Unmarshal(data, &rc) != nil || !rc.Enabled {
		return
	}
	if err := s.enableGPG(rc.Name, rc.Email); err != nil {
		log.Printf("Warning: failed to re-enable dashboard-managed GPG signing: %v", err)
		return
	}
	log.Printf("GPG signing re-enabled from dashboard-managed config (key %s)", s.gpgSigner.KeyID())
}

// enableGPG builds an enabled signer (auto-creating a key when none exists)
// and swaps it in.
func (s *Server) enableGPG(name, email string) error {
	if name == "" {
		name = "Portage Engine"
	}
	if email == "" {
		email = "portage@localhost"
	}
	var opts []gpg.SignerOption
	if s.config.GPGHome != "" {
		opts = append(opts, gpg.WithGnupgHome(s.config.GPGHome))
	}
	opts = append(opts, gpg.WithAutoCreate(name, email))
	signer := gpg.NewSigner(s.config.GPGKeyID, s.config.GPGKeyPath, true, opts...)
	if err := signer.Initialize(); err != nil {
		return err
	}
	s.settingsMu.Lock()
	s.gpgSigner = signer
	s.settingsMu.Unlock()
	return nil
}

// gpgKeyMaterial exports the signing key material (key ID, armored public
// key, armored secret key) for deployment, verification, and publication.
// Returns zero values when signing is disabled.
func (s *Server) gpgKeyMaterial() (string, []byte, []byte) {
	s.settingsMu.Lock()
	signer := s.gpgSigner
	s.settingsMu.Unlock()
	if signer == nil || !signer.IsEnabled() || signer.KeyID() == "" {
		return "", nil, nil
	}
	dir, err := os.MkdirTemp("", "pe-gpg-export")
	if err != nil {
		return "", nil, nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	pubPath, secPath, err := signer.ExportKeyPair(dir)
	if err != nil {
		log.Printf("Warning: GPG key export failed: %v", err)
		return "", nil, nil
	}
	pub, _ := os.ReadFile(pubPath)
	sec, _ := os.ReadFile(secPath)
	return signer.KeyID(), pub, sec
}

// handleGPGPubkey serves the armored public signing key so clients (and the
// mirror) can trust the binhost.
func (s *Server) handleGPGPubkey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	keyID, pub, _ := s.gpgKeyMaterial()
	if keyID == "" || len(pub) == 0 {
		http.Error(w, "GPG signing is not enabled", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/pgp-keys")
	w.Header().Set("Content-Disposition", `attachment; filename="portage-engine.asc"`)
	_, _ = w.Write(pub)
}

// handleGPGStatus reports whether signing is enabled and with which key.
func (s *Server) handleGPGStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.settingsMu.Lock()
	signer := s.gpgSigner
	s.settingsMu.Unlock()
	writeJSON(w, map[string]any{
		"enabled": signer.IsEnabled(),
		"key_id":  signer.KeyID(),
	})
}

// handleGPGGenerate creates (or adopts) a signing key at runtime, enables
// signing, and persists the choice.
func (s *Server) handleGPGGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req gpgRuntimeConfig
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := s.enableGPG(req.Name, req.Email); err != nil {
		http.Error(w, "GPG setup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req.Enabled = true
	if data, err := json.MarshalIndent(&req, "", "  "); err == nil {
		if err := os.MkdirAll(filepath.Dir(s.gpgRuntimePath()), 0o750); err == nil {
			_ = os.WriteFile(s.gpgRuntimePath(), data, 0o600)
		}
	}

	log.Printf("GPG signing enabled via dashboard (key %s)", s.gpgSigner.KeyID())
	writeJSON(w, map[string]any{
		"enabled": true,
		"key_id":  s.gpgSigner.KeyID(),
	})
}

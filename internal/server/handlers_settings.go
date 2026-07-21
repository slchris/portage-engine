package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/slchris/portage-engine/internal/iac"
	"github.com/slchris/portage-engine/pkg/config"
)

// handlers_settings.go exposes the dashboard-managed runtime configuration:
// cloud provisioning settings can be viewed and edited over the API instead of
// hand-editing server.conf. Updates apply immediately (next build uses them)
// and persist to DATA_DIR/cloud-settings.json, which overrides the static conf
// at startup.

// cloudSettingsResponse is a CloudSettings with secrets redacted; booleans
// tell the UI whether a secret is stored (an empty secret on PUT means "keep
// the stored one").
type cloudSettingsResponse struct {
	config.CloudSettings
	HasPVETokenSecret bool `json:"has_pve_token_secret"`
	HasPVEPassword    bool `json:"has_pve_password"`
	HasAWSSecretKey   bool `json:"has_aws_secret_key"`
	HasUploadPassword bool `json:"has_upload_password"`
}

func redactedCloudSettings(cs *config.CloudSettings) cloudSettingsResponse {
	resp := cloudSettingsResponse{
		CloudSettings:     *cs.Clone(),
		HasPVETokenSecret: cs.PVETokenSecret != "",
		HasPVEPassword:    cs.PVEPassword != "",
		HasAWSSecretKey:   cs.AWSSecretKey != "",
		HasUploadPassword: cs.UploadPassword != "",
	}
	resp.PVETokenSecret = ""
	resp.PVEPassword = ""
	resp.AWSSecretKey = ""
	resp.UploadPassword = ""
	return resp
}

// cloudSettingsPath is where dashboard-edited settings persist across
// restarts.
func (s *Server) cloudSettingsPath() string {
	dataDir := s.config.DataDir
	if dataDir == "" {
		dataDir = "/var/lib/portage-engine/server"
	}
	return filepath.Join(dataDir, "cloud-settings.json")
}

// loadCloudSettingsOverride applies a previously saved dashboard-managed
// settings file over the conf/env defaults. Called once at startup.
func (s *Server) loadCloudSettingsOverride() {
	data, err := os.ReadFile(s.cloudSettingsPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: cannot read cloud settings override: %v", err)
		}
		return
	}
	var cs config.CloudSettings
	if err := json.Unmarshal(data, &cs); err != nil {
		log.Printf("Warning: ignoring corrupt cloud settings override %s: %v", s.cloudSettingsPath(), err)
		return
	}
	s.builder.UpdateCloudSettings(&cs)
	log.Printf("Applied dashboard-managed cloud settings from %s (overrides server.conf)", s.cloudSettingsPath())
}

// handleCloudSettings serves GET (redacted view) and PUT (update + persist).
func (s *Server) handleCloudSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, redactedCloudSettings(s.builder.CloudSettings()))
	case http.MethodPut:
		s.updateCloudSettings(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateCloudSettings(w http.ResponseWriter, r *http.Request) {
	var in config.CloudSettings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	switch in.Provider {
	case "", "pve", "gcp", "aws":
	default:
		http.Error(w, fmt.Sprintf("unsupported provider %q", in.Provider), http.StatusBadRequest)
		return
	}
	if in.InstanceTTLMinutes < 0 {
		http.Error(w, "instance_ttl_minutes must be >= 0", http.StatusBadRequest)
		return
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	// An empty secret means "keep the stored one" so the UI never has to
	// round-trip (or even see) the real secrets.
	cur := s.builder.CloudSettings()
	if in.PVETokenSecret == "" {
		in.PVETokenSecret = cur.PVETokenSecret
	}
	if in.PVEPassword == "" {
		in.PVEPassword = cur.PVEPassword
	}
	if in.AWSSecretKey == "" {
		in.AWSSecretKey = cur.AWSSecretKey
	}
	if in.UploadPassword == "" {
		in.UploadPassword = cur.UploadPassword
	}

	s.builder.UpdateCloudSettings(&in)

	// Persist (with the secret — mode 0600) so the settings survive restarts.
	path := s.cloudSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		http.Error(w, "settings applied but not persisted: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.MarshalIndent(&in, "", "  ")
	if err != nil {
		http.Error(w, "settings applied but not persisted: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		http.Error(w, "settings applied but not persisted: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Cloud settings updated via API (provider=%s, pve_endpoint=%s)", in.Provider, in.PVEEndpoint)
	writeJSON(w, redactedCloudSettings(&in))
}

// handleCloudSettingsTest validates PVE connectivity for the posted settings
// (falling back to stored values for omitted fields, notably the secret) and
// returns the cluster's nodes so the UI can show what the scheduler sees.
func (s *Server) handleCloudSettingsTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var in config.CloudSettings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	cur := s.builder.CloudSettings()
	if in.PVEEndpoint == "" {
		in.PVEEndpoint = cur.PVEEndpoint
	}
	if in.PVETokenID == "" {
		in.PVETokenID = cur.PVETokenID
	}
	if in.PVETokenSecret == "" {
		in.PVETokenSecret = cur.PVETokenSecret
	}
	if in.PVEUsername == "" {
		in.PVEUsername = cur.PVEUsername
	}
	if in.PVEPassword == "" {
		in.PVEPassword = cur.PVEPassword
	}
	if in.PVETemplate == "" {
		in.PVETemplate = cur.PVETemplate
	}

	type testResponse struct {
		OK    bool              `json:"ok"`
		Error string            `json:"error,omitempty"`
		Nodes []iac.PVENodeInfo `json:"nodes,omitempty"`
	}

	auth := iac.PVEAuth{
		TokenID:     in.PVETokenID,
		TokenSecret: in.PVETokenSecret,
		Username:    in.PVEUsername,
		Password:    in.PVEPassword,
		Insecure:    in.PVEInsecure,
	}
	nodes, err := iac.PVEClusterNodes(in.PVEEndpoint, auth, in.PVETemplate)
	if err != nil {
		writeJSON(w, testResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, testResponse{OK: true, Nodes: nodes})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

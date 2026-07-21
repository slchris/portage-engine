package builder

import (
	"strings"
	"testing"

	"github.com/slchris/portage-engine/pkg/config"
)

// TestVerifyInstallRejectsInjection ensures the verify path validates the
// package atom before it can reach a shell (docker script) or an emerge argv,
// closing command/option injection via /api/v1/verify.
func TestVerifyInstallRejectsInjection(t *testing.T) {
	lb := &LocalBuilder{cfg: &config.BuilderConfig{UseDocker: false}}
	bad := []string{
		"app-misc/jq; rm -rf /", // shell metacharacters
		"$(reboot)",
		"`id`",
		"--root=/etc",       // emerge option smuggling
		"-K app-misc/jq",    // leading dash
		"app-misc/jq && ls", // command chaining
		"",
	}
	for _, atom := range bad {
		if _, err := lb.VerifyInstall(atom, "http://binhost", "", false); err == nil {
			t.Errorf("VerifyInstall(%q) accepted a malicious atom", atom)
		} else if !strings.Contains(err.Error(), "invalid package atom") {
			t.Errorf("VerifyInstall(%q) failed with %v, want atom-validation error", atom, err)
		}
	}
}

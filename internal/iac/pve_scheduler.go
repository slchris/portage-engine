package iac

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pve_scheduler.go implements automatic node placement for PVE build VMs
// (CLOUD_PVE_NODE=auto). Proxmox itself has no scheduler for API-created VMs —
// a clone must name an explicit target node (the built-in CRS only manages
// HA-registered resources) — so placement is decided here: the cluster's live
// load is read via GET /api2/json/cluster/resources and the least-loaded
// eligible node wins.

// pveNodeQueryTimeout bounds the cluster-resources API call so a hung PVE API
// cannot stall a build worker.
const pveNodeQueryTimeout = 10 * time.Second

// pveClusterResource is one entry of GET /api2/json/cluster/resources. The
// same list carries nodes (type "node") and guests (type "qemu"/"lxc").
type pveClusterResource struct {
	Type     string  `json:"type"`
	Node     string  `json:"node"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Template int     `json:"template"`
	MaxMem   int64   `json:"maxmem"`
	Mem      int64   `json:"mem"`
	CPU      float64 `json:"cpu"`
}

// PVEAuth carries the credentials for direct PVE API calls. Either an API
// token (preferred) or username+password (exchanged for a ticket) must be set.
type PVEAuth struct {
	TokenID     string
	TokenSecret string
	Username    string
	Password    string
	Insecure    bool
}

func (a PVEAuth) valid() bool {
	return (a.TokenID != "" && a.TokenSecret != "") || (a.Username != "" && a.Password != "")
}

// SelectPVENode picks the node a new build VM should be cloned onto: the
// online node with the most free memory (ties broken by lower CPU load).
//
// Eligibility: when candidates is non-empty it is the allowed placement list —
// the operator asserts the template is usable from every listed node (shared
// storage, or a same-named template copy per node). When empty, placement is
// restricted to nodes that host a template named templateName, which is
// clone-safe regardless of storage type.
func SelectPVENode(endpoint string, auth PVEAuth, candidates []string, templateName string) (string, error) {
	if !auth.valid() {
		return "", fmt.Errorf("automatic node selection requires PVE credentials (API token or username/password)")
	}

	resources, err := fetchPVEClusterResources(endpoint, auth)
	if err != nil {
		return "", err
	}

	// Determine the eligible node set.
	eligible := make(map[string]bool)
	if len(candidates) > 0 {
		for _, c := range candidates {
			if c = strings.TrimSpace(c); c != "" {
				eligible[c] = true
			}
		}
	} else {
		for _, r := range resources {
			if (r.Type == "qemu") && r.Template == 1 && r.Name == templateName {
				eligible[r.Node] = true
			}
		}
		if len(eligible) == 0 {
			return "", fmt.Errorf("no node in the cluster hosts a template named %q (set CLOUD_PVE_NODES to name placement candidates explicitly)", templateName)
		}
	}

	// Pick the least-loaded online eligible node.
	var best *pveClusterResource
	for i := range resources {
		r := &resources[i]
		if r.Type != "node" || r.Status != "online" || !eligible[r.Node] {
			continue
		}
		if best == nil || nodeLessLoaded(r, best) {
			best = r
		}
	}
	if best == nil {
		return "", fmt.Errorf("no eligible PVE node is online (eligible: %s)", strings.Join(mapKeys(eligible), ", "))
	}
	return best.Node, nil
}

// PVENodeInfo summarizes one cluster node for connectivity tests and UI
// display.
type PVENodeInfo struct {
	Node        string  `json:"node"`
	Status      string  `json:"status"`
	FreeMemGB   float64 `json:"free_mem_gb"`
	CPULoad     float64 `json:"cpu_load"`
	HasTemplate bool    `json:"has_template"`
}

// PVEClusterNodes lists the cluster's nodes with load info. templateName, when
// non-empty, marks which nodes host a template of that name. Used by the
// dashboard's "test connection" flow to validate endpoint + credentials +
// template in one call.
func PVEClusterNodes(endpoint string, auth PVEAuth, templateName string) ([]PVENodeInfo, error) {
	if !auth.valid() {
		return nil, fmt.Errorf("PVE credentials are required (API token or username/password)")
	}
	resources, err := fetchPVEClusterResources(endpoint, auth)
	if err != nil {
		return nil, err
	}

	templateNodes := make(map[string]bool)
	for _, r := range resources {
		if r.Type == "qemu" && r.Template == 1 && r.Name == templateName {
			templateNodes[r.Node] = true
		}
	}

	var nodes []PVENodeInfo
	for _, r := range resources {
		if r.Type != "node" {
			continue
		}
		nodes = append(nodes, PVENodeInfo{
			Node:        r.Node,
			Status:      r.Status,
			FreeMemGB:   float64(r.MaxMem-r.Mem) / (1 << 30),
			CPULoad:     r.CPU,
			HasTemplate: templateNodes[r.Node],
		})
	}
	return nodes, nil
}

// nodeLessLoaded reports whether a is a better placement target than b:
// more free memory, ties broken by lower CPU load.
func nodeLessLoaded(a, b *pveClusterResource) bool {
	freeA, freeB := a.MaxMem-a.Mem, b.MaxMem-b.Mem
	if freeA != freeB {
		return freeA > freeB
	}
	return a.CPU < b.CPU
}

// pveHTTPClient builds the client used for direct PVE API calls.
func pveHTTPClient(insecure bool) *http.Client {
	client := &http.Client{Timeout: pveNodeQueryTimeout}
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- mirrors CLOUD_PVE_INSECURE, the operator's explicit opt-in for self-signed PVE certs.
		}
	}
	return client
}

// pveTicket exchanges username+password for an auth ticket (the PVE cookie
// auth flow — API tokens don't need this).
func pveTicket(endpoint string, auth PVEAuth) (string, error) {
	form := url.Values{}
	form.Set("username", auth.Username)
	form.Set("password", auth.Password)

	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(endpoint, "/")+"/api2/json/access/ticket",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build ticket request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := pveHTTPClient(auth.Insecure).Do(req)
	if err != nil {
		return "", fmt.Errorf("PVE ticket request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PVE authentication failed (status %d) — check username/password", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode ticket response: %w", err)
	}
	if payload.Data.Ticket == "" {
		return "", fmt.Errorf("PVE returned an empty auth ticket")
	}
	return payload.Data.Ticket, nil
}

// fetchPVEClusterResources reads the cluster resource list, authenticating
// with an API token when available, else a username/password ticket.
func fetchPVEClusterResources(endpoint string, auth PVEAuth) ([]pveClusterResource, error) {
	reqURL := strings.TrimRight(endpoint, "/") + "/api2/json/cluster/resources"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build cluster-resources request: %w", err)
	}

	if auth.TokenID != "" && auth.TokenSecret != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", auth.TokenID, auth.TokenSecret))
	} else {
		ticket, err := pveTicket(endpoint, auth)
		if err != nil {
			return nil, err
		}
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
	}

	resp, err := pveHTTPClient(auth.Insecure).Do(req)
	if err != nil {
		return nil, fmt.Errorf("query PVE cluster resources: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PVE cluster resources returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data []pveClusterResource `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode cluster resources: %w", err)
	}
	return payload.Data, nil
}

// WaitForPVEGuestIP polls the QEMU guest agent through the PVE API until the
// VM reports a usable IPv4 address. telmate rc04's default_ipv4_address is
// frequently empty right after apply (the agent hasn't reported yet), so the
// IP is resolved here instead of failing the deployment on an empty address.
func WaitForPVEGuestIP(endpoint string, auth PVEAuth, node string, vmid string, timeout time.Duration, sink func(string)) (string, error) {
	deadline := time.Now().Add(timeout)
	reqURL := fmt.Sprintf("%s/api2/json/nodes/%s/qemu/%s/agent/network-get-interfaces",
		strings.TrimRight(endpoint, "/"), node, vmid)

	var ticket string
	if auth.TokenID == "" || auth.TokenSecret == "" {
		t, err := pveTicket(endpoint, auth)
		if err != nil {
			return "", err
		}
		ticket = t
	}

	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		req, err := http.NewRequest(http.MethodGet, reqURL, nil)
		if err != nil {
			return "", err
		}
		if ticket != "" {
			req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		} else {
			req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", auth.TokenID, auth.TokenSecret))
		}

		resp, err := pveHTTPClient(auth.Insecure).Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			var payload struct {
				Data struct {
					Result []struct {
						Name string `json:"name"`
						IPs  []struct {
							Type    string `json:"ip-address-type"`
							Address string `json:"ip-address"`
						} `json:"ip-addresses"`
					} `json:"result"`
				} `json:"data"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
			_ = resp.Body.Close()
			if decodeErr == nil {
				for _, iface := range payload.Data.Result {
					if iface.Name == "lo" || strings.HasPrefix(iface.Name, "docker") {
						continue
					}
					for _, ip := range iface.IPs {
						if ip.Type == "ipv4" && ip.Address != "" && !strings.HasPrefix(ip.Address, "127.") {
							return ip.Address, nil
						}
					}
				}
			}
		} else if resp != nil {
			_ = resp.Body.Close()
		}

		if attempt%6 == 0 && sink != nil {
			sink(fmt.Sprintf("[provision] still waiting for the guest agent to report an IP (%ds)…", attempt*5))
		}
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("guest agent did not report an IPv4 address within %s (is qemu-guest-agent installed in the template?)", timeout)
}

// mapKeys returns the keys of a string set for error messages.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Package iac manages infrastructure provisioning using Terraform.
package iac

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CloudCredentials holds cloud provider credentials.
type CloudCredentials struct {
	// Aliyun
	AliyunAccessKey string
	AliyunSecretKey string

	// GCP
	GCPKeyFile string

	// AWS
	AWSAccessKey string
	AWSSecretKey string

	// PVE (Proxmox VE)
	PVETokenID     string
	PVETokenSecret string
	PVEUsername    string
	PVEPassword    string
}

// SSHConfig holds SSH configuration for instance setup.
type SSHConfig struct {
	KeyPath string
	User    string
	// KnownHostsPath, when set, is used for SSH host-key verification instead of
	// the insecure default. Ignored when InsecureHostKey is true.
	KnownHostsPath string
	// InsecureHostKey opts in to disabling SSH host-key verification
	// (StrictHostKeyChecking=no / UserKnownHostsFile=/dev/null). This is required
	// for freshly-created cloud instances whose host key is not yet known, but it
	// enables man-in-the-middle attacks, so it must be requested explicitly.
	InsecureHostKey bool
}

// Command timeouts prevent a hung terraform/ssh invocation from blocking a build
// worker forever. They are generous enough for real provisioning work.
const (
	terraformInitTimeout    = 10 * time.Minute
	terraformApplyTimeout   = 30 * time.Minute
	terraformDestroyTimeout = 30 * time.Minute
	terraformOutputTimeout  = 2 * time.Minute
	sshCommandTimeout       = 5 * time.Minute
)

// ProvisionRequest represents an infrastructure provisioning request.
type ProvisionRequest struct {
	Provider        string            `json:"provider"`
	Arch            string            `json:"arch"`
	Spec            map[string]string `json:"spec"`
	Credentials     *CloudCredentials `json:"-"`
	SSH             *SSHConfig        `json:"-"`
	ServerCallback  string            `json:"server_callback"`
	BuilderPort     int               `json:"builder_port"`
	BuilderToken    string            `json:"-"` // Shared secret the deployed builder requires
	BinpkgHost      string            `json:"binpkg_host"`
	AllowedIPRanges []string          `json:"allowed_ip_ranges"`
	TTL             time.Duration     `json:"ttl"` // Instance TTL, 0 uses default
}

// Instance represents a provisioned instance.
type Instance struct {
	ID              string            `json:"id"`
	Provider        string            `json:"provider"`
	Status          string            `json:"status"`
	IPAddress       string            `json:"ip_address"`
	PublicIP        string            `json:"public_ip"`
	PrivateIP       string            `json:"private_ip"`
	Arch            string            `json:"arch"`
	Metadata        map[string]string `json:"metadata"`
	TerraformDir    string            `json:"-"`
	SSHUser         string            `json:"ssh_user"`
	BuilderEndpoint string            `json:"builder_endpoint"`
	LastHeartbeat   time.Time         `json:"last_heartbeat"`
	CreatedAt       time.Time         `json:"created_at"`
	TTL             time.Duration     `json:"ttl"`           // Time to live, 0 means no auto-termination
	LastActivity    time.Time         `json:"last_activity"` // Last time the instance had activity
	ActiveTasks     int               `json:"active_tasks"`  // Number of active tasks on this instance
	// destroyEnv is the credential environment used to provision the instance;
	// Terminate reuses it so `terraform destroy` authenticates the same way as
	// apply did. Not serialized (contains secrets).
	destroyEnv []string
}

// Manager manages infrastructure provisioning using Terraform.
type Manager struct {
	instances       map[string]*Instance
	mu              sync.RWMutex
	workspaceDir    string
	defaultTTL      time.Duration
	stopChan        chan struct{}
	cleanupInterval time.Duration
}

// ManagerOption is a functional option for configuring the Manager.
type ManagerOption func(*Manager)

// WithDefaultTTL sets the default TTL for instances.
func WithDefaultTTL(ttl time.Duration) ManagerOption {
	return func(m *Manager) {
		m.defaultTTL = ttl
	}
}

// WithCleanupInterval sets the interval for checking and cleaning up expired instances.
func WithCleanupInterval(interval time.Duration) ManagerOption {
	return func(m *Manager) {
		m.cleanupInterval = interval
	}
}

// NewManager creates a new IaC manager.
func NewManager(opts ...ManagerOption) *Manager {
	workspaceDir := filepath.Join(os.TempDir(), "portage-terraform")
	_ = os.MkdirAll(workspaceDir, 0750)

	m := &Manager{
		instances:       make(map[string]*Instance),
		workspaceDir:    workspaceDir,
		defaultTTL:      60 * time.Minute, // Default 1 hour
		stopChan:        make(chan struct{}),
		cleanupInterval: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// StartCleanupRoutine starts the background cleanup routine for expired instances.
func (m *Manager) StartCleanupRoutine() {
	go m.cleanupRoutine()
}

// StopCleanupRoutine stops the background cleanup routine.
func (m *Manager) StopCleanupRoutine() {
	close(m.stopChan)
}

// cleanupRoutine periodically checks and terminates expired instances.
func (m *Manager) cleanupRoutine() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanupExpiredInstances()
		case <-m.stopChan:
			return
		}
	}
}

// cleanupExpiredInstances terminates instances that have exceeded their TTL without activity.
func (m *Manager) cleanupExpiredInstances() {
	m.mu.RLock()
	var expiredIDs []string
	now := time.Now()

	for id, inst := range m.instances {
		// Always retry instances whose destroy previously failed — they are
		// billing with no owner.
		if inst.Status == "destroy_failed" {
			expiredIDs = append(expiredIDs, id)
			continue
		}

		// Skip if TTL is 0 (no auto-termination)
		if inst.TTL == 0 {
			continue
		}

		// Skip if instance has active tasks
		if inst.ActiveTasks > 0 {
			continue
		}

		// Check if instance has exceeded TTL since last activity
		if now.Sub(inst.LastActivity) > inst.TTL {
			expiredIDs = append(expiredIDs, id)
		}
	}
	m.mu.RUnlock()

	// Terminate expired instances
	for _, id := range expiredIDs {
		fmt.Printf("Auto-terminating expired instance: %s\n", id)
		if err := m.Terminate(id); err != nil {
			fmt.Printf("Failed to terminate expired instance %s: %v\n", id, err)
		}
	}
}

// UpdateInstanceActivity updates the last activity time for an instance.
func (m *Manager) UpdateInstanceActivity(instanceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.instances[instanceID]; ok {
		inst.LastActivity = time.Now()
	}
}

// SetInstanceActiveTasks sets the number of active tasks for an instance.
func (m *Manager) SetInstanceActiveTasks(instanceID string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.instances[instanceID]; ok {
		inst.ActiveTasks = count
		if count > 0 {
			inst.LastActivity = time.Now()
		}
	}
}

// GetExpiredInstances returns a list of instances that have exceeded their TTL.
func (m *Manager) GetExpiredInstances() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var expired []*Instance
	now := time.Now()

	for _, inst := range m.instances {
		if inst.TTL == 0 {
			continue
		}
		if inst.ActiveTasks > 0 {
			continue
		}
		if now.Sub(inst.LastActivity) > inst.TTL {
			expired = append(expired, inst)
		}
	}

	return expired
}

// supportedProviders lists the providers Provision can fully and correctly
// provision. GCP and PVE are validated against live environments. AWS generates
// complete, valid Terraform (dynamic Ubuntu AMI, injected SSH key, arch-aware
// instance type, security group) but has NOT been validated against a live AWS
// account — treat it as beta. Aliyun remains a non-functional stub and is
// intentionally excluded so provisioning returns a clear error instead of
// creating an unusable instance.
var supportedProviders = map[string]bool{
	"gcp": true,
	"pve": true,
	"aws": true,
}

// Provision provisions a new instance using Terraform.
func (m *Manager) Provision(req *ProvisionRequest) (*Instance, error) {
	if !supportedProviders[req.Provider] {
		return nil, fmt.Errorf("provider %q not implemented", req.Provider)
	}

	instanceID := fmt.Sprintf("%s-%d", req.Provider, time.Now().UnixNano())
	terraformDir := filepath.Join(m.workspaceDir, instanceID)

	if err := os.MkdirAll(terraformDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create terraform directory: %w", err)
	}

	// Set defaults
	if req.BuilderPort == 0 {
		req.BuilderPort = 9090
	}
	if req.SSH == nil {
		req.SSH = &SSHConfig{
			User: "root",
		}
	}

	// Generate Terraform configuration with credentials
	tfConfig := m.generateTerraformConfig(req)
	tfFile := filepath.Join(terraformDir, "main.tf")
	if err := os.WriteFile(tfFile, []byte(tfConfig), 0600); err != nil {
		return nil, fmt.Errorf("failed to write terraform config: %w", err)
	}

	// Generate firewall rules
	firewallConfig := m.generateFirewallConfig(req)
	fwFile := filepath.Join(terraformDir, "firewall.tf")
	if err := os.WriteFile(fwFile, []byte(firewallConfig), 0600); err != nil {
		return nil, fmt.Errorf("failed to write firewall config: %w", err)
	}

	// Set environment variables for cloud credentials
	env := m.prepareEnvironment(req)

	// Determine TTL up front.
	ttl := req.TTL
	if ttl == 0 {
		ttl = m.defaultTTL
	}

	// Record the instance BEFORE apply completes, so that if apply partially
	// creates resources (VPC/subnet/instance) and then errors, cleanup can still
	// find the terraform dir and destroy it. destroyEnv carries the credentials
	// so a later destroy authenticates the same way apply did.
	now := time.Now()
	instance := &Instance{
		ID:            instanceID,
		Provider:      req.Provider,
		Status:        "provisioning",
		Arch:          req.Arch,
		Metadata:      req.Spec,
		TerraformDir:  terraformDir,
		SSHUser:       req.SSH.User,
		LastHeartbeat: now,
		CreatedAt:     now,
		TTL:           ttl,
		LastActivity:  now,
		destroyEnv:    env,
	}
	m.mu.Lock()
	m.instances[instanceID] = instance
	m.mu.Unlock()

	// Run Terraform init with a bounded timeout so a hung init cannot block the
	// build worker forever.
	initCtx, cancelInit := context.WithTimeout(context.Background(), terraformInitTimeout)
	errInit := m.runTerraformCommand(initCtx, terraformDir, env, "init")
	cancelInit()
	if errInit != nil {
		m.rollback(instance)
		return nil, fmt.Errorf("terraform init failed: %w", errInit)
	}

	// Run Terraform apply with a bounded timeout. On any error after this point,
	// roll back (destroy) so partially-created resources do not leak.
	applyCtx, cancelApply := context.WithTimeout(context.Background(), terraformApplyTimeout)
	errApply := m.runTerraformCommand(applyCtx, terraformDir, env, "apply", "-auto-approve")
	cancelApply()
	if errApply != nil {
		m.rollback(instance)
		return nil, fmt.Errorf("terraform apply failed: %w", errApply)
	}

	// Get outputs.
	ipAddress, err := m.getTerraformOutput(terraformDir, env, "ip_address")
	if err != nil {
		m.rollback(instance)
		return nil, fmt.Errorf("failed to get IP address: %w", err)
	}
	privateIP, _ := m.getTerraformOutput(terraformDir, env, "private_ip")

	m.mu.Lock()
	instance.IPAddress = ipAddress
	instance.PublicIP = ipAddress
	instance.PrivateIP = privateIP
	instance.BuilderEndpoint = fmt.Sprintf("http://%s:%d", ipAddress, req.BuilderPort)
	m.mu.Unlock()

	// Deploy builder software via SSH.
	if req.SSH.KeyPath != "" {
		if err := m.deployBuilder(instance, req); err != nil {
			// Deployment failed on a live, billed VM — destroy it rather than
			// leaving an orphan running.
			m.setInstanceStatus(instance, "deployment_failed")
			m.rollback(instance)
			return nil, fmt.Errorf("builder deployment failed: %w", err)
		}
	}

	m.setInstanceStatus(instance, "running")
	return instance, nil
}

// setInstanceStatus updates an instance's Status under the manager lock, so it
// does not race the cleanup goroutine's status reads.
func (m *Manager) setInstanceStatus(instance *Instance, status string) {
	m.mu.Lock()
	instance.Status = status
	m.mu.Unlock()
}

// rollback destroys a partially- or fully-provisioned instance and, on success,
// stops tracking it. If destroy fails the instance is kept (with its terraform
// dir and credential env) so the cleanup routine can retry it later.
func (m *Manager) rollback(instance *Instance) {
	if err := m.destroyInstance(instance); err != nil {
		fmt.Printf("Warning: rollback destroy failed for %s (will retry later): %v\n", instance.ID, err)
		m.mu.Lock()
		instance.Status = "destroy_failed"
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	delete(m.instances, instance.ID)
	m.mu.Unlock()
	_ = os.RemoveAll(instance.TerraformDir)
}

// destroyInstance runs `terraform destroy` for an instance using the credential
// environment captured at provision time. It returns an error if destroy fails
// so callers can decide whether to keep tracking the instance for a retry.
func (m *Manager) destroyInstance(instance *Instance) error {
	m.mu.RLock()
	env := instance.destroyEnv
	dir := instance.TerraformDir
	m.mu.RUnlock()

	// Bounded timeout so a hung destroy cannot block the cleanup routine forever.
	ctx, cancel := context.WithTimeout(context.Background(), terraformDestroyTimeout)
	defer cancel()
	if err := m.runTerraformCommand(ctx, dir, env, "destroy", "-auto-approve"); err != nil {
		return fmt.Errorf("terraform destroy failed: %w", err)
	}
	return nil
}

// Terminate destroys an instance. If destroy succeeds the instance is untracked
// and its terraform dir removed. If destroy FAILS the instance is kept (state
// and credentials intact) so the cleanup routine can retry — otherwise the VM
// would keep billing with no way left to destroy it.
func (m *Manager) Terminate(instanceID string) error {
	m.mu.RLock()
	instance, exists := m.instances[instanceID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	if err := m.destroyInstance(instance); err != nil {
		m.mu.Lock()
		instance.Status = "destroy_failed"
		m.mu.Unlock()
		fmt.Printf("Warning: %v (instance %s kept for retry)\n", err, instanceID)
		return err
	}

	// Destroy succeeded — clean up state and stop tracking.
	_ = os.RemoveAll(instance.TerraformDir)
	m.mu.Lock()
	delete(m.instances, instanceID)
	m.mu.Unlock()

	return nil
}

// GetInstance returns an instance by ID.
func (m *Manager) GetInstance(instanceID string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	return instance, nil
}

// ListInstances returns all active instances.
func (m *Manager) ListInstances() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instances := make([]*Instance, 0, len(m.instances))
	for _, instance := range m.instances {
		instances = append(instances, instance)
	}

	return instances
}

// getOrDefault retrieves a value from a map with a default fallback.
func getOrDefault(m map[string]string, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		return val
	}
	return defaultValue
}

// UpdateHeartbeat updates the last heartbeat time for an instance.
func (m *Manager) UpdateHeartbeat(instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	instance.LastHeartbeat = time.Now()
	instance.Status = "running"
	return nil
}

// CheckStaleInstances returns instances that haven't reported in a while.
func (m *Manager) CheckStaleInstances(timeout time.Duration) []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stale := make([]*Instance, 0)
	now := time.Now()

	for _, instance := range m.instances {
		if now.Sub(instance.LastHeartbeat) > timeout {
			stale = append(stale, instance)
		}
	}

	return stale
}

// runTerraformCommand executes a terraform command with the given arguments.
func (m *Manager) runTerraformCommand(ctx context.Context, dir string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform %s failed: %w\nstderr: %s", args[0], err, stderr.String())
	}

	return nil
}

// getTerraformOutput retrieves an output value from terraform.
func (m *Manager) getTerraformOutput(dir string, env []string, output string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), terraformOutputTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "terraform", "output", "-raw", output)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get output %s: %w", output, err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// prepareEnvironment prepares environment variables for terraform based on cloud provider.
func (m *Manager) prepareEnvironment(req *ProvisionRequest) []string {
	env := []string{}

	if req.Credentials == nil {
		return env
	}

	switch req.Provider {
	case "aliyun":
		if req.Credentials.AliyunAccessKey != "" {
			env = append(env, "ALICLOUD_ACCESS_KEY="+req.Credentials.AliyunAccessKey)
			env = append(env, "ALICLOUD_SECRET_KEY="+req.Credentials.AliyunSecretKey)
		}
	case "gcp":
		if req.Credentials.GCPKeyFile != "" {
			env = append(env, "GOOGLE_APPLICATION_CREDENTIALS="+req.Credentials.GCPKeyFile)
		}
	case "aws":
		if req.Credentials.AWSAccessKey != "" {
			env = append(env, "AWS_ACCESS_KEY_ID="+req.Credentials.AWSAccessKey)
			env = append(env, "AWS_SECRET_ACCESS_KEY="+req.Credentials.AWSSecretKey)
		}
	case "pve":
		if req.Credentials.PVETokenID != "" {
			env = append(env, "PM_API_TOKEN_ID="+req.Credentials.PVETokenID)
			env = append(env, "PM_API_TOKEN_SECRET="+req.Credentials.PVETokenSecret)
			// The generated main.tf references var.pve_token_secret so the secret
			// is not written to disk; supply its value here (finding #30).
			env = append(env, "TF_VAR_pve_token_secret="+req.Credentials.PVETokenSecret)
		} else if req.Credentials.PVEUsername != "" {
			env = append(env, "PM_USER="+req.Credentials.PVEUsername)
			env = append(env, "PM_PASS="+req.Credentials.PVEPassword)
			// The generated main.tf references var.pve_password (finding #30).
			env = append(env, "TF_VAR_pve_password="+req.Credentials.PVEPassword)
		}
	}

	return env
}

// deployBuilder deploys the builder software to the instance via SSH.
func (m *Manager) deployBuilder(instance *Instance, req *ProvisionRequest) error {
	// Wait for instance to be SSH-accessible
	if err := m.waitForSSH(instance, req.SSH, 5*time.Minute); err != nil {
		return fmt.Errorf("instance not accessible: %w", err)
	}

	// Create deployment script
	script := m.generateDeploymentScript(req)
	scriptPath := filepath.Join(instance.TerraformDir, "deploy.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return fmt.Errorf("failed to write deployment script: %w", err)
	}
	// Make script executable (owner-only; the exec bit is required to run it).
	if err := os.Chmod(scriptPath, 0700); err != nil { // #nosec G302 -- deploy script needs the owner execute bit.
		return fmt.Errorf("failed to make script executable: %w", err)
	}

	// Copy script to instance
	if err := m.sshCopyFile(instance, req.SSH, scriptPath, "/tmp/deploy.sh"); err != nil {
		return fmt.Errorf("failed to copy deployment script: %w", err)
	}

	// Execute deployment script
	if err := m.sshExecute(instance, req.SSH, "chmod +x /tmp/deploy.sh && /tmp/deploy.sh"); err != nil {
		return fmt.Errorf("failed to execute deployment script: %w", err)
	}

	return nil
}

// sshHostKeyArgs returns the ssh/scp options governing host-key verification.
//
// Security tradeoff (finding #50): for a freshly-created cloud instance we do
// not yet know the host key, so verification cannot succeed on the first
// connection. Rather than silently disabling verification we require the caller
// to opt in via SSHConfig.InsecureHostKey. When a KnownHostsPath is provided we
// use it for real verification instead. Disabling verification (the insecure
// path) exposes the connection to man-in-the-middle attacks and must only be
// used on trusted networks or for throwaway build instances.
func sshHostKeyArgs(cfg *SSHConfig) []string {
	if cfg != nil && cfg.KnownHostsPath != "" {
		return []string{
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=" + cfg.KnownHostsPath,
		}
	}
	if cfg != nil && cfg.InsecureHostKey {
		// Explicitly opted-in insecure mode. Enables MITM — see doc comment.
		return []string{
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
		}
	}
	// Default: rely on the user's known_hosts with strict checking. This fails
	// closed for an unknown host rather than trusting it blindly.
	return []string{
		"-o", "StrictHostKeyChecking=yes",
	}
}

// waitForSSH waits for SSH to become available on the instance.
func (m *Manager) waitForSSH(instance *Instance, cfg *SSHConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		err := m.sshExecute(instance, cfg, "echo ok")
		if err == nil {
			return nil
		}
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("SSH connection timeout")
}

// sshExecute executes a command on the instance via SSH.
func (m *Manager) sshExecute(instance *Instance, cfg *SSHConfig, command string) error {
	keyPath := ""
	if cfg != nil {
		keyPath = cfg.KeyPath
	}

	args := sshHostKeyArgs(cfg)
	args = append(args, "-o", "ConnectTimeout=10")

	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}

	args = append(args, fmt.Sprintf("%s@%s", instance.SSHUser, instance.PublicIP), command)

	// Bound each SSH invocation so a hung connection cannot block a build worker.
	ctx, cancel := context.WithTimeout(context.Background(), sshCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh command failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// sshCopyFile copies a file to the instance via SCP.
func (m *Manager) sshCopyFile(instance *Instance, cfg *SSHConfig, localPath, remotePath string) error {
	keyPath := ""
	if cfg != nil {
		keyPath = cfg.KeyPath
	}

	args := sshHostKeyArgs(cfg)

	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}

	args = append(args, localPath, fmt.Sprintf("%s@%s:%s", instance.SSHUser, instance.PublicIP, remotePath))

	// Bound each SCP invocation so a hung transfer cannot block a build worker.
	ctx, cancel := context.WithTimeout(context.Background(), sshCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// generateDeploymentScript generates a shell script to deploy the builder onto
// a provisioned instance.
func (m *Manager) generateDeploymentScript(req *ProvisionRequest) string {
	arch := req.Arch
	if arch == "" {
		arch = "amd64"
	}
	config := &CloudInitConfig{
		DockerImage:       "gentoo/stage3:latest",
		PullLatestImage:   true,
		PortageTreeSync:   true,
		PortageMirror:     "https://distfiles.gentoo.org",
		PortageBinpkgHost: req.BinpkgHost,
		BuilderPort:       req.BuilderPort,
		BuilderToken:      req.BuilderToken,
		ServerCallbackURL: req.ServerCallback,
		Architecture:      arch,
		DataDir:           "/var/lib/portage-engine",
		WorkDir:           "/var/tmp/portage-builds",
		ArtifactDir:       "/var/tmp/portage-artifacts",
		SwapSizeGB:        4,
		EnableFirewall:    true,
	}

	return GenerateCloudInitScript(config)
}

// generateTerraformConfig generates Terraform configuration based on provider.
func (m *Manager) generateTerraformConfig(req *ProvisionRequest) string {
	region := getOrDefault(req.Spec, "region", "us-central1")
	zone := getOrDefault(req.Spec, "zone", "")

	switch req.Provider {
	case "aliyun":
		return m.generateAliyunConfig(req, region, zone)
	case "gcp":
		return m.generateGCPConfig(req, region, zone)
	case "aws":
		return m.generateAWSConfig(req, region, zone)
	case "pve":
		return m.generatePVEConfig(req)
	default:
		return ""
	}
}

// generateFirewallConfig generates firewall rules for the instance.
func (m *Manager) generateFirewallConfig(req *ProvisionRequest) string {
	allowedIPs := req.AllowedIPRanges
	if len(allowedIPs) == 0 {
		allowedIPs = []string{"0.0.0.0/0"} // Warning: open to world
	}

	switch req.Provider {
	case "aliyun":
		return m.generateAliyunFirewall(req, allowedIPs)
	case "gcp":
		return m.generateGCPFirewall(req, allowedIPs)
	case "aws":
		return m.generateAWSFirewall(req, allowedIPs)
	case "pve":
		return "" // PVE uses Proxmox's built-in firewall, configured via API
	default:
		return ""
	}
}

// generateAliyunConfig generates Aliyun-specific Terraform config.
func (m *Manager) generateAliyunConfig(req *ProvisionRequest, region, zone string) string {
	if zone == "" {
		zone = region + "-a"
	}

	return fmt.Sprintf(`
terraform {
  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "~> 1.0"
    }
  }
}

provider "alicloud" {
  region = "%s"
}

resource "alicloud_vpc" "portage" {
  vpc_name   = "portage-vpc"
  cidr_block = "10.0.0.0/16"
}

resource "alicloud_vswitch" "portage" {
  vpc_id     = alicloud_vpc.portage.id
  cidr_block = "10.0.1.0/24"
  zone_id    = "%s"
}

resource "alicloud_instance" "portage_builder" {
  instance_name   = "portage-builder-%s"
  instance_type   = "ecs.c6.large"
  image_id        = "ubuntu_20_04_x64_20G_alibase_20210420.vhd"
  vswitch_id      = alicloud_vswitch.portage.id
  security_groups = [alicloud_security_group.portage.id]

  internet_max_bandwidth_out = 100
  system_disk_category       = "cloud_efficiency"
  system_disk_size          = 50

  tags = {
    Purpose = "PortageBuild"
    Arch    = "%s"
  }
}

output "ip_address" {
  value = alicloud_instance.portage_builder.public_ip
}

output "private_ip" {
  value = alicloud_instance.portage_builder.private_ip
}
`, region, zone, req.Arch, req.Arch)
}

// generateAliyunFirewall generates Aliyun security group rules.
//
// Alicloud's security_group_rule.cidr_ip accepts a single CIDR, so we emit one
// rule resource per allowed CIDR instead of a single rule with a comma-joined
// (invalid) cidr_ip.
func (m *Manager) generateAliyunFirewall(req *ProvisionRequest, allowedIPs []string) string {
	builderRules := ""
	for i, cidr := range allowedIPs {
		builderRules += fmt.Sprintf(`
resource "alicloud_security_group_rule" "builder_%d" {
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "%d/%d"
  security_group_id = alicloud_security_group.portage.id
  cidr_ip           = "%s"
}
`, i, req.BuilderPort, req.BuilderPort, cidr)
	}

	return fmt.Sprintf(`
resource "alicloud_security_group" "portage" {
  name   = "portage-builder-sg"
  vpc_id = alicloud_vpc.portage.id
}

resource "alicloud_security_group_rule" "ssh" {
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "22/22"
  security_group_id = alicloud_security_group.portage.id
  cidr_ip           = "0.0.0.0/0"
}
%s`, builderRules)
}

// generateGCPConfig generates GCP-specific Terraform config.
func (m *Manager) generateGCPConfig(req *ProvisionRequest, region, zone string) string {
	if zone == "" {
		zone = region + "-a"
	}

	// Create GCPInstanceSpec from request
	spec := GCPInstanceSpecFromMap(req.Spec)

	// Override with request values if empty in spec
	if spec.Region == "" || spec.Region == "us-central1" {
		spec.Region = region
	}
	if spec.Zone == "" || spec.Zone == "us-central1-a" {
		spec.Zone = zone
	}

	// Create GCP config
	gcpConfig := &GCPConfig{
		Project:     spec.Project,
		Region:      spec.Region,
		Zone:        spec.Zone,
		StateDir:    m.workspaceDir,
		BuilderPort: req.BuilderPort,
	}

	if req.SSH != nil {
		gcpConfig.SSHKeyPath = req.SSH.KeyPath
		gcpConfig.SSHUser = req.SSH.User
	}

	provisioner, err := NewGCPProvisioner(gcpConfig)
	if err != nil {
		// Fallback to basic config on error
		return m.generateBasicGCPConfig(req, region, zone)
	}

	instanceName := fmt.Sprintf("portage-builder-%s-%d", req.Arch, time.Now().Unix())
	return provisioner.GenerateMainTF(spec, instanceName)
}

// generateBasicGCPConfig generates basic GCP Terraform config (fallback).
func (m *Manager) generateBasicGCPConfig(req *ProvisionRequest, region, zone string) string {
	project := getOrDefault(req.Spec, "project", "portage-engine")

	return fmt.Sprintf(`
terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = "%s"
  region  = "%s"
}

resource "google_compute_instance" "portage_builder" {
  name         = "portage-builder-%s"
  machine_type = "n1-standard-4"
  zone         = "%s"

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 100
    }
  }

  network_interface {
    network = "default"
    access_config {}
  }

  tags = ["portage-builder", "allow-builder-%d"]

  metadata = {
    ssh-keys = "root:${file("~/.ssh/id_rsa.pub")}"
  }
}

output "ip_address" {
  value = google_compute_instance.portage_builder.network_interface[0].access_config[0].nat_ip
}

output "private_ip" {
  value = google_compute_instance.portage_builder.network_interface[0].network_ip
}
`, project, region, req.Arch, zone, req.BuilderPort)
}

// generateGCPFirewall generates GCP firewall rules.
func (m *Manager) generateGCPFirewall(req *ProvisionRequest, allowedIPs []string) string {
	gcpConfig := &GCPConfig{
		Project:         getOrDefault(req.Spec, "project", "portage-engine"),
		Region:          getOrDefault(req.Spec, "region", "us-central1"),
		Zone:            getOrDefault(req.Spec, "zone", "us-central1-a"),
		StateDir:        m.workspaceDir,
		BuilderPort:     req.BuilderPort,
		AllowedIPRanges: allowedIPs,
	}

	provisioner, err := NewGCPProvisioner(gcpConfig)
	if err != nil {
		// Fallback to basic firewall config
		return m.generateBasicGCPFirewall(req, allowedIPs)
	}

	instanceName := fmt.Sprintf("portage-builder-%s-%d", req.Arch, time.Now().Unix())
	return provisioner.GenerateFirewallTF(instanceName)
}

// generateBasicGCPFirewall generates basic GCP firewall rules (fallback).
func (m *Manager) generateBasicGCPFirewall(req *ProvisionRequest, allowedIPs []string) string {
	return fmt.Sprintf(`
resource "google_compute_firewall" "portage_ssh" {
  name    = "portage-builder-ssh-%d"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["portage-builder"]
}

resource "google_compute_firewall" "portage_builder" {
  name    = "portage-builder-port-%d"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["%d"]
  }

  source_ranges = ["%s"]
  target_tags   = ["allow-builder-%d"]
}
`, time.Now().Unix(), time.Now().Unix(), req.BuilderPort, strings.Join(allowedIPs, "\", \""), req.BuilderPort)
}

// awsInstanceTypeForArch returns a sensible default EC2 instance type for the
// requested build arch (Graviton for arm64, x86 otherwise). A caller-supplied
// spec["instance_type"] overrides this.
func awsInstanceTypeForArch(arch string) string {
	switch arch {
	case "arm64", "aarch64":
		return "t4g.large"
	default:
		return "t3.large"
	}
}

// awsAMIArchFilter maps a build arch to the EC2 AMI "architecture" filter value
// (x86_64 / arm64), which differs from the name-arch token below.
func awsAMIArchFilter(arch string) string {
	switch arch {
	case "arm64", "aarch64":
		return "arm64"
	default:
		return "x86_64"
	}
}

// awsAMINameArch maps a build arch to Canonical's AMI-name arch token
// (amd64 / arm64), used in the image name glob.
func awsAMINameArch(arch string) string {
	switch arch {
	case "arm64", "aarch64":
		return "arm64"
	default:
		return "amd64"
	}
}

// generateAWSConfig generates AWS-specific Terraform config.
//
// NOTE: this HCL is written to be valid (terraform validate passes) and to wire
// up everything the builder deploy needs — a region-agnostic Ubuntu AMI looked
// up via a data source (not a hardcoded, region-specific AMI), an injected SSH
// key pair so deployBuilder can connect, an arch-appropriate instance type, and
// the security group / networking. It has NOT been validated against a live AWS
// account, so real provisioning may still surface AMI/cloud-init/timing details
// that only a real run reveals.
func (m *Manager) generateAWSConfig(req *ProvisionRequest, region, zone string) string {
	if zone == "" {
		zone = region + "a"
	}

	instanceType := getOrDefault(req.Spec, "instance_type", awsInstanceTypeForArch(req.Arch))
	amiArch := awsAMIArchFilter(req.Arch)
	amiNameArch := awsAMINameArch(req.Arch)

	// SSH key injection: create an aws_key_pair from the configured public key
	// and attach it to the instance, so deployBuilder can SSH in. Without a key,
	// fall back to no key_name (the instance still boots but cannot be deployed
	// to — the caller is warned by the missing-SSH error path in the builder).
	keyPairResource := ""
	keyNameLine := ""
	if req.SSH != nil && req.SSH.KeyPath != "" {
		keyPairResource = fmt.Sprintf(`
resource "aws_key_pair" "portage" {
  key_name   = "portage-builder-%d"
  public_key = file("%s")
}
`, time.Now().UnixNano(), req.SSH.KeyPath+".pub")
		keyNameLine = "  key_name               = aws_key_pair.portage.key_name\n"
	}

	return fmt.Sprintf(`
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "%s"
}

# Latest Ubuntu 22.04 AMI for the target arch, resolved at apply time so the
# config is not tied to a single region's hardcoded AMI ID.
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-%s-server-*"]
  }
  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
  filter {
    name   = "architecture"
    values = ["%s"]
  }
}

resource "aws_vpc" "portage" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true

  tags = {
    Name = "portage-vpc"
  }
}

resource "aws_subnet" "portage" {
  vpc_id                  = aws_vpc.portage.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = "%s"
  map_public_ip_on_launch = true

  tags = {
    Name = "portage-subnet"
  }
}

resource "aws_internet_gateway" "portage" {
  vpc_id = aws_vpc.portage.id

  tags = {
    Name = "portage-igw"
  }
}

resource "aws_route_table" "portage" {
  vpc_id = aws_vpc.portage.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.portage.id
  }

  tags = {
    Name = "portage-rt"
  }
}

resource "aws_route_table_association" "portage" {
  subnet_id      = aws_subnet.portage.id
  route_table_id = aws_route_table.portage.id
}
%s
resource "aws_instance" "portage_builder" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = "%s"
  subnet_id              = aws_subnet.portage.id
  vpc_security_group_ids = [aws_security_group.portage.id]
%s
  root_block_device {
    volume_size = 50
    volume_type = "gp3"
  }

  tags = {
    Name    = "portage-builder-%s"
    Purpose = "PortageBuild"
    Arch    = "%s"
  }
}

output "ip_address" {
  value = aws_instance.portage_builder.public_ip
}

output "private_ip" {
  value = aws_instance.portage_builder.private_ip
}
`, region, amiNameArch, amiArch, zone, keyPairResource, instanceType, keyNameLine, req.Arch, req.Arch)
}

// generateAWSFirewall generates AWS security group rules.
func (m *Manager) generateAWSFirewall(req *ProvisionRequest, allowedIPs []string) string {
	ingressRules := ""
	for _, cidr := range allowedIPs {
		ingressRules += fmt.Sprintf(`
  ingress {
    from_port   = %d
    to_port     = %d
    protocol    = "tcp"
    cidr_blocks = ["%s"]
  }
`, req.BuilderPort, req.BuilderPort, cidr)
	}

	return fmt.Sprintf(`
resource "aws_security_group" "portage" {
  name        = "portage-builder-sg"
  description = "Security group for Portage Builder"
  vpc_id      = aws_vpc.portage.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

%s

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "portage-builder-sg"
  }
}
`, ingressRules)
}

// generatePVEConfig generates PVE-specific Terraform config.
func (m *Manager) generatePVEConfig(req *ProvisionRequest) string {
	spec := PVEInstanceSpecFromMap(req.Spec)

	endpoint := getOrDefault(req.Spec, "endpoint", "")
	if endpoint == "" {
		return ""
	}

	// Create PVE config
	pveConfig := &PVEConfig{
		Endpoint:    endpoint,
		Node:        spec.Node,
		StateDir:    m.workspaceDir,
		BuilderPort: req.BuilderPort,
		Insecure:    getOrDefault(req.Spec, "insecure", "false") == "true",
	}

	// Set authentication
	if req.Credentials != nil {
		if req.Credentials.PVETokenID != "" {
			pveConfig.TokenID = req.Credentials.PVETokenID
			pveConfig.TokenSecret = req.Credentials.PVETokenSecret
		} else if req.Credentials.PVEUsername != "" {
			pveConfig.Username = req.Credentials.PVEUsername
			pveConfig.Password = req.Credentials.PVEPassword
		}
	}

	if req.SSH != nil {
		pveConfig.SSHKeyPath = req.SSH.KeyPath
		pveConfig.SSHUser = req.SSH.User
	}

	provisioner, err := NewPVEProvisioner(pveConfig)
	if err != nil {
		return ""
	}

	instanceName := fmt.Sprintf("portage-builder-%s-%d", req.Arch, time.Now().Unix())
	return provisioner.GenerateMainTF(spec, instanceName)
}

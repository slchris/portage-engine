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
}

// SSHConfig holds SSH configuration for instance setup.
type SSHConfig struct {
	KeyPath string
	User    string
}

// ProvisionRequest represents an infrastructure provisioning request.
type ProvisionRequest struct {
	Provider        string            `json:"provider"`
	Arch            string            `json:"arch"`
	Spec            map[string]string `json:"spec"`
	Credentials     *CloudCredentials `json:"-"`
	SSH             *SSHConfig        `json:"-"`
	ServerCallback  string            `json:"server_callback"`
	BuilderPort     int               `json:"builder_port"`
	AllowedIPRanges []string          `json:"allowed_ip_ranges"`
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
}

// Manager manages infrastructure provisioning using Terraform.
type Manager struct {
	instances    map[string]*Instance
	mu           sync.RWMutex
	workspaceDir string
}

// NewManager creates a new IaC manager.
func NewManager() *Manager {
	workspaceDir := filepath.Join(os.TempDir(), "portage-terraform")
	_ = os.MkdirAll(workspaceDir, 0750)

	return &Manager{
		instances:    make(map[string]*Instance),
		workspaceDir: workspaceDir,
	}
}

// Provision provisions a new instance using Terraform.
func (m *Manager) Provision(req *ProvisionRequest) (*Instance, error) {
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

	// Run Terraform init
	if err := m.runTerraformCommand(context.Background(), terraformDir, env, "init"); err != nil {
		return nil, fmt.Errorf("terraform init failed: %w", err)
	}

	// Run Terraform apply
	if err := m.runTerraformCommand(context.Background(), terraformDir, env, "apply", "-auto-approve"); err != nil {
		return nil, fmt.Errorf("terraform apply failed: %w", err)
	}

	// Get outputs
	ipAddress, err := m.getTerraformOutput(terraformDir, env, "ip_address")
	if err != nil {
		return nil, fmt.Errorf("failed to get IP address: %w", err)
	}

	privateIP, _ := m.getTerraformOutput(terraformDir, env, "private_ip")

	instance := &Instance{
		ID:              instanceID,
		Provider:        req.Provider,
		Status:          "provisioning",
		IPAddress:       ipAddress,
		PublicIP:        ipAddress,
		PrivateIP:       privateIP,
		Arch:            req.Arch,
		Metadata:        req.Spec,
		TerraformDir:    terraformDir,
		SSHUser:         req.SSH.User,
		BuilderEndpoint: fmt.Sprintf("http://%s:%d", ipAddress, req.BuilderPort),
		LastHeartbeat:   time.Now(),
	}

	m.mu.Lock()
	m.instances[instanceID] = instance
	m.mu.Unlock()

	// Deploy builder software via SSH
	if req.SSH.KeyPath != "" {
		if err := m.deployBuilder(instance, req); err != nil {
			instance.Status = "deployment_failed"
			return instance, fmt.Errorf("builder deployment failed: %w", err)
		}
	}

	instance.Status = "running"
	return instance, nil
}

// Terminate terminates an instance using Terraform destroy.
func (m *Manager) Terminate(instanceID string) error {
	m.mu.RLock()
	instance, exists := m.instances[instanceID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	// Run terraform destroy
	env := []string{}
	ctx := context.Background()
	if err := m.runTerraformCommand(ctx, instance.TerraformDir, env, "destroy", "-auto-approve"); err != nil {
		fmt.Printf("Warning: terraform destroy failed: %v\n", err)
	}

	// Cleanup terraform directory
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
	cmd := exec.Command("terraform", "output", "-raw", output)
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
	}

	return env
}

// deployBuilder deploys the builder software to the instance via SSH.
func (m *Manager) deployBuilder(instance *Instance, req *ProvisionRequest) error {
	// Wait for instance to be SSH-accessible
	if err := m.waitForSSH(instance, req.SSH.KeyPath, 5*time.Minute); err != nil {
		return fmt.Errorf("instance not accessible: %w", err)
	}

	// Create deployment script
	script := m.generateDeploymentScript(req.ServerCallback, req.BuilderPort)
	scriptPath := filepath.Join(instance.TerraformDir, "deploy.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return fmt.Errorf("failed to write deployment script: %w", err)
	}

	// Copy script to instance
	if err := m.sshCopyFile(instance, req.SSH.KeyPath, scriptPath, "/tmp/deploy.sh"); err != nil {
		return fmt.Errorf("failed to copy deployment script: %w", err)
	}

	// Execute deployment script
	if err := m.sshExecute(instance, req.SSH.KeyPath, "chmod +x /tmp/deploy.sh && /tmp/deploy.sh"); err != nil {
		return fmt.Errorf("failed to execute deployment script: %w", err)
	}

	return nil
}

// waitForSSH waits for SSH to become available on the instance.
func (m *Manager) waitForSSH(instance *Instance, keyPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		err := m.sshExecute(instance, keyPath, "echo ok")
		if err == nil {
			return nil
		}
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("SSH connection timeout")
}

// sshExecute executes a command on the instance via SSH.
func (m *Manager) sshExecute(instance *Instance, keyPath, command string) error {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
	}

	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}

	args = append(args, fmt.Sprintf("%s@%s", instance.SSHUser, instance.PublicIP), command)

	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh command failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// sshCopyFile copies a file to the instance via SCP.
func (m *Manager) sshCopyFile(instance *Instance, keyPath, localPath, remotePath string) error {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}

	args = append(args, localPath, fmt.Sprintf("%s@%s:%s", instance.SSHUser, instance.PublicIP, remotePath))

	cmd := exec.Command("scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// generateDeploymentScript generates a shell script to deploy the builder.
func (m *Manager) generateDeploymentScript(serverCallback string, builderPort int) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

# Install dependencies (example for Gentoo)
if command -v emerge &> /dev/null; then
    emerge --sync || true
    emerge -u dev-lang/go || true
fi

# Create portage-builder directory
mkdir -p /opt/portage-builder
cd /opt/portage-builder

# Download or build portage-builder binary
# In production, this would download from a release URL
# For now, we assume it's pre-installed or deployed separately

# Create systemd service
cat > /etc/systemd/system/portage-builder.service <<EOF
[Unit]
Description=Portage Builder Service
After=network.target

[Service]
Type=simple
ExecStart=/opt/portage-builder/portage-builder
Restart=always
RestartSec=10
Environment="SERVER_URL=%s"
Environment="BUILDER_PORT=%d"

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
systemctl daemon-reload
systemctl enable portage-builder
systemctl start portage-builder

echo "Builder deployment complete"
`, serverCallback, builderPort)
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
func (m *Manager) generateAliyunFirewall(req *ProvisionRequest, allowedIPs []string) string {
	rules := ""
	for _, cidr := range allowedIPs {
		rules += fmt.Sprintf(`
  ingress {
    from_port   = %d
    to_port     = %d
    ip_protocol = "tcp"
    cidr_ip     = "%s"
  }
`, req.BuilderPort, req.BuilderPort, cidr)
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

resource "alicloud_security_group_rule" "builder" {
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "%d/%d"
  security_group_id = alicloud_security_group.portage.id
  cidr_ip           = "%s"
}
`, req.BuilderPort, req.BuilderPort, strings.Join(allowedIPs, ","))
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

// generateAWSConfig generates AWS-specific Terraform config.
func (m *Manager) generateAWSConfig(req *ProvisionRequest, region, zone string) string {
	if zone == "" {
		zone = region + "a"
	}

	return fmt.Sprintf(`
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 4.0"
    }
  }
}

provider "aws" {
  region = "%s"
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

resource "aws_instance" "portage_builder" {
  ami                    = "ami-0c55b159cbfafe1f0"
  instance_type          = "t3.large"
  subnet_id              = aws_subnet.portage.id
  vpc_security_group_ids = [aws_security_group.portage.id]

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
`, region, zone, req.Arch, req.Arch)
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

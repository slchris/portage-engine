// Package iac manages infrastructure provisioning using Terraform.
package iac

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PVEInstanceSpec defines the specification for a Proxmox VE instance.
type PVEInstanceSpec struct {
	Node        string   `json:"node"`      // PVE node name
	VMID        int      `json:"vmid"`      // VM ID (0 = auto-assign)
	Name        string   `json:"name"`      // VM name
	Cores       int      `json:"cores"`     // CPU cores
	Sockets     int      `json:"sockets"`   // CPU sockets
	MemoryMB    int      `json:"memory_mb"` // Memory in MB
	DiskSizeGB  int      `json:"disk_size_gb"`
	DiskType    string   `json:"disk_type"`  // scsi, virtio, ide
	Storage     string   `json:"storage"`    // Storage pool name
	Template    string   `json:"template"`   // Template/Clone source
	OSType      string   `json:"os_type"`    // l26 (Linux 2.6+), win10, etc.
	Network     string   `json:"network"`    // Network bridge (e.g., vmbr0)
	VLAN        int      `json:"vlan"`       // VLAN tag (0 = no VLAN)
	IPConfig    string   `json:"ip_config"`  // IP configuration (dhcp or static)
	Gateway     string   `json:"gateway"`    // Gateway for static IP
	Nameserver  string   `json:"nameserver"` // DNS server
	SSHKeys     string   `json:"ssh_keys"`   // SSH public keys
	Tags        []string `json:"tags"`       // PVE tags
	Pool        string   `json:"pool"`       // PVE resource pool
	StartOnBoot bool     `json:"start_onboot"`
	Agent       bool     `json:"agent"`      // Enable QEMU guest agent
	CloudInit   bool     `json:"cloud_init"` // Use cloud-init
}

// PVEConfig holds PVE-specific configuration for IaC.
type PVEConfig struct {
	Endpoint          string   `json:"endpoint"`          // PVE API endpoint (e.g., https://pve.example.com:8006)
	Node              string   `json:"node"`              // Default PVE node
	TokenID           string   `json:"token_id"`          // API token ID (user@realm!tokenname)
	TokenSecret       string   `json:"token_secret"`      // API token secret
	Username          string   `json:"username"`          // Alternative: username for password auth
	Password          string   `json:"password"`          // Alternative: password for password auth
	Insecure          bool     `json:"insecure"`          // Skip TLS verification
	StateDir          string   `json:"state_dir"`         // Terraform state directory
	SSHKeyPath        string   `json:"ssh_key_path"`      // SSH private key path
	SSHUser           string   `json:"ssh_user"`          // SSH user
	Storage           string   `json:"storage"`           // Default storage pool
	Network           string   `json:"network"`           // Default network bridge
	Template          string   `json:"template"`          // Default VM template
	AllowedIPRanges   []string `json:"allowed_ip_ranges"` // Allowed IP ranges for firewall
	BuilderPort       int      `json:"builder_port"`      // Builder service port
	ServerCallbackURL string   `json:"server_callback_url"`
	InstanceTTL       int      `json:"instance_ttl"` // TTL in minutes
}

// DefaultPVEInstanceSpec returns the default PVE instance specification.
func DefaultPVEInstanceSpec() *PVEInstanceSpec {
	return &PVEInstanceSpec{
		Node:        "pve",
		VMID:        0, // Auto-assign
		Name:        "portage-builder",
		Cores:       4,
		Sockets:     1,
		MemoryMB:    8192,
		DiskSizeGB:  50,
		DiskType:    "scsi",
		Storage:     "local-lvm",
		Template:    "local:vztmpl/debian-12-standard_12.2-1_amd64.tar.zst",
		OSType:      "l26",
		Network:     "vmbr0",
		VLAN:        0,
		IPConfig:    "dhcp",
		Tags:        []string{"portage-builder"},
		StartOnBoot: true,
		Agent:       true,
		CloudInit:   true,
	}
}

// PVEInstanceSpecFromMap creates a PVEInstanceSpec from a map.
func PVEInstanceSpecFromMap(m map[string]string) *PVEInstanceSpec {
	spec := DefaultPVEInstanceSpec()

	setStringField := func(key string, target *string) {
		if v, ok := m[key]; ok && v != "" {
			*target = v
		}
	}

	setIntField := func(key string, target *int) {
		if v, ok := m[key]; ok && v != "" {
			_, _ = fmt.Sscanf(v, "%d", target)
		}
	}

	setBoolField := func(key string, target *bool) {
		if v, ok := m[key]; ok {
			*target = v == "true" || v == "1"
		}
	}

	setStringField("node", &spec.Node)
	setIntField("vmid", &spec.VMID)
	setStringField("name", &spec.Name)
	setIntField("cores", &spec.Cores)
	setIntField("sockets", &spec.Sockets)
	setIntField("memory_mb", &spec.MemoryMB)
	setIntField("disk_size_gb", &spec.DiskSizeGB)
	setStringField("disk_type", &spec.DiskType)
	setStringField("storage", &spec.Storage)
	setStringField("template", &spec.Template)
	setStringField("os_type", &spec.OSType)
	setStringField("network", &spec.Network)
	setIntField("vlan", &spec.VLAN)
	setStringField("ip_config", &spec.IPConfig)
	setStringField("gateway", &spec.Gateway)
	setStringField("nameserver", &spec.Nameserver)
	setStringField("ssh_keys", &spec.SSHKeys)
	setStringField("pool", &spec.Pool)
	setBoolField("cloud_init", &spec.CloudInit)
	setBoolField("agent", &spec.Agent)

	return spec
}

// PVEProvisioner handles PVE-specific infrastructure provisioning.
type PVEProvisioner struct {
	config   *PVEConfig
	stateDir string
}

// NewPVEProvisioner creates a new PVE provisioner.
func NewPVEProvisioner(config *PVEConfig) (*PVEProvisioner, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if config.Endpoint == "" {
		return nil, fmt.Errorf("PVE endpoint is required")
	}

	stateDir := config.StateDir
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "portage-terraform", "pve")
	}

	if err := os.MkdirAll(stateDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	return &PVEProvisioner{
		config:   config,
		stateDir: stateDir,
	}, nil
}

// GenerateMainTF generates the main.tf file for PVE.
func (p *PVEProvisioner) GenerateMainTF(spec *PVEInstanceSpec, instanceName string) string {
	if spec == nil {
		spec = DefaultPVEInstanceSpec()
	}

	// Override with config values if config has them set
	if p.config.Node != "" {
		spec.Node = p.config.Node
	}
	if p.config.Storage != "" {
		spec.Storage = p.config.Storage
	}
	if p.config.Network != "" {
		spec.Network = p.config.Network
	}
	if p.config.Template != "" {
		spec.Template = p.config.Template
	}

	tags := spec.Tags
	if len(tags) == 0 {
		tags = []string{"portage-builder"}
	}
	tagsStr := strings.Join(tags, ",")

	// Build network configuration
	networkConfig := p.generateNetworkConfig(spec)

	// Build cloud-init configuration
	cloudInitConfig := ""
	if spec.CloudInit {
		cloudInitConfig = p.generateCloudInitConfig(spec, instanceName)
	}

	// SSH keys block
	sshKeysBlock := ""
	if p.config.SSHKeyPath != "" {
		sshKeysBlock = fmt.Sprintf(`
  ssh_keys = file("%s.pub")`, p.config.SSHKeyPath)
	} else if spec.SSHKeys != "" {
		sshKeysBlock = fmt.Sprintf(`
  ssh_keys = <<-EOF
%s
EOF`, spec.SSHKeys)
	}

	// Pool configuration
	poolBlock := ""
	if spec.Pool != "" {
		poolBlock = fmt.Sprintf(`
  pool = "%s"`, spec.Pool)
	}

	// VMID configuration
	vmidBlock := ""
	if spec.VMID > 0 {
		vmidBlock = fmt.Sprintf(`
  vmid = %d`, spec.VMID)
	}

	// Determine auth method
	authBlock := p.generateAuthBlock()

	return fmt.Sprintf(`# Generated by Portage Engine IaC
# Instance: %s
# Generated at: %s

terraform {
  required_version = ">= 1.0.0"

  required_providers {
    proxmox = {
      source  = "telmate/proxmox"
      version = "~> 3.0"
    }
  }

  backend "local" {
    path = "terraform.tfstate"
  }
}

provider "proxmox" {
  pm_api_url      = "%s/api2/json"
  pm_tls_insecure = %t
%s
}

resource "proxmox_vm_qemu" "portage_builder" {
  name        = "%s"
  target_node = "%s"
%s%s
  cores       = %d
  sockets     = %d
  memory      = %d
  agent       = %d
  onboot      = %t
  os_type     = "%s"

  clone       = "%s"
  full_clone  = true

  disk {
    storage = "%s"
    size    = "%dG"
    type    = "%s"
  }

%s
%s
%s

  tags = "%s"

  lifecycle {
    ignore_changes = [
      network,
      disk,
    ]
  }
}

output "instance_name" {
  value = proxmox_vm_qemu.portage_builder.name
}

output "vmid" {
  value = proxmox_vm_qemu.portage_builder.vmid
}

output "ip_address" {
  value = proxmox_vm_qemu.portage_builder.default_ipv4_address
}

output "node" {
  value = proxmox_vm_qemu.portage_builder.target_node
}
`,
		instanceName,
		time.Now().Format(time.RFC3339),
		p.config.Endpoint,
		p.config.Insecure,
		authBlock,
		instanceName,
		spec.Node,
		vmidBlock,
		poolBlock,
		spec.Cores,
		spec.Sockets,
		spec.MemoryMB,
		boolToInt(spec.Agent),
		spec.StartOnBoot,
		spec.OSType,
		spec.Template,
		spec.Storage,
		spec.DiskSizeGB,
		spec.DiskType,
		networkConfig,
		cloudInitConfig,
		sshKeysBlock,
		tagsStr,
	)
}

// generateAuthBlock generates the authentication block for the provider.
func (p *PVEProvisioner) generateAuthBlock() string {
	if p.config.TokenID != "" && p.config.TokenSecret != "" {
		return fmt.Sprintf(`  pm_api_token_id     = "%s"
  pm_api_token_secret = "%s"`, p.config.TokenID, p.config.TokenSecret)
	}
	if p.config.Username != "" && p.config.Password != "" {
		return fmt.Sprintf(`  pm_user     = "%s"
  pm_password = "%s"`, p.config.Username, p.config.Password)
	}
	return ""
}

// generateNetworkConfig generates the network configuration block.
func (p *PVEProvisioner) generateNetworkConfig(spec *PVEInstanceSpec) string {
	vlanTag := ""
	if spec.VLAN > 0 {
		vlanTag = fmt.Sprintf(`
    tag = %d`, spec.VLAN)
	}

	return fmt.Sprintf(`  network {
    model  = "virtio"
    bridge = "%s"%s
  }`, spec.Network, vlanTag)
}

// generateCloudInitConfig generates cloud-init configuration.
func (p *PVEProvisioner) generateCloudInitConfig(spec *PVEInstanceSpec, _ string) string {
	ipConfigBlock := ""
	if spec.IPConfig == "dhcp" {
		ipConfigBlock = `  ipconfig0 = "ip=dhcp"`
	} else if spec.IPConfig != "" {
		gateway := ""
		if spec.Gateway != "" {
			gateway = fmt.Sprintf(",gw=%s", spec.Gateway)
		}
		ipConfigBlock = fmt.Sprintf(`  ipconfig0 = "ip=%s%s"`, spec.IPConfig, gateway)
	}

	nameserverBlock := ""
	if spec.Nameserver != "" {
		nameserverBlock = fmt.Sprintf(`
  nameserver = "%s"`, spec.Nameserver)
	}

	ciuserBlock := ""
	if p.config.SSHUser != "" {
		ciuserBlock = fmt.Sprintf(`
  ciuser = "%s"`, p.config.SSHUser)
	}

	return fmt.Sprintf(`%s%s%s`, ipConfigBlock, nameserverBlock, ciuserBlock)
}

// GenerateVariablesTF generates variables.tf for PVE.
func (p *PVEProvisioner) GenerateVariablesTF(spec *PVEInstanceSpec) string {
	if spec == nil {
		spec = DefaultPVEInstanceSpec()
	}

	return fmt.Sprintf(`# Variables for Portage Builder PVE Infrastructure

variable "pve_endpoint" {
  description = "Proxmox VE API endpoint"
  type        = string
  default     = "%s"
}

variable "pve_node" {
  description = "Proxmox VE node name"
  type        = string
  default     = "%s"
}

variable "vm_cores" {
  description = "Number of CPU cores"
  type        = number
  default     = %d
}

variable "vm_memory" {
  description = "Memory in MB"
  type        = number
  default     = %d
}

variable "vm_disk_size" {
  description = "Disk size in GB"
  type        = number
  default     = %d
}

variable "vm_storage" {
  description = "Storage pool name"
  type        = string
  default     = "%s"
}

variable "vm_network" {
  description = "Network bridge"
  type        = string
  default     = "%s"
}
`,
		p.config.Endpoint,
		spec.Node,
		spec.Cores,
		spec.MemoryMB,
		spec.DiskSizeGB,
		spec.Storage,
		spec.Network,
	)
}

// boolToInt converts a boolean to an integer (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

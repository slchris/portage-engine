package config

// CloudSettings is the runtime-adjustable subset of ServerConfig that drives
// on-demand cloud builders. It can be edited through the dashboard's Settings
// page (persisted by the server to DATA_DIR/cloud-settings.json, which
// overrides the static conf/env values at startup). The static config file
// remains the source of initial defaults.
type CloudSettings struct {
	Provider string `json:"provider"` // gcp | aws | pve

	// Static remote builders (dispatch targets). Empty = provision on demand.
	RemoteBuilders []string `json:"remote_builders,omitempty"`

	// GCP
	GCPProject string `json:"gcp_project"`
	GCPRegion  string `json:"gcp_region"`
	GCPZone    string `json:"gcp_zone"`
	GCPKeyFile string `json:"gcp_key_file"`

	// AWS
	AWSRegion    string `json:"aws_region"`
	AWSZone      string `json:"aws_zone"`
	AWSAccessKey string `json:"aws_access_key"`
	AWSSecretKey string `json:"aws_secret_key,omitempty"`

	// PVE (Proxmox VE)
	PVEEndpoint    string   `json:"pve_endpoint"`
	PVENode        string   `json:"pve_node"` // node name, or "auto"
	PVENodes       []string `json:"pve_nodes,omitempty"`
	PVETokenID     string   `json:"pve_token_id"`
	PVETokenSecret string   `json:"pve_token_secret,omitempty"`
	PVEUsername    string   `json:"pve_username"`
	PVEPassword    string   `json:"pve_password,omitempty"`
	PVEInsecure    bool     `json:"pve_insecure"`
	PVEStorage     string   `json:"pve_storage"`
	PVENetwork     string   `json:"pve_network"`
	PVETemplate    string   `json:"pve_template"`
	PVECICustom    string   `json:"pve_cicustom"`
	// PVENameserver is pushed to build VMs via cloud-init so internal domains
	// (registry/mirror hosts) resolve; DHCP-provided DNS often lacks the zone.
	PVENameserver string `json:"pve_nameserver"`

	// SSH deployment
	SSHKeyPath         string `json:"ssh_key_path"`
	SSHUser            string `json:"ssh_user"`
	SSHKnownHosts      string `json:"ssh_known_hosts,omitempty"`
	SSHInsecureHostKey bool   `json:"ssh_insecure_host_key"`

	// Mirror acceleration for build instances (all optional)
	AptMirror            string `json:"apt_mirror"`
	DockerDownloadMirror string `json:"docker_download_mirror"`
	DockerRegistryMirror string `json:"docker_registry_mirror"`
	GentooMirror         string `json:"gentoo_mirror"`
	PortageSyncURI       string `json:"portage_sync_uri"`
	// PortageSyncMethod selects how build instances fetch the portage tree:
	// "webrsync" (snapshot tarball via GENTOO_MIRRORS, default) or "rsync"
	// (incremental via PortageSyncURI).
	PortageSyncMethod string `json:"portage_sync_method"`

	// MakeConfExtra is appended to the generated make.conf on build instances
	// (global USE, ACCEPT_LICENSE, FEATURES, ...).
	MakeConfExtra string `json:"make_conf_extra"`

	// BuildFeatures is appended to the build container's make.conf FEATURES.
	// Docker builds need "-userpriv -usersandbox"; a native Gentoo VM leaves it
	// empty. Empty in the settings payload falls back to the container default.
	BuildFeatures string `json:"build_features"`

	// BuildMode selects the build environment: "" or "docker" = Debian+Docker
	// container builds; "native-gentoo" = native build on a Gentoo VM cloned
	// from the Gentoo cloud-init template (UEFI; in-emerge signing works).
	BuildMode string `json:"build_mode"`

	// SignBinpkgs enables in-emerge gpkg signing on the builder. Portage's
	// post-sign self-verification requires getuto's CA to certify the signing
	// key, which is not achievable inside the Docker build container — enable
	// this only on native Gentoo VM builders. When off, packages are built
	// unsigned; the signing pubkey is still distributed and install
	// verification still imports it (it just does not require a signature).
	SignBinpkgs bool `json:"sign_binpkgs"`

	// Reachability / delivery
	ServerCallbackURL string `json:"server_callback_url"`
	BuilderBinaryPath string `json:"builder_binary_path,omitempty"`
	BuilderBinaryURL  string `json:"builder_binary_url,omitempty"`

	InstanceTTLMinutes int `json:"instance_ttl_minutes"`

	// DockerImage is the build container image pulled on fresh instances
	// (default gentoo/stage3:latest).
	DockerImage string `json:"docker_image"`

	// SkipVerifyInstall disables the post-build install verification stage
	// (a pristine container installing the fresh binpkg from the binhost).
	// Verification is ON by default; this is the explicit opt-out.
	SkipVerifyInstall bool `json:"skip_verify_install"`

	// Artifact upload: when UploadURL is set, freshly built packages (plus the
	// Packages index and the signing pubkey) are pushed to the internal
	// mirror's artifact API, and install verification uses the mirror's public
	// URL as the binhost.
	UploadURL      string `json:"upload_url"`
	UploadUser     string `json:"upload_user"`
	UploadPassword string `json:"upload_password,omitempty"`
	UploadDir      string `json:"upload_dir"`
}

// CloudSettingsFromServerConfig extracts the runtime-adjustable cloud settings
// from a loaded server configuration (the startup defaults).
func CloudSettingsFromServerConfig(cfg *ServerConfig) *CloudSettings {
	return &CloudSettings{
		Provider:           cfg.CloudProvider,
		RemoteBuilders:     cfg.RemoteBuilders,
		GCPProject:         cfg.CloudGCPProject,
		GCPRegion:          cfg.CloudGCPRegion,
		GCPZone:            cfg.CloudGCPZone,
		GCPKeyFile:         cfg.CloudGCPKeyFile,
		AWSRegion:          cfg.CloudAWSRegion,
		AWSZone:            cfg.CloudAWSZone,
		AWSAccessKey:       cfg.CloudAWSAccessKey,
		AWSSecretKey:       cfg.CloudAWSSecretKey,
		PVEEndpoint:        cfg.CloudPVEEndpoint,
		PVENode:            cfg.CloudPVENode,
		PVENodes:           cfg.CloudPVENodes,
		PVETokenID:         cfg.CloudPVETokenID,
		PVETokenSecret:     cfg.CloudPVETokenSecret,
		PVEInsecure:        cfg.CloudPVEInsecure,
		PVEStorage:         cfg.CloudPVEStorage,
		PVENetwork:         cfg.CloudPVENetwork,
		PVETemplate:        cfg.CloudPVETemplate,
		SSHKeyPath:         cfg.CloudSSHKeyPath,
		SSHUser:            cfg.CloudSSHUser,
		SSHKnownHosts:      cfg.CloudSSHKnownHosts,
		SSHInsecureHostKey: cfg.CloudSSHInsecureHostKey,
		ServerCallbackURL:  cfg.ServerCallbackURL,
		BuilderBinaryPath:  cfg.CloudBuilderBinaryPath,
		BuilderBinaryURL:   cfg.CloudBuilderBinaryURL,
		InstanceTTLMinutes: cfg.CloudInstanceTTL,
	}
}

// Clone returns a deep copy (slices are the only reference fields).
func (s *CloudSettings) Clone() *CloudSettings {
	c := *s
	if s.PVENodes != nil {
		c.PVENodes = append([]string(nil), s.PVENodes...)
	}
	if s.RemoteBuilders != nil {
		c.RemoteBuilders = append([]string(nil), s.RemoteBuilders...)
	}
	return &c
}

# Testing Portage Engine with Proxmox VE (PVE)

This walkthrough runs a real end-to-end cloud build on a Proxmox VE host:
the server provisions a VM via Terraform (telmate/proxmox provider), deploys a
builder onto it over SSH, submits the build, streams status back, and destroys
the VM when done.

## What the flow looks like

```
client build … ──▶ server (/api/v1/builds/submit)
                     │  REMOTE_BUILDERS empty → cloud path
                     ▼
                terraform apply  ──▶ PVE: clone template → VM boots (cloud-init)
                     │                      qemu-guest-agent reports the IP
                     ▼
                SSH deploy: install Docker, sync portage tree, push builder
                     │       binary, start portage-builder.service (port 9090)
                     ▼
                submit build to VM builder → poll to completion → artifact
                     ▼
                terraform destroy (always — also on failure, and via TTL cleanup)
```

## Requirements on the PVE side

### 1. VM template (the image requirements)

The provisioner **clones a QEMU VM template by name** (`proxmox_vm_qemu` with
`clone = "<CLOUD_PVE_TEMPLATE>"`). Requirements:

- It must be a **QEMU VM template**, not an LXC container template
  (`local:vztmpl/...` paths cannot be used).
- The guest OS must be **Debian or Ubuntu** (recommended; the deployment script
  uses `apt-get`. RHEL-family with `yum` also works). It does **not** need to
  be Gentoo — builds run inside a `gentoo/stage3` Docker container on the VM.
- **cloud-init** must be installed in the guest (cloud images ship it) — the
  provider injects the SSH key, user, and network config via a cloud-init drive.
- **qemu-guest-agent** must be installed in the guest. This is mandatory: the
  Terraform provider reads the VM's IP through the guest agent; without it,
  `terraform apply` hangs and times out waiting for an IP.
- Disk ≥ 50 GB recommended (Gentoo tree + stage3 image + build workspace);
  default instance spec is 4 cores / 8 GB RAM / 50 GB disk (override via
  `machine_spec` or the spec defaults in config).

Recipe (run on the PVE node) — Debian 12 genericcloud + agent baked in:

```bash
apt-get install -y libguestfs-tools   # provides virt-customize

wget https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2
# genericcloud has cloud-init but NOT qemu-guest-agent — bake it in:
virt-customize -a debian-12-genericcloud-amd64.qcow2 --install qemu-guest-agent

qm create 9000 --name debian-12-cloudinit-template --memory 2048 \
    --net0 virtio,bridge=vmbr0 --scsihw virtio-scsi-pci
qm set 9000 --scsi0 local-lvm:0,import-from=$PWD/debian-12-genericcloud-amd64.qcow2
qm set 9000 --ide2 local-lvm:cloudinit
qm set 9000 --boot order=scsi0
qm set 9000 --serial0 socket --vga serial0
qm set 9000 --agent enabled=1
qm template 9000
```

`CLOUD_PVE_TEMPLATE` is then `debian-12-cloudinit-template`. (The generated
Terraform attaches its own cloud-init drive on `ide2`, so strictly the template
does not need one pre-attached — but including it is harmless and lets you test
the template manually.)

### 2. API token

Create a token for Terraform, e.g.:

```bash
pveum user token add root@pam terraform --privsep 0
```

(`--privsep 0` gives the token the user's full privileges; for least privilege
create a role with `VM.Allocate VM.Clone VM.Config.* VM.Monitor VM.Audit
VM.PowerMgmt Datastore.AllocateSpace Datastore.Audit` and bind it instead.)

The token secret is only ever passed to Terraform via environment variables
(`TF_VAR_pve_token_secret`) — it is not written into `main.tf`.

## Requirements on the server side

### 1. Cross-compile the builder for the VM

The server pushes a builder binary onto the VM during deployment. Build one
matching the VM's OS/arch (pure Go, no CGO needed):

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/portage-builder-linux-amd64 ./cmd/builder
```

### 2. Server configuration

Minimal `server.conf` additions (see `configs/server.conf` for the full
annotated version):

```ini
# Take the cloud path: leave REMOTE_BUILDERS empty
REMOTE_BUILDERS=

CLOUD_DEFAULT_PROVIDER=pve
CLOUD_PVE_ENDPOINT=https://your-pve-host:8006
CLOUD_PVE_NODE=pve
CLOUD_PVE_TOKEN_ID=root@pam!terraform
CLOUD_PVE_TOKEN_SECRET=<token secret>
CLOUD_PVE_INSECURE=true            # if the PVE API uses a self-signed cert
CLOUD_PVE_STORAGE=local-lvm
CLOUD_PVE_NETWORK=vmbr0
CLOUD_PVE_TEMPLATE=debian-12-cloudinit-template

# SSH deployment (user MUST be root: the deploy script has no sudo)
CLOUD_SSH_KEY_PATH=/path/to/id_ed25519          # its .pub is injected via cloud-init
CLOUD_SSH_USER=root
# New VMs have unknown host keys; on a trusted LAN opt in to skipping
# verification (or maintain CLOUD_SSH_KNOWN_HOSTS instead):
CLOUD_SSH_INSECURE_HOST_KEY=true

# How the VM reaches this server (use the LAN IP, not localhost!)
SERVER_CALLBACK_URL=http://<server-lan-ip>:8080

# Builder binary delivery
CLOUD_BUILDER_BINARY_PATH=bin/portage-builder-linux-amd64

# Auth between server and the deployed builder
BUILDER_TOKEN=<openssl rand -hex 32>

# VM auto-destroy if a build wedges (minutes)
CLOUD_INSTANCE_TTL=60
```

Notes:

- `SERVER_CALLBACK_URL` must be reachable **from the VM** — the deployed
  builder registers against it and pulls binpkgs from `<callback>/binpkgs`.
- `terraform` must be on the server's `PATH`.
- The per-request `machine_spec` map can override any spec key
  (`node`, `storage`, `template`, `cores`, `memory_mb`, `disk_size_gb`,
  `ip_config`, `vlan`, …) — the `CLOUD_PVE_*` values are just defaults.

## Run the test

```bash
# 1. start the server
./bin/portage-server -config configs/server.conf

# 2. submit a small package and wait
./bin/portage-client build -server http://localhost:8080 -api-key <key> \
    -wait app-misc/jq

# 3. watch progress
#    - server log: terraform apply → deploy → build → destroy
#    - PVE UI: a portage-builder-amd64-<ts> VM appears, then disappears
```

Success criteria: the job reaches `completed`, an artifact reference is
recorded, and the VM is destroyed (check the PVE UI; `destroy_failed` instances
are retried by the TTL cleanup routine).

## Parallel builds, node scheduling, and artifact convergence

**Package-level parallelism is the built-in scaling model**: each submitted
build gets its own VM, and up to `MAX_WORKERS` builds run concurrently. Submit
five packages → up to five VMs build in parallel (each is destroyed when its
build finishes or fails).

**Automatic node placement** (`CLOUD_PVE_NODE=auto`): on a multi-node PVE
cluster the scheduler queries live cluster load (`/cluster/resources`) per
build and clones onto the least-loaded online node, so parallel builds spread
across the cluster instead of piling onto one node. Eligible nodes are those
hosting the template (always clone-safe), or the explicit `CLOUD_PVE_NODES`
list when set — use the list with shared storage, where any node can clone the
template. Requires API token auth. Per-request override: `machine_spec`
`node=<name>` pins a node, `node=auto` forces scheduling.

**Artifact convergence**: when a build succeeds, the server downloads the
artifact off the instance **before destroying it** and stores it under
`BINPKG_PATH/<category>/`, then immediately regenerates the `Packages` index.
So N parallel builders all feed the one binhost, and clients simply
`emerge --getbinpkg` against `<server>/binpkgs`. Builds whose artifact cannot
be retrieved are marked failed (never silently lost). Artifacts from static
`REMOTE_BUILDERS` converge the same way.

**Why not distcc?** distcc distributes the *compilation of one package* across
helper hosts. It needs identical toolchains on every helper, extra daemons,
and an open (weakly authenticated) network port — and it only helps wall-clock
latency of single huge packages (gcc, llvm, chromium). For a binhost farm,
package-level parallelism plus a bigger VM for heavy packages
(`machine_spec: cores=16, memory_mb=32768`; the generated make.conf derives
`MAKEOPTS -j` from the VM's core count at boot) covers the same ground with
none of the moving parts. If single-package latency ever becomes the
bottleneck, a distcc helper pool could be added to the cloud-init bootstrap —
treat it as a future enhancement.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `terraform apply` times out waiting for IP | qemu-guest-agent not installed in the template |
| `provider ... no available releases match` | version constraint edited back to `~> 3.0` — telmate has no stable 3.x; keep the exact pinned rc version |
| `500 unable to find configuration file .../<vmid>.conf` during clone | `CLOUD_PVE_TEMPLATE` name doesn't match an existing template on that node |
| SSH deploy fails immediately (`Host key verification failed`) | neither `CLOUD_SSH_KNOWN_HOSTS` nor `CLOUD_SSH_INSECURE_HOST_KEY` set |
| deploy fails on `apt-get`/permissions | `CLOUD_SSH_USER` is not root |
| VM builds nothing, poll fails with connection refused | builder binary never delivered — set `CLOUD_BUILDER_BINARY_PATH` (or bake the binary into the template) |
| builder up but registration/binpkg fetch fails | `SERVER_CALLBACK_URL` points at localhost or an address the VM cannot reach |

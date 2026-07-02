# Using Portage Engine

Portage Engine has two distinct jobs, and it is important to keep them separate:

| Job | Who does it | How |
| --- | --- | --- |
| **Consume** prebuilt packages | Portage itself (`emerge`) | Configure a binhost once, then `emerge --getbinpkg` |
| **Request** a build | `portage-client build` | Only needed because Portage has no native "ask the binhost to build X" |

Installing packages is **not** done by a custom client — it is done natively by
Portage against the server's binhost. The client is a management/request tool.

---

## 1. Consuming packages (the normal path)

The server exposes a standard Gentoo binary host at `/binpkgs/` (a real `PKGDIR`
with a Portage `Packages` index). Any Gentoo machine consumes it the standard
way.

### One-time setup on the client

Point Portage at the binhost. The client can write the file for you:

```bash
sudo ./bin/portage-client configure -server=http://your-server:8080
```

This writes `/etc/portage/binrepos.conf/portage-engine.conf`:

```ini
[portage-engine]
priority = 1
sync-uri = http://your-server:8080/binpkgs
verify-signature = true
```

`verify-signature = true` makes Portage refuse any package whose GPG signature
does not verify. The server builds **signed GPKG** packages, so this works out
of the box; import the server's public key into the client keyring first:

```bash
curl -s http://your-server:8080/api/v1/gpg/public-key | sudo gpg --import
```

### Enable binary fetching

Either per invocation:

```bash
emerge --getbinpkg dev-lang/python
```

or make it the default in `/etc/portage/make.conf`:

```bash
FEATURES="getbinpkg"
```

With `--getbinpkg`, emerge fetches the prebuilt package when the binhost has it
and **falls back to a normal source build** when it does not. There is nothing
else to install or wrap.

---

## 2. Requesting a build (optional)

Portage cannot ask a binhost to build something it lacks. When you want the
server to build a package — typically with specific USE flags — use the client:

```bash
# Request a build and wait for it to finish
./bin/portage-client build \
  -server=http://your-server:8080 \
  -package=dev-lang/python -version=3.11 \
  -use=ssl,threads,sqlite -wait
```

Flags:

| Flag | Meaning |
| --- | --- |
| `-server` | Server URL (default `http://localhost:8080`) |
| `-api-key` | API key, or set `PORTAGE_ENGINE_API_KEY` |
| `-package` | Package atom, e.g. `dev-lang/python` |
| `-version` | Package version (optional) |
| `-use` | USE flags, comma-separated |
| `-keywords` | Keywords, comma-separated |
| `-config` | A Portage config JSON file (see below) |
| `-portage-dir` | Read config from a Portage dir (see [SYSTEM_CONFIG_USAGE.md](SYSTEM_CONFIG_USAGE.md)) |
| `-wait` | Block until the build reaches a terminal state |

Check a job later:

```bash
./bin/portage-client status -server=http://your-server:8080 -job=<job-id>
```

Once the build completes, the package lands on the binhost and any client with
`--getbinpkg` will pick it up — so the two halves connect: **request once,
consume everywhere.**

---

## 3. Authentication

When the server is started with `API_KEY` set, every `/api/v1/*` request needs
it. The binhost (`/binpkgs/`) is intentionally public and read-only, because
`emerge` cannot present an API key — signatures, not the API key, are what
protect package integrity.

Pass the key to the client via `-api-key` or the `PORTAGE_ENGINE_API_KEY`
environment variable:

```bash
export PORTAGE_ENGINE_API_KEY=$(cat /path/to/key)
./bin/portage-client build -server=http://your-server:8080 -package=app-misc/hello
```

---

## 4. Config bundle files

To capture a Portage configuration without submitting a build (for review, or to
submit later), generate a bundle:

```bash
./bin/portage-client bundle \
  -portage-dir=/etc/portage -package=dev-lang/python:3.11 \
  -out=python-bundle.tar.gz

tar -tzf python-bundle.tar.gz   # inspect
```

A bundle can be replayed with `-config` on a later `build`.

# Using System Portage Configuration

When you request a build, you can hand the server your machine's exact Portage
configuration instead of re-specifying USE flags and settings by hand. Pass
`-portage-dir` to the client and it reads your `/etc/portage` directly.

```bash
./bin/portage-client build \
  -server=http://your-server:8080 \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11 -wait
```

The build then compiles the package with the same USE flags, keywords, masks,
and make.conf settings your system uses — so the resulting binary package
matches what a local build would have produced.

## What gets read

`-portage-dir` reads the following from the given directory (default
`/etc/portage`). Each file is optional; missing files are skipped.

| Source | Purpose |
| --- | --- |
| `make.conf` | Global settings (USE, CFLAGS, MAKEOPTS, FEATURES, …). Falls back to `/etc/make.conf`. |
| `package.use` (file or directory) | Per-package USE flags |
| `package.accept_keywords` (file or directory) | Per-package keywords (e.g. `~amd64`) |
| `package.mask` (file or directory) | Masked packages/versions |
| `package.unmask` (file or directory) | Unmasked packages/versions |
| `repos.conf` | Repository / overlay definitions |

Both the single-file and the split-directory (`package.use/`) layouts are
supported.

> Note: settings from your `make.conf` are **appended** to the build
> container's own `make.conf`, so the stage3's `CHOST`/`CFLAGS` are preserved
> and your overrides are layered on top.

## Generating a bundle instead of building

To capture your system configuration into a portable file (to review, archive,
or submit later) without starting a build, use the `bundle` subcommand:

```bash
./bin/portage-client bundle \
  -portage-dir=/etc/portage \
  -package=dev-lang/python:3.11 \
  -out=python-system-config.tar.gz

# Inspect what was captured
tar -tzf python-system-config.tar.gz
```

The bundle contains the collected `/etc/portage` fragments plus the package
specification. You can replay it later with `-config` on a `build` command.

## Why use it

- **USE-flag consistency** — the built binary matches your system's flags, so
  `emerge --getbinpkg` will accept it instead of rebuilding from source.
- **No manual re-specification** — no need to copy USE flags into `-use`.
- **Keywords and masks respected** — testing keywords and masks are honored.

See [USAGE.md](USAGE.md) for the full consume-vs-request overview.

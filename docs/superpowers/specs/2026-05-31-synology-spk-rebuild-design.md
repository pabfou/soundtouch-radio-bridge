# Synology SPK Rebuild — Design

**Date:** 2026-05-31
**Status:** Approved (rebuild + add build automation)

## Goal

Produce a fresh `.spk` for the **Synology DS215j** (Marvell Armada 375, ARMv7) that contains the current `main` HEAD of `soundtouch-radio-bridge` — i.e. everything that just merged from `worktree-feature-speaker-management` (speaker management UI, profiles, /settings page, stop endpoint, .m3u8 auto-proxy, HTTPS CA bundle, ST-10 icon).

User installs the resulting `.spk` via DSM **Package Center → Manual Install** (drag-and-drop).

## Non-goals

- No DSM UI wizard, no permission tweaks, no language packs.
- No support for other Synology architectures in this round (apollolake/etc.).
- No CI build of the SPK — the build script runs locally on the Mac.
- No Docker-on-Synology path — separate from this work.

## What changes vs. the existing SPK

The existing artifact (`dist/SoundTouchBridge-1.0.0-20260528.spk`) was hand-built. Its structure is correct; only the binary and version differ.

| Item | Existing SPK | New SPK |
| --- | --- | --- |
| `INFO` `package` | `SoundTouchBridge` | unchanged |
| `INFO` `arch` | `armada375` | unchanged (DS215j) |
| `INFO` `version` | `1.0.0-20260528` | **`1.0.1-20260531`** |
| `INFO` `adminport`, etc. | `8080`, etc. | unchanged |
| `scripts/*` (postinst, preupgrade, postupgrade, start-stop-status, etc.) | well-formed | unchanged |
| `package.tgz` layout | `bin/bridge`, `etc/config.yaml.example`, `var/` | unchanged |
| `bin/bridge` | binary built 2026-05-28 | **rebuilt from `main` HEAD** |
| `etc/config.yaml.example` | from 2026-05-28 | **refreshed from current `deploy/synology/config.yaml`** if it has drifted |

## Build pipeline (new)

Add `scripts/build-spk.sh` to the repo so any future rebuild is one command. The script:

1. Cross-compile the bridge for ARMv7:
   `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o <tmp>/bin/bridge .`
   Strip + `-trimpath` for smaller, reproducible binary. CGO is not used anywhere in the project (verified — no `import "C"`), so cross-compile is straightforward.
2. Stage the `package.tgz` payload in a temp directory:
   - `bin/bridge` (the built binary)
   - `etc/config.yaml.example` (extracted from the existing `dist/SoundTouchBridge-1.0.0-20260528.spk` for now — it's a sanitized example with no real speaker IP. Will be moved into `packaging/synology/config.yaml.example` so the build script doesn't depend on the prior `.spk`.)
   - empty `var/` (created)
3. `tar -czf package.tgz -C <staged> .`
4. Stage SPK root:
   - `INFO` (rendered from `packaging/synology/INFO.template` — version substituted via `sed`; version comes from `$1` if provided, else defaults to `1.0.1-$(date +%Y%m%d)`)
   - `package.tgz` (from step 3)
   - `scripts/` (copied verbatim from a checked-in `packaging/synology/scripts/` directory)
5. `tar -cf dist/SoundTouchBridge-<version>-armada375.spk -C <staged-spk-root> .`

Why this layout in the repo:

- `packaging/synology/INFO.template` — text template, version substituted at build time.
- `packaging/synology/scripts/` — the six lifecycle scripts, source-controlled (currently they only live inside the built `.spk`, which is bad — losing the SPK loses the scripts).
- `scripts/build-spk.sh` — the entry point.

This is the only structural change to the repo beyond the build script: pull the script bodies out of the binary `.spk` blob and into version control. Otherwise we'd have to keep round-tripping through `tar -xf` to read or edit them.

## Verification

- `file <built>/bin/bridge` reports `ELF 32-bit LSB executable, ARM, EABI5 version 1`.
- `tar -tf dist/SoundTouchBridge-1.0.1-20260531-armada375.spk` contains `INFO`, `package.tgz`, `scripts/`.
- `tar -tzf package.tgz` matches the existing SPK layout.
- (Manual, on-device) install via DSM, browse to `http://<ds215j>:8080`, confirm `/settings` page loads (new in this build) and that an existing config is preserved through an upgrade install over the prior SPK.

## Rollback

If the new SPK misbehaves, DSM's Package Center supports "Stop" and "Uninstall". The user's `config.yaml` survives uninstall (it stays at `/var/packages/SoundTouchBridge/target/etc/config.yaml`); a clean reinstall of the older `dist/SoundTouchBridge-1.0.0-20260528.spk` reverts the code.

## Out of scope (deferred)

- Multi-arch SPK (apollolake, etc.) — add if a second Synology shows up.
- CI build of SPK on tag push — manual build is fine for now.
- DSM UI wizard for setting initial speaker IP — first-run UX uses mDNS auto-discovery already.

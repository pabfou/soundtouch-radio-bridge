# Speaker Discovery & Management UI — Design Spec

**Date:** 2026-05-31
**Status:** Approved
**Builds on:** [2026-05-28-soundtouch-radio-bridge-design.md](2026-05-28-soundtouch-radio-bridge-design.md)

## Overview

Adds a Settings page to the web UI for managing SoundTouch speakers. Today, speaker configuration requires editing `config.yaml` by hand and restarting the container. After this change, users can:

- Trigger mDNS discovery from the browser to find speakers on the LAN.
- Save multiple speakers in `config.yaml`, with one marked as active.
- Switch the active speaker (the one that receives playback) without restart.
- Rename and remove saved speakers.

Single-speaker simultaneous playback is preserved — only the active speaker receives commands. Multi-speaker concurrent playback is explicitly out of scope.

---

## UI

### Header (`web/index.html` line ~572)

Existing header gets a circular gear icon button next to the online/offline badge:

```html
<div class="header">
  <h1>SoundTouch Radio</h1>
  <div class="header-actions">
    <span id="badge" class="badge offline">Offline</span>
    <a href="/settings" id="settings-btn" aria-label="Settings" class="icon-btn">⚙</a>
  </div>
</div>
```

New CSS class `.icon-btn`: 38×38px, `border-radius: 980px`, matches the existing card shadow and accent-soft hover state. Inline SVG gear preferred over the emoji glyph for cross-platform consistency.

### Settings page (`web/settings.html`)

Served by the existing `embed.FS` — no router code required. Plain `<a href="/settings">` navigation, no JS hijack.

Layout (reuses the existing CSS variables and card pattern from `index.html`):

```
┌─ ← Back ──────────────────────────────── ⚙ Settings ─┐
│                                                       │
│  Active Speaker                                       │
│  ┌─────────────────────────────────────────────────┐ │
│  │ ● Palma Sola              192.168.212.66       │ │
│  └─────────────────────────────────────────────────┘ │
│                                                       │
│  Saved Speakers                  [ Scan network ]   │
│  ┌─────────────────────────────────────────────────┐ │
│  │ ○ Living Room             192.168.1.50  ✏ 🗑   │ │
│  │ ○ Kitchen                 192.168.1.51  ✏ 🗑   │ │
│  └─────────────────────────────────────────────────┘ │
│                                                       │
│  Discovered on network                                │
│  ┌─────────────────────────────────────────────────┐ │
│  │ Office (192.168.1.52)                  [Add]   │ │
│  └─────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────┘
```

**Behaviors:**
- **Active Speaker** — the speaker that playback targets. Radio-button style; tapping any saved row promotes it to active.
- **Saved Speakers** — every entry in `config.yaml`. Pencil icon triggers inline rename (name becomes a text input with ✓/✕; Enter submits, Esc cancels). Trash icon removes; disabled on the active row with a tooltip "Switch active speaker first."
- **Scan network** — calls `POST /api/discover`. Shows a spinner during the 5s scan.
- **Discovered on network** — section appears only after a scan. Shows mDNS results minus speakers already saved. Each row has an Add button that calls `POST /api/speakers`.

Toast notifications (top-right, auto-dismiss 3s) surface errors: duplicate names, switching failures, etc.

---

## Data Model

### Config schema (`config.yaml`)

Backwards-compatible — `active_speaker` is new and optional:

```yaml
active_speaker: Palma Sola
speakers:
  - name: Palma Sola
    ip: 192.168.212.66
  - name: Kitchen
    ip: 192.168.1.51
```

**Resolution rules:**
- If `active_speaker` is set and matches a name in `speakers`, that speaker is active.
- If `active_speaker` is unset, `speakers[0]` is treated as active (preserves existing configs).
- If `active_speaker` is set but points to an unknown name, fall back to `speakers[0]` and log a warning.

No migration step — resolution happens at read time. The field is written on first mutation (switch, add, remove, rename).

### Speaker identity

`name` is the stable identifier. Names must be unique across `speakers[]`. Names are case-sensitive, trimmed of leading/trailing whitespace, non-empty. No length limit beyond YAML practicality.

IP is the connection target; can change (DHCP). Re-running discovery and re-adding via the UI is the path to update an IP — explicit IP editing is out of scope.

---

## HTTP API

Added in `internal/api/router.go` and `internal/api/handlers.go`:

| Method | Path | Body | Returns |
|---|---|---|---|
| `GET` | `/api/speakers` | — | `{"active":"...", "speakers":[{"name":"...","ip":"..."}, ...]}` |
| `POST` | `/api/speakers` | `{"name":"...","ip":"..."}` | 201 / 400 / 409 |
| `PATCH` | `/api/speakers/{name}` | `{"name":"newName"}` | 200 / 400 / 404 / 409 |
| `DELETE` | `/api/speakers/{name}` | — | 204 / 404 / 409 |
| `POST` | `/api/speakers/active` | `{"name":"..."}` | 200 / 404 / 503 |
| `POST` | `/api/discover` | — | `{"found":[{"name":"...","ip":"..."}, ...]}` |

**Error codes:**
- `400` — invalid body (empty name, malformed IP, missing fields)
- `404` — named speaker not found
- `409` — duplicate name (add or rename collision) / attempted removal of active speaker
- `503` — switch failed because another switch is in flight, or new speaker WS connect timed out

Path parameters are URL-encoded by the client (`encodeURIComponent`). Go 1.22 mux decodes `r.PathValue("name")` automatically.

**Discovery semantics:** `POST /api/discover` does NOT mutate config. The UI calls it, displays the result, and the user explicitly adds via `POST /api/speakers`. This keeps discovery idempotent and avoids surprises when multiple speakers are on the LAN.

---

## Backend Implementation

### Config Store (`internal/config/config.go`)

Add `ActiveSpeaker string` field to the `Config` struct (YAML tag `active_speaker,omitempty`).

New methods on `*Store`:
- `Speakers() []Speaker` — snapshot copy
- `Active() (Speaker, bool)` — resolves active per the rules above
- `SetActive(name string) error` — `ErrUnknownSpeaker` if not in list
- `AddSpeaker(spk Speaker) error` — validates name+IP; `ErrDuplicateName` on collision; `ErrEmptyName` / `ErrInvalidIP` for malformed input
- `RemoveSpeaker(name string) error` — `ErrUnknownSpeaker`; `ErrActiveSpeaker` if it's the active one (caller must switch active first)
- `RenameSpeaker(oldName, newName string) error` — `ErrUnknownSpeaker`, `ErrEmptyName`, `ErrDuplicateName`. If `oldName` is the active speaker, updates `ActiveSpeaker` in the same locked operation and single `save()`.

All mutating methods acquire `s.mu` and write `config.yaml` via the existing `s.save()`.

### Discovery interface (`internal/speaker/discover.go`)

Refactor the existing `Discover` function into an interface so handlers can be tested without real mDNS:

```go
type Discoverer interface {
    Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error)
}

type MDNSDiscoverer struct{}
func (MDNSDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
    // existing zeroconf implementation
}
```

`main.go` injects `MDNSDiscoverer{}` into the API handlers. Tests inject a fake.

### Manager retargeting (`internal/speaker/manager.go`)

Add `(m *Manager) SetTarget(ip string) error`:
1. Acquire `m.switchMu` (separate from any per-command lock).
2. Cancel the current WebSocket context.
3. Wait briefly (≤500ms) for the WS goroutine to exit cleanly.
4. Replace `m.ip` with the new value.
5. Reconnect WebSocket against the new IP.
6. Return on connect success, or after a 2s timeout (caller may still consider switch logically applied; UI reflects offline state).

`main.go` constructs the `*Manager` once and passes it to `api.New(...)` alongside the `Store` and `Discoverer`. Handlers call `mgr.SetTarget(newIP)` after a successful `store.SetActive(name)`.

### Switching sequence (`POST /api/speakers/active`)

1. Read body → `name`.
2. Look up speaker in `store.Speakers()` → `404` if unknown.
3. If a current active speaker exists, best-effort UPnP `Stop` on it. Log on failure, continue. Skipped if no prior active (first-time setup). Avoids leaving the old speaker playing.
4. `store.SetActive(name)` → persist.
5. `mgr.SetTarget(newSpeaker.IP)`.
6. Return `200` with `{"active":"newName","connected":true|false}`.

The "stop old, then switch" order matters — we lose the old speaker reference after step 5.

---

## Concurrency

- **Config writes:** existing `s.mu sync.Mutex` covers all new mutating methods.
- **Manager switching:** dedicated `m.switchMu`. In-flight play/preset commands either complete first (short critical sections) or return `ErrSwitching` (mapped to `503`) if they collide with a switch.
- **Discovery single-flight:** if a scan is already in flight when a second request arrives, the second request waits and receives the same result. Worst case 5s wait (the scan timeout). Implemented with `golang.org/x/sync/singleflight`.

---

## Backwards Compatibility

- Existing `config.yaml` files without `active_speaker` continue to work — `speakers[0]` is treated as active.
- `active_speaker` is written on the first mutation through the new API.
- The first-start auto-discovery in `main.go:48` is extended: when it auto-saves a discovered speaker into an empty config, it also sets `ActiveSpeaker = discovered.Name`.

---

## Testing

| Layer | Approach |
|---|---|
| `config.Store` new methods | Unit tests against tempfile config, following the existing `config_test.go` pattern. Cover: backwards-compat resolve (no `active_speaker` set), SetActive unknown name, RemoveSpeaker on active speaker, AddSpeaker duplicate, RenameSpeaker happy path, rename of active updates `ActiveSpeaker`, rename to empty/duplicate, persistence round-trip after each mutation. |
| HTTP handlers | Table-driven `httptest` tests, following `internal/api/handlers_test.go` pattern. Inject fake `Discoverer` and fake `Manager` (define minimal interfaces). Cover happy paths and all documented error codes. |
| `Manager.SetTarget` | Two `httptest.NewServer` instances simulating two SoundTouches. Verify: WS reconnects to new IP, Stop fires against the old IP, switch returns success when new WS connects, switch returns timeout result when new WS hangs. |
| Discovery | The `Discoverer` interface boundary is unit-tested via fakes; the real mDNS implementation continues to rely on manual verification (no automated mDNS responder in tests). |
| Web UI | No automated tests (existing pattern). Manual smoke: scan with one speaker on LAN, scan with multiple, add discovered, rename saved, attempt rename to existing name, switch active, attempt to remove active, remove non-active. |

---

## Out of Scope

Listed explicitly so they don't sneak back in:

- **Direct IP editing** in the UI — replace via remove+rediscover+add.
- **MAC / deviceID identity** — `name` is fine for a single LAN. Speakers that change IP via DHCP can be re-discovered.
- **Simultaneous multi-speaker playback** — requires a per-speaker Manager. Different design.
- **Reordering saved speakers** in the UI.
- **Per-speaker preset assignments** — presets remain global, applied to the active speaker.
- **Authentication** on the settings page — LAN-only app, consistent with the rest of the API.
- **Speaker grouping / zones** — Bose multi-room SoundTouch grouping APIs are separate territory.

---

## Files Touched (estimated)

- `internal/config/config.go` — `ActiveSpeaker` field, new methods, errors
- `internal/config/config_test.go` — new test cases
- `internal/speaker/discover.go` — extract `Discoverer` interface
- `internal/speaker/manager.go` — `SetTarget` method, `switchMu`
- `internal/speaker/manager_test.go` — `SetTarget` test
- `internal/api/router.go` — wire 6 new routes
- `internal/api/handlers.go` — 6 new handlers (`GET` / `POST` / `PATCH` / `DELETE` on speakers, `POST` on speakers/active, `POST` on discover)
- `internal/api/handlers_test.go` — new test cases
- `internal/api/api.go` (or wherever `New` lives) — accept `Discoverer` and `*Manager` (already accepts Store)
- `main.go` — instantiate `MDNSDiscoverer{}`, pass through; extend first-start auto-discovery to set `ActiveSpeaker`
- `web/index.html` — add `.icon-btn` CSS, gear link in header
- `web/settings.html` — new file
- `web/settings.js` (or inline) — fetch/render/mutate logic
- `web/settings.css` (or inline) — page-specific styles reusing existing variables

# Settings UI — Speaker Discovery & Station Profiles — Design Spec

**Date:** 2026-05-31
**Status:** Approved
**Builds on:** [2026-05-28-soundtouch-radio-bridge-design.md](2026-05-28-soundtouch-radio-bridge-design.md)

## Overview

Two related web UI additions that together remove the need to ever edit `config.yaml` by hand or restart the container after first install.

**Speaker management** (new Settings page):
- Trigger mDNS discovery from the browser to find speakers on the LAN.
- Save multiple speakers in `config.yaml`, with one marked as active.
- Switch the active speaker (the one that receives playback) without restart.
- Rename and remove saved speakers.

**Station profiles** (Presets card on the main page):
- Save and restore named snapshots of stations + preset assignments.
- Ships seeded with three profiles: **PalmaSola** (curated from current `presets.txt`), **VertChasseur** (empty), **Autres** (empty).
- Add / rename / delete profiles in a manage modal.

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

**Speakers:**
- Existing `config.yaml` files without `active_speaker` continue to work — `speakers[0]` is treated as active.
- `active_speaker` is written on the first mutation through the new API.
- The first-start auto-discovery in `main.go:48` is extended: when it auto-saves a discovered speaker into an empty config, it also sets `ActiveSpeaker = discovered.Name`.

**Profiles:**
- Existing `config.yaml` files without `profiles:` work unchanged — `Profiles()` falls back to embedded factory profiles. Current `stations`+`presets` remain whatever the user has.
- `profiles:` is written on the first mutation through the new API (`POST /api/profiles/{name}/save`, `POST /api/profiles`, etc.).
- The first-run seeding rule (empty `stations`+`presets` AND no `profiles:` → populate from embedded PalmaSola) only fires on truly fresh configs. An existing user with populated `stations` but no `profiles:` is **not** re-seeded — they keep their data and can opt in by clicking Save.

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

## Station Profiles

A named snapshot of `stations` + `presets`. Lets a user keep multiple curated radio sets (e.g. one per deployment site) and switch between them without retyping URLs. Ships seeded with three profiles, expandable from the UI.

### Data model

Added to `config.yaml`:

```yaml
profiles:
  - name: PalmaSola
    stations:
      - { id: rtbf-la-premiere, name: RTBF La Première, url: http://radios.rtbf.be/laprem1ere-128.mp3 }
      # … 8 stations from current presets.txt
    presets:
      1: rtbf-la-premiere
      2: france-culture
      # … etc.
  - name: VertChasseur
    stations: []
    presets: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null }
  - name: Autres
    stations: []
    presets: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null }
active_profile: PalmaSola
```

### Embedded factory baseline

New file `internal/config/factory_profiles.yaml`, embedded via `go:embed`, with the three profiles above (PalmaSola seeded from current `presets.txt`, the other two empty). Used when `config.yaml` has no `profiles:` section.

**Resolution rules:**
- `Profiles()` returns `config.profiles` if non-empty, else the embedded factory profiles.
- `ActiveProfile()` returns `config.active_profile` if set and matches a profile name, else the first profile.
- **First-run seeding:** when the bridge starts with a config that has empty `stations` and `presets` AND no `profiles:` section, the current `stations`+`presets` are also populated from the embedded *PalmaSola* profile. Fresh install boots with the curated stations, identical to today's hand-edited setup.

`presets.txt` is deleted as part of this change. `factory_profiles.yaml` is canonical; README gets a one-line pointer.

### HTTP API

| Method | Path | Body | Returns |
|---|---|---|---|
| `GET` | `/api/profiles` | — | `{"active":"PalmaSola", "profiles":[{"name":"...","stations":[...],"presets":{...}}, ...]}` |
| `POST` | `/api/profiles` | `{"name":"..."}` | 201 / 400 / 409 — creates a new empty profile |
| `PATCH` | `/api/profiles/{name}` | `{"name":"newName"}` | 200 / 400 / 404 / 409 — rename |
| `DELETE` | `/api/profiles/{name}` | — | 204 / 404 / 409 |
| `POST` | `/api/profiles/{name}/save` | — | 200 — snapshot current `stations`+`presets` into this profile |
| `POST` | `/api/profiles/{name}/load` | — | 200 — replace current `stations`+`presets` from this profile |
| `POST` | `/api/profiles/active` | `{"name":"..."}` | 200 / 404 — set the dropdown selection |

`409` on DELETE means either: profile is the currently-active one, or it's the last remaining profile (at least one must always exist).

Path parameters URL-encoded; Go 1.22 mux decodes.

### Config Store additions (`internal/config/config.go`)

- `Profiles() []Profile`, `ActiveProfile() string`
- `SetActiveProfile(name string) error` — `ErrUnknownProfile`
- `AddProfile(name string) error` — creates empty profile; `ErrEmptyName`, `ErrDuplicateName`
- `RenameProfile(oldName, newName string) error` — validates name; updates `active_profile` in same locked save if renaming the active
- `RemoveProfile(name string) error` — `ErrActiveProfile` if active, `ErrLastProfile` if it's the only one
- `SaveProfile(name string) error` — snapshot current state into named profile; `ErrUnknownProfile`
- `LoadProfile(name string) error` — replace current `stations`+`presets` from named profile; `ErrUnknownProfile`

All mutating methods acquire `s.mu` and call `s.save()`.

### UI

Modifications to the existing Presets card on the main page (`web/index.html` line ~590):

```
┌─ Presets ─ [Profile: PalmaSola ▾] [↻ Reload] [⭐ Save] [⚙] ────┐
│  1: France Inter                                       ▶  ✕   │
│  …                                                              │
└────────────────────────────────────────────────────────────────┘
```

- **Profile dropdown** — lists all profile names with the active one selected. Changing the selection calls `POST /api/profiles/active` (server-side state). Does **not** auto-load — switching the dropdown is only a focus change for the action buttons.
- **↻ Reload** — `POST /api/profiles/{active}/load`. Confirmation: *"Replace current stations and presets with the **PalmaSola** profile?"*
- **⭐ Save** — `POST /api/profiles/{active}/save`. Confirmation: *"Overwrite the **PalmaSola** profile with current stations and presets?"*
- **⚙ Manage profiles** — opens a modal (reusing `.modal-header`) listing all profiles with inline rename (pencil → text input + ✓/✕, same pattern as speaker rename) and delete (trash). An "Add profile" input at the bottom creates a new empty profile. Active row's trash is disabled with tooltip *"Switch active profile first."* Last remaining profile's trash is disabled with tooltip *"Cannot delete the only profile."*

**Why no auto-load on dropdown change:** users may have unsaved experimentation. Explicit Reload prevents silent data loss. Two clicks (pick + Reload) is the cost.

### Edge cases

- Saving over a profile is destructive — confirmation modal is the only guard.
- Loading a profile replaces stations and presets atomically. The Speaker Manager is not touched; whatever's currently playing keeps playing (the speaker doesn't care about our config until the next preset press or play action).
- Renaming the active profile updates `active_profile` in the same locked save.
- Adding a profile with a duplicate name → 409.
- Profile names: non-empty, trimmed of whitespace, no length limit beyond YAML practicality. Case-sensitive.

### Testing

- `config.Store`: factory fallback when `profiles:` absent; first-run seeding populates `stations`+`presets` from embedded PalmaSola; SaveProfile round-trip; LoadProfile round-trip; LoadProfile with unknown name; RenameProfile of active updates `active_profile`; RemoveProfile rejects active; RemoveProfile rejects last; AddProfile duplicate.
- Handlers: happy paths for all 7 endpoints + documented error codes; URL-encoded names in path.
- Manual UI smoke: switch profile in dropdown, click Reload, click Save, open manage modal, rename, add, attempt delete of active (denied), delete a non-active, attempt delete of last remaining (denied).

---

## Out of Scope

Listed explicitly so they don't sneak back in:

**Speakers:**
- **Direct IP editing** in the UI — replace via remove+rediscover+add.
- **MAC / deviceID identity** — `name` is fine for a single LAN. Speakers that change IP via DHCP can be re-discovered.
- **Simultaneous multi-speaker playback** — requires a per-speaker Manager. Different design.
- **Reordering saved speakers** in the UI.
- **Per-speaker preset assignments** — presets remain global, applied to the active speaker.
- **Speaker grouping / zones** — Bose multi-room SoundTouch grouping APIs are separate territory.

**Profiles:**
- **Auto-load on profile switch** — Reload is always explicit.
- **Per-profile speaker association** — tempting (PalmaSola the profile naturally maps to Palma Sola the speaker) but not in this design. Profiles and speakers stay independent.
- **Import / export of profiles** across devices.
- **Versioning / undo** of profile saves.
- **Reordering profiles** in the UI.

**Both:**
- **Authentication** on the settings page or any of the new endpoints — LAN-only app, consistent with the rest of the API.

---

## Files Touched (estimated)

**Speaker management:**
- `internal/config/config.go` — `ActiveSpeaker` field, new Speaker methods, errors
- `internal/speaker/discover.go` — extract `Discoverer` interface
- `internal/speaker/manager.go` — `SetTarget` method, `switchMu`
- `internal/speaker/manager_test.go` — `SetTarget` test
- `internal/api/api.go` (or wherever `New` lives) — accept `Discoverer` and `*Manager`
- `main.go` — instantiate `MDNSDiscoverer{}`, pass through; extend first-start auto-discovery to set `ActiveSpeaker`
- `web/index.html` — add `.icon-btn` CSS, gear link in header, Presets card additions (dropdown + Reload/Save/Manage buttons)
- `web/settings.html` — new file (speaker settings page)

**Profiles:**
- `internal/config/config.go` — `Profile` type, `Profiles` field, `ActiveProfile` field, profile methods
- `internal/config/factory_profiles.yaml` — new embedded file with three seeded profiles
- `internal/config/factory.go` (or similar) — `go:embed` line + factory profile loader
- `presets.txt` — **delete**; README updated to point at `factory_profiles.yaml`

**Both:**
- `internal/config/config_test.go` — new test cases for speakers and profiles
- `internal/api/router.go` — wire 13 new routes (6 speakers + 7 profiles)
- `internal/api/handlers.go` — 13 new handlers
- `internal/api/handlers_test.go` — new test cases
- `web/manage-profiles.modal.html` (or inline in `index.html`) — Manage Profiles modal markup
- `web/main.js` / `web/settings.js` (or inline) — fetch/render/mutate logic for new features

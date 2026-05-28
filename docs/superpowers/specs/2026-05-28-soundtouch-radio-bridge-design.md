# SoundTouch Radio Bridge — Design Spec

**Date:** 2026-05-28
**Status:** Approved

## Overview

A self-contained Docker container that serves internet radio stations to a Bose SoundTouch 10 speaker, replacing the decommissioned Bose cloud service. Designed to run on a Firewalla Purple (ARM64, ~1GB RAM shared) with a minimum footprint target of ~50MB RAM at idle. No firmware changes to the speaker are required.

The speaker streams audio directly from internet radio sources — the container handles only signalling (pushing URLs to the speaker and listening for button events). Zero audio bytes pass through the container.

---

## Runtime Stack

**Language:** Go (single static binary)
**Docker image:** multi-stage build → `FROM scratch` final layer
**Target size:** ~12MB image, ~15MB RAM idle
**Platforms:** `linux/arm64` (Firewalla), `linux/amd64` / `darwin/arm64` (Mac dev/test)

---

## Architecture

Three internal components:

### 1. HTTP Server (`:8080`)

Serves the embedded web UI and REST API. All HTML/CSS/JS is compiled into the binary via Go's `embed` package — no external static files, no build step.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Web UI (single-page) |
| GET | `/api/stations` | List all stations |
| POST | `/api/stations` | Add a station |
| DELETE | `/api/stations/:id` | Remove a station |
| GET | `/api/presets` | Get preset assignments |
| POST | `/api/presets` | Assign a station to a preset slot |
| POST | `/api/play` | Push a station to the speaker immediately |
| GET | `/api/search?q=` | Proxy TuneIn search, returns station list with resolved stream URLs |
| GET | `/api/status` | Speaker connection state + now playing |

### 2. Speaker Manager

Manages the connection to the SoundTouch speaker and implements both preset strategies.

**WebSocket listener** (`ws://SPEAKER_IP:8090/webapi/`)
- Maintained at all times; auto-reconnects every 5 seconds on drop
- Receives `userActivityUpdate` events for preset button presses
- On preset press → look up mapped station in config → call `POST /select` on the speaker

**REST client** (`http://SPEAKER_IP:8090`)
- Used to push stream URLs (`POST /select` with XML `ContentItem`)
- Used to probe and write presets (`POST /presets`)

**Preset sync (Strategy 1 — proactive):**
On startup, after connecting, the Speaker Manager probes whether the speaker accepts `POST /presets` with `source="INTERNET_RADIO"`. If supported, it writes all assigned presets to the speaker immediately. This makes preset buttons work natively — even if the container is stopped. Re-syncs whenever a preset assignment changes.

**WebSocket interception (Strategy 2 — reactive):**
Always active as a safety net. Catches preset button presses and pushes the stream URL regardless of whether Strategy 1 succeeded. ~200ms delay from button press to playback start.

Both strategies run in parallel. Strategy 1 makes the speaker self-sufficient; Strategy 2 is the reliable fallback.

### 3. Config Store

Reads and writes `config.yaml` (mounted as a Docker volume). File is the single source of truth. Uses a read-write mutex for safe concurrent access between the WebSocket listener and HTTP handlers.

---

## Config Schema

```yaml
# Speaker(s) on the local network
speakers:
  - name: Living Room
    ip: 192.168.1.50

# Station library
stations:
  - id: bbc-radio-4          # auto-generated slug, used as foreign key
    name: BBC Radio 4
    url: http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm
    logo: https://...         # optional, shown in web UI

  - id: france-inter
    name: France Inter
    url: http://direct.franceinter.fr/live/franceinter-midfi.mp3

# Preset button → station mapping (keys 1–6, null = unassigned)
presets:
  1: bbc-radio-4
  2: france-inter
  3: null
  4: null
  5: null
  6: null
```

**Rules:**
- Station IDs are auto-generated slugs (lowercase name, spaces → hyphens, non-alphanumeric stripped, suffix counter on collision e.g. `bbc-radio-4-2`); never typed manually
- The same station may be assigned to multiple preset slots (no restriction)
- Deleting a station automatically sets any preset slots referencing it to `null`
- Station library is independent of preset slots (can have 20 stations with only 6 presets assigned)
- Speaker list supports future multi-speaker; v1 uses the first entry only
- Missing config file on startup: start with empty config and log a warning

---

## Data Flows

### Preset button pressed
1. Speaker emits `userActivityUpdate` via WebSocket
2. Speaker Manager reads preset slot number from event
3. Config Store lookup: `presets[N]` → station ID → stream URL
4. Speaker Manager calls `POST :8090/select` with stream URL as `ContentItem`
5. Speaker begins streaming directly from the internet (~200ms total)

### Web UI: add station (manual URL)
1. `POST /api/stations` `{name, url, logo?}`
2. Config Store generates slug ID, appends to `stations`, writes YAML
3. Response: `{id: "..."}`

### Web UI: add station (TuneIn search)
1. `GET /api/search?q=BBC`
2. HTTP Server calls TuneIn's public OPML endpoint (`opml.radiotime.com/Search.ashx`), resolves stream URLs
3. Returns list of `{name, url, logo}`
4. User selects → same flow as manual URL

### Web UI: assign preset
1. `POST /api/presets` `{slot: 1, stationId: "wnyc"}`
2. Config Store updates `presets[1]`, writes YAML
3. Speaker Manager re-syncs preset to speaker (Strategy 1, if supported)
4. Response: `200 OK`

### Web UI: play now
1. `POST /api/play` `{stationId: "wnyc"}`
2. Speaker Manager calls `POST :8090/select`
3. Response: `200 OK`

---

## Web UI

Single-page, vanilla HTML/CSS/JS. Embedded into the binary at compile time. Mobile-friendly. No framework, no build step.

**Layout (top to bottom):**

1. **Header bar** — app name + speaker connection badge (green "● Living Room" / red "● Offline")
2. **Now playing** — station name, stream URL, Stop button
3. **Preset grid** — 6 buttons in 2×3 grid. Shows assigned station name or "+ assign". Active preset has green border.
4. **Station library** — scrollable list. Each row: logo, name, preset badge (if assigned), Play button, ⋯ menu.
5. **Add station** — single input for TuneIn search or direct URL paste + Search button.

**Preset assignment flow:**

- **Empty slot**: tap → station picker opens immediately → select station → "Assign [Name]" confirm button → done
- **Assigned slot**: tap → shows current station + "Play now" / "Change…" buttons → tap "Change…" → station picker with Cancel / "Assign [Name]" confirm → done (2 steps, no third screen)
- Station picker shows the **complete library**. Each station shows its current preset assignment on the right ("Preset N"). Confirm button is disabled until a station is selected.
- Assignment also available from station ⋯ menu → "Assign to preset" → slot picker

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Speaker unreachable | WebSocket retries every 5s; web UI shows "Offline" badge; play requests return 503 |
| Strategy 1 unsupported | Logged at startup; Strategy 2 continues normally |
| TuneIn unavailable | Search returns 502 with user-facing message; manual URL entry unaffected |
| Config file missing | Start with empty config, log warning, no crash |
| Unassigned preset pressed | Silently ignored |
| Config write failure | Return 500, log error, in-memory state unchanged |

---

## Deployment

### Development (Mac)

```bash
# Run binary directly — fastest iteration
go run . --config ./config.yaml

# Or test the container
docker compose -f compose.dev.yaml up
```

`compose.dev.yaml`:
```yaml
services:
  bridge:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/config.yaml
```

Docker Desktop on Mac routes container traffic through the host's network stack, so the container can reach the speaker by IP on the LAN.

### Production (Firewalla Purple)

Build the ARM64 image on Mac using buildx, then copy it to the Firewalla:

```bash
# On Mac — build for ARM64
docker buildx build --platform linux/arm64 -t soundtouch-radio-bridge:latest --load .

# Save and copy to Firewalla
docker save soundtouch-radio-bridge:latest | ssh root@firewalla docker load

# On Firewalla
docker compose up -d
```

`compose.yaml`:
```yaml
services:
  bridge:
    image: soundtouch-radio-bridge:latest
    network_mode: host       # same subnet as speaker
    volumes:
      - ./config.yaml:/config.yaml
    restart: unless-stopped
```

### Dockerfile (multi-stage)

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o bridge .

FROM scratch
COPY --from=builder /app/bridge /bridge
EXPOSE 8080
CMD ["/bridge", "--config", "/config.yaml"]
```

---

## Project Structure

```
soundtouch-radio-bridge/
├── main.go
├── internal/
│   ├── config/       # load/save config.yaml, mutex-guarded
│   ├── speaker/      # WebSocket listener + REST client
│   ├── tunein/       # TuneIn search + URL resolution
│   └── api/          # HTTP handlers
├── web/
│   └── index.html    # embedded into binary via go:embed
├── config.yaml       # user's station/preset data (gitignored)
├── compose.yaml      # production (Firewalla)
├── compose.dev.yaml  # development (Mac)
├── Dockerfile
└── docs/
    └── superpowers/specs/
        └── 2026-05-28-soundtouch-radio-bridge-design.md
```

---

## Testing Plan

1. **Unit tests** — config parsing/writing, slug generation, TuneIn URL resolution
2. **Mock speaker** — a small Go test server that emulates the SoundTouch WebSocket + REST API, used to test Speaker Manager without a physical device
3. **Mac integration** — run binary against the real SoundTouch 10 on the LAN before containerising
4. **Container test** — `compose.dev.yaml` on Mac, verify preset buttons and web UI end-to-end
5. **Firewalla deploy** — final validation on target hardware

---

## Out of Scope (v1)

- Multi-room / grouped playback
- mDNS speaker auto-discovery (speaker IP is static in config)
- Volume control via web UI
- Authentication / access control on the web UI
- Local audio file playback

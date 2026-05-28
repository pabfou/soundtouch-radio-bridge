# SoundTouch Radio Bridge

A self-contained Docker container that revives internet radio on Bose
SoundTouch speakers after Bose decommissioned their cloud service.

Built specifically for the **Bose SoundTouch 10** running the final
firmware (`27.0.6`, August 2022), but should work on other SoundTouch
models that share the same UPnP/AVTransport implementation. Pressing
preset buttons plays internet radio again, and a small web UI lets you
manage your station library from any browser on the LAN.

```
┌─────────────────┐   WebSocket   ┌───────────┐   UPnP    ┌───────────┐
│  preset button  │──nowSelected──▶  Bridge   │──Play()──▶│  Speaker  │
│  pressed        │               │  (Go)     │           │           │
└─────────────────┘               │  ~5MB     │◀──stream──┤           │
                                  │           │           └───────────┘
                                  └───────────┘
                                       │
                                       │  HTTP/HTTPS/HLS
                                       ▼
                                  internet radio
```

## Why this exists

In 2024, Bose decommissioned the cloud service that backed the SoundTouch
line. The final firmware Bose pushed strips out the `INTERNET_RADIO` and
`TUNEIN` sources from the speaker's API. The preset buttons still emit
events when pressed, but the stored preset URLs no longer play — the
speaker no longer knows how.

This bridge runs on your LAN, intercepts the preset-button WebSocket
events from the speaker, and pushes the stream to the speaker over UPnP
AVTransport — the one streaming source that still works.

## Status — what works and what doesn't

| Capability | Status |
|---|---|
| Preset buttons (1–6) play internet radio | ✅ |
| Web UI for adding/removing/assigning stations | ✅ |
| Direct MP3/AAC streams | ✅ |
| HTTPS streams (NPR, RNE, etc.) | ✅ via built-in proxy |
| HLS streams (BBC Sounds, Onda Cero, etc.) | ✅ via built-in transmuxer |
| TuneIn search for adding stations | ✅ |
| Stations whose hosts forbid HEAD probes (BBC legacy) | ✅ via built-in proxy |
| Volume control from the web UI | ❌ not implemented |
| Multi-speaker / grouped playback | ❌ not implemented |
| HTTPS-only streams that BBC has locked down | ❌ no live URL available |

## Quickstart

If you have the same setup (SoundTouch on the LAN, a small always-on
machine like a Synology, Raspberry Pi, or Firewalla Purple to run a
container):

```bash
# 1. Find your speaker's IP
dns-sd -B _soundtouch._tcp local.       # macOS
# or check your router's DHCP leases

# 2. Create a config.yaml
cat > config.yaml <<EOF
speakers:
  - name: Living Room
    ip: 192.168.1.50               # your speaker's IP

stations:
  - id: france-inter
    name: France Inter
    url: http://direct.franceinter.fr/live/franceinter-midfi.mp3

presets:
  1: france-inter
  2: null
  3: null
  4: null
  5: null
  6: null
EOF

# 3. Run it
docker run -d --name soundtouch --restart unless-stopped \
  --network host \
  -v "$PWD/config.yaml:/config.yaml" \
  ghcr.io/<your-fork>/soundtouch-radio-bridge:latest    # or build locally
```

Then open `http://<host>:8080` in any browser, add stations, assign
presets, and press the physical buttons on the speaker. Audio should
play within ~200 ms of the press.

To build the image yourself:

```bash
docker buildx build --platform linux/amd64 -t soundtouch-radio-bridge .
# or --platform linux/arm64 for Raspberry Pi / Firewalla Purple
```

`--network host` is required because the bridge needs to receive callbacks
from the speaker (the UPnP stream proxy URL is reachable on the bridge's
LAN IP).

## How it works — three strategies, three speaker quirks

The post-cloud firmware has several quirks the bridge has to work around.
None of these are obvious from documentation; they were discovered by
poking the speaker.

### 1. The preset buttons emit WebSocket events but don't play anything

When you press a preset, the speaker still fires a `nowSelectionUpdated`
event on its WebSocket (`ws://SPEAKER:8080`, subprotocol `gabbo`). The
event contains the slot number. The bridge subscribes to this and uses
the slot to look up the station in `config.yaml`, then triggers playback
via UPnP.

You only need an outbound WebSocket connection; nothing inbound.

### 2. `POST /select` rejects `INTERNET_RADIO` with `UNKNOWN_SOURCE_ERROR`

The legacy SoundTouch API endpoint that used to play a stream URL no
longer accepts that source. The bridge uses **UPnP AVTransport** instead,
which is the one streaming source the speaker still supports:

```
Stop(InstanceID=0)
SetAVTransportURI(InstanceID=0, CurrentURI=<URL>, CurrentURIMetaData=<DIDL-Lite>)
Play(InstanceID=0, Speed=1)
```

The control URL is discoverable via SSDP at
`http://SPEAKER:8091/XD/BO5EBO5E-F00D-F00D-FEED-<deviceID>.xml` and is
typically `/AVTransport/Control` on port `8091`.

### 3. The speaker probes the stream URL with HEAD before playing

If the host returns 4xx on HEAD (BBC, some Akamai CDNs), the speaker
silently aborts and sits at `INVALID_SOURCE`. Worse, the SoundTouch 10
has **no TLS support at all** — any `https://` URL returns HTTP 500 on
`SetAVTransportURI`. And the speaker can't parse HLS playlists or
MPEG-TS containers.

The bridge runs an internal proxy at `/stream/{id}` that:

* Returns `200 audio/mpeg` to HEAD probes (regardless of the upstream's
  behaviour).
* On GET, fetches the upstream as a normal HTTP client (with proper
  User-Agent, TLS support, and following redirects).
* If the upstream is **HLS** (`.m3u8` or `application/vnd.apple.mpegurl`),
  the bridge pulls each segment, demuxes MPEG-TS to extract the AAC PES
  payloads (or passes through `.aac` segments untouched), and writes a
  continuous ADTS-framed AAC stream to the speaker.

The proxy is opt-in per station. When you add a station via the web UI,
the bridge probes it once with HEAD and stores the result in
`config.yaml` as `needs_proxy: true|false`. The Manager picks the right
path at play time:

```
                ┌─ direct URL ─────────► speaker fetches the upstream itself
playback URL ───┤
                └─ proxy URL ──► bridge ─► HEAD-friendly response
                                       │
                                       └─► GET upstream and stream through
```

This keeps the speaker doing its own fetching (zero audio through the
container) wherever possible, and only routes through the proxy when the
URL is HTTPS, HLS, or HEAD-hostile.

## Config schema

`config.yaml`:

```yaml
speakers:
  - name: Living Room
    ip: 192.168.1.50              # required

stations:
  - id: france-inter               # auto-generated slug, do not edit
    name: France Inter
    url: http://direct.franceinter.fr/live/franceinter-midfi.mp3
    logo: https://...              # optional
    needs_proxy: false             # set automatically on add

presets:
  1: france-inter                  # station id, or null
  2: null
  3: null
  4: null
  5: null
  6: null
```

A missing config file at startup is fine — the bridge starts with an
empty library and you can populate it via the web UI.

## Stream URL hunting — known-good sources

Finding URLs is the hardest part of this hobby. A few that work as of
2026-05:

| Station | URL | Notes |
|---|---|---|
| France Inter | `http://direct.franceinter.fr/live/franceinter-midfi.mp3` | direct |
| France Culture | `http://direct.franceculture.fr/live/franceculture-midfi.mp3` | direct |
| FIP | `http://direct.fipradio.fr/live/fip-midfi.mp3` | direct |
| RTBF La Première | `http://radios.rtbf.be/laprem1ere-128.mp3` | direct |
| RTL (FR) | `http://streamer-02.rtl.fr/rtl-1-44-128` | direct |
| RNE (Spain) | `https://dispatcher.rndfnk.com/crtve/rne1/mad/mp3/high` | HTTPS → proxy |
| Onda Cero (Spain) | `https://atres-live.ondacero.es/live/ondacero/bitrate_1.m3u8` | HLS → transmux |
| BBC Radio One | `http://as-hls-ww-live.akamaized.net/pool_01505109/live/ww/bbc_radio_one/bbc_radio_one.isml/bbc_radio_one-audio=96000.norewind.m3u8` | HLS → transmux |
| NPR News Now | `https://npr-ice.streamguys1.com/live.mp3` | HTTPS → proxy |
| WNYC | `https://stream.wnyc.org/wnycfm` | HTTPS → proxy |

[radio-browser.info](https://www.radio-browser.info/) is a good place to
search for current stream URLs.

## Deployment notes

* **Mac / Linux desktop**: just `go run . --config ./config.yaml` for
  development.
* **Synology (Container Manager)**: `network_mode: host` works; mount
  `config.yaml` from a shared folder. If the speaker is on a different
  VLAN, you need firewall rules allowing bidirectional traffic
  (Synology → speaker on ports 8080/8090/8091, speaker → Synology on
  port 8080 if you use HTTPS or HLS stations).
* **Firewalla Purple**: enable Docker (`sudo systemctl start docker`),
  then `docker run` as for Mac. Build for `linux/arm64`. Running on the
  Firewalla itself is the simplest deployment because it's already on
  the speaker's subnet — no VPN or cross-VLAN routing.
* **Raspberry Pi**: same as Firewalla Purple, ARM64 build.

## Project layout

```
soundtouch-radio-bridge/
├── main.go                       # entry point, embed web/
├── internal/
│   ├── api/                      # HTTP handlers + stream proxy
│   ├── config/                   # YAML config store, mutex-guarded
│   ├── hls/                      # HLS playlist client + MPEG-TS demux
│   ├── speaker/                  # UPnP client, WebSocket listener, manager
│   └── tunein/                   # OPML search + URL resolution
├── web/index.html                # single-page UI, vanilla JS, no build step
├── Dockerfile                    # multi-stage, scratch base + CA bundle
├── docs/superpowers/specs/       # design docs
└── deploy/                       # sample compose files for Synology / Purple
```

## License

MIT — see [LICENSE](./LICENSE).

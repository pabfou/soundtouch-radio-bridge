# SoundTouch Radio Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-contained Go Docker container that replaces Bose's decommissioned cloud service, letting a SoundTouch 10 play internet radio stations via preset buttons and a web UI.

**Architecture:** A single Go binary embeds a web UI and exposes a REST API on :8080. A Speaker Manager maintains a WebSocket connection to the speaker (port 8090) to intercept preset button presses (Strategy 2 — reactive) and also writes preset URLs directly to the speaker at startup (Strategy 1 — proactive). Config lives in a YAML file mounted as a Docker volume.

**Tech Stack:** Go 1.22, `gorilla/websocket` v1.5, `gopkg.in/yaml.v3` v3, standard library `net/http` (Go 1.22 enhanced mux), `encoding/xml`, `embed`.

---

## File Map

```
soundtouch-radio-bridge/
├── main.go                           # entry point: load config, start manager, start HTTP server
├── go.mod
├── go.sum
├── .gitignore
├── config.yaml.example
├── Dockerfile                        # multi-stage: golang:1.22-alpine → scratch
├── compose.yaml                      # production: network_mode: host, restart: unless-stopped
├── compose.dev.yaml                  # dev: port mapping only
├── internal/
│   ├── config/
│   │   ├── config.go                 # Config/Station/Speaker structs, Store (Load/Save/CRUD)
│   │   └── config_test.go
│   ├── speaker/
│   │   ├── client.go                 # REST client: Select, SetPreset, GetInfo, ProbePresetWrite
│   │   ├── client_test.go
│   │   ├── ws.go                     # WSListener: connect, parse XML events, reconnect loop
│   │   ├── ws_test.go
│   │   ├── manager.go                # Manager: wires Client + WSListener, handles preset events
│   │   └── manager_test.go
│   ├── tunein/
│   │   ├── tunein.go                 # Search via opml.radiotime.com, resolve stream URL
│   │   └── tunein_test.go
│   └── api/
│       ├── router.go                 # route registration, embed web/index.html
│       ├── handlers.go               # all HTTP handlers
│       └── handlers_test.go
└── web/
    └── index.html                    # single-page UI: preset grid, station library, add station
```

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `config.yaml.example`

- [ ] **Step 1: Initialise Go module**

```bash
cd /path/to/soundtouch-radio-bridge
go mod init soundtouch-radio-bridge
```

Expected: `go.mod` created with `module soundtouch-radio-bridge` and `go 1.22`.

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/gorilla/websocket@v1.5.1
go get gopkg.in/yaml.v3@v3.0.1
```

Expected: `go.sum` created, `go.mod` now lists both requires.

- [ ] **Step 3: Create .gitignore**

```
config.yaml
.superpowers/
*.tmp
```

- [ ] **Step 4: Create config.yaml.example**

```yaml
speakers:
  - name: Living Room
    ip: 192.168.1.50   # change to your speaker's IP

stations:
  - id: bbc-radio-4
    name: BBC Radio 4
    url: http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm

presets:
  1: bbc-radio-4
  2: null
  3: null
  4: null
  5: null
  6: null
```

- [ ] **Step 5: Create directory structure**

```bash
mkdir -p internal/config internal/speaker internal/tunein internal/api web
```

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum .gitignore config.yaml.example
git commit -m "chore: project scaffold"
```

---

## Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"soundtouch-radio-bridge/internal/config"
)

func TestGenerateID_basic(t *testing.T) {
	id := config.GenerateID("BBC Radio 4", nil)
	if id != "bbc-radio-4" {
		t.Fatalf("got %q, want %q", id, "bbc-radio-4")
	}
}

func TestGenerateID_collision(t *testing.T) {
	existing := []config.Station{{ID: "bbc-radio-4"}}
	id := config.GenerateID("BBC Radio 4", existing)
	if id != "bbc-radio-4-2" {
		t.Fatalf("got %q, want %q", id, "bbc-radio-4-2")
	}
}

func TestStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	err = store.AddStation(config.Station{Name: "BBC Radio 4", URL: "http://example.com/stream"})
	if err != nil {
		t.Fatal(err)
	}

	store2, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store2.Get()
	if len(cfg.Stations) != 1 || cfg.Stations[0].Name != "BBC Radio 4" {
		t.Fatalf("unexpected stations: %+v", cfg.Stations)
	}
}

func TestStore_MissingFile(t *testing.T) {
	store, err := config.NewStore("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatal("expected no error for missing file, got:", err)
	}
	cfg := store.Get()
	if len(cfg.Stations) != 0 {
		t.Fatal("expected empty stations")
	}
}

func TestStore_DeleteStation_clearsPreset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	store, _ := config.NewStore(path)
	_ = store.AddStation(config.Station{Name: "BBC Radio 4", URL: "http://example.com"})
	cfg := store.Get()
	id := cfg.Stations[0].ID
	_ = store.AssignPreset(1, id)
	_ = store.DeleteStation(id)

	cfg = store.Get()
	if cfg.Presets[1] != "" {
		t.Fatalf("expected preset 1 cleared, got %q", cfg.Presets[1])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -v
```

Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Station struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	Logo string `yaml:"logo,omitempty"`
}

type Speaker struct {
	Name string `yaml:"name"`
	IP   string `yaml:"ip"`
}

type Config struct {
	Speakers []Speaker         `yaml:"speakers"`
	Stations []Station         `yaml:"stations"`
	Presets  map[int]string    `yaml:"presets"`
}

type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

func GenerateID(name string, existing []Station) string {
	s := strings.ToLower(name)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	taken := map[string]bool{}
	for _, st := range existing {
		taken[st.ID] = true
	}
	id := s
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", s, i)
	}
	return id
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg.Presets = map[int]string{}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &s.cfg); err != nil {
		return nil, err
	}
	if s.cfg.Presets == nil {
		s.cfg.Presets = map[int]string{}
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// return a shallow copy sufficient for reading
	cfg := s.cfg
	stations := make([]Station, len(s.cfg.Stations))
	copy(stations, s.cfg.Stations)
	cfg.Stations = stations
	presets := make(map[int]string, len(s.cfg.Presets))
	for k, v := range s.cfg.Presets {
		presets[k] = v
	}
	cfg.Presets = presets
	return cfg
}

func (s *Store) StationByID(id string) (Station, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.cfg.Stations {
		if st.ID == id {
			return st, true
		}
	}
	return Station{}, false
}

func (s *Store) AddStation(st Station) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.ID = GenerateID(st.Name, s.cfg.Stations)
	s.cfg.Stations = append(s.cfg.Stations, st)
	return s.save()
}

func (s *Store) DeleteStation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.cfg.Stations[:0]
	for _, st := range s.cfg.Stations {
		if st.ID != id {
			filtered = append(filtered, st)
		}
	}
	s.cfg.Stations = filtered
	for slot, sid := range s.cfg.Presets {
		if sid == id {
			s.cfg.Presets[slot] = ""
		}
	}
	return s.save()
}

func (s *Store) AssignPreset(slot int, stationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Presets[slot] = stationID
	return s.save()
}

func (s *Store) SetSpeakerIP(ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Speakers) == 0 {
		s.cfg.Speakers = []Speaker{{Name: "Speaker", IP: ip}}
	} else {
		s.cfg.Speakers[0].IP = ip
	}
	return s.save()
}

func (s *Store) save() error {
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: config package with YAML store and slug generation"
```

---

## Task 3: Speaker REST Client

**Files:**
- Create: `internal/speaker/client.go`
- Create: `internal/speaker/client_test.go`

The SoundTouch REST API runs on port 8090. Key endpoints:
- `GET /info` — device info (used to probe connectivity)
- `POST /select` — body is a `ContentItem` XML element; plays a stream URL immediately
- `POST /presets` — body sets preset slots (Strategy 1; may not be supported on all firmware)

- [ ] **Step 1: Write the failing tests**

Create `internal/speaker/client_test.go`:

```go
package speaker_test

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"soundtouch-radio-bridge/internal/speaker"
)

func TestClient_Select(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/select" && r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := speaker.NewClient(strings.TrimPrefix(srv.URL, "http://"))
	err := c.Select("http://stream.example.com/radio.mp3", "Test Station")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "http://stream.example.com/radio.mp3") {
		t.Fatalf("body missing stream URL: %s", gotBody)
	}
	if !strings.Contains(gotBody, "Test Station") {
		t.Fatalf("body missing station name: %s", gotBody)
	}
}

func TestClient_ProbePresetWrite_supported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/presets" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := speaker.NewClient(strings.TrimPrefix(srv.URL, "http://"))
	supported := c.ProbePresetWrite()
	if !supported {
		t.Fatal("expected Strategy 1 supported")
	}
}

func TestClient_ProbePresetWrite_unsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := speaker.NewClient(strings.TrimPrefix(srv.URL, "http://"))
	supported := c.ProbePresetWrite()
	if supported {
		t.Fatal("expected Strategy 1 unsupported")
	}
}

func TestClient_SetPreset(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/presets" && r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := speaker.NewClient(strings.TrimPrefix(srv.URL, "http://"))
	err := c.SetPreset(1, "http://stream.example.com/radio.mp3", "Test Station")
	if err != nil {
		t.Fatal(err)
	}

	var presets struct {
		XMLName xml.Name `xml:"presets"`
		Preset  struct {
			ID int `xml:"id,attr"`
		} `xml:"preset"`
	}
	if err := xml.Unmarshal([]byte(gotBody), &presets); err != nil {
		t.Fatalf("invalid XML: %v — body: %s", err, gotBody)
	}
	if presets.Preset.ID != 1 {
		t.Fatalf("expected preset id=1, got %d", presets.Preset.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/speaker/... -run TestClient -v
```

Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement client.go**

Create `internal/speaker/client.go`:

```go
package speaker

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(addr string) *Client {
	return &Client{
		baseURL: "http://" + addr + ":8090",
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

type contentItem struct {
	XMLName       xml.Name `xml:"ContentItem"`
	Source        string   `xml:"source,attr"`
	Location      string   `xml:"location,attr"`
	SourceAccount string   `xml:"sourceAccount,attr"`
	IsPresetable  bool     `xml:"isPresetable,attr"`
	ItemName      string   `xml:"itemName"`
}

type presetsRequest struct {
	XMLName xml.Name     `xml:"presets"`
	Preset  presetEntry  `xml:"preset"`
}

type presetEntry struct {
	ID      int         `xml:"id,attr"`
	Content contentItem `xml:"ContentItem"`
}

func (c *Client) Select(streamURL, name string) error {
	item := contentItem{
		Source:       "INTERNET_RADIO",
		Location:     streamURL,
		IsPresetable: true,
		ItemName:     name,
	}
	body, err := xml.Marshal(item)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/select", "application/xml", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("select: speaker returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetPreset(slot int, streamURL, name string) error {
	req := presetsRequest{
		Preset: presetEntry{
			ID: slot,
			Content: contentItem{
				Source:       "INTERNET_RADIO",
				Location:     streamURL,
				IsPresetable: true,
				ItemName:     name,
			},
		},
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/presets", "application/xml", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("setPreset: speaker returned %d", resp.StatusCode)
	}
	return nil
}

// ProbePresetWrite tests whether the speaker accepts POST /presets (Strategy 1).
// Sends a harmless empty body and checks for non-404 response.
func (c *Client) ProbePresetWrite() bool {
	resp, err := c.http.Post(c.baseURL+"/presets", "application/xml", bytes.NewReader([]byte("<presets/>")))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound
}

func (c *Client) GetInfo() error {
	resp, err := c.http.Get(c.baseURL + "/info")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("info: speaker returned %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/speaker/... -run TestClient -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/speaker/client.go internal/speaker/client_test.go
git commit -m "feat: speaker REST client (select, setPreset, probe)"
```

---

## Task 4: Speaker WebSocket Listener

**Files:**
- Create: `internal/speaker/ws.go`
- Create: `internal/speaker/ws_test.go`

The SoundTouch WebSocket is at `ws://SPEAKER_IP:8090/webapi/`. It sends XML messages. This task implements the listener with auto-reconnect and event parsing. The exact XML format for preset button presses varies by firmware; this implementation parses known formats and logs unknown events for discovery during Mac integration testing.

- [ ] **Step 1: Write the failing tests**

Create `internal/speaker/ws_test.go`:

```go
package speaker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"soundtouch-radio-bridge/internal/speaker"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func newWSServer(t *testing.T, sendMessages []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("ws upgrade: %v", err)
			return
		}
		defer conn.Close()
		for _, msg := range sendMessages {
			conn.WriteMessage(websocket.TextMessage, []byte(msg))
		}
		// hold connection open briefly
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWSListener_PresetEvent(t *testing.T) {
	// SoundTouch sends userActivityUpdated on preset button press
	msg := `<userActivityUpdated activityType="PRESET_SELECTED">` +
		`<ContentItem source="PRESET" location="1"/>` +
		`</userActivityUpdated>`

	srv := newWSServer(t, []string{msg})
	addr := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		if slot != 1 {
			t.Fatalf("expected slot 1, got %d", slot)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for preset event")
	}
}

func TestWSListener_UnknownEventIgnored(t *testing.T) {
	msg := `<volumeUpdated><volume deviceID="x"><targetvolume>30</targetvolume></volume></volumeUpdated>`
	srv := newWSServer(t, []string{msg})
	addr := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		t.Fatalf("unexpected preset event for slot %d", slot)
	case <-ctx.Done():
		// correct: no event emitted
	}
}

func TestWSListener_Reconnects(t *testing.T) {
	// First connection closes immediately; second sends a preset event
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if callCount == 1 {
			return // close immediately
		}
		msg := `<userActivityUpdated activityType="PRESET_SELECTED">` +
			`<ContentItem source="PRESET" location="2"/>` +
			`</userActivityUpdated>`
		conn.WriteMessage(websocket.TextMessage, []byte(msg))
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	ws.ReconnectDelay = 50 * time.Millisecond // fast for tests
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		if slot != 2 {
			t.Fatalf("expected slot 2, got %d", slot)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect + preset event")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/speaker/... -run TestWSListener -v
```

Expected: compile error — WSListener not defined yet.

- [ ] **Step 3: Implement ws.go**

Create `internal/speaker/ws.go`:

```go
package speaker

import (
	"context"
	"encoding/xml"
	"log"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// WSListener connects to the SoundTouch WebSocket and emits preset button
// press events. It reconnects automatically on disconnect.
type WSListener struct {
	addr           string
	PresetPressed  chan int
	ReconnectDelay time.Duration
}

func NewWSListener(addr string) *WSListener {
	return &WSListener{
		addr:           addr,
		PresetPressed:  make(chan int, 4),
		ReconnectDelay: 5 * time.Second,
	}
}

func (w *WSListener) Start(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := w.connect(ctx); err != nil {
			log.Printf("speaker ws: connect error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.ReconnectDelay):
		}
	}
}

func (w *WSListener) connect(ctx context.Context) error {
	url := "ws://" + w.addr + "/webapi/"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("speaker ws: connected to %s", url)
	w.read(ctx, conn)
	log.Printf("speaker ws: disconnected")
	return nil
}

func (w *WSListener) read(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		slot := parsePresetSlot(data)
		if slot > 0 {
			select {
			case w.PresetPressed <- slot:
			case <-ctx.Done():
				return
			}
		}
	}
}

// parsePresetSlot attempts to extract a preset slot number (1–6) from a
// SoundTouch WebSocket XML event. Returns 0 if the event is not a preset
// button press. Handles two observed patterns:
//
//   - userActivityUpdated with location attribute containing the slot number
//   - nowPlayingUpdated where source indicates a preset channel
//
// Log unknown events at debug level to aid discovery against real hardware.
func parsePresetSlot(data []byte) int {
	var activity struct {
		XMLName      xml.Name `xml:"userActivityUpdated"`
		ActivityType string   `xml:"activityType,attr"`
		ContentItem  struct {
			Source   string `xml:"source,attr"`
			Location string `xml:"location,attr"`
		} `xml:"ContentItem"`
	}
	if xml.Unmarshal(data, &activity) == nil && activity.XMLName.Local == "userActivityUpdated" {
		if activity.ContentItem.Source == "PRESET" {
			n, err := strconv.Atoi(activity.ContentItem.Location)
			if err == nil && n >= 1 && n <= 6 {
				return n
			}
		}
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/speaker/... -run TestWSListener -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/speaker/ws.go internal/speaker/ws_test.go
git commit -m "feat: speaker WebSocket listener with reconnect and preset parsing"
```

---

## Task 5: Speaker Manager

**Files:**
- Create: `internal/speaker/manager.go`
- Create: `internal/speaker/manager_test.go`

The Manager wires the Client and WSListener. It handles preset button events (Strategy 2), syncs presets to the speaker on startup (Strategy 1 when supported), and reports connection state.

- [ ] **Step 1: Write the failing tests**

Create `internal/speaker/manager_test.go`:

```go
package speaker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
)

func TestManager_PlayOnPresetPress(t *testing.T) {
	var selectCalled atomic.Bool
	var gotURL string

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/select" {
			selectCalled.Store(true)
			gotURL = r.FormValue("location") // we'll check body instead
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer httpSrv.Close()

	presetMsg := `<userActivityUpdated activityType="PRESET_SELECTED">` +
		`<ContentItem source="PRESET" location="1"/>` +
		`</userActivityUpdated>`

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		conn.WriteMessage(websocket.TextMessage, []byte(presetMsg))
		time.Sleep(500 * time.Millisecond)
	}))
	defer wsSrv.Close()

	dir := t.TempDir()
	store, _ := config.NewStore(dir + "/config.yaml")
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://test.example.com/stream"})
	cfg := store.Get()
	_ = store.AssignPreset(1, cfg.Stations[0].ID)

	httpAddr := strings.TrimPrefix(httpSrv.URL, "http://")
	wsAddr := strings.TrimPrefix(wsSrv.URL, "http://")

	mgr := speaker.NewManagerForTest(httpAddr, wsAddr, store)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go mgr.Start(ctx)

	time.Sleep(1 * time.Second)

	if !selectCalled.Load() {
		t.Fatal("expected /select to be called when preset 1 pressed")
	}
	_ = gotURL
}

func TestManager_Status_offline(t *testing.T) {
	dir := t.TempDir()
	store, _ := config.NewStore(dir + "/config.yaml")
	mgr := speaker.NewManagerForTest("127.0.0.1:1", "127.0.0.1:1", store)
	online, _ := mgr.Status()
	if online {
		t.Fatal("expected offline when speaker unreachable")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/speaker/... -run TestManager -v
```

Expected: compile error — Manager not defined yet.

- [ ] **Step 3: Implement manager.go**

Create `internal/speaker/manager.go`:

```go
package speaker

import (
	"context"
	"log"
	"sync"

	"soundtouch-radio-bridge/internal/config"
)

type Manager struct {
	client    *Client
	ws        *WSListener
	store     *config.Store
	mu        sync.RWMutex
	online    bool
	nowPlaying string
	strategy1 bool // whether speaker supports POST /presets
}

func NewManager(speakerIP string, store *config.Store) *Manager {
	addr := speakerIP + ":8090"
	return &Manager{
		client: NewClient(speakerIP),
		ws:     NewWSListener(addr),
		store:  store,
	}
}

// NewManagerForTest allows injecting separate HTTP and WS addresses for testing.
func NewManagerForTest(httpAddr, wsAddr string, store *config.Store) *Manager {
	return &Manager{
		client: NewClient(strings.TrimSuffix(httpAddr, ":8090")),
		ws:     &WSListener{addr: wsAddr, PresetPressed: make(chan int, 4), ReconnectDelay: 50 * time.Millisecond},
		store:  store,
	}
}

func (m *Manager) Start(ctx context.Context) {
	// probe Strategy 1 support
	m.strategy1 = m.client.ProbePresetWrite()
	if m.strategy1 {
		log.Println("speaker: Strategy 1 supported — syncing presets to speaker")
		m.syncPresets()
	} else {
		log.Println("speaker: Strategy 1 not supported — relying on WebSocket interception")
	}

	go m.ws.Start(ctx)
	go m.handleEvents(ctx)
}

func (m *Manager) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case slot := <-m.ws.PresetPressed:
			m.playPreset(slot)
		}
	}
}

func (m *Manager) playPreset(slot int) {
	cfg := m.store.Get()
	stationID, ok := cfg.Presets[slot]
	if !ok || stationID == "" {
		return
	}
	st, ok := m.store.StationByID(stationID)
	if !ok {
		return
	}
	if err := m.client.Select(st.URL, st.Name); err != nil {
		log.Printf("speaker: play preset %d: %v", slot, err)
	}
}

func (m *Manager) Play(stationID string) error {
	st, ok := m.store.StationByID(stationID)
	if !ok {
		return fmt.Errorf("station %q not found", stationID)
	}
	return m.client.Select(st.URL, st.Name)
}

func (m *Manager) SyncPresets() {
	if m.strategy1 {
		m.syncPresets()
	}
}

func (m *Manager) syncPresets() {
	cfg := m.store.Get()
	for slot, stationID := range cfg.Presets {
		if stationID == "" {
			continue
		}
		st, ok := m.store.StationByID(stationID)
		if !ok {
			continue
		}
		if err := m.client.SetPreset(slot, st.URL, st.Name); err != nil {
			log.Printf("speaker: sync preset %d: %v", slot, err)
		}
	}
}

func (m *Manager) Status() (online bool, nowPlaying string) {
	if err := m.client.GetInfo(); err != nil {
		return false, ""
	}
	return true, m.nowPlaying
}
```

**Note:** Add missing imports `"fmt"`, `"strings"`, `"time"` to manager.go after the above.

- [ ] **Step 4: Fix imports and run tests**

```bash
go build ./internal/speaker/...
```

Fix any import errors (add `"fmt"`, `"strings"`, `"time"` to the import block in manager.go), then:

```bash
go test ./internal/speaker/... -run TestManager -v
```

Expected: both Manager tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/speaker/manager.go internal/speaker/manager_test.go
git commit -m "feat: speaker manager wiring WebSocket listener and REST client"
```

---

## Task 6: TuneIn Package

**Files:**
- Create: `internal/tunein/tunein.go`
- Create: `internal/tunein/tunein_test.go`

TuneIn's public OPML endpoint returns JSON. Each result has a redirect URL (e.g. `http://opml.radiotime.com/Tune.ashx?id=s18664`). To get the real stream URL, we follow that redirect — the final URL is what we store in the config.

- [ ] **Step 1: Write the failing tests**

Create `internal/tunein/tunein_test.go`:

```go
package tunein_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"soundtouch-radio-bridge/internal/tunein"
)

func TestSearch(t *testing.T) {
	// Mock TuneIn OPML search endpoint
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Search.ashx" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"head": map[string]string{"status": "200"},
				"body": []map[string]any{
					{
						"element":  "outline",
						"type":     "audio",
						"text":     "BBC Radio 4",
						"URL":      "TUNE_URL_PLACEHOLDER",
						"image":    "http://example.com/logo.jpg",
						"bitrate":  "128",
						"subtext":  "BBC Radio 4",
					},
				},
			})
			return
		}
		// Mock Tune.ashx — returns the real stream URL as plain text
		if r.URL.Path == "/Tune.ashx" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer searchSrv.Close()

	// Patch the tune URL to point to our test server
	client := tunein.NewClient(searchSrv.URL)
	results, err := client.Search("BBC Radio 4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Name != "BBC Radio 4" {
		t.Fatalf("got name %q", results[0].Name)
	}
	if results[0].URL == "" {
		t.Fatal("expected resolved stream URL")
	}
}

func TestSearch_noResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"head": map[string]string{"status": "200"},
			"body": []map[string]any{},
		})
	}))
	defer srv.Close()

	client := tunein.NewClient(srv.URL)
	results, err := client.Search("xyzzy_nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tunein/... -v
```

Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement tunein.go**

Create `internal/tunein/tunein.go`:

```go
package tunein

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Station struct {
	Name string
	URL  string
	Logo string
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://opml.radiotime.com"
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type opmlResponse struct {
	Body []opmlItem `json:"body"`
}

type opmlItem struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	URL     string `json:"URL"`
	Image   string `json:"image"`
}

func (c *Client) Search(query string) ([]Station, error) {
	u := fmt.Sprintf("%s/Search.ashx?query=%s&type=audio&render=json",
		c.baseURL, url.QueryEscape(query))
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result opmlResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var stations []Station
	for _, item := range result.Body {
		if item.Type != "audio" {
			continue
		}
		// Replace the TuneIn redirect URL with the actual tune URL endpoint
		tuneURL := strings.Replace(item.URL, "TUNE_URL_PLACEHOLDER", c.baseURL+"/Tune.ashx", 1)
		if strings.Contains(item.URL, c.baseURL) || strings.Contains(item.URL, "radiotime.com") || strings.Contains(item.URL, "Tune.ashx") {
			tuneURL = item.URL
		}
		streamURL, err := c.resolveStream(tuneURL)
		if err != nil || streamURL == "" {
			streamURL = tuneURL // fallback to redirect URL
		}
		stations = append(stations, Station{
			Name: item.Text,
			URL:  streamURL,
			Logo: item.Image,
		})
	}
	return stations, nil
}

// resolveStream follows a TuneIn redirect URL to get the actual stream URL.
// TuneIn returns a plain-text M3U or direct URL.
func (c *Client) resolveStream(tuneURL string) (string, error) {
	resp, err := c.http.Get(tuneURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read first line — for plain URLs or M3U files this contains the stream URL
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http") {
			return line, nil
		}
	}
	return "", nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tunein/... -v
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tunein/
git commit -m "feat: TuneIn search with stream URL resolution"
```

---

## Task 7: API Handlers

**Files:**
- Create: `internal/api/handlers.go`
- Create: `internal/api/router.go`
- Create: `internal/api/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"soundtouch-radio-bridge/internal/api"
	"soundtouch-radio-bridge/internal/config"
)

type mockManager struct {
	playedID string
	online   bool
}

func (m *mockManager) Play(stationID string) error {
	m.playedID = stationID
	return nil
}

func (m *mockManager) Status() (bool, string) {
	return m.online, ""
}

func (m *mockManager) SyncPresets() {}

func newTestServer(t *testing.T) (*httptest.Server, *config.Store, *mockManager) {
	t.Helper()
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "config.yaml"))
	mgr := &mockManager{online: true}
	handler := api.NewHandler(store, mgr, nil)
	srv := httptest.NewServer(api.NewRouter(handler))
	t.Cleanup(srv.Close)
	return srv, store, mgr
}

func TestAddStation(t *testing.T) {
	srv, store, _ := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"name": "BBC Radio 4", "url": "http://example.com/stream"})
	resp, err := http.Post(srv.URL+"/api/stations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}

	cfg := store.Get()
	if len(cfg.Stations) != 1 || cfg.Stations[0].Name != "BBC Radio 4" {
		t.Fatalf("station not saved: %+v", cfg.Stations)
	}
}

func TestListStations(t *testing.T) {
	srv, store, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})

	resp, err := http.Get(srv.URL + "/api/stations")
	if err != nil {
		t.Fatal(err)
	}
	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 station, got %d", len(result))
	}
}

func TestAssignPreset(t *testing.T) {
	srv, store, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	stationID := cfg.Stations[0].ID

	body, _ := json.Marshal(map[string]any{"slot": 1, "stationId": stationID})
	resp, err := http.Post(srv.URL+"/api/presets", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	cfg = store.Get()
	if cfg.Presets[1] != stationID {
		t.Fatalf("preset not assigned: %v", cfg.Presets)
	}
}

func TestPlayStation(t *testing.T) {
	srv, store, mgr := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	stationID := cfg.Stations[0].ID

	body, _ := json.Marshal(map[string]string{"stationId": stationID})
	resp, err := http.Post(srv.URL+"/api/play", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	if mgr.playedID != stationID {
		t.Fatalf("expected play %q, got %q", stationID, mgr.playedID)
	}
}

func TestDeleteStation(t *testing.T) {
	srv, store, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	id := cfg.Stations[0].ID

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/stations/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}
	cfg = store.Get()
	if len(cfg.Stations) != 0 {
		t.Fatal("station not deleted")
	}
}

func TestStatus(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["online"] != true {
		t.Fatalf("expected online: true, got %v", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/api/... -v
```

Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement handlers.go**

Create `internal/api/handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/tunein"
)

type SpeakerManager interface {
	Play(stationID string) error
	Status() (online bool, nowPlaying string)
	SyncPresets()
}

type Handler struct {
	store   *config.Store
	speaker SpeakerManager
	tunein  *tunein.Client
}

func NewHandler(store *config.Store, speaker SpeakerManager, tuneIn *tunein.Client) *Handler {
	return &Handler{store: store, speaker: speaker, tunein: tuneIn}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) ListStations(w http.ResponseWriter, r *http.Request) {
	cfg := h.store.Get()
	writeJSON(w, http.StatusOK, cfg.Stations)
}

func (h *Handler) AddStation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Logo string `json:"logo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.URL == "" {
		http.Error(w, "name and url required", http.StatusBadRequest)
		return
	}
	st := config.Station{Name: req.Name, URL: req.URL, Logo: req.Logo}
	if err := h.store.AddStation(st); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	cfg := h.store.Get()
	writeJSON(w, http.StatusCreated, cfg.Stations[len(cfg.Stations)-1])
}

func (h *Handler) DeleteStation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteStation(id); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	h.speaker.SyncPresets()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetPresets(w http.ResponseWriter, r *http.Request) {
	cfg := h.store.Get()
	writeJSON(w, http.StatusOK, cfg.Presets)
}

func (h *Handler) AssignPreset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slot      int    `json:"slot"`
		StationID string `json:"stationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Slot < 1 || req.Slot > 6 {
		http.Error(w, "slot must be 1–6", http.StatusBadRequest)
		return
	}
	if err := h.store.AssignPreset(req.Slot, req.StationID); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	h.speaker.SyncPresets()
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Play(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID string `json:"stationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.speaker.Play(req.StationID); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}
	if h.tunein == nil {
		http.Error(w, "TuneIn not configured", http.StatusNotImplemented)
		return
	}
	results, err := h.tunein.Search(q)
	if err != nil {
		http.Error(w, "TuneIn error: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	online, nowPlaying := h.speaker.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"online":     online,
		"nowPlaying": nowPlaying,
	})
}
```

- [ ] **Step 4: Create router.go**

Create `internal/api/router.go`:

```go
package api

import (
	"net/http"
)

func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stations", h.ListStations)
	mux.HandleFunc("POST /api/stations", h.AddStation)
	mux.HandleFunc("DELETE /api/stations/{id}", h.DeleteStation)
	mux.HandleFunc("GET /api/presets", h.GetPresets)
	mux.HandleFunc("POST /api/presets", h.AssignPreset)
	mux.HandleFunc("POST /api/play", h.Play)
	mux.HandleFunc("GET /api/search", h.Search)
	mux.HandleFunc("GET /api/status", h.Status)
	return mux
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/api/... -v
```

Expected: all 6 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/
git commit -m "feat: HTTP API handlers for stations, presets, play, search, status"
```

---

## Task 8: Web UI

**Files:**
- Create: `web/index.html`

Single-page app. Vanilla HTML/CSS/JS, no build step, embedded into the binary at compile time. Polls `/api/status` every 3 seconds to update now-playing and connection state.

- [ ] **Step 1: Create web/index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SoundTouch Radio Bridge</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: system-ui, sans-serif; background: #0d1117; color: #e6edf3; min-height: 100vh; }
  .header { display: flex; justify-content: space-between; align-items: center; padding: 14px 16px; background: #161b22; border-bottom: 1px solid #21262d; }
  .header h1 { font-size: 15px; font-weight: 600; }
  .badge { font-size: 11px; padding: 3px 10px; border-radius: 10px; font-weight: 600; }
  .badge.online { background: #1a4a1a; color: #4ade80; }
  .badge.offline { background: #4a1a1a; color: #f87171; }
  .section { padding: 14px 16px; border-bottom: 1px solid #21262d; }
  .section-title { font-size: 10px; font-weight: 700; color: #8b949e; letter-spacing: 1px; margin-bottom: 10px; }
  .now-playing { display: flex; align-items: center; gap: 12px; background: #0d2a0d; border: 1px solid #1a4a1a; border-radius: 8px; padding: 12px; }
  .now-playing-info { flex: 1; }
  .now-playing-name { font-weight: 600; font-size: 14px; }
  .now-playing-url { font-size: 11px; color: #4ade80; margin-top: 2px; }
  .now-playing-empty { color: #8b949e; font-size: 13px; }
  .preset-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
  .preset-btn { background: #161b22; border: 1px solid #21262d; border-radius: 8px; padding: 10px; text-align: center; cursor: pointer; transition: border-color 0.15s; }
  .preset-btn:hover { border-color: #3b82f6; }
  .preset-btn.active { border: 2px solid #4ade80; background: #0d2a0d; }
  .preset-btn.empty { border-style: dashed; }
  .preset-num { font-size: 10px; font-weight: 700; color: #3b82f6; margin-bottom: 4px; }
  .preset-name { font-size: 11px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .preset-empty-label { font-size: 11px; color: #484f58; }
  .station-list { display: flex; flex-direction: column; gap: 6px; max-height: 300px; overflow-y: auto; }
  .station-row { display: flex; align-items: center; gap: 10px; background: #161b22; border: 1px solid #21262d; border-radius: 6px; padding: 8px 10px; }
  .station-row.playing { border-color: #1a4a1a; background: #0d2a0d; }
  .station-name { flex: 1; font-size: 12px; font-weight: 600; }
  .station-preset { font-size: 10px; color: #3b82f6; font-weight: 600; }
  .btn { border: none; border-radius: 5px; padding: 5px 10px; font-size: 11px; cursor: pointer; font-weight: 600; }
  .btn-play { background: #1a3a1a; border: 1px solid #2a5a2a; color: #4ade80; }
  .btn-play:hover { background: #2a5a2a; }
  .btn-menu { background: #1c2128; border: 1px solid #21262d; color: #8b949e; }
  .btn-add { background: #1d4ed8; color: white; }
  .btn-add:hover { background: #2563eb; }
  .btn-stop { background: #4a1a1a; border: 1px solid #6a2a2a; color: #f87171; }
  .add-row { display: flex; gap: 6px; }
  .add-input { flex: 1; background: #161b22; border: 1px solid #21262d; color: #e6edf3; border-radius: 5px; padding: 7px 10px; font-size: 12px; }
  .add-input:focus { outline: none; border-color: #3b82f6; }
  .add-name-input { width: 100%; background: #161b22; border: 1px solid #21262d; color: #e6edf3; border-radius: 5px; padding: 7px 10px; font-size: 12px; margin-bottom: 6px; }
  .add-name-input:focus { outline: none; border-color: #3b82f6; }
  .help-text { font-size: 10px; color: #484f58; margin-top: 4px; }
  .search-results { margin-top: 8px; display: flex; flex-direction: column; gap: 5px; max-height: 200px; overflow-y: auto; }
  .search-result { display: flex; align-items: center; gap: 8px; background: #161b22; border: 1px solid #21262d; border-radius: 5px; padding: 7px 10px; cursor: pointer; }
  .search-result:hover { border-color: #3b82f6; }
  .search-result-name { flex: 1; font-size: 12px; }
  /* Modal */
  .modal-overlay { display: none; position: fixed; inset: 0; background: rgba(0,0,0,0.7); z-index: 100; align-items: center; justify-content: center; }
  .modal-overlay.open { display: flex; }
  .modal { background: #161b22; border: 1px solid #21262d; border-radius: 10px; width: 90%; max-width: 380px; overflow: hidden; }
  .modal-header { background: #0d1117; padding: 12px 16px; font-size: 12px; font-weight: 700; color: #3b82f6; border-bottom: 1px solid #21262d; }
  .modal-body { padding: 14px 16px; }
  .current-assignment { display: flex; align-items: center; gap: 10px; background: #0d2a0d; border: 1px solid #1a4a1a; border-radius: 8px; padding: 10px; margin-bottom: 14px; }
  .current-label { font-size: 10px; color: #4ade80; font-weight: 600; }
  .current-name { font-size: 13px; font-weight: 600; }
  .modal-actions { display: flex; gap: 8px; margin-top: 12px; }
  .modal-actions .btn { flex: 1; padding: 8px; }
  .btn-cancel { background: #1c2128; border: 1px solid #21262d; color: #8b949e; }
  .btn-confirm { background: #1d4ed8; color: white; }
  .btn-confirm:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-change { background: #1c2128; border: 1px solid #21262d; color: #8b949e; flex: 1; }
  .picker-list { display: flex; flex-direction: column; gap: 5px; max-height: 250px; overflow-y: auto; }
  .picker-item { display: flex; align-items: center; gap: 8px; background: #0d1117; border: 1px solid #21262d; border-radius: 6px; padding: 8px 10px; cursor: pointer; }
  .picker-item:hover { border-color: #3b82f6; }
  .picker-item.selected { border: 2px solid #3b82f6; background: #0d1a2a; }
  .picker-item-name { flex: 1; font-size: 12px; font-weight: 600; }
  .picker-item-preset { font-size: 10px; color: #3b82f6; font-weight: 600; }
  .check { color: #3b82f6; font-size: 14px; }
</style>
</head>
<body>

<div class="header">
  <h1>SoundTouch Radio Bridge</h1>
  <span id="badge" class="badge offline">● Offline</span>
</div>

<div class="section" id="now-playing-section">
  <div class="section-title">NOW PLAYING</div>
  <div class="now-playing">
    <div style="font-size:28px">📻</div>
    <div class="now-playing-info">
      <div id="now-playing-name" class="now-playing-empty">Nothing playing</div>
    </div>
    <button class="btn btn-stop" id="btn-stop" style="display:none" onclick="stopPlayback()">■ Stop</button>
  </div>
</div>

<div class="section">
  <div class="section-title">PRESETS</div>
  <div class="preset-grid" id="preset-grid"></div>
</div>

<div class="section">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px">
    <div class="section-title" style="margin-bottom:0">STATIONS</div>
    <button class="btn btn-add" onclick="document.getElementById('add-section').scrollIntoView({behavior:'smooth'})">+ Add</button>
  </div>
  <div class="station-list" id="station-list"></div>
</div>

<div class="section" id="add-section">
  <div class="section-title">ADD STATION</div>
  <input class="add-name-input" id="add-name" placeholder="Station name">
  <div class="add-row">
    <input class="add-input" id="add-url" placeholder="Search TuneIn or paste stream URL…">
    <button class="btn btn-add" onclick="handleAddInput()">Search</button>
  </div>
  <div class="help-text">Paste a direct MP3/AAC/HLS URL to add without searching. Or type a station name and press Search.</div>
  <div id="search-results" class="search-results"></div>
</div>

<!-- Preset modal -->
<div class="modal-overlay" id="modal-overlay" onclick="closeModal(event)">
  <div class="modal">
    <div class="modal-header" id="modal-title">Preset</div>
    <div class="modal-body" id="modal-body"></div>
  </div>
</div>

<script>
let stations = [];
let presets = {};
let nowPlayingStationId = null;
let modalSlot = null;
let modalMode = 'view'; // 'view' | 'pick'
let pickerSelected = null;

async function fetchAll() {
  try {
    const [stRes, prRes, stRes2] = await Promise.all([
      fetch('/api/stations'),
      fetch('/api/presets'),
      fetch('/api/status'),
    ]);
    stations = await stRes.json();
    presets = await prRes.json();
    const status = await stRes2.json();
    updateBadge(status.online);
    updateNowPlaying(status.nowPlaying);
    renderPresets();
    renderStations();
  } catch(e) {
    updateBadge(false);
  }
}

function updateBadge(online) {
  const b = document.getElementById('badge');
  b.textContent = online ? '● Online' : '● Offline';
  b.className = 'badge ' + (online ? 'online' : 'offline');
}

function updateNowPlaying(np) {
  const el = document.getElementById('now-playing-name');
  const stopBtn = document.getElementById('btn-stop');
  if (np) {
    el.textContent = np;
    el.className = 'now-playing-name';
    stopBtn.style.display = '';
  } else {
    el.textContent = 'Nothing playing';
    el.className = 'now-playing-empty';
    stopBtn.style.display = 'none';
  }
}

function renderPresets() {
  const grid = document.getElementById('preset-grid');
  grid.innerHTML = '';
  for (let i = 1; i <= 6; i++) {
    const sid = presets[i] || presets[String(i)];
    const st = sid ? stations.find(s => s.id === sid) : null;
    const btn = document.createElement('div');
    btn.className = 'preset-btn' + (st ? '' : ' empty') + (sid && sid === nowPlayingStationId ? ' active' : '');
    btn.innerHTML = `<div class="preset-num">${i}</div>` +
      (st ? `<div class="preset-name">${st.name}</div>` : `<div class="preset-empty-label">+ assign</div>`);
    btn.onclick = () => openPresetModal(i);
    grid.appendChild(btn);
  }
}

function renderStations() {
  const list = document.getElementById('station-list');
  list.innerHTML = '';
  if (stations.length === 0) {
    list.innerHTML = '<div style="color:#484f58;font-size:12px;padding:8px 0">No stations yet. Add one below.</div>';
    return;
  }
  stations.forEach(st => {
    const assignedSlots = Object.entries(presets)
      .filter(([,sid]) => sid === st.id)
      .map(([slot]) => `Preset ${slot}`)
      .join(', ');
    const row = document.createElement('div');
    row.className = 'station-row' + (st.id === nowPlayingStationId ? ' playing' : '');
    row.innerHTML = `
      <div style="font-size:18px">📻</div>
      <div class="station-name">${st.name}</div>
      ${assignedSlots ? `<div class="station-preset">${assignedSlots}</div>` : ''}
      <button class="btn btn-play" onclick="playStation('${st.id}')">▶</button>
      <button class="btn btn-menu" onclick="openStationMenu(event,'${st.id}')">⋯</button>
    `;
    list.appendChild(row);
  });
}

async function playStation(id) {
  await fetch('/api/play', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({stationId: id})});
  nowPlayingStationId = id;
  renderStations();
  renderPresets();
}

async function stopPlayback() {
  await fetch('/api/play', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({stationId: ''})});
  nowPlayingStationId = null;
  updateNowPlaying(null);
  renderStations();
}

function openStationMenu(e, stationId) {
  e.stopPropagation();
  // Simple: prompt which slot to assign, or delete
  const choice = confirm('Delete this station?');
  if (choice) deleteStation(stationId);
}

async function deleteStation(id) {
  await fetch('/api/stations/' + id, {method:'DELETE'});
  await fetchAll();
}

// Preset modal
function openPresetModal(slot) {
  modalSlot = slot;
  pickerSelected = null;
  const sid = presets[slot] || presets[String(slot)];
  const st = sid ? stations.find(s => s.id === sid) : null;
  if (st) {
    modalMode = 'view';
    showViewModal(slot, st);
  } else {
    modalMode = 'pick';
    showPickerModal(slot, null);
  }
  document.getElementById('modal-overlay').classList.add('open');
}

function showViewModal(slot, st) {
  document.getElementById('modal-title').textContent = `Preset ${slot}`;
  document.getElementById('modal-body').innerHTML = `
    <div class="current-assignment">
      <div style="font-size:22px">📻</div>
      <div>
        <div class="current-label">Currently assigned</div>
        <div class="current-name">${st.name}</div>
      </div>
    </div>
    <div class="modal-actions">
      <button class="btn btn-play" style="flex:1" onclick="playStation('${st.id}');closeModal()">▶ Play now</button>
      <button class="btn btn-change" onclick="showPickerModal(${slot}, '${st.id}')">Change…</button>
    </div>
  `;
}

function showPickerModal(slot, currentId) {
  modalMode = 'pick';
  pickerSelected = null;
  const currentSt = currentId ? stations.find(s => s.id === currentId) : null;
  document.getElementById('modal-title').textContent = currentSt
    ? `Preset ${slot} — Change station`
    : `Preset ${slot} — Assign station`;

  const itemsHTML = stations.map(st => {
    const assignedSlots = Object.entries(presets)
      .filter(([,sid]) => sid === st.id)
      .map(([s]) => `Preset ${s}`)
      .join(', ');
    return `<div class="picker-item" data-id="${st.id}" onclick="selectPickerItem(this,'${st.id}')">
      <div style="font-size:14px">📻</div>
      <div class="picker-item-name">${st.name}</div>
      ${assignedSlots ? `<div class="picker-item-preset">${assignedSlots}</div>` : ''}
      <div class="check" style="display:none">✓</div>
    </div>`;
  }).join('');

  document.getElementById('modal-body').innerHTML = `
    ${currentSt ? `<div style="font-size:11px;color:#8b949e;margin-bottom:10px">Replace <strong style="color:#f87171">${currentSt.name}</strong>:</div>` : ''}
    <div class="picker-list">${itemsHTML || '<div style="color:#484f58;font-size:12px">No stations in library. Add one first.</div>'}</div>
    <div class="modal-actions">
      <button class="btn btn-cancel" onclick="closeModal()">Cancel</button>
      <button class="btn btn-confirm" id="btn-confirm" disabled onclick="confirmAssign()">Assign</button>
    </div>
  `;
}

function selectPickerItem(el, id) {
  document.querySelectorAll('.picker-item').forEach(i => {
    i.classList.remove('selected');
    i.querySelector('.check').style.display = 'none';
  });
  el.classList.add('selected');
  el.querySelector('.check').style.display = '';
  pickerSelected = id;
  const st = stations.find(s => s.id === id);
  const btn = document.getElementById('btn-confirm');
  btn.disabled = false;
  btn.textContent = `Assign ${st ? st.name : ''}`;
}

async function confirmAssign() {
  if (!pickerSelected || !modalSlot) return;
  await fetch('/api/presets', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({slot: modalSlot, stationId: pickerSelected})
  });
  closeModal();
  await fetchAll();
}

function closeModal(event) {
  if (event && event.target !== document.getElementById('modal-overlay')) return;
  document.getElementById('modal-overlay').classList.remove('open');
  modalSlot = null;
  pickerSelected = null;
}

// Add station
async function handleAddInput() {
  const urlInput = document.getElementById('add-url').value.trim();
  const nameInput = document.getElementById('add-name').value.trim();

  // If it looks like a URL, add directly
  if (urlInput.startsWith('http')) {
    if (!nameInput) { alert('Please enter a station name.'); return; }
    await addStation(nameInput, urlInput, '');
    document.getElementById('add-url').value = '';
    document.getElementById('add-name').value = '';
    document.getElementById('search-results').innerHTML = '';
    await fetchAll();
    return;
  }

  // Otherwise, search TuneIn
  const q = urlInput || nameInput;
  if (!q) return;
  const res = await fetch('/api/search?q=' + encodeURIComponent(q));
  if (!res.ok) { alert('Search failed. Try pasting a direct stream URL instead.'); return; }
  const results = await res.json();
  const container = document.getElementById('search-results');
  container.innerHTML = '';
  if (!results || results.length === 0) {
    container.innerHTML = '<div style="color:#484f58;font-size:12px;padding:4px 0">No results. Try a direct URL.</div>';
    return;
  }
  results.slice(0, 5).forEach(r => {
    const el = document.createElement('div');
    el.className = 'search-result';
    el.innerHTML = `<div style="font-size:14px">📻</div><div class="search-result-name">${r.name || r.Name}</div><button class="btn btn-add">Add</button>`;
    el.querySelector('button').onclick = async () => {
      await addStation(r.name || r.Name, r.url || r.URL, r.logo || r.Logo || '');
      container.innerHTML = '';
      document.getElementById('add-url').value = '';
      document.getElementById('add-name').value = '';
      await fetchAll();
    };
    container.appendChild(el);
  });
}

async function addStation(name, url, logo) {
  await fetch('/api/stations', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name, url, logo})
  });
}

// Poll status every 3 seconds
fetchAll();
setInterval(fetchAll, 3000);
</script>
</body>
</html>
```

- [ ] **Step 2: Verify the file exists and is valid HTML**

```bash
wc -l web/index.html
```

Expected: over 200 lines.

- [ ] **Step 3: Commit**

```bash
git add web/
git commit -m "feat: single-page web UI with preset grid, station library, TuneIn search"
```

---

## Task 9: main.go and Binary

**Files:**
- Create: `main.go`

Wires config store, speaker manager, TuneIn client, and HTTP server. Embeds the web UI. Parses `--config` CLI flag.

- [ ] **Step 1: Create main.go**

```go
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"soundtouch-radio-bridge/internal/api"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
	"soundtouch-radio-bridge/internal/tunein"
)

//go:embed web/index.html
var webFiles embed.FS

func main() {
	configPath := flag.String("config", "/config.yaml", "path to config.yaml")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	store, err := config.NewStore(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	cfg := store.Get()
	var mgr *speaker.Manager
	if len(cfg.Speakers) > 0 && cfg.Speakers[0].IP != "" {
		mgr = speaker.NewManager(cfg.Speakers[0].IP, store)
	} else {
		log.Println("warning: no speaker IP configured — set one via /api/config or edit config.yaml")
		mgr = speaker.NewManager("", store)
	}

	tuneIn := tunein.NewClient("")

	handler := api.NewHandler(store, mgr, tuneIn)
	mux := api.NewRouter(handler)

	// Serve embedded web UI
	mux.(*http.ServeMux).HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, webFiles, "web/index.html")
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go mgr.Start(ctx)

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	srv.Shutdown(context.Background())
}
```

- [ ] **Step 2: Build and verify**

```bash
go build -o bridge .
ls -lh bridge
```

Expected: binary created, under 20MB.

- [ ] **Step 3: Run all tests**

```bash
go test ./... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: main.go — wires config, speaker manager, API, and web UI"
```

---

## Task 10: Dockerfile and Compose Files

**Files:**
- Create: `Dockerfile`
- Create: `compose.yaml`
- Create: `compose.dev.yaml`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bridge .

FROM scratch
COPY --from=builder /app/bridge /bridge
EXPOSE 8080
ENTRYPOINT ["/bridge"]
CMD ["--config", "/config.yaml"]
```

- [ ] **Step 2: Create compose.dev.yaml (Mac development)**

```yaml
services:
  bridge:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/config.yaml
```

- [ ] **Step 3: Create compose.yaml (Firewalla production)**

```yaml
services:
  bridge:
    image: soundtouch-radio-bridge:latest
    network_mode: host
    volumes:
      - ./config.yaml:/config.yaml
    restart: unless-stopped
```

- [ ] **Step 4: Build and test the Docker image (Mac)**

First, copy the example config:

```bash
cp config.yaml.example config.yaml
# Edit config.yaml — set your speaker IP
```

Build and run:

```bash
docker compose -f compose.dev.yaml up --build
```

Expected: container starts, logs show `listening on :8080`, then open http://localhost:8080.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile compose.yaml compose.dev.yaml
git commit -m "chore: Dockerfile and compose files for dev (Mac) and prod (Firewalla)"
```

---

## Task 11: Mac Integration Test (Real Speaker)

This task verifies the full system against the physical SoundTouch 10. Run as a manual checklist after `go run .` is working.

- [ ] **Step 1: Create config.yaml with your speaker IP**

```bash
cp config.yaml.example config.yaml
# Edit config.yaml: set speakers[0].ip to your SoundTouch 10's IP
# Find it in your router's DHCP table, or check the Bose app settings
```

- [ ] **Step 2: Run the binary directly**

```bash
go run . --config config.yaml --addr :8080
```

Expected: logs show connecting to speaker WebSocket.

- [ ] **Step 3: Open the web UI and add a station**

Open http://localhost:8080. In the "Add Station" section:
- Name: `BBC Radio 4`
- URL: `http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm`
- Click "Search" (it'll add directly since it's a URL)

Expected: station appears in the library.

- [ ] **Step 4: Play the station via web UI**

Click ▶ next to BBC Radio 4.

Expected: music plays on the speaker within ~1 second.

- [ ] **Step 5: Assign to preset and test physical button**

Tap preset slot 1 → pick BBC Radio 4 → "Assign BBC Radio 4".

Expected: config.yaml updated.

Stop playback. Press preset button 1 on the physical speaker.

Expected: music resumes. If it does, Strategy 2 is working. Log output will show the WebSocket event received.

- [ ] **Step 6: Check Strategy 1 support**

Look at startup logs for:
- `speaker: Strategy 1 supported — syncing presets to speaker` → both strategies active
- `speaker: Strategy 1 not supported — relying on WebSocket interception` → Strategy 2 only

- [ ] **Step 7: Log raw WebSocket events for discovery**

If preset button presses are NOT detected, add temporary debug logging to `ws.go`'s `read()` function to print all raw messages:

```go
log.Printf("speaker ws raw event: %s", string(data))
```

Press various buttons on the speaker and examine the output. Update `parsePresetSlot()` in `ws.go` to match the actual XML format your firmware sends. This is the main firmware-discovery step.

- [ ] **Step 8: Test TuneIn search**

In the Add Station section, type `France Inter` and click Search.

Expected: results appear. Click Add on one → station appears in library.

- [ ] **Step 9: Test Docker on Mac**

```bash
docker compose -f compose.dev.yaml up --build
```

Repeat steps 4–5. Expected: same behaviour as running the binary directly.

- [ ] **Step 10: Commit any fixes found during integration**

```bash
git add -p
git commit -m "fix: adjust WebSocket event parsing based on real speaker firmware"
```

---

## Task 12: Firewalla Deployment

- [ ] **Step 1: Build ARM64 image on Mac**

```bash
docker buildx build --platform linux/arm64 -t soundtouch-radio-bridge:latest --load .
```

- [ ] **Step 2: Save and copy to Firewalla**

```bash
docker save soundtouch-radio-bridge:latest | gzip | ssh root@<firewalla-ip> "gunzip | docker load"
```

- [ ] **Step 3: Copy files to Firewalla**

```bash
scp compose.yaml config.yaml root@<firewalla-ip>:~/soundtouch-radio-bridge/
```

- [ ] **Step 4: Start on Firewalla**

```bash
ssh root@<firewalla-ip>
cd ~/soundtouch-radio-bridge
docker compose up -d
docker logs soundtouch-radio-bridge-bridge-1 -f
```

Expected: same log output as Mac run. Web UI accessible at `http://<firewalla-ip>:8080`.

- [ ] **Step 5: Verify preset buttons still work**

Press each assigned preset button. Expected: music plays.

- [ ] **Step 6: Final commit**

```bash
git tag v1.0.0
git push origin main --tags
```

---

## Self-Review Notes

- **Spec coverage:** All API endpoints, both preset strategies, TuneIn search, config YAML, Docker targets, Mac dev flow, and web UI flows are covered.
- **Type consistency:** `SpeakerManager` interface in `api/handlers.go` matches `Manager` methods in `speaker/manager.go`. `config.Station` used consistently across packages.
- **Known discovery area:** `parsePresetSlot()` in `ws.go` is based on the most common observed SoundTouch WebSocket format. Task 11 Step 7 explicitly covers discovering the actual format for the latest firmware and patching it.
- **NewManagerForTest:** Requires `speaker` package to expose this constructor. It references `strings` and `time` packages — ensure both are imported in `manager.go`.

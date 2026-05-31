package api_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"soundtouch-radio-bridge/internal/api"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
)

type mockManager struct {
	playedID string
	online   bool
	targetIP string
}

func (m *mockManager) Play(stationID string) error {
	m.playedID = stationID
	return nil
}

func (m *mockManager) Status() (bool, string) {
	return m.online, ""
}

func (m *mockManager) SyncPresets() {}

func (m *mockManager) SetTarget(ip string) error {
	m.targetIP = ip
	return nil
}

type mockDiscoverer struct {
	results []speaker.Discovered
	err     error
}

func (d mockDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]speaker.Discovered, error) {
	return d.results, d.err
}

func newTestServer(t *testing.T) (*httptest.Server, *config.Store, *mockManager, *mockDiscoverer) {
	t.Helper()
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "config.yaml"))
	mgr := &mockManager{online: true}
	disc := &mockDiscoverer{}
	handler := api.NewHandler(store, mgr, nil, disc)
	srv := httptest.NewServer(api.NewRouter(handler, embed.FS{}))
	t.Cleanup(srv.Close)
	return srv, store, mgr, disc
}

func TestAddStation(t *testing.T) {
	srv, store, _, _ := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"name": "BBC Radio 4", "url": "http://example.com/stream"})
	resp, err := http.Post(srv.URL+"/api/stations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}

	cfg := store.Get()
	if len(cfg.Stations) != 1 || cfg.Stations[0].Name != "BBC Radio 4" {
		t.Fatalf("station not saved: %+v", cfg.Stations)
	}
}

func TestListStations(t *testing.T) {
	srv, store, _, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})

	resp, err := http.Get(srv.URL + "/api/stations")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 station, got %d", len(result))
	}
}

func TestAssignPreset(t *testing.T) {
	srv, store, _, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	stationID := cfg.Stations[0].ID

	body, _ := json.Marshal(map[string]any{"slot": 1, "stationId": stationID})
	resp, err := http.Post(srv.URL+"/api/presets", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	cfg = store.Get()
	if cfg.Presets[1] != stationID {
		t.Fatalf("preset not assigned: %v", cfg.Presets)
	}
}

func TestPlayStation(t *testing.T) {
	srv, store, mgr, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	stationID := cfg.Stations[0].ID

	body, _ := json.Marshal(map[string]string{"stationId": stationID})
	resp, err := http.Post(srv.URL+"/api/play", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	if mgr.playedID != stationID {
		t.Fatalf("expected play %q, got %q", stationID, mgr.playedID)
	}
}

func TestDeleteStation(t *testing.T) {
	srv, store, _, _ := newTestServer(t)
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://example.com"})
	cfg := store.Get()
	id := cfg.Stations[0].ID

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/stations/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}
	cfg = store.Get()
	if len(cfg.Stations) != 0 {
		t.Fatal("station not deleted")
	}
}

func TestStatus(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["online"] != true {
		t.Fatalf("expected online: true, got %v", result)
	}
}

// ===== Speaker management handler tests =====

func TestListSpeakers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("GET", "/api/speakers", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Active   string           `json:"active"`
		Speakers []config.Speaker `json:"speakers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Active != "A" || len(got.Speakers) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestAddSpeakerHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "c.yaml"))
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/speakers", strings.NewReader(`{"name":"Kitchen","ip":"192.168.1.50"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 201 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.Speakers()) != 1 {
		t.Fatal("not saved")
	}
}

func TestAddSpeakerHandler_Duplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("speakers:\n  - name: Kitchen\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/speakers", strings.NewReader(`{"name":"Kitchen","ip":"2.2.2.2"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestAddSpeakerHandler_BadIP(t *testing.T) {
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "c.yaml"))
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/speakers", strings.NewReader(`{"name":"Kitchen","ip":"not-an-ip"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 400 {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}

func TestRemoveSpeakerHandler_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("DELETE", "/api/speakers/B", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 204 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.Speakers()) != 1 {
		t.Fatal("not removed")
	}
}

func TestRemoveSpeakerHandler_RejectsActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("DELETE", "/api/speakers/A", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestRemoveSpeakerHandler_Unknown(t *testing.T) {
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "c.yaml"))
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("DELETE", "/api/speakers/Ghost", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}

func TestRenameSpeakerHandler_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("PATCH", "/api/speakers/A", strings.NewReader(`{"name":"Alpha"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if store.Speakers()[0].Name != "Alpha" {
		t.Fatalf("not renamed: %+v", store.Speakers())
	}
	act, _ := store.Active()
	if act.Name != "Alpha" {
		t.Fatalf("active not updated: %q", act.Name)
	}
}

func TestRenameSpeakerHandler_Conflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("PATCH", "/api/speakers/A", strings.NewReader(`{"name":"B"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestSetActiveSpeakerHandler_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	mgr := &mockManager{}
	h := api.NewHandler(store, mgr, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/speakers/active", strings.NewReader(`{"name":"B"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	act, _ := store.Active()
	if act.Name != "B" {
		t.Fatalf("active = %q, want B", act.Name)
	}
	if mgr.targetIP != "2.2.2.2" {
		t.Fatalf("SetTarget called with %q, want 2.2.2.2", mgr.targetIP)
	}
}

func TestSetActiveSpeakerHandler_Unknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/speakers/active", strings.NewReader(`{"name":"Ghost"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}

func TestDiscoverHandler_ReturnsResults(t *testing.T) {
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "c.yaml"))
	disc := mockDiscoverer{results: []speaker.Discovered{
		{Name: "Living Room", IP: "192.168.1.50"},
		{Name: "Kitchen", IP: "192.168.1.51"},
	}}
	h := api.NewHandler(store, &mockManager{}, nil, disc)
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/discover", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Found []speaker.Discovered `json:"found"`
	}
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Found) != 2 || got.Found[0].Name != "Living Room" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiscoverHandler_EmptyResults(t *testing.T) {
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "c.yaml"))
	h := api.NewHandler(store, &mockManager{}, nil, mockDiscoverer{})
	mux := api.NewRouter(h, embed.FS{})

	req := httptest.NewRequest("POST", "/api/discover", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Found []speaker.Discovered `json:"found"`
	}
	json.NewDecoder(rr.Body).Decode(&got)
	if got.Found == nil {
		t.Fatal("got nil slice, want []")
	}
}

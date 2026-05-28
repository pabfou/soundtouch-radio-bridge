package api_test

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
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
	srv := httptest.NewServer(api.NewRouter(handler, embed.FS{}))
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
	srv, store, _ := newTestServer(t)
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
	srv, store, _ := newTestServer(t)
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
	srv, store, mgr := newTestServer(t)
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
	srv, store, _ := newTestServer(t)
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
	srv, _, _ := newTestServer(t)
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

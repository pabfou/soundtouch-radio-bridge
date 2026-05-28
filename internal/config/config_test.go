package config_test

import (
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

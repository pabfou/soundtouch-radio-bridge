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

func TestSpeakers_SnapshotIsIndependent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Speakers()
	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Fatalf("got %+v", got)
	}
	got[0].Name = "MUTATED"
	again := s.Speakers()
	if again[0].Name == "MUTATED" {
		t.Fatal("Speakers() returned shared slice; expected a copy")
	}
}

func TestActive_FallsBackToFirstWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := config.NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "A" {
		t.Fatalf("expected A, got %+v ok=%v", act, ok)
	}
}

func TestActive_RespectsActiveSpeakerField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("active_speaker: B\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := config.NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "B" {
		t.Fatalf("expected B, got %+v ok=%v", act, ok)
	}
}

func TestActive_FallsBackOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("active_speaker: NOPE\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := config.NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "A" {
		t.Fatalf("expected fallback to A, got %+v ok=%v", act, ok)
	}
}

func TestActive_FalseWhenNoSpeakers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := config.NewStore(path)
	_, ok := s.Active()
	if ok {
		t.Fatal("expected ok=false when no speakers")
	}
}

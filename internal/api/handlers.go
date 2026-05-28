package api

import (
	"encoding/json"
	"io"
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
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
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
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	var req struct {
		Slot      int    `json:"slot"`
		StationID string `json:"stationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Slot < 1 || req.Slot > 6 {
		http.Error(w, "slot must be 1-6", http.StatusBadRequest)
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
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
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

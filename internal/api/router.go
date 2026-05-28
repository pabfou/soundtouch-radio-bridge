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

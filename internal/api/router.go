package api

import (
	"embed"
	"io/fs"
	"net/http"
)

func NewRouter(h *Handler, webFS embed.FS) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stations", h.ListStations)
	mux.HandleFunc("POST /api/stations", h.AddStation)
	mux.HandleFunc("DELETE /api/stations/{id}", h.DeleteStation)
	mux.HandleFunc("GET /api/presets", h.GetPresets)
	mux.HandleFunc("POST /api/presets", h.AssignPreset)
	mux.HandleFunc("POST /api/play", h.Play)
	mux.HandleFunc("GET /api/search", h.Search)
	mux.HandleFunc("GET /api/status", h.Status)
	mux.HandleFunc("GET /stream/{id}", h.Stream)
	mux.HandleFunc("HEAD /stream/{id}", h.Stream)

	sub, err := fs.Sub(webFS, "web")
	if err == nil {
		mux.Handle("GET /", http.FileServer(http.FS(sub)))
	}
	return mux
}

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
	mux.HandleFunc("POST /api/stop", h.Stop)
	mux.HandleFunc("GET /api/search", h.Search)
	mux.HandleFunc("GET /api/status", h.Status)
	mux.HandleFunc("GET /stream/{id}", h.Stream)
	mux.HandleFunc("HEAD /stream/{id}", h.Stream)

	// Speaker management
	mux.HandleFunc("GET /api/speakers", h.ListSpeakers)
	mux.HandleFunc("POST /api/speakers", h.AddSpeakerHandler)
	mux.HandleFunc("DELETE /api/speakers/{name}", h.RemoveSpeakerHandler)
	mux.HandleFunc("PATCH /api/speakers/{name}", h.RenameSpeakerHandler)
	mux.HandleFunc("POST /api/speakers/active", h.SetActiveSpeakerHandler)
	mux.HandleFunc("POST /api/discover", h.DiscoverHandler)

	// Profile management
	mux.HandleFunc("GET /api/profiles", h.ListProfilesHandler)
	mux.HandleFunc("POST /api/profiles", h.AddProfileHandler)
	mux.HandleFunc("PATCH /api/profiles/{name}", h.RenameProfileHandler)
	mux.HandleFunc("DELETE /api/profiles/{name}", h.RemoveProfileHandler)
	mux.HandleFunc("POST /api/profiles/{name}/save", h.SaveProfileHandler)
	mux.HandleFunc("POST /api/profiles/{name}/load", h.LoadProfileHandler)
	mux.HandleFunc("POST /api/profiles/active", h.SetActiveProfileHandler)

	sub, err := fs.Sub(webFS, "web")
	if err == nil {
		mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, sub, "settings.html")
		})
		mux.Handle("GET /", http.FileServer(http.FS(sub)))
	}
	return mux
}

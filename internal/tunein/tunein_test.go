package tunein_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"soundtouch-radio-bridge/internal/tunein"
)

func TestSearch(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Search.ashx" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"head": map[string]string{"status": "200"},
				"body": []map[string]any{
					{
						"element": "outline",
						"type":    "audio",
						"text":    "BBC Radio 4",
						"URL":     srvURL + "/Tune.ashx?id=s1234",
						"image":   "http://example.com/logo.jpg",
					},
				},
			})
			return
		}
		if r.URL.Path == "/Tune.ashx" {
			// Return the actual stream URL as plain text
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	srvURL = srv.URL // set after server starts, before Search is called

	client := tunein.NewClient(srv.URL)
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
	if results[0].URL != "http://stream.live.vc.bbcmedia.co.uk/bbc_radio_fourfm" {
		t.Fatalf("got URL %q", results[0].URL)
	}
	if results[0].Logo != "http://example.com/logo.jpg" {
		t.Fatalf("got Logo %q", results[0].Logo)
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

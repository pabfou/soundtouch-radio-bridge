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
		if r.URL.Path == "/storePreset" && r.Method == http.MethodPost {
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
		if r.URL.Path == "/storePreset" && r.Method == http.MethodPost {
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

	var preset struct {
		XMLName xml.Name `xml:"preset"`
		ID      int      `xml:"id,attr"`
	}
	if err := xml.Unmarshal([]byte(gotBody), &preset); err != nil {
		t.Fatalf("invalid XML: %v — body: %s", err, gotBody)
	}
	if preset.ID != 1 {
		t.Fatalf("expected preset id=1, got %d", preset.ID)
	}
}

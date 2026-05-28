package speaker_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
)

func TestManager_PlayOnPresetPress(t *testing.T) {
	var selectCalled atomic.Bool

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/select" && r.Method == http.MethodPost {
			io.Copy(io.Discard, r.Body)
			selectCalled.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer httpSrv.Close()

	// Correct event format: nowSelectionUpdated with preset id
	presetMsg := `<nowSelectionUpdated><preset id="1" /></nowSelectionUpdated>`

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		conn.WriteMessage(websocket.TextMessage, []byte(presetMsg))
		time.Sleep(500 * time.Millisecond)
	}))
	defer wsSrv.Close()

	dir := t.TempDir()
	store, _ := config.NewStore(dir + "/config.yaml")
	_ = store.AddStation(config.Station{Name: "Test FM", URL: "http://test.example.com/stream"})
	cfg := store.Get()
	_ = store.AssignPreset(1, cfg.Stations[0].ID)

	httpAddr := strings.TrimPrefix(httpSrv.URL, "http://")
	wsAddr := strings.TrimPrefix(wsSrv.URL, "http://")

	mgr := speaker.NewManagerForTest(httpAddr, wsAddr, store)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go mgr.Start(ctx)

	time.Sleep(1 * time.Second)

	if !selectCalled.Load() {
		t.Fatal("expected /select to be called when preset 1 pressed")
	}
}

func TestManager_Status_offline(t *testing.T) {
	dir := t.TempDir()
	store, _ := config.NewStore(dir + "/config.yaml")
	mgr := speaker.NewManagerForTest("127.0.0.1:1", "127.0.0.1:1", store)
	online, _ := mgr.Status()
	if online {
		t.Fatal("expected offline when speaker unreachable")
	}
}

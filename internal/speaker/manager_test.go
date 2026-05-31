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
	var playSetURI atomic.Bool

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/AVTransport/Control" && r.Method == http.MethodPost {
			action := r.Header.Get("SOAPAction")
			if strings.Contains(action, "SetAVTransportURI") {
				playSetURI.Store(true)
			}
		}
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer httpSrv.Close()

	// Speaker wraps events in <updates>...</updates>
	presetMsg := `<updates><nowSelectionUpdated><preset id="1" /></nowSelectionUpdated></updates>`

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

	if !playSetURI.Load() {
		t.Fatal("expected UPnP SetAVTransportURI when preset 1 pressed")
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

func TestSetTarget_SwapsHTTPClient(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<info deviceID="aaa"></info>`))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<info deviceID="bbb"></info>`))
	}))
	defer srvB.Close()

	addrA := strings.TrimPrefix(srvA.URL, "http://")
	addrB := strings.TrimPrefix(srvB.URL, "http://")

	store, err := config.NewStore(t.TempDir() + "/c.yaml")
	if err != nil {
		t.Fatal(err)
	}

	m := speaker.NewManagerForTest(addrA, "127.0.0.1:1", store)
	if online, _ := m.Status(); !online {
		t.Fatal("expected online against A")
	}

	if err := m.SetTarget(addrB); err != nil {
		t.Fatalf("SetTarget: %v", err)
	}

	if online, _ := m.Status(); !online {
		t.Fatal("expected online against B")
	}
}

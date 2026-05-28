package speaker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"soundtouch-radio-bridge/internal/speaker"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func newWSServer(t *testing.T, sendMessages []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("ws upgrade: %v", err)
			return
		}
		defer conn.Close()
		for _, msg := range sendMessages {
			conn.WriteMessage(websocket.TextMessage, []byte(msg))
		}
		// hold connection open briefly
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWSListener_PresetEvent(t *testing.T) {
	msg := `<nowSelectionUpdated><preset id="1" /></nowSelectionUpdated>`

	srv := newWSServer(t, []string{msg})
	addr := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		if slot != 1 {
			t.Fatalf("expected slot 1, got %d", slot)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for preset event")
	}
}

func TestWSListener_UnknownEventIgnored(t *testing.T) {
	msg := `<volumeUpdated><volume deviceID="x"><targetvolume>30</targetvolume></volume></volumeUpdated>`
	srv := newWSServer(t, []string{msg})
	addr := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		t.Fatalf("unexpected preset event for slot %d", slot)
	case <-ctx.Done():
		// correct: no event emitted
	}
}

func TestWSListener_Reconnects(t *testing.T) {
	// First connection closes immediately; second sends a preset event
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if n == 1 {
			return // close immediately
		}
		msg := `<nowSelectionUpdated><preset id="2" /></nowSelectionUpdated>`
		conn.WriteMessage(websocket.TextMessage, []byte(msg))
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ws := speaker.NewWSListener(addr)
	ws.ReconnectDelay = 50 * time.Millisecond // fast for tests
	go ws.Start(ctx)

	select {
	case slot := <-ws.PresetPressed:
		if slot != 2 {
			t.Fatalf("expected slot 2, got %d", slot)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect + preset event")
	}
}

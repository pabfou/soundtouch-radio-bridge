package speaker

import (
	"context"
	"encoding/xml"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type WSListener struct {
	addr           string
	PresetPressed  chan int
	ReconnectDelay time.Duration
}

func NewWSListener(addr string) *WSListener {
	return &WSListener{
		addr:           addr,
		PresetPressed:  make(chan int, 4),
		ReconnectDelay: 5 * time.Second,
	}
}

func (w *WSListener) Start(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := w.connect(ctx); err != nil {
			log.Printf("speaker ws: connect error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.ReconnectDelay):
		}
	}
}

func (w *WSListener) connect(ctx context.Context) error {
	// WebSocket is on port 8080; addr may already include port (tests) or be host-only (real speaker)
	wsURL := "ws://" + w.addr
	if !strings.Contains(w.addr, ":") {
		wsURL = "ws://" + w.addr + ":8080"
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("speaker ws: connected to %s", wsURL)
	w.read(ctx, conn)
	log.Printf("speaker ws: disconnected")
	return nil
}

func (w *WSListener) read(ctx context.Context, conn *websocket.Conn) {
	// Close the connection when ctx is cancelled so ReadMessage unblocks.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		slot := parsePresetSlot(data)
		if slot > 0 {
			select {
			case w.PresetPressed <- slot:
			case <-ctx.Done():
				return
			}
		}
	}
}

// parsePresetSlot extracts a preset slot number from a SoundTouch WebSocket
// XML event. Returns 0 if the event is not a preset button press.
// The speaker sends nowSelectionUpdated with a nested <preset id="N"> element.
func parsePresetSlot(data []byte) int {
	var event struct {
		XMLName xml.Name `xml:"nowSelectionUpdated"`
		Preset  struct {
			ID string `xml:"id,attr"`
		} `xml:"preset"`
	}
	if xml.Unmarshal(data, &event) == nil && event.XMLName.Local == "nowSelectionUpdated" {
		n, err := strconv.Atoi(event.Preset.ID)
		if err == nil && n >= 1 && n <= 6 {
			return n
		}
	}
	return 0
}

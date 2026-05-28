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
	// SoundTouch requires the "gabbo" subprotocol to emit events
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{"gabbo"}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("speaker ws: connected to %s (subprotocol=%q)", wsURL, conn.Subprotocol())
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
// The speaker sends events wrapped in <updates>...</updates>:
//
//	<updates ...><nowSelectionUpdated><preset id="N">...</preset></nowSelectionUpdated></updates>
//
// Older firmware may also send the inner element directly.
func parsePresetSlot(data []byte) int {
	// Try wrapped form first
	var wrapped struct {
		XMLName  xml.Name `xml:"updates"`
		Updated  struct {
			Preset struct {
				ID string `xml:"id,attr"`
			} `xml:"preset"`
		} `xml:"nowSelectionUpdated"`
	}
	if xml.Unmarshal(data, &wrapped) == nil && wrapped.XMLName.Local == "updates" {
		if n, err := strconv.Atoi(wrapped.Updated.Preset.ID); err == nil && n >= 1 && n <= 6 {
			return n
		}
	}
	// Fall back to bare form
	var bare struct {
		XMLName xml.Name `xml:"nowSelectionUpdated"`
		Preset  struct {
			ID string `xml:"id,attr"`
		} `xml:"preset"`
	}
	if xml.Unmarshal(data, &bare) == nil && bare.XMLName.Local == "nowSelectionUpdated" {
		if n, err := strconv.Atoi(bare.Preset.ID); err == nil && n >= 1 && n <= 6 {
			return n
		}
	}
	return 0
}

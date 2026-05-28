package speaker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"soundtouch-radio-bridge/internal/config"
)

type Manager struct {
	client     *Client
	ws         *WSListener
	store      *config.Store
	mu         sync.RWMutex
	nowPlaying string
	strategy1  bool
}

// NewManager creates a Manager for a real speaker at speakerIP (bare IP, no port).
// REST client connects to :8090; WebSocket connects to :8080.
func NewManager(speakerIP string, store *config.Store) *Manager {
	return &Manager{
		client: NewClient(speakerIP),
		ws:     NewWSListener(speakerIP),
		store:  store,
	}
}

// NewManagerForTest allows injecting separate HTTP and WS addresses (host:port) for unit tests.
func NewManagerForTest(httpAddr, wsAddr string, store *config.Store) *Manager {
	return &Manager{
		client: NewClient(httpAddr),
		ws: &WSListener{
			addr:           wsAddr,
			PresetPressed:  make(chan int, 4),
			ReconnectDelay: 50 * time.Millisecond,
		},
		store: store,
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.strategy1 = m.client.ProbePresetWrite()
	if m.strategy1 {
		log.Println("speaker: Strategy 1 supported — syncing presets to speaker")
		m.syncPresets()
	} else {
		log.Println("speaker: Strategy 1 not supported — relying on WebSocket interception")
	}

	go m.ws.Start(ctx)
	m.handleEvents(ctx)
}

func (m *Manager) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case slot := <-m.ws.PresetPressed:
			m.playPreset(slot)
		}
	}
}

func (m *Manager) playPreset(slot int) {
	cfg := m.store.Get()
	stationID, ok := cfg.Presets[slot]
	if !ok || stationID == "" {
		return
	}
	st, ok := m.store.StationByID(stationID)
	if !ok {
		return
	}
	if err := m.client.Select(st.URL, st.Name); err != nil {
		log.Printf("speaker: play preset %d: %v", slot, err)
		return
	}
	m.mu.Lock()
	m.nowPlaying = st.Name
	m.mu.Unlock()
}

// Play immediately plays a station by ID on the speaker.
func (m *Manager) Play(stationID string) error {
	st, ok := m.store.StationByID(stationID)
	if !ok {
		return fmt.Errorf("station %q not found", stationID)
	}
	err := m.client.Select(st.URL, st.Name)
	if err == nil {
		m.mu.Lock()
		m.nowPlaying = st.Name
		m.mu.Unlock()
	}
	return err
}

// SyncPresets re-syncs all assigned presets to the speaker (Strategy 1).
// No-op if Strategy 1 is not supported.
func (m *Manager) SyncPresets() {
	if m.strategy1 {
		m.syncPresets()
	}
}

func (m *Manager) syncPresets() {
	cfg := m.store.Get()
	for slot, stationID := range cfg.Presets {
		if stationID == "" {
			continue
		}
		st, ok := m.store.StationByID(stationID)
		if !ok {
			continue
		}
		if err := m.client.SetPreset(slot, st.URL, st.Name); err != nil {
			log.Printf("speaker: sync preset %d: %v", slot, err)
		}
	}
}

// Status checks if the speaker is reachable and returns what's currently playing.
func (m *Manager) Status() (online bool, nowPlaying string) {
	if err := m.client.GetInfo(); err != nil {
		return false, ""
	}
	m.mu.RLock()
	np := m.nowPlaying
	m.mu.RUnlock()
	return true, np
}

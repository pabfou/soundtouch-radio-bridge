package speaker

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"soundtouch-radio-bridge/internal/config"
)

type Manager struct {
	switchMu   sync.RWMutex // held write by SetTarget, read by Play/Status
	client     *Client
	upnp       *UPnPClient
	ws         *WSListener
	store      *config.Store
	mu         sync.RWMutex
	nowPlaying string
	strategy1  bool
	// bridgeURL is set to e.g. "http://192.168.x.y:8080" if the bridge should
	// proxy streams via /stream/{id}. Empty = send the upstream URL directly.
	bridgeURL string

	// Set by Start; used to re-derive WS context on SetTarget.
	parentCtx context.Context
	wsCancel  context.CancelFunc
}

// SetBridgeURL configures the bridge's own reachable URL so playback can be
// routed through the /stream/{id} proxy.
func (m *Manager) SetBridgeURL(u string) {
	m.mu.Lock()
	m.bridgeURL = u
	m.mu.Unlock()
}

func (m *Manager) playbackURL(st config.Station) string {
	m.mu.RLock()
	b := m.bridgeURL
	m.mu.RUnlock()
	// SoundTouch 10 (2015 hardware) can't fetch HTTPS and can't parse HLS
	// playlists — always proxy those so /stream/{id} transmuxes/relays.
	mustProxy := st.NeedsProxy ||
		strings.HasPrefix(st.URL, "https://") ||
		strings.HasSuffix(st.URL, ".m3u8")
	if !mustProxy || b == "" {
		log.Printf("speaker: playing %q via direct URL %s", st.Name, st.URL)
		return st.URL
	}
	proxy := b + "/stream/" + st.ID
	log.Printf("speaker: playing %q via proxy %s (upstream %s)", st.Name, proxy, st.URL)
	return proxy
}

// NewManager creates a Manager for a real speaker at speakerIP (bare IP, no port).
// REST client uses :8090; UPnP uses :8091; WebSocket uses :8080.
func NewManager(speakerIP string, store *config.Store) *Manager {
	return &Manager{
		client: NewClient(speakerIP),
		upnp:   NewUPnPClient(speakerIP),
		ws:     NewWSListener(speakerIP),
		store:  store,
	}
}

// NewManagerForTest allows injecting separate HTTP and WS addresses (host:port) for unit tests.
// upnpAddr may be empty for tests that don't exercise UPnP.
func NewManagerForTest(httpAddr, wsAddr string, store *config.Store) *Manager {
	return &Manager{
		client: NewClient(httpAddr),
		upnp:   NewUPnPClient(httpAddr),
		ws: &WSListener{
			addr:           wsAddr,
			PresetPressed:  make(chan int, 4),
			ReconnectDelay: 50 * time.Millisecond,
		},
		store: store,
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.parentCtx = ctx
	m.mu.Unlock()

	s1 := m.client.ProbePresetWrite()
	m.mu.Lock()
	m.strategy1 = s1
	m.mu.Unlock()
	if s1 {
		log.Println("speaker: Strategy 1 supported — syncing presets to speaker")
		m.syncPresets()
	} else {
		log.Println("speaker: Strategy 1 not supported — relying on WebSocket interception")
	}

	wsCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.wsCancel = cancel
	m.mu.Unlock()

	go m.ws.Start(wsCtx)
	m.handleEvents(ctx)
}

// SetTarget swaps the underlying speaker connection to a new IP. Cancels the
// existing WebSocket goroutine, replaces client/upnp/ws references, and
// re-spawns the WS goroutine against the new target using the parent context
// captured in Start. Safe to call before Start (test path): swaps connections
// without spawning a WS goroutine.
func (m *Manager) SetTarget(speakerIP string) error {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()

	m.mu.RLock()
	parent := m.parentCtx
	cancel := m.wsCancel
	m.mu.RUnlock()

	if parent == nil {
		m.client = NewClient(speakerIP)
		m.upnp = NewUPnPClient(speakerIP)
		m.ws = NewWSListener(speakerIP)
		return nil
	}

	if cancel != nil {
		cancel()
	}

	m.client = NewClient(speakerIP)
	m.upnp = NewUPnPClient(speakerIP)
	m.ws = NewWSListener(speakerIP)

	wsCtx, newCancel := context.WithCancel(parent)
	m.mu.Lock()
	m.wsCancel = newCancel
	m.nowPlaying = ""
	m.mu.Unlock()

	go m.ws.Start(wsCtx)
	return nil
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
	if err := m.upnp.Play(m.playbackURL(st), st.Name); err != nil {
		log.Printf("speaker: play preset %d: %v", slot, err)
		return
	}
	m.mu.Lock()
	m.nowPlaying = st.Name
	m.mu.Unlock()
}

// Play immediately plays a station by ID on the speaker.
func (m *Manager) Play(stationID string) error {
	m.switchMu.RLock()
	defer m.switchMu.RUnlock()
	st, ok := m.store.StationByID(stationID)
	if !ok {
		return fmt.Errorf("station %q not found", stationID)
	}
	err := m.upnp.Play(m.playbackURL(st), st.Name)
	if err == nil {
		m.mu.Lock()
		m.nowPlaying = st.Name
		m.mu.Unlock()
	}
	return err
}

// Stop halts playback on the speaker.
func (m *Manager) Stop() error {
	m.switchMu.RLock()
	defer m.switchMu.RUnlock()
	if err := m.upnp.Stop(); err != nil {
		return err
	}
	m.mu.Lock()
	m.nowPlaying = ""
	m.mu.Unlock()
	return nil
}

// SyncPresets re-syncs all assigned presets to the speaker (Strategy 1).
// No-op if Strategy 1 is not supported.
func (m *Manager) SyncPresets() {
	m.mu.RLock()
	s1 := m.strategy1
	m.mu.RUnlock()
	if s1 {
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
	m.switchMu.RLock()
	defer m.switchMu.RUnlock()
	if err := m.client.GetInfo(); err != nil {
		return false, ""
	}
	m.mu.RLock()
	np := m.nowPlaying
	m.mu.RUnlock()
	return true, np
}

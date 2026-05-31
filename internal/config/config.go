package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Station struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	Logo string `yaml:"logo,omitempty" json:"logo,omitempty"`
	// NeedsProxy is true when the upstream URL rejects HEAD probes (e.g. BBC).
	// In that case the speaker is given the bridge's /stream/{id} URL instead
	// of the upstream URL directly. Probed once when the station is added.
	NeedsProxy bool `yaml:"needs_proxy,omitempty" json:"needsProxy,omitempty"`
}

type Speaker struct {
	Name string `yaml:"name"`
	IP   string `yaml:"ip"`
}

type Config struct {
	Speakers      []Speaker      `yaml:"speakers"`
	ActiveSpeaker string         `yaml:"active_speaker,omitempty"`
	Stations      []Station      `yaml:"stations"`
	Presets       map[int]string `yaml:"presets"`
}

type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

func GenerateID(name string, existing []Station) string {
	s := strings.ToLower(name)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	taken := map[string]bool{}
	for _, st := range existing {
		taken[st.ID] = true
	}
	id := s
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", s, i)
	}
	return id
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg.Presets = map[int]string{}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &s.cfg); err != nil {
		return nil, err
	}
	if s.cfg.Presets == nil {
		s.cfg.Presets = map[int]string{}
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	speakers := make([]Speaker, len(s.cfg.Speakers))
	copy(speakers, s.cfg.Speakers)
	cfg.Speakers = speakers
	stations := make([]Station, len(s.cfg.Stations))
	copy(stations, s.cfg.Stations)
	cfg.Stations = stations
	presets := make(map[int]string, len(s.cfg.Presets))
	for k, v := range s.cfg.Presets {
		presets[k] = v
	}
	cfg.Presets = presets
	return cfg
}

func (s *Store) StationByID(id string) (Station, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.cfg.Stations {
		if st.ID == id {
			return st, true
		}
	}
	return Station{}, false
}

func (s *Store) AddStation(st Station) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.ID = GenerateID(st.Name, s.cfg.Stations)
	s.cfg.Stations = append(s.cfg.Stations, st)
	return s.save()
}

func (s *Store) DeleteStation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]Station, 0, len(s.cfg.Stations))
	for _, st := range s.cfg.Stations {
		if st.ID != id {
			filtered = append(filtered, st)
		}
	}
	s.cfg.Stations = filtered
	for slot, sid := range s.cfg.Presets {
		if sid == id {
			s.cfg.Presets[slot] = ""
		}
	}
	return s.save()
}

func (s *Store) AssignPreset(slot int, stationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Presets[slot] = stationID
	return s.save()
}

func (s *Store) SetSpeakerIP(ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Speakers) == 0 {
		s.cfg.Speakers = []Speaker{{Name: "Speaker", IP: ip}}
	} else {
		s.cfg.Speakers[0].IP = ip
	}
	return s.save()
}

// SetSpeaker replaces the first speaker entry (or adds one) with the given
// name and IP. Used by startup auto-discovery.
func (s *Store) SetSpeaker(spk Speaker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Speakers) == 0 {
		s.cfg.Speakers = []Speaker{spk}
	} else {
		s.cfg.Speakers[0] = spk
	}
	return s.save()
}

func (s *Store) save() error {
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Speakers returns a snapshot copy of the saved speaker list.
func (s *Store) Speakers() []Speaker {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Speaker, len(s.cfg.Speakers))
	copy(out, s.cfg.Speakers)
	return out
}

// Active resolves the active speaker. Returns ok=false if no speakers exist.
// If active_speaker is set and matches a name, that speaker wins. If unset or
// pointing at an unknown name, falls back to speakers[0].
func (s *Store) Active() (Speaker, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.cfg.Speakers) == 0 {
		return Speaker{}, false
	}
	if s.cfg.ActiveSpeaker != "" {
		for _, sp := range s.cfg.Speakers {
			if sp.Name == s.cfg.ActiveSpeaker {
				return sp, true
			}
		}
	}
	return s.cfg.Speakers[0], true
}

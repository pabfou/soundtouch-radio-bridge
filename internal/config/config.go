package config

import (
	"errors"
	"fmt"
	"net"
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
	Name string `yaml:"name" json:"name"`
	IP   string `yaml:"ip" json:"ip"`
}

type Profile struct {
	Name     string         `yaml:"name" json:"name"`
	Stations []Station      `yaml:"stations" json:"stations"`
	Presets  map[int]string `yaml:"presets" json:"presets"`
}

type Config struct {
	Speakers      []Speaker      `yaml:"speakers"`
	ActiveSpeaker string         `yaml:"active_speaker,omitempty"`
	Stations      []Station      `yaml:"stations"`
	Presets       map[int]string `yaml:"presets"`
	Profiles      []Profile      `yaml:"profiles,omitempty"`
	ActiveProfile string         `yaml:"active_profile,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

var (
	ErrUnknownSpeaker   = errors.New("speaker not found")
	ErrDuplicateName    = errors.New("speaker name already exists")
	ErrEmptyName        = errors.New("name is empty")
	ErrInvalidIP        = errors.New("speaker ip is invalid")
	ErrActiveSpeaker    = errors.New("cannot remove the active speaker")
	ErrUnknownProfile   = errors.New("profile not found")
	ErrDuplicateProfile = errors.New("profile name already exists")
	ErrActiveProfile    = errors.New("cannot remove the active profile")
	ErrLastProfile      = errors.New("cannot remove the only profile")
)

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

// SetActive sets the active speaker by name. Returns ErrUnknownSpeaker if no
// saved speaker matches name. Persists to disk.
func (s *Store) SetActive(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sp := range s.cfg.Speakers {
		if sp.Name == name {
			s.cfg.ActiveSpeaker = name
			return s.save()
		}
	}
	return ErrUnknownSpeaker
}

// AddSpeaker validates and appends a new speaker. Name is trimmed; IP must
// parse as net.IP.
func (s *Store) AddSpeaker(spk Speaker) error {
	spk.Name = strings.TrimSpace(spk.Name)
	if spk.Name == "" {
		return ErrEmptyName
	}
	if net.ParseIP(spk.IP) == nil {
		return ErrInvalidIP
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.cfg.Speakers {
		if existing.Name == spk.Name {
			return ErrDuplicateName
		}
	}
	s.cfg.Speakers = append(s.cfg.Speakers, spk)
	return s.save()
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

// RemoveSpeaker deletes a speaker by name. Returns ErrUnknownSpeaker if not
// present, ErrActiveSpeaker if attempting to remove the currently-active one.
func (s *Store) RemoveSpeaker(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	activeName := s.cfg.ActiveSpeaker
	if activeName == "" && len(s.cfg.Speakers) > 0 {
		activeName = s.cfg.Speakers[0].Name
	}
	if name == activeName {
		return ErrActiveSpeaker
	}
	idx := -1
	for i, sp := range s.cfg.Speakers {
		if sp.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrUnknownSpeaker
	}
	s.cfg.Speakers = append(s.cfg.Speakers[:idx], s.cfg.Speakers[idx+1:]...)
	return s.save()
}

// RenameSpeaker changes a speaker's name. If the renamed speaker is the
// active one, ActiveSpeaker is updated in the same locked save.
func (s *Store) RenameSpeaker(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return ErrEmptyName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, sp := range s.cfg.Speakers {
		if sp.Name == oldName {
			idx = i
		}
		if sp.Name == newName && sp.Name != oldName {
			return ErrDuplicateName
		}
	}
	if idx < 0 {
		return ErrUnknownSpeaker
	}
	wasActive := s.cfg.ActiveSpeaker == oldName
	s.cfg.Speakers[idx].Name = newName
	if wasActive {
		s.cfg.ActiveSpeaker = newName
	}
	return s.save()
}

// ===== Profiles =====

// Profiles returns a snapshot of profiles. Falls back to the embedded factory
// set when the config has no profiles defined.
func (s *Store) Profiles() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.cfg.Profiles) == 0 {
		return FactoryProfiles()
	}
	return cloneProfiles(s.cfg.Profiles)
}

// ActiveProfile returns the name of the active profile. If active_profile is
// empty or unknown, returns the first profile name.
func (s *Store) ActiveProfile() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profiles := s.cfg.Profiles
	if len(profiles) == 0 {
		profiles = factoryProfiles
	}
	if s.cfg.ActiveProfile != "" {
		for _, p := range profiles {
			if p.Name == s.cfg.ActiveProfile {
				return p.Name
			}
		}
	}
	if len(profiles) > 0 {
		return profiles[0].Name
	}
	return ""
}

// materializeProfilesLocked copies the factory set into config when no
// profiles have been persisted yet. Caller must hold s.mu (write).
func (s *Store) materializeProfilesLocked() {
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
	}
}

// SetActiveProfile sets the active profile by name. Returns ErrUnknownProfile
// if no saved profile matches name.
func (s *Store) SetActiveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeProfilesLocked()
	for _, p := range s.cfg.Profiles {
		if p.Name == name {
			s.cfg.ActiveProfile = name
			return s.save()
		}
	}
	return ErrUnknownProfile
}

// AddProfile creates an empty profile (no stations, six empty preset slots).
func (s *Store) AddProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeProfilesLocked()
	for _, p := range s.cfg.Profiles {
		if p.Name == name {
			return ErrDuplicateProfile
		}
	}
	s.cfg.Profiles = append(s.cfg.Profiles, Profile{
		Name:     name,
		Stations: []Station{},
		Presets:  map[int]string{1: "", 2: "", 3: "", 4: "", 5: "", 6: ""},
	})
	return s.save()
}

// RenameProfile changes a profile's name. If renaming the active profile,
// ActiveProfile is updated in the same locked save.
func (s *Store) RenameProfile(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return ErrEmptyName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeProfilesLocked()
	idx := -1
	for i, p := range s.cfg.Profiles {
		if p.Name == oldName {
			idx = i
		}
		if p.Name == newName && p.Name != oldName {
			return ErrDuplicateProfile
		}
	}
	if idx < 0 {
		return ErrUnknownProfile
	}
	wasActive := s.cfg.ActiveProfile == oldName
	s.cfg.Profiles[idx].Name = newName
	if wasActive {
		s.cfg.ActiveProfile = newName
	}
	return s.save()
}

// RemoveProfile deletes a profile by name. Refuses to remove the active
// profile or the last remaining profile.
func (s *Store) RemoveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeProfilesLocked()
	if len(s.cfg.Profiles) <= 1 {
		return ErrLastProfile
	}
	activeName := s.cfg.ActiveProfile
	if activeName == "" {
		activeName = s.cfg.Profiles[0].Name
	}
	if name == activeName {
		return ErrActiveProfile
	}
	idx := -1
	for i, p := range s.cfg.Profiles {
		if p.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrUnknownProfile
	}
	s.cfg.Profiles = append(s.cfg.Profiles[:idx], s.cfg.Profiles[idx+1:]...)
	return s.save()
}

// SaveProfile snapshots the current top-level Stations + Presets into the
// named profile, overwriting whatever was there.
func (s *Store) SaveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeProfilesLocked()
	idx := -1
	for i, p := range s.cfg.Profiles {
		if p.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrUnknownProfile
	}
	stationsCopy := append([]Station(nil), s.cfg.Stations...)
	presetsCopy := map[int]string{}
	for k, v := range s.cfg.Presets {
		presetsCopy[k] = v
	}
	s.cfg.Profiles[idx].Stations = stationsCopy
	s.cfg.Profiles[idx].Presets = presetsCopy
	return s.save()
}

// LoadProfile replaces the current top-level Stations + Presets with a copy
// of the named profile's contents.
func (s *Store) LoadProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles := s.cfg.Profiles
	if len(profiles) == 0 {
		profiles = factoryProfiles
	}
	for _, p := range profiles {
		if p.Name == name {
			s.cfg.Stations = append([]Station(nil), p.Stations...)
			s.cfg.Presets = map[int]string{}
			for k, v := range p.Presets {
				s.cfg.Presets[k] = v
			}
			return s.save()
		}
	}
	return ErrUnknownProfile
}

// MaybeSeedFromFactory is called once at startup. If the config has no
// stations, no presets, and no profiles, copy the PalmaSola factory profile's
// stations and presets into the top-level config, so first-boot shows the
// curated radio set. No-op otherwise.
func (s *Store) MaybeSeedFromFactory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Stations) > 0 || len(s.cfg.Presets) > 0 || len(s.cfg.Profiles) > 0 {
		return nil
	}
	for _, p := range factoryProfiles {
		if p.Name == "PalmaSola" {
			s.cfg.Stations = append([]Station(nil), p.Stations...)
			s.cfg.Presets = map[int]string{}
			for k, v := range p.Presets {
				s.cfg.Presets[k] = v
			}
			return s.save()
		}
	}
	return nil
}

# Station Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add named station/preset profiles (PalmaSola / VertChasseur / Autres seeded out of the box) with Save / Reload / Add / Rename / Delete from the web UI, eliminating the need to hand-edit `config.yaml` between deployment sites.

**Architecture:** Add a `Profile` type and a `profiles` list to `config.yaml`, alongside `active_profile`. Ship a bundled `internal/config/factory_profiles.yaml` (embedded via `go:embed`) that seeds PalmaSola from the current `presets.txt` content. `Profiles()` falls back to the embedded factory when the user's config has no profiles section. Seven REST endpoints under `/api/profiles` plus a dropdown + Reload/Save/Manage modal on the Presets card.

**Tech Stack:** Go 1.22 (stdlib `embed` + `net/http` mux path params), `gopkg.in/yaml.v3`, vanilla HTML/JS for the new dropdown and modal.

**Spec:** [docs/superpowers/specs/2026-05-31-speaker-discovery-ui-design.md](../specs/2026-05-31-speaker-discovery-ui-design.md) (Station Profiles section)

**Depends on:** Nothing in the Speaker Management plan. They share `internal/config/config.go` and `internal/api/router.go`, so the two plans can be merged sequentially without conflict, but neither blocks the other.

---

## File Structure

**Create:**
- `internal/config/factory_profiles.yaml` — embedded seed data (PalmaSola + two empties)
- `internal/config/factory.go` — `//go:embed` directive + parsed `factoryProfiles` package variable

**Modify:**
- `internal/config/config.go` — `Profile` type, `Profiles []Profile` + `ActiveProfile string` on `Config`, seven profile methods + error sentinels
- `internal/config/config_test.go` — tests for all new methods + factory fallback + first-run seeding
- `internal/api/handlers.go` — seven new handlers
- `internal/api/handlers_test.go` — handler tests
- `internal/api/router.go` — wire seven new routes
- `main.go` — call new `store.MaybeSeedFromFactory()` on startup
- `web/index.html` — Presets card additions (dropdown, Reload/Save/Manage buttons, Manage modal, JS)
- `README.md` — replace pointer to `presets.txt` with pointer to `factory_profiles.yaml`

**Delete:**
- `presets.txt`

---

## Task 1: Define `Profile` type, factory YAML, embed loader

**Files:**
- Create: `internal/config/factory_profiles.yaml`
- Create: `internal/config/factory.go`
- Modify: `internal/config/config.go` (add `Profile` type + `Profiles`/`ActiveProfile` fields)
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestFactoryProfiles_LoadsThreeSeeded(t *testing.T) {
	profiles := factoryProfiles
	if len(profiles) != 3 {
		t.Fatalf("expected 3 factory profiles, got %d", len(profiles))
	}
	names := map[string]bool{}
	for _, p := range profiles {
		names[p.Name] = true
	}
	for _, want := range []string{"PalmaSola", "VertChasseur", "Autres"} {
		if !names[want] {
			t.Errorf("missing factory profile %q", want)
		}
	}
}

func TestFactoryProfiles_PalmaSolaSeededFromPresetsTxt(t *testing.T) {
	var palma Profile
	for _, p := range factoryProfiles {
		if p.Name == "PalmaSola" {
			palma = p
			break
		}
	}
	if len(palma.Stations) < 6 {
		t.Fatalf("PalmaSola has %d stations, want at least 6 (RTBF, France Culture, France Inter, RNE, Onda Cero, BBC One)", len(palma.Stations))
	}
	// Spot-check one preset assignment.
	if palma.Presets[3] != "france-inter" {
		t.Errorf("expected preset 3 = france-inter, got %q", palma.Presets[3])
	}
}

func TestFactoryProfiles_OthersEmpty(t *testing.T) {
	for _, p := range factoryProfiles {
		if p.Name == "PalmaSola" {
			continue
		}
		if len(p.Stations) != 0 {
			t.Errorf("%s should have no stations, got %d", p.Name, len(p.Stations))
		}
		for slot := 1; slot <= 6; slot++ {
			if p.Presets[slot] != "" {
				t.Errorf("%s preset %d should be empty, got %q", p.Name, slot, p.Presets[slot])
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestFactoryProfiles -v`

Expected: compile errors — `factoryProfiles undefined`, `Profile undefined`.

- [ ] **Step 3: Add the `Profile` type and update `Config`**

In `internal/config/config.go`, add the `Profile` type below `Speaker`:

```go
type Profile struct {
	Name     string         `yaml:"name" json:"name"`
	Stations []Station      `yaml:"stations" json:"stations"`
	Presets  map[int]string `yaml:"presets" json:"presets"`
}
```

Update the `Config` struct to add the new fields (append to the struct):

```go
type Config struct {
	Speakers      []Speaker      `yaml:"speakers"`
	ActiveSpeaker string         `yaml:"active_speaker,omitempty"`
	Stations      []Station      `yaml:"stations"`
	Presets       map[int]string `yaml:"presets"`
	Profiles      []Profile      `yaml:"profiles,omitempty"`
	ActiveProfile string         `yaml:"active_profile,omitempty"`
}
```

(If you're integrating this plan after Plan A, the `ActiveSpeaker` field already exists; just add `Profiles` and `ActiveProfile`.)

- [ ] **Step 4: Create the factory YAML**

Write to `internal/config/factory_profiles.yaml`:

```yaml
profiles:
  - name: PalmaSola
    stations:
      - id: rtbf-la-premiere
        name: RTBF La Première
        url: http://radios.rtbf.be/laprem1ere-128.mp3
      - id: france-culture
        name: France Culture
        url: http://direct.franceculture.fr/live/franceculture-midfi.mp3
      - id: france-inter
        name: France Inter
        url: http://direct.franceinter.fr/live/franceinter-midfi.mp3
      - id: rne
        name: RNE (Spain)
        url: https://dispatcher.rndfnk.com/crtve/rne1/mad/mp3/high
      - id: onda-cero
        name: Onda Cero (Spain)
        url: https://atres-live.ondacero.es/live/ondacero/bitrate_1.m3u8
      - id: bbc-radio-one
        name: BBC Radio One
        url: http://as-hls-ww-live.akamaized.net/pool_01505109/live/ww/bbc_radio_one/bbc_radio_one.isml/bbc_radio_one-audio=96000.norewind.m3u8
      - id: rtl
        name: RTL
        url: http://streamer-02.rtl.fr/rtl-1-44-128
      - id: npr-news
        name: NPR News
        url: https://npr-ice.streamguys1.com/live.mp3
    presets:
      1: rtbf-la-premiere
      2: france-culture
      3: france-inter
      4: rne
      5: onda-cero
      6: bbc-radio-one
  - name: VertChasseur
    stations: []
    presets:
      1: ""
      2: ""
      3: ""
      4: ""
      5: ""
      6: ""
  - name: Autres
    stations: []
    presets:
      1: ""
      2: ""
      3: ""
      4: ""
      5: ""
      6: ""
```

- [ ] **Step 5: Create the embed loader**

Write to `internal/config/factory.go`:

```go
package config

import (
	_ "embed"
	"log"

	"gopkg.in/yaml.v3"
)

//go:embed factory_profiles.yaml
var factoryProfilesYAML []byte

// factoryProfiles is parsed once at package init. It is treated as immutable.
var factoryProfiles []Profile

func init() {
	var wrapper struct {
		Profiles []Profile `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(factoryProfilesYAML, &wrapper); err != nil {
		log.Fatalf("config: parse factory_profiles.yaml: %v", err)
	}
	factoryProfiles = wrapper.Profiles
}

// FactoryProfiles returns a deep copy of the embedded factory profile set.
func FactoryProfiles() []Profile {
	return cloneProfiles(factoryProfiles)
}

func cloneProfiles(in []Profile) []Profile {
	out := make([]Profile, len(in))
	for i, p := range in {
		out[i] = Profile{
			Name:     p.Name,
			Stations: append([]Station(nil), p.Stations...),
			Presets:  map[int]string{},
		}
		for k, v := range p.Presets {
			out[i].Presets[k] = v
		}
	}
	return out
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestFactoryProfiles -v`

Expected: 3 PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/factory_profiles.yaml internal/config/factory.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): Profile type + embedded factory_profiles.yaml"
```

---

## Task 2: Config — `Profiles()` + `ActiveProfile()` resolution

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestProfiles_FallsBackToFactory(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers: []\n"), 0644)
	s, _ := NewStore(path)
	got := s.Profiles()
	if len(got) != 3 {
		t.Fatalf("expected factory fallback (3 profiles), got %d", len(got))
	}
}

func TestProfiles_UsesConfigWhenSet(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: OnlyOne\n    stations: []\n    presets: {1: \"\"}\n"), 0644)
	s, _ := NewStore(path)
	got := s.Profiles()
	if len(got) != 1 || got[0].Name != "OnlyOne" {
		t.Fatalf("got %+v", got)
	}
}

func TestActiveProfile_FallsBackToFirst(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers: []\n"), 0644)
	s, _ := NewStore(path)
	if name := s.ActiveProfile(); name != "PalmaSola" {
		t.Fatalf("got %q, want PalmaSola (first factory)", name)
	}
}

func TestActiveProfile_RespectsField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_profile: VertChasseur\nspeakers: []\n"), 0644)
	s, _ := NewStore(path)
	if name := s.ActiveProfile(); name != "VertChasseur" {
		t.Fatalf("got %q, want VertChasseur", name)
	}
}

func TestActiveProfile_FallsBackOnUnknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_profile: NopeProfile\nspeakers: []\n"), 0644)
	s, _ := NewStore(path)
	if name := s.ActiveProfile(); name != "PalmaSola" {
		t.Fatalf("got %q, want fallback PalmaSola", name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestProfiles_|TestActiveProfile_' -v`

Expected: compile errors — `s.Profiles undefined`, `s.ActiveProfile undefined`.

- [ ] **Step 3: Implement resolution methods**

Append to `internal/config/config.go`:

```go
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

// ActiveProfile returns the name of the active profile. If the configured
// active_profile is empty or refers to an unknown profile, returns the first
// profile in the effective list.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestProfiles_|TestActiveProfile_' -v`

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): Profiles + ActiveProfile resolution with factory fallback"
```

---

## Task 3: Config — `SetActiveProfile`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSetActiveProfile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.SetActiveProfile("B"); err != nil {
		t.Fatal(err)
	}
	if name := s.ActiveProfile(); name != "B" {
		t.Fatalf("got %q, want B", name)
	}
}

func TestSetActiveProfile_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.SetActiveProfile("Nope"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("got %v, want ErrUnknownProfile", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestSetActiveProfile -v`

Expected: compile errors — `s.SetActiveProfile undefined`, `ErrUnknownProfile undefined`.

- [ ] **Step 3: Add profile error sentinels and `SetActiveProfile`**

In `internal/config/config.go`, extend the existing error block (or add this block if Plan A wasn't merged):

```go
var (
	ErrUnknownProfile  = errors.New("profile not found")
	ErrDuplicateProfile = errors.New("profile name already exists")
	ErrActiveProfile   = errors.New("cannot remove the active profile")
	ErrLastProfile     = errors.New("cannot remove the only profile")
)
```

(`ErrEmptyName` from Plan A is reused; if Plan A isn't merged, add it too — see Plan A Task 2 Step 3.)

Append:

```go
// SetActiveProfile sets the active profile by name. Materializes the factory
// profile set into config on first call (so the persisted active_profile is
// meaningful).
func (s *Store) SetActiveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Materialize factory profiles on first mutation so they're persisted.
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
	}
	for _, p := range s.cfg.Profiles {
		if p.Name == name {
			s.cfg.ActiveProfile = name
			return s.save()
		}
	}
	return ErrUnknownProfile
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestSetActiveProfile -v`

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): SetActiveProfile + profile error sentinels"
```

---

## Task 4: Config — `AddProfile`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestAddProfile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.AddProfile("New"); err != nil {
		t.Fatal(err)
	}
	got := s.Profiles()
	if len(got) != 2 || got[1].Name != "New" {
		t.Fatalf("got %+v", got)
	}
	// New profile starts with empty stations and empty preset slots 1-6.
	if len(got[1].Stations) != 0 {
		t.Fatalf("new profile should have no stations")
	}
	for slot := 1; slot <= 6; slot++ {
		if got[1].Presets[slot] != "" {
			t.Fatalf("preset %d should be empty, got %q", slot, got[1].Presets[slot])
		}
	}
}

func TestAddProfile_Duplicate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.AddProfile("A"); !errors.Is(err, ErrDuplicateProfile) {
		t.Fatalf("got %v, want ErrDuplicateProfile", err)
	}
}

func TestAddProfile_EmptyName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.AddProfile("   "); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("got %v, want ErrEmptyName", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestAddProfile -v`

Expected: compile error — `s.AddProfile undefined`.

- [ ] **Step 3: Implement `AddProfile`**

Append to `internal/config/config.go`:

```go
// AddProfile creates an empty profile (no stations, six empty preset slots).
func (s *Store) AddProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
	}
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestAddProfile -v`

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): AddProfile"
```

---

## Task 5: Config — `RenameProfile`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestRenameProfile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameProfile("A", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if s.Profiles()[0].Name != "Alpha" {
		t.Fatalf("not renamed: %+v", s.Profiles())
	}
}

func TestRenameProfile_OfActiveUpdatesActiveProfile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_profile: A\nprofiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameProfile("A", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if s.ActiveProfile() != "Alpha" {
		t.Fatalf("got %q, want Alpha", s.ActiveProfile())
	}
}

func TestRenameProfile_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameProfile("Nope", "X"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("got %v, want ErrUnknownProfile", err)
	}
}

func TestRenameProfile_Duplicate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameProfile("A", "B"); !errors.Is(err, ErrDuplicateProfile) {
		t.Fatalf("got %v, want ErrDuplicateProfile", err)
	}
}

func TestRenameProfile_EmptyNewName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameProfile("A", "   "); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("got %v, want ErrEmptyName", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestRenameProfile -v`

Expected: compile error — `s.RenameProfile undefined`.

- [ ] **Step 3: Implement `RenameProfile`**

Append to `internal/config/config.go`:

```go
// RenameProfile changes a profile's name. If renaming the active profile,
// ActiveProfile is updated in the same locked save.
func (s *Store) RenameProfile(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return ErrEmptyName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
	}
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestRenameProfile -v`

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): RenameProfile"
```

---

## Task 6: Config — `RemoveProfile`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestRemoveProfile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_profile: A\nprofiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveProfile("B"); err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles()) != 1 {
		t.Fatalf("not removed: %+v", s.Profiles())
	}
}

func TestRemoveProfile_RejectsActive(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_profile: A\nprofiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveProfile("A"); !errors.Is(err, ErrActiveProfile) {
		t.Fatalf("got %v, want ErrActiveProfile", err)
	}
}

func TestRemoveProfile_RejectsLast(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveProfile("A"); !errors.Is(err, ErrLastProfile) {
		t.Fatalf("got %v, want ErrLastProfile", err)
	}
}

func TestRemoveProfile_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n  - name: B\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveProfile("Nope"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("got %v, want ErrUnknownProfile", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestRemoveProfile -v`

Expected: compile error — `s.RemoveProfile undefined`.

- [ ] **Step 3: Implement `RemoveProfile`**

Append to `internal/config/config.go`:

```go
// RemoveProfile deletes a profile by name. Refuses to remove the active
// profile or the last remaining profile.
func (s *Store) RemoveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
	}
	if len(s.cfg.Profiles) <= 1 {
		return ErrLastProfile
	}
	// Resolve current active even if ActiveProfile is unset.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestRemoveProfile -v`

Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): RemoveProfile, rejects active and last"
```

---

## Task 7: Config — `SaveProfile` (snapshot current → profile)

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSaveProfile_SnapshotsCurrentStationsAndPresets(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("stations:\n  - id: foo\n    name: Foo\n    url: http://foo\npresets:\n  1: foo\nprofiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.SaveProfile("A"); err != nil {
		t.Fatal(err)
	}
	got := s.Profiles()
	if len(got[0].Stations) != 1 || got[0].Stations[0].ID != "foo" {
		t.Fatalf("profile stations not captured: %+v", got[0].Stations)
	}
	if got[0].Presets[1] != "foo" {
		t.Fatalf("profile presets not captured: %+v", got[0].Presets)
	}
}

func TestSaveProfile_UnknownName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.SaveProfile("Nope"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("got %v, want ErrUnknownProfile", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestSaveProfile -v`

Expected: compile error — `s.SaveProfile undefined`.

- [ ] **Step 3: Implement `SaveProfile`**

Append to `internal/config/config.go`:

```go
// SaveProfile snapshots the current top-level Stations + Presets into the
// named profile, overwriting whatever was there.
func (s *Store) SaveProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cfg.Profiles) == 0 {
		s.cfg.Profiles = cloneProfiles(factoryProfiles)
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
	stationsCopy := append([]Station(nil), s.cfg.Stations...)
	presetsCopy := map[int]string{}
	for k, v := range s.cfg.Presets {
		presetsCopy[k] = v
	}
	s.cfg.Profiles[idx].Stations = stationsCopy
	s.cfg.Profiles[idx].Presets = presetsCopy
	return s.save()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestSaveProfile -v`

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): SaveProfile snapshots stations+presets"
```

---

## Task 8: Config — `LoadProfile` (profile → current)

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestLoadProfile_ReplacesCurrentStationsAndPresets(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte(`stations:
  - id: old
    name: Old
    url: http://old
presets:
  1: old
profiles:
  - name: A
    stations:
      - id: new
        name: New
        url: http://new
    presets:
      1: new
`), 0644)
	s, _ := NewStore(path)
	if err := s.LoadProfile("A"); err != nil {
		t.Fatal(err)
	}
	cfg := s.Get()
	if len(cfg.Stations) != 1 || cfg.Stations[0].ID != "new" {
		t.Fatalf("stations not replaced: %+v", cfg.Stations)
	}
	if cfg.Presets[1] != "new" {
		t.Fatalf("presets not replaced: %+v", cfg.Presets)
	}
}

func TestLoadProfile_UnknownName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: A\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	if err := s.LoadProfile("Nope"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("got %v, want ErrUnknownProfile", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestLoadProfile -v`

Expected: compile error — `s.LoadProfile undefined`.

- [ ] **Step 3: Implement `LoadProfile`**

Append to `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestLoadProfile -v`

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): LoadProfile replaces stations+presets"
```

---

## Task 9: Config — `MaybeSeedFromFactory` for first-run

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

The spec's first-run-seeding rule: when an empty fresh config (no stations, no presets, no profiles) is loaded, populate `stations` + `presets` from the embedded PalmaSola profile so the bridge boots curated.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestMaybeSeedFromFactory_FreshConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	s, err := NewStore(path) // file does not exist
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MaybeSeedFromFactory(); err != nil {
		t.Fatal(err)
	}
	cfg := s.Get()
	if len(cfg.Stations) == 0 {
		t.Fatal("expected stations to be seeded from PalmaSola")
	}
	if cfg.Presets[3] != "france-inter" {
		t.Fatalf("expected preset 3 = france-inter, got %q", cfg.Presets[3])
	}
}

func TestMaybeSeedFromFactory_DoesNotOverwriteExistingStations(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("stations:\n  - id: mine\n    name: Mine\n    url: http://mine\n"), 0644)
	s, _ := NewStore(path)
	_ = s.MaybeSeedFromFactory()
	cfg := s.Get()
	if len(cfg.Stations) != 1 || cfg.Stations[0].ID != "mine" {
		t.Fatalf("seed clobbered existing stations: %+v", cfg.Stations)
	}
}

func TestMaybeSeedFromFactory_DoesNotOverwriteExistingProfiles(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("profiles:\n  - name: Custom\n    stations: []\n    presets: {}\n"), 0644)
	s, _ := NewStore(path)
	_ = s.MaybeSeedFromFactory()
	cfg := s.Get()
	if len(cfg.Stations) != 0 {
		t.Fatalf("expected no station seed when profiles section already present, got %+v", cfg.Stations)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestMaybeSeedFromFactory -v`

Expected: compile error — `s.MaybeSeedFromFactory undefined`.

- [ ] **Step 3: Implement `MaybeSeedFromFactory`**

Append to `internal/config/config.go`:

```go
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
```

Note: the `Presets` map check is "len > 0", but in `NewStore` an empty map is initialized when the file is missing. Adjust by tracking whether presets were *populated* not just *non-nil*. The condition `len(s.cfg.Presets) > 0` is correct — an empty map has length 0.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestMaybeSeedFromFactory -v`

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): MaybeSeedFromFactory for first-run station seed"
```

---

## Task 10: `main.go` — call `MaybeSeedFromFactory` on startup

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add the call**

In `main.go`, just after the existing `store, err := config.NewStore(*configPath)` block (around line 41-44), insert:

```go
	if err := store.MaybeSeedFromFactory(); err != nil {
		log.Printf("factory seed failed: %v", err)
	}
```

(Failure is non-fatal — log and continue. Worst case the user has an empty config and the existing auto-discovery and UI paths still work.)

- [ ] **Step 2: Build to verify compile**

Run: `go build ./...`

Expected: success.

- [ ] **Step 3: Manual smoke test**

```bash
rm -f /tmp/config-test.yaml
go run . -config /tmp/config-test.yaml -addr :8080 &
sleep 2
curl -s http://localhost:8080/api/stations | head -c 300
kill %1
```

Expected: the JSON response includes the 8 PalmaSola stations (RTBF, France Culture, etc.).

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat(main): seed stations from factory on first run"
```

---

## Task 11: API handler — `GET /api/profiles`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/api/handlers_test.go`:

```go
func TestListProfilesHandler_FactoryFallback(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	rr := httptest.NewRecorder()
	h.ListProfilesHandler(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Active   string           `json:"active"`
		Profiles []config.Profile `json:"profiles"`
	}
	json.NewDecoder(rr.Body).Decode(&got)
	if got.Active != "PalmaSola" || len(got.Profiles) != 3 {
		t.Fatalf("got %+v", got)
	}
}
```

If the Plan A `mockDiscoverer`/`mockManager` types aren't present (because Plan A wasn't merged first), define them here as in Plan A Task 9.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestListProfilesHandler -v`

Expected: compile error — `h.ListProfilesHandler undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) ListProfilesHandler(w http.ResponseWriter, r *http.Request) {
	resp := struct {
		Active   string           `json:"active"`
		Profiles []config.Profile `json:"profiles"`
	}{
		Active:   h.store.ActiveProfile(),
		Profiles: h.store.Profiles(),
	}
	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("GET /api/profiles", h.ListProfilesHandler)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestListProfilesHandler -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): GET /api/profiles"
```

---

## Task 12: API handler — `POST /api/profiles` (add)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestAddProfileHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(`{"name":"New"}`))
	rr := httptest.NewRecorder()
	h.AddProfileHandler(rr, req)

	if rr.Code != 201 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	names := []string{}
	for _, p := range store.Profiles() {
		names = append(names, p.Name)
	}
	found := false
	for _, n := range names {
		if n == "New" {
			found = true
		}
	}
	if !found {
		t.Fatalf("not added: %+v", names)
	}
}

func TestAddProfileHandler_Duplicate(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(`{"name":"PalmaSola"}`))
	rr := httptest.NewRecorder()
	h.AddProfileHandler(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestAddProfileHandler -v`

Expected: compile error — `h.AddProfileHandler undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) AddProfileHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch err := h.store.AddProfile(req.Name); {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{"name": strings.TrimSpace(req.Name)})
	case errors.Is(err, config.ErrEmptyName):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, config.ErrDuplicateProfile):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/profiles", h.AddProfileHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestAddProfileHandler -v`

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/profiles"
```

---

## Task 13: API handler — `PATCH /api/profiles/{name}` (rename)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestRenameProfileHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("PATCH", "/api/profiles/Autres", strings.NewReader(`{"name":"Voyage"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	found := false
	for _, p := range store.Profiles() {
		if p.Name == "Voyage" {
			found = true
		}
	}
	if !found {
		t.Fatal("rename didn't take")
	}
}

func TestRenameProfileHandler_Conflict(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("PATCH", "/api/profiles/Autres", strings.NewReader(`{"name":"PalmaSola"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestRenameProfileHandler -v`

Expected: 404 (route not wired).

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) RenameProfileHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	oldName := r.PathValue("name")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch err := h.store.RenameProfile(oldName, req.Name); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"name": strings.TrimSpace(req.Name)})
	case errors.Is(err, config.ErrEmptyName):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, config.ErrUnknownProfile):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, config.ErrDuplicateProfile):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("PATCH /api/profiles/{name}", h.RenameProfileHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRenameProfileHandler -v`

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): PATCH /api/profiles/{name}"
```

---

## Task 14: API handler — `DELETE /api/profiles/{name}`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestRemoveProfileHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	// Force the factory set into the config so we can delete a non-active one.
	_ = store.SetActiveProfile("PalmaSola")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("DELETE", "/api/profiles/Autres", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 204 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}

func TestRemoveProfileHandler_RejectsActive(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	_ = store.SetActiveProfile("PalmaSola")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("DELETE", "/api/profiles/PalmaSola", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestRemoveProfileHandler -v`

Expected: 404 (route not wired).

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) RemoveProfileHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch err := h.store.RemoveProfile(name); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, config.ErrUnknownProfile):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, config.ErrActiveProfile), errors.Is(err, config.ErrLastProfile):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("DELETE /api/profiles/{name}", h.RemoveProfileHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRemoveProfileHandler -v`

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): DELETE /api/profiles/{name}"
```

---

## Task 15: API handler — `POST /api/profiles/{name}/save`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/api/handlers_test.go`:

```go
func TestSaveProfileHandler(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte(`stations:
  - id: foo
    name: Foo
    url: http://foo
presets:
  1: foo
`), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("POST", "/api/profiles/Autres/save", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	for _, p := range store.Profiles() {
		if p.Name == "Autres" {
			if len(p.Stations) != 1 || p.Stations[0].ID != "foo" {
				t.Fatalf("autres profile didn't capture current stations: %+v", p.Stations)
			}
			return
		}
	}
	t.Fatal("Autres profile vanished")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestSaveProfileHandler -v`

Expected: 404 (route not wired).

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) SaveProfileHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch err := h.store.SaveProfile(name); {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, config.ErrUnknownProfile):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/profiles/{name}/save", h.SaveProfileHandler)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestSaveProfileHandler -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/profiles/{name}/save"
```

---

## Task 16: API handler — `POST /api/profiles/{name}/load`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/api/handlers_test.go`:

```go
func TestLoadProfileHandler(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte(`stations:
  - id: old
    name: Old
    url: http://old
presets:
  1: old
`), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("POST", "/api/profiles/PalmaSola/load", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	cfg := store.Get()
	if cfg.Stations[0].ID == "old" {
		t.Fatal("stations were not replaced from profile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestLoadProfileHandler -v`

Expected: 404 (route not wired).

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) LoadProfileHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch err := h.store.LoadProfile(name); {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, config.ErrUnknownProfile):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/profiles/{name}/load", h.LoadProfileHandler)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestLoadProfileHandler -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/profiles/{name}/load"
```

---

## Task 17: API handler — `POST /api/profiles/active`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestSetActiveProfileHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/profiles/active", strings.NewReader(`{"name":"VertChasseur"}`))
	rr := httptest.NewRecorder()
	h.SetActiveProfileHandler(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if store.ActiveProfile() != "VertChasseur" {
		t.Fatalf("active = %q, want VertChasseur", store.ActiveProfile())
	}
}

func TestSetActiveProfileHandler_Unknown(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/profiles/active", strings.NewReader(`{"name":"Ghost"}`))
	rr := httptest.NewRecorder()
	h.SetActiveProfileHandler(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestSetActiveProfileHandler -v`

Expected: compile error — `h.SetActiveProfileHandler undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) SetActiveProfileHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch err := h.store.SetActiveProfile(req.Name); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"active": req.Name})
	case errors.Is(err, config.ErrUnknownProfile):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/profiles/active", h.SetActiveProfileHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestSetActiveProfileHandler -v`

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/profiles/active"
```

---

## Task 18: Web UI — Presets card dropdown + Reload/Save/Manage buttons + modal

**Files:**
- Modify: `web/index.html`

The change to `index.html` is contained: extend the existing Presets card header with the profile dropdown and three buttons, add a hidden Manage Profiles modal, add a `<script>` block (or extend the existing one) with the profile-management JS.

- [ ] **Step 1: Find and inspect the existing Presets card and modal pattern**

Grep to confirm locations:

```bash
grep -n "Presets\|modal-header\|modal-overlay" web/index.html
```

Expected output points to a Presets card around line 590 and the modal markup around line 618. Note these line numbers — they will guide your inserts.

- [ ] **Step 2: Add CSS for the new controls**

Inside the existing `<style>` block in `web/index.html`, after the existing `.card-header` rule, add:

```css
  .profile-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .profile-select {
    font: inherit;
    padding: 6px 28px 6px 12px;
    border: 1px solid var(--divider);
    border-radius: 980px;
    background: var(--card);
    color: var(--text);
    cursor: pointer;
    appearance: none;
    -webkit-appearance: none;
    background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'><path fill='%236e6e73' d='M0 0l5 6 5-6z'/></svg>");
    background-repeat: no-repeat;
    background-position: right 10px center;
  }
  .btn-mini {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    padding: 6px 10px;
    font-size: 13px;
    border-radius: 980px;
  }
  .btn-mini:hover { background: var(--accent-soft); }
  .modal-overlay-pf { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: none; align-items: center; justify-content: center; z-index: 50; }
  .modal-overlay-pf.show { display: flex; }
  .modal-pf { background: var(--card); border-radius: 14px; padding: 20px; width: min(420px, calc(100vw - 40px)); box-shadow: 0 24px 64px rgba(0,0,0,0.2); }
  .profile-row { display: flex; align-items: center; padding: 8px 0; border-bottom: 1px solid var(--divider-soft); }
  .profile-row:last-child { border-bottom: none; }
  .profile-row .pname { flex: 1; font-size: 15px; }
  .profile-row .pactions { display: flex; gap: 4px; }
  .add-row { display: flex; gap: 8px; padding-top: 12px; margin-top: 8px; border-top: 1px solid var(--divider-soft); }
  .add-row input { flex: 1; padding: 6px 10px; border: 1px solid var(--divider); border-radius: 8px; font: inherit; }
```

- [ ] **Step 3: Replace the Presets card header**

Locate the existing Presets card. Its header element looks like:

```html
<div class="card-header"><span class="card-title">Presets</span></div>
```

Replace it with:

```html
<div class="card-header">
  <span class="card-title">Presets</span>
  <div class="profile-toolbar">
    <select id="profile-select" class="profile-select" aria-label="Profile"></select>
    <button id="reload-profile" class="btn-mini" title="Reload from selected profile">↻ Reload</button>
    <button id="save-profile" class="btn-mini" title="Save current to selected profile">⭐ Save</button>
    <button id="manage-profiles" class="btn-mini" title="Manage profiles">⚙</button>
  </div>
</div>
```

- [ ] **Step 4: Add the Manage Profiles modal markup**

At the bottom of `<body>`, **before** the existing modal (or after — order doesn't matter as long as it's outside any other modal), add:

```html
<div id="modal-profiles" class="modal-overlay-pf">
  <div class="modal-pf">
    <div class="modal-header" id="modal-profiles-title">Manage Profiles</div>
    <div id="profiles-list"></div>
    <div class="add-row">
      <input type="text" id="new-profile-name" placeholder="New profile name">
      <button class="btn-mini" id="add-profile-btn">Add</button>
    </div>
    <div style="text-align: right; margin-top: 14px;">
      <button class="btn-mini" id="close-profiles-modal">Close</button>
    </div>
  </div>
</div>
```

- [ ] **Step 5: Add the JS for the dropdown, buttons, and modal**

Inside the existing `<script>` block at the bottom of `web/index.html`, append:

```javascript
async function loadProfiles() {
  const res = await fetch('/api/profiles');
  const data = await res.json();
  const sel = document.getElementById('profile-select');
  sel.innerHTML = '';
  for (const p of data.profiles) {
    const opt = document.createElement('option');
    opt.value = p.name;
    opt.textContent = p.name;
    if (p.name === data.active) opt.selected = true;
    sel.appendChild(opt);
  }
  return data;
}

document.getElementById('profile-select').addEventListener('change', async (e) => {
  await fetch('/api/profiles/active', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name: e.target.value}),
  });
});

document.getElementById('reload-profile').addEventListener('click', async () => {
  const name = document.getElementById('profile-select').value;
  if (!confirm(`Replace current stations and presets with the "${name}" profile?`)) return;
  const res = await fetch('/api/profiles/' + encodeURIComponent(name) + '/load', {method: 'POST'});
  if (res.ok) location.reload();
  else alert('Reload failed: ' + (await res.text()));
});

document.getElementById('save-profile').addEventListener('click', async () => {
  const name = document.getElementById('profile-select').value;
  if (!confirm(`Overwrite the "${name}" profile with current stations and presets?`)) return;
  const res = await fetch('/api/profiles/' + encodeURIComponent(name) + '/save', {method: 'POST'});
  if (!res.ok) alert('Save failed: ' + (await res.text()));
});

document.getElementById('manage-profiles').addEventListener('click', async () => {
  await renderProfilesModal();
  document.getElementById('modal-profiles').classList.add('show');
});
document.getElementById('close-profiles-modal').addEventListener('click', () => {
  document.getElementById('modal-profiles').classList.remove('show');
});

async function renderProfilesModal() {
  const data = await loadProfiles();
  const list = document.getElementById('profiles-list');
  list.innerHTML = data.profiles.map(p => {
    const isActive = p.name === data.active;
    const isLast = data.profiles.length === 1;
    return `<div class="profile-row" data-name="${escapeHTML(p.name)}">
      <div class="pname">${escapeHTML(p.name)}${isActive ? ' (active)' : ''}</div>
      <div class="pactions">
        <button class="btn-mini" data-pf-action="rename">✏</button>
        <button class="btn-mini" data-pf-action="delete"
          ${isActive ? 'disabled title="Switch active profile first"' :
            isLast ? 'disabled title="Cannot delete the only profile"' : ''}>🗑</button>
      </div>
    </div>`;
  }).join('');
}

document.getElementById('profiles-list').addEventListener('click', async (e) => {
  const action = e.target.dataset.pfAction;
  if (!action) return;
  const row = e.target.closest('[data-name]');
  const name = row.dataset.name;
  if (action === 'delete') {
    if (!confirm(`Delete profile "${name}"?`)) return;
    const res = await fetch('/api/profiles/' + encodeURIComponent(name), {method: 'DELETE'});
    if (res.ok) await renderProfilesModal();
    else alert('Delete failed: ' + (await res.text()));
  } else if (action === 'rename') {
    const newName = prompt('New name for "' + name + '":', name);
    if (!newName || newName === name) return;
    const res = await fetch('/api/profiles/' + encodeURIComponent(name), {
      method: 'PATCH',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({name: newName}),
    });
    if (res.ok) await renderProfilesModal();
    else alert('Rename failed: ' + (await res.text()));
  }
});

document.getElementById('add-profile-btn').addEventListener('click', async () => {
  const input = document.getElementById('new-profile-name');
  const name = input.value.trim();
  if (!name) return;
  const res = await fetch('/api/profiles', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name}),
  });
  if (res.ok) {
    input.value = '';
    await renderProfilesModal();
  } else {
    alert('Add failed: ' + (await res.text()));
  }
});

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, c => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c]));
}

loadProfiles();
```

- [ ] **Step 6: Manual smoke test**

```bash
rm -f /tmp/config-test.yaml
go run . -config /tmp/config-test.yaml -addr :8080
```

Open `http://localhost:8080` and verify:
1. Dropdown shows PalmaSola / VertChasseur / Autres with PalmaSola selected.
2. The Presets card under "Now Playing" shows the 6 seeded preset slots.
3. Switching dropdown to VertChasseur changes the selection (server-side via POST /api/profiles/active) but stations/presets remain.
4. Clicking *⭐ Save* → confirm → no error.
5. Clicking *↻ Reload* on VertChasseur → confirm → page reloads, presets are now empty (since VertChasseur is the empty profile).
6. Clicking *⚙* opens the Manage Profiles modal.
7. Add a profile "Test" → it appears in the list and the dropdown after closing/reopening (or next render).
8. Delete "Test" → it disappears.
9. Active profile shows "(active)" label and trash is disabled.
10. With only one profile remaining (delete the others), the last trash is also disabled.

- [ ] **Step 7: Commit**

```bash
git add web/index.html
git commit -m "feat(ui): profile dropdown + Reload/Save/Manage controls on Presets card"
```

---

## Task 19: Delete `presets.txt` and update README

**Files:**
- Delete: `presets.txt`
- Modify: `README.md`

- [ ] **Step 1: Verify nothing still references `presets.txt`**

Run:

```bash
grep -rn "presets\.txt" --include="*.go" --include="*.md" --include="*.html" .
```

Expected: zero matches in code; only `README.md` may still mention it.

- [ ] **Step 2: Update README**

Read the README and find any mention of `presets.txt`. Replace with a note pointing at the canonical location. For example, if the README has a line like:

```
The default radio stations are listed in `presets.txt`.
```

Replace with:

```
The default radio stations bundled in the binary are defined in `internal/config/factory_profiles.yaml` (PalmaSola profile). They are seeded on first run when `config.yaml` has no stations, presets, or profiles.
```

If `presets.txt` is not mentioned in the README, skip this step.

- [ ] **Step 3: Delete the file**

```bash
git rm presets.txt
```

- [ ] **Step 4: Build to verify nothing broke**

Run: `go build ./... && go test ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "chore: replace presets.txt with embedded factory_profiles.yaml"
```

---

## Wrap-up checklist

After Task 19:

- [ ] Run the full test suite: `go test ./...` — all PASS.
- [ ] Build images: `docker buildx build --platform linux/amd64 -t soundtouch-radio-bridge:amd64 --load .` (Synology) and `--platform linux/arm64 ... :arm64 --load .` (Firewalla Purple).
- [ ] Manual end-to-end on the Mac: visit `/`, change profile in dropdown, hit Save, hit Reload, open Manage modal, add+rename+delete a profile.
- [ ] If first install fails to seed stations, check the logs for `factory seed failed:`.
- [ ] Update README's feature list to mention named profiles.

Plan is complete.

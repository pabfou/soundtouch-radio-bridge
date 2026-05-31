# Speaker Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a web-based Settings page that lets users discover, save, switch, rename, and remove SoundTouch speakers without editing `config.yaml` or restarting.

**Architecture:** Extend the config Store with named-speaker CRUD operations and an `active_speaker` field. Refactor `speaker.Discover` into a `Discoverer` interface so the new HTTP API can be tested without real mDNS. Add a `Manager.SetTarget(ip)` operation that cancels the existing WebSocket goroutine, swaps the speaker client/upnp/ws references, and reconnects — letting the active speaker change without process restart. Six new REST endpoints under `/api/speakers` + a `/settings` HTML page served from the existing `embed.FS`.

**Tech Stack:** Go 1.22 (stdlib `net/http` mux with path params), `gopkg.in/yaml.v3`, `github.com/grandcat/zeroconf` (existing), `golang.org/x/sync/singleflight` (new), vanilla HTML/JS for the settings page.

**Spec:** [docs/superpowers/specs/2026-05-31-speaker-discovery-ui-design.md](../specs/2026-05-31-speaker-discovery-ui-design.md)

---

## File Structure

**Modify:**
- `internal/config/config.go` — `ActiveSpeaker` field, named-speaker CRUD methods, errors
- `internal/config/config_test.go` — tests for new methods
- `internal/speaker/discover.go` — extract `Discoverer` interface; preserve `Discover` as a method on `MDNSDiscoverer`
- `internal/speaker/manager.go` — `SetTarget` method, `switchMu sync.RWMutex`, parent-ctx capture
- `internal/speaker/manager_test.go` — `SetTarget` test
- `internal/api/handlers.go` — extend `SpeakerManager` interface; add `Discoverer` to `Handler`; six new handlers
- `internal/api/handlers_test.go` — tests for new handlers with fake `Discoverer` + fake `Manager`
- `internal/api/router.go` — wire six new routes
- `main.go` — instantiate `MDNSDiscoverer{}`, extend first-start auto-discovery to set `ActiveSpeaker`, change embed directive to `web/*`
- `web/index.html` — add `.header-actions` wrapper, `.icon-btn` CSS, `<a href="/settings">` link

**Create:**
- `web/settings.html` — new settings page (HTML + inline JS + inline CSS reusing the index.html variables)

**New dependency:** `golang.org/x/sync` (for `singleflight`). Add via `go get`.

---

## Task 1: Config — `ActiveSpeaker` field + `Speakers()` + `Active()` resolution

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSpeakers_SnapshotIsIndependent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Speakers()
	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Fatalf("got %+v", got)
	}
	got[0].Name = "MUTATED"
	again := s.Speakers()
	if again[0].Name == "MUTATED" {
		t.Fatal("Speakers() returned shared slice; expected a copy")
	}
}

func TestActive_FallsBackToFirstWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "A" {
		t.Fatalf("expected A, got %+v ok=%v", act, ok)
	}
}

func TestActive_RespectsActiveSpeakerField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("active_speaker: B\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "B" {
		t.Fatalf("expected B, got %+v ok=%v", act, ok)
	}
}

func TestActive_FallsBackOnUnknownName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("active_speaker: NOPE\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	act, ok := s.Active()
	if !ok || act.Name != "A" {
		t.Fatalf("expected fallback to A, got %+v ok=%v", act, ok)
	}
}

func TestActive_FalseWhenNoSpeakers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	_, ok := s.Active()
	if ok {
		t.Fatal("expected ok=false when no speakers")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestSpeakers|TestActive' -v`

Expected: compile errors — `s.Speakers undefined`, `s.Active undefined`.

- [ ] **Step 3: Add `ActiveSpeaker` field and resolution methods**

In `internal/config/config.go`, add `ActiveSpeaker` to the `Config` struct (between `Speakers` and `Stations`):

```go
type Config struct {
	Speakers      []Speaker      `yaml:"speakers"`
	ActiveSpeaker string         `yaml:"active_speaker,omitempty"`
	Stations      []Station      `yaml:"stations"`
	Presets       map[int]string `yaml:"presets"`
}
```

Append these methods at the bottom of `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestSpeakers|TestActive' -v`

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): ActiveSpeaker field + Speakers/Active resolution"
```

---

## Task 2: Config — `SetActive` method

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSetActive_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	if err := s.SetActive("B"); err != nil {
		t.Fatal(err)
	}
	act, _ := s.Active()
	if act.Name != "B" {
		t.Fatalf("active = %q, want B", act.Name)
	}
	// Verify persisted.
	s2, _ := NewStore(path)
	act2, _ := s2.Active()
	if act2.Name != "B" {
		t.Fatalf("after reload, active = %q, want B", act2.Name)
	}
}

func TestSetActive_UnknownName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	if err := s.SetActive("Nope"); !errors.Is(err, ErrUnknownSpeaker) {
		t.Fatalf("got %v, want ErrUnknownSpeaker", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestSetActive -v`

Expected: compile errors — `s.SetActive undefined`, `ErrUnknownSpeaker undefined`.

- [ ] **Step 3: Add the error sentinels and `SetActive`**

In `internal/config/config.go`, just below the `import` block, add a sentinel-errors block:

```go
var (
	ErrUnknownSpeaker  = errors.New("speaker not found")
	ErrDuplicateName   = errors.New("speaker name already exists")
	ErrEmptyName       = errors.New("speaker name is empty")
	ErrInvalidIP       = errors.New("speaker ip is invalid")
	ErrActiveSpeaker   = errors.New("cannot remove the active speaker")
)
```

Append at the bottom of `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestSetActive -v`

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): SetActive(name) with error sentinels"
```

---

## Task 3: Config — `AddSpeaker` method

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestAddSpeaker_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	if err := s.AddSpeaker(Speaker{Name: "Kitchen", IP: "192.168.1.50"}); err != nil {
		t.Fatal(err)
	}
	sp := s.Speakers()
	if len(sp) != 1 || sp[0].Name != "Kitchen" {
		t.Fatalf("got %+v", sp)
	}
}

func TestAddSpeaker_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := NewStore(path)
	if err := s.AddSpeaker(Speaker{Name: "A", IP: "2.2.2.2"}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("got %v, want ErrDuplicateName", err)
	}
}

func TestAddSpeaker_EmptyName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers: []\n"), 0644)
	s, _ := NewStore(path)
	if err := s.AddSpeaker(Speaker{Name: "   ", IP: "1.1.1.1"}); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("got %v, want ErrEmptyName", err)
	}
}

func TestAddSpeaker_InvalidIP(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers: []\n"), 0644)
	s, _ := NewStore(path)
	if err := s.AddSpeaker(Speaker{Name: "X", IP: "not-an-ip"}); !errors.Is(err, ErrInvalidIP) {
		t.Fatalf("got %v, want ErrInvalidIP", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestAddSpeaker -v`

Expected: compile error — `s.AddSpeaker undefined`.

- [ ] **Step 3: Add `AddSpeaker`**

Add to the imports in `internal/config/config.go`: `"net"`.

Append at the bottom of `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestAddSpeaker -v`

Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): AddSpeaker with name+IP validation"
```

---

## Task 4: Config — `RemoveSpeaker` method

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestRemoveSpeaker_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveSpeaker("B"); err != nil {
		t.Fatal(err)
	}
	sp := s.Speakers()
	if len(sp) != 1 || sp[0].Name != "A" {
		t.Fatalf("got %+v", sp)
	}
}

func TestRemoveSpeaker_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveSpeaker("Nope"); !errors.Is(err, ErrUnknownSpeaker) {
		t.Fatalf("got %v, want ErrUnknownSpeaker", err)
	}
}

func TestRemoveSpeaker_RejectsActive(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RemoveSpeaker("A"); !errors.Is(err, ErrActiveSpeaker) {
		t.Fatalf("got %v, want ErrActiveSpeaker", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestRemoveSpeaker -v`

Expected: compile error — `s.RemoveSpeaker undefined`.

- [ ] **Step 3: Add `RemoveSpeaker`**

Append at the bottom of `internal/config/config.go`:

```go
// RemoveSpeaker deletes a speaker by name. Returns ErrUnknownSpeaker if not
// present, ErrActiveSpeaker if attempting to remove the currently-active one.
func (s *Store) RemoveSpeaker(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Resolve current active even if ActiveSpeaker is unset (falls back to [0]).
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestRemoveSpeaker -v`

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): RemoveSpeaker, rejects active"
```

---

## Task 5: Config — `RenameSpeaker` method

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestRenameSpeaker_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameSpeaker("A", "Alpha"); err != nil {
		t.Fatal(err)
	}
	sp := s.Speakers()
	if sp[0].Name != "Alpha" {
		t.Fatalf("got %q, want Alpha", sp[0].Name)
	}
}

func TestRenameSpeaker_RenameOfActiveUpdatesActiveSpeaker(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameSpeaker("A", "Alpha"); err != nil {
		t.Fatal(err)
	}
	act, _ := s.Active()
	if act.Name != "Alpha" {
		t.Fatalf("active = %q, want Alpha", act.Name)
	}
	// Verify persisted.
	s2, _ := NewStore(path)
	act2, _ := s2.Active()
	if act2.Name != "Alpha" {
		t.Fatalf("after reload, active = %q, want Alpha", act2.Name)
	}
}

func TestRenameSpeaker_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameSpeaker("Nope", "Anything"); !errors.Is(err, ErrUnknownSpeaker) {
		t.Fatalf("got %v, want ErrUnknownSpeaker", err)
	}
}

func TestRenameSpeaker_Duplicate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameSpeaker("A", "B"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("got %v, want ErrDuplicateName", err)
	}
}

func TestRenameSpeaker_EmptyNewName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	s, _ := NewStore(path)
	if err := s.RenameSpeaker("A", "   "); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("got %v, want ErrEmptyName", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestRenameSpeaker -v`

Expected: compile error — `s.RenameSpeaker undefined`.

- [ ] **Step 3: Add `RenameSpeaker`**

Append at the bottom of `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestRenameSpeaker -v`

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): RenameSpeaker, updates ActiveSpeaker on rename of active"
```

---

## Task 6: Speaker — extract `Discoverer` interface

**Files:**
- Modify: `internal/speaker/discover.go`

Existing free function `Discover(ctx, timeout)` is called from `main.go:50`. The refactor preserves it as a method on a new `MDNSDiscoverer` struct, plus a package-level wrapper for the existing call site so we don't have to touch `main.go` in this task.

- [ ] **Step 1: Write the (compile-time-only) failing usage check**

This refactor is a pure interface extraction. Verify by attempting a compile that uses the new interface. Create `internal/speaker/discover_test.go` (new file):

```go
package speaker

import (
	"context"
	"testing"
	"time"
)

// Compile-time check that MDNSDiscoverer satisfies the Discoverer interface.
var _ Discoverer = MDNSDiscoverer{}

func TestMDNSDiscoverer_ImplementsInterface(t *testing.T) {
	var d Discoverer = MDNSDiscoverer{}
	// Smoke call — may return empty on systems without mDNS. We only care that
	// it doesn't panic / compile cleanly.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = d.Discover(ctx, 100*time.Millisecond)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/speaker/ -run TestMDNSDiscoverer -v`

Expected: compile errors — `Discoverer undefined`, `MDNSDiscoverer undefined`.

- [ ] **Step 3: Refactor `discover.go`**

Replace `internal/speaker/discover.go` entirely with:

```go
package speaker

import (
	"context"
	"fmt"
	"time"

	"github.com/grandcat/zeroconf"
)

// Discovered describes one speaker found via mDNS.
type Discovered struct {
	Name string
	IP   string
}

// Discoverer browses the network for SoundTouch speakers.
type Discoverer interface {
	Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error)
}

// MDNSDiscoverer uses Apple Bonjour / mDNS-SD via the grandcat/zeroconf
// library.
type MDNSDiscoverer struct{}

func (MDNSDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("mdns: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 8)
	browseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := resolver.Browse(browseCtx, "_soundtouch._tcp", "local.", entries); err != nil {
		return nil, fmt.Errorf("mdns browse: %w", err)
	}

	var found []Discovered
	seen := make(map[string]bool)
	for {
		select {
		case e, ok := <-entries:
			if !ok {
				return found, nil
			}
			ip := ""
			if len(e.AddrIPv4) > 0 {
				ip = e.AddrIPv4[0].String()
			}
			if ip == "" || seen[ip] {
				continue
			}
			seen[ip] = true
			found = append(found, Discovered{Name: e.Instance, IP: ip})
		case <-browseCtx.Done():
			return found, nil
		}
	}
}

// Discover is preserved as a package-level wrapper so existing callers
// (main.go bootstrap) keep working without modification in this commit.
// New code should depend on the Discoverer interface.
func Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	return MDNSDiscoverer{}.Discover(ctx, timeout)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/speaker/ -run TestMDNSDiscoverer -v`

Expected: PASS.

Run the broader speaker test suite to verify no regressions:

`go test ./internal/speaker/ -v`

Expected: all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/speaker/discover.go internal/speaker/discover_test.go
git commit -m "refactor(speaker): extract Discoverer interface, preserve Discover wrapper"
```

---

## Task 7: Manager — `SetTarget(ip)` for live-switching speakers

**Files:**
- Modify: `internal/speaker/manager.go`
- Test: `internal/speaker/manager_test.go`

The Manager today captures references to `client`, `upnp`, `ws` set at construction. The WS goroutine receives a context from `Start()`. To switch live, we need to:
1. Cancel the in-flight WS goroutine's context.
2. Replace the `client`/`upnp`/`ws` references atomically under a write lock.
3. Re-spawn the WS goroutine against the new IP using a fresh child context derived from the parent context captured during `Start()`.

We also add an `RWMutex` (`switchMu`) so `Play`/`Status` use `RLock` and `SetTarget` uses `Lock`. The existing `mu sync.RWMutex` continues to guard `nowPlaying`/`strategy1`/`bridgeURL`. We add `switchMu` separately to keep the locking story clear.

- [ ] **Step 1: Write the failing test**

Append to `internal/speaker/manager_test.go`:

```go
func TestSetTarget_SwapsHTTPClient(t *testing.T) {
	// Two fake speakers — only the GET /info handler matters here.
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<info deviceID="aaa"></info>`))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<info deviceID="bbb"></info>`))
	}))
	defer srvB.Close()

	// httptest URLs look like http://127.0.0.1:PORT — strip the scheme for NewManagerForTest.
	addrA := strings.TrimPrefix(srvA.URL, "http://")
	addrB := strings.TrimPrefix(srvB.URL, "http://")

	store, err := config.NewStore(t.TempDir() + "/c.yaml")
	if err != nil {
		t.Fatal(err)
	}

	m := NewManagerForTest(addrA, "127.0.0.1:1", store)
	// Pre-switch: client points at A.
	if online, _ := m.Status(); !online {
		t.Fatal("expected online against A")
	}

	if err := m.SetTarget(addrB); err != nil {
		t.Fatalf("SetTarget: %v", err)
	}

	// Post-switch: status still works (against B now). We can't trivially
	// assert which backend served it without a probe, so the meaningful
	// signal is just "no panic, status still resolves".
	if online, _ := m.Status(); !online {
		t.Fatal("expected online against B")
	}
}
```

Make sure the test file imports include `net/http`, `net/http/httptest`, `strings`, and `soundtouch-radio-bridge/internal/config` (some may already be present — check before editing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/speaker/ -run TestSetTarget -v`

Expected: compile error — `m.SetTarget undefined`.

- [ ] **Step 3: Implement `SetTarget`**

Add to the `Manager` struct in `internal/speaker/manager.go` (replace the existing struct definition):

```go
type Manager struct {
	switchMu  sync.RWMutex   // held write by SetTarget, read by Play/Status
	client    *Client
	upnp      *UPnPClient
	ws        *WSListener
	store     *config.Store
	mu        sync.RWMutex
	nowPlaying string
	strategy1  bool
	bridgeURL  string

	// Set by Start; used to re-derive WS context on SetTarget.
	parentCtx context.Context
	wsCancel  context.CancelFunc
}
```

Replace the existing `Start` method body so it captures `parentCtx` and uses a child context for the WS goroutine:

```go
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
```

Add the `SetTarget` method (append below `Play`):

```go
// SetTarget swaps the underlying speaker connection to a new IP. Cancels the
// existing WebSocket goroutine, replaces client/upnp/ws references, and
// re-spawns the WS goroutine against the new target using the parent context
// captured in Start. Returns an error only if Start has not been called yet.
func (m *Manager) SetTarget(speakerIP string) error {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()

	m.mu.RLock()
	parent := m.parentCtx
	cancel := m.wsCancel
	m.mu.RUnlock()
	if parent == nil {
		return fmt.Errorf("manager not started")
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
```

Wrap `Play` and `Status` with the switch read-lock so they don't race a `SetTarget`. Replace the `Play` method:

```go
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
```

Replace the `Status` method:

```go
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
```

For unit tests that construct via `NewManagerForTest` and skip `Start`, `SetTarget` needs to work without a parent context. Loosen by allowing a nil parent to behave as a swap-only (no WS re-spawn). Replace the `parent == nil` early-return with:

```go
	if parent == nil {
		// Test path: swap connection but don't spawn WS (no Start called).
		m.client = NewClient(speakerIP)
		m.upnp = NewUPnPClient(speakerIP)
		m.ws = NewWSListener(speakerIP)
		return nil
	}
```

(Place this before the `if cancel != nil { cancel() }` line, replacing the previous "return error" branch.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/speaker/ -v`

Expected: all tests pass including the new `TestSetTarget_SwapsHTTPClient`.

- [ ] **Step 5: Commit**

```bash
git add internal/speaker/manager.go internal/speaker/manager_test.go
git commit -m "feat(speaker): Manager.SetTarget for live speaker switching"
```

---

## Task 8: API — extend `SpeakerManager` interface, add `Discoverer` dep

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go` (no new test, just update existing fakes)
- Modify: `main.go` (constructor call site — minimal change here)

- [ ] **Step 1: Read the existing `SpeakerManager` interface and locate the constructor call**

Already read in plan prep: `internal/api/handlers.go:16-30` defines `SpeakerManager` with `Play / Status / SyncPresets`, and `NewHandler(store, speaker, tunein)`. `main.go:88` is the call site.

- [ ] **Step 2: Extend the interface and constructor**

In `internal/api/handlers.go`, replace the `SpeakerManager` interface and `Handler` + `NewHandler`:

```go
type SpeakerManager interface {
	Play(stationID string) error
	Status() (online bool, nowPlaying string)
	SyncPresets()
	SetTarget(ip string) error
}

type Handler struct {
	store      *config.Store
	speaker    SpeakerManager
	tunein     *tunein.Client
	discoverer speaker.Discoverer
}

func NewHandler(store *config.Store, spk SpeakerManager, tuneIn *tunein.Client, disc speaker.Discoverer) *Handler {
	return &Handler{store: store, speaker: spk, tunein: tuneIn, discoverer: disc}
}
```

Note the import of `soundtouch-radio-bridge/internal/speaker` already exists.

- [ ] **Step 3: Update the `main.go` call site**

In `main.go`, replace the `handler :=` line (currently `main.go:88`) with:

```go
	handler := api.NewHandler(store, mgr, tuneIn, speaker.MDNSDiscoverer{})
```

(The existing `import` of `internal/speaker` already covers the new reference.)

- [ ] **Step 4: Extend `mockManager`, add `mockDiscoverer`, update `newTestServer`**

In `internal/api/handlers_test.go`:

a. Add `SetTarget` to `mockManager` (the existing type at line ~17). Append after the existing `SyncPresets` method:

```go
func (m *mockManager) SetTarget(ip string) error {
	m.targetIP = ip
	return nil
}
```

Add a `targetIP string` field to the `mockManager` struct definition.

b. Add a new fake discoverer (place anywhere above `newTestServer`):

```go
type mockDiscoverer struct {
	results []speaker.Discovered
	err     error
}

func (d mockDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]speaker.Discovered, error) {
	return d.results, d.err
}
```

Add to the imports at the top of the file:
- `"context"`
- `"time"`
- `"soundtouch-radio-bridge/internal/speaker"`

c. Update `newTestServer` (currently at line ~33) to inject a discoverer and update the signature. Replace it with:

```go
func newTestServer(t *testing.T) (*httptest.Server, *config.Store, *mockManager, *mockDiscoverer) {
	t.Helper()
	store, _ := config.NewStore(filepath.Join(t.TempDir(), "config.yaml"))
	mgr := &mockManager{online: true}
	disc := &mockDiscoverer{}
	handler := api.NewHandler(store, mgr, nil, disc)
	srv := httptest.NewServer(api.NewRouter(handler, embed.FS{}))
	t.Cleanup(srv.Close)
	return srv, store, mgr, disc
}
```

d. Update every existing call site of `newTestServer` in the file. Search for `newTestServer(t)` and change destructuring:

Before: `srv, store, _ := newTestServer(t)` or `srv, _, _ := newTestServer(t)`
After: `srv, store, _, _ := newTestServer(t)` (add a fourth blank for the discoverer)

- [ ] **Step 5: Verify the package builds and existing tests still pass**

Run: `go build ./... && go test ./...`

Expected: all PASS (no behavior change yet, only interface/constructor extension).

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go main.go
git commit -m "refactor(api): SpeakerManager.SetTarget + Discoverer dependency"
```

---

## Task 9: API handler — `GET /api/speakers`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/api/handlers_test.go`:

```go
func TestListSpeakers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/c.yaml"
	if err := os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("GET", "/api/speakers", nil)
	rr := httptest.NewRecorder()
	h.ListSpeakers(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Active   string           `json:"active"`
		Speakers []config.Speaker `json:"speakers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Active != "A" || len(got.Speakers) != 2 {
		t.Fatalf("got %+v", got)
	}
}
```

If `mockDiscoverer` doesn't exist yet, define it in the same test file:

```go
type mockDiscoverer struct {
	results []speaker.Discovered
	err     error
}

func (f mockDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]speaker.Discovered, error) {
	return f.results, f.err
}
```

Add any missing imports: `context`, `encoding/json`, `net/http/httptest`, `os`, `time`, `soundtouch-radio-bridge/internal/speaker`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestListSpeakers -v`

Expected: compile error — `h.ListSpeakers undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) ListSpeakers(w http.ResponseWriter, r *http.Request) {
	active := ""
	if a, ok := h.store.Active(); ok {
		active = a.Name
	}
	resp := struct {
		Active   string           `json:"active"`
		Speakers []config.Speaker `json:"speakers"`
	}{
		Active:   active,
		Speakers: h.store.Speakers(),
	}
	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add (within `NewRouter`, alongside the other registrations):

```go
	mux.HandleFunc("GET /api/speakers", h.ListSpeakers)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestListSpeakers -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): GET /api/speakers"
```

---

## Task 10: API handler — `POST /api/speakers` (add)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestAddSpeakerHandler_HappyPath(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	body := strings.NewReader(`{"name":"Kitchen","ip":"192.168.1.50"}`)
	req := httptest.NewRequest("POST", "/api/speakers", body)
	rr := httptest.NewRecorder()
	h.AddSpeakerHandler(rr, req)

	if rr.Code != 201 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.Speakers()) != 1 {
		t.Fatalf("not saved")
	}
}

func TestAddSpeakerHandler_Duplicate(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: Kitchen\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	body := strings.NewReader(`{"name":"Kitchen","ip":"2.2.2.2"}`)
	req := httptest.NewRequest("POST", "/api/speakers", body)
	rr := httptest.NewRecorder()
	h.AddSpeakerHandler(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestAddSpeakerHandler_BadIP(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	body := strings.NewReader(`{"name":"Kitchen","ip":"not-an-ip"}`)
	req := httptest.NewRequest("POST", "/api/speakers", body)
	rr := httptest.NewRecorder()
	h.AddSpeakerHandler(rr, req)

	if rr.Code != 400 {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestAddSpeakerHandler -v`

Expected: compile error — `h.AddSpeakerHandler undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) AddSpeakerHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	var req struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch err := h.store.AddSpeaker(config.Speaker{Name: req.Name, IP: req.IP}); {
	case err == nil:
		writeJSON(w, http.StatusCreated, config.Speaker{Name: strings.TrimSpace(req.Name), IP: req.IP})
	case errors.Is(err, config.ErrEmptyName), errors.Is(err, config.ErrInvalidIP):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, config.ErrDuplicateName):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Add `"errors"` to the imports.

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/speakers", h.AddSpeakerHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestAddSpeakerHandler -v`

Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/speakers"
```

---

## Task 11: API handler — `DELETE /api/speakers/{name}`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestRemoveSpeakerHandler_HappyPath(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	// Use the actual router so PathValue works.
	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("DELETE", "/api/speakers/B", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 204 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.Speakers()) != 1 {
		t.Fatal("not removed")
	}
}

func TestRemoveSpeakerHandler_RejectsActive(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("DELETE", "/api/speakers/A", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestRemoveSpeakerHandler_Unknown(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("DELETE", "/api/speakers/Ghost", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}
```

Add `"embed"` to the imports if not present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestRemoveSpeakerHandler -v`

Expected: 404 from the router (no route matched) — confirming the route isn't wired yet.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) RemoveSpeakerHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch err := h.store.RemoveSpeaker(name); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, config.ErrUnknownSpeaker):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, config.ErrActiveSpeaker):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("DELETE /api/speakers/{name}", h.RemoveSpeakerHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRemoveSpeakerHandler -v`

Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): DELETE /api/speakers/{name}"
```

---

## Task 12: API handler — `PATCH /api/speakers/{name}` (rename)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestRenameSpeakerHandler_HappyPath(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("PATCH", "/api/speakers/A", strings.NewReader(`{"name":"Alpha"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if store.Speakers()[0].Name != "Alpha" {
		t.Fatalf("not renamed: %+v", store.Speakers())
	}
	act, _ := store.Active()
	if act.Name != "Alpha" {
		t.Fatalf("active not updated: %q", act.Name)
	}
}

func TestRenameSpeakerHandler_Conflict(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("PATCH", "/api/speakers/A", strings.NewReader(`{"name":"B"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 409 {
		t.Fatalf("status %d, want 409", rr.Code)
	}
}

func TestRenameSpeakerHandler_Empty(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	mux := NewRouter(h, embed.FS{})
	req := httptest.NewRequest("PATCH", "/api/speakers/A", strings.NewReader(`{"name":""}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 400 {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestRenameSpeakerHandler -v`

Expected: 404 from the router (route not wired yet).

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) RenameSpeakerHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	oldName := r.PathValue("name")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch err := h.store.RenameSpeaker(oldName, req.Name); {
	case err == nil:
		writeJSON(w, http.StatusOK, config.Speaker{Name: strings.TrimSpace(req.Name)})
	case errors.Is(err, config.ErrEmptyName):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, config.ErrUnknownSpeaker):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, config.ErrDuplicateName):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("PATCH /api/speakers/{name}", h.RenameSpeakerHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRenameSpeakerHandler -v`

Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): PATCH /api/speakers/{name}"
```

---

## Task 13: API handler — `POST /api/speakers/active` (switch + retarget Manager)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

(The `mockManager.targetIP` field and recording `SetTarget` were already added in Task 8 Step 4. Tests below assert against `mgr.targetIP` directly.)

Append to `internal/api/handlers_test.go`:

```go
func TestSetActiveSpeakerHandler_HappyPath(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("active_speaker: A\nspeakers:\n  - name: A\n    ip: 1.1.1.1\n  - name: B\n    ip: 2.2.2.2\n"), 0644)
	store, _ := config.NewStore(path)
	spk := &mockManager{}
	h := NewHandler(store, spk, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/speakers/active", strings.NewReader(`{"name":"B"}`))
	rr := httptest.NewRecorder()
	h.SetActiveSpeakerHandler(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	act, _ := store.Active()
	if act.Name != "B" {
		t.Fatalf("active = %q, want B", act.Name)
	}
	if spk.targetIP != "2.2.2.2" {
		t.Fatalf("SetTarget called with %q, want 2.2.2.2", spk.targetIP)
	}
}

func TestSetActiveSpeakerHandler_Unknown(t *testing.T) {
	path := t.TempDir() + "/c.yaml"
	_ = os.WriteFile(path, []byte("speakers:\n  - name: A\n    ip: 1.1.1.1\n"), 0644)
	store, _ := config.NewStore(path)
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/speakers/active", strings.NewReader(`{"name":"Ghost"}`))
	rr := httptest.NewRecorder()
	h.SetActiveSpeakerHandler(rr, req)

	if rr.Code != 404 {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestSetActiveSpeakerHandler -v`

Expected: compile error — `h.SetActiveSpeakerHandler undefined`.

- [ ] **Step 3: Implement the handler**

Append to `internal/api/handlers.go`:

```go
func (h *Handler) SetActiveSpeakerHandler(w http.ResponseWriter, r *http.Request) {
	defer func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Look up new speaker before mutating.
	var newSpeaker config.Speaker
	found := false
	for _, sp := range h.store.Speakers() {
		if sp.Name == req.Name {
			newSpeaker = sp
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "speaker not found", http.StatusNotFound)
		return
	}

	if err := h.store.SetActive(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.speaker.SetTarget(newSpeaker.IP); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": req.Name})
}
```

(Spec also describes a best-effort Stop on the old speaker before switching; we omit it here because the current `SpeakerManager` interface doesn't expose a Stop. Plan B / a follow-up can extend the interface if desired. Filed as a deliberate simplification.)

- [ ] **Step 4: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/speakers/active", h.SetActiveSpeakerHandler)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestSetActiveSpeakerHandler -v`

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): POST /api/speakers/active (switch + retarget manager)"
```

---

## Task 14: API handler — `POST /api/discover` (with singleflight)

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/router.go`
- Modify: `go.mod` (new dep)

- [ ] **Step 1: Add the singleflight dependency**

Run:

```bash
go get golang.org/x/sync/singleflight
```

Expected: `go.mod` and `go.sum` are updated.

- [ ] **Step 2: Write the failing tests**

Append to `internal/api/handlers_test.go`:

```go
func TestDiscoverHandler_ReturnsResults(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	disc := mockDiscoverer{results: []speaker.Discovered{
		{Name: "Living Room", IP: "192.168.1.50"},
		{Name: "Kitchen", IP: "192.168.1.51"},
	}}
	h := NewHandler(store, &mockManager{}, nil, disc)

	req := httptest.NewRequest("POST", "/api/discover", nil)
	rr := httptest.NewRecorder()
	h.DiscoverHandler(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Found []speaker.Discovered `json:"found"`
	}
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Found) != 2 || got.Found[0].Name != "Living Room" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiscoverHandler_EmptyResults(t *testing.T) {
	store, _ := config.NewStore(t.TempDir() + "/c.yaml")
	h := NewHandler(store, &mockManager{}, nil, mockDiscoverer{})

	req := httptest.NewRequest("POST", "/api/discover", nil)
	rr := httptest.NewRecorder()
	h.DiscoverHandler(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Found []speaker.Discovered `json:"found"`
	}
	json.NewDecoder(rr.Body).Decode(&got)
	if got.Found == nil {
		// JSON should serialize empty-but-non-nil; we declare the slice via
		// make in the handler to avoid `null` on the wire.
		t.Fatal("got nil slice, want []")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestDiscoverHandler -v`

Expected: compile error — `h.DiscoverHandler undefined`.

- [ ] **Step 4: Implement the handler**

In `internal/api/handlers.go`, add to the import block: `"golang.org/x/sync/singleflight"`.

Add a package-level singleflight group at the bottom of the imports section (above any var/func):

```go
var discoverSF singleflight.Group
```

Append the handler:

```go
func (h *Handler) DiscoverHandler(w http.ResponseWriter, r *http.Request) {
	v, err, _ := discoverSF.Do("discover", func() (any, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		return h.discoverer.Discover(ctx, 5*time.Second)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	found, _ := v.([]speaker.Discovered)
	if found == nil {
		found = []speaker.Discovered{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found})
}
```

Add `"context"` and `"time"` to the imports if not present.

- [ ] **Step 5: Wire the route**

In `internal/api/router.go`, add:

```go
	mux.HandleFunc("POST /api/discover", h.DiscoverHandler)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`

Expected: all PASS (including the existing handler tests).

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go go.mod go.sum
git commit -m "feat(api): POST /api/discover with singleflight"
```

---

## Task 15: `main.go` — auto-discovery sets `ActiveSpeaker`, embed all of `web/`

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update the `go:embed` directive**

In `main.go`, replace:

```go
//go:embed web/index.html
var webFS embed.FS
```

with:

```go
//go:embed web
var webFS embed.FS
```

This makes the new `web/settings.html` (added in Task 17) automatically available via the existing `fs.Sub(webFS, "web")` call in the router.

- [ ] **Step 2: Extend the auto-discovery branch to set `ActiveSpeaker`**

In `main.go`, locate the auto-discovery block at lines ~47-72 (the `if len(cfg.Speakers) == 0 { ... }` block). In the `default:` case (success path), the existing code does:

```go
spk := config.Speaker{Name: found[0].Name, IP: found[0].IP}
if err := store.SetSpeaker(spk); err != nil {
    log.Printf("failed to save discovered speaker: %v", err)
} else {
    log.Printf("auto-discovered speaker %q at %s — saved to config", spk.Name, spk.IP)
}
```

Replace with a sequence that also sets `ActiveSpeaker` (use `AddSpeaker` to go through the validated path and `SetActive` to mark it):

```go
spk := config.Speaker{Name: found[0].Name, IP: found[0].IP}
if err := store.AddSpeaker(spk); err != nil {
    log.Printf("failed to save discovered speaker: %v", err)
} else if err := store.SetActive(spk.Name); err != nil {
    log.Printf("failed to set active speaker: %v", err)
} else {
    log.Printf("auto-discovered speaker %q at %s — saved to config", spk.Name, spk.IP)
}
```

The existing `SetSpeaker` method (config.go:153) is no longer referenced — it can stay for now (no behavior change), or be removed in a cleanup pass.

- [ ] **Step 3: Build to verify compile**

Run: `go build ./...`

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat(main): auto-discovery sets ActiveSpeaker; embed all of web/"
```

---

## Task 16: Web UI — header gear icon and Settings link

**Files:**
- Modify: `web/index.html`

- [ ] **Step 1: Add the `.icon-btn` and `.header-actions` CSS**

Open `web/index.html` and locate the `.header` CSS block around line 41-52. After the existing `.header h1 { ... }` rule, add:

```css
  .header-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    border-radius: 980px;
    background: var(--card);
    color: var(--text-secondary);
    box-shadow: var(--shadow);
    text-decoration: none;
    font-size: 18px;
    transition: background 0.15s ease, transform 0.1s ease;
  }
  .icon-btn:hover {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .icon-btn:active {
    transform: scale(0.96);
  }
```

- [ ] **Step 2: Replace the existing `.header` markup**

Around line 572-575, replace:

```html
<div class="header">
    <h1>SoundTouch Radio</h1>
    <span id="badge" class="badge offline">Offline</span>
</div>
```

with:

```html
<div class="header">
    <h1>SoundTouch Radio</h1>
    <div class="header-actions">
      <span id="badge" class="badge offline">Offline</span>
      <a href="/settings" id="settings-btn" aria-label="Settings" class="icon-btn" title="Settings">⚙</a>
    </div>
</div>
```

- [ ] **Step 3: Manual smoke test**

Build and run:

```bash
go run . -config /tmp/config-test.yaml -addr :8080
```

Open `http://localhost:8080`. Verify the gear icon appears next to the Offline badge, hover shows the accent-soft background, click navigates to `/settings` (which will 404 until Task 17 ships).

- [ ] **Step 4: Commit**

```bash
git add web/index.html
git commit -m "feat(ui): gear icon in header linking to /settings"
```

---

## Task 17: Web UI — `/settings` page

**Files:**
- Create: `web/settings.html`

The page reuses the design tokens (CSS variables) from `index.html`. We inline a small CSS block for the page-specific styles to avoid building a shared stylesheet for one extra page.

- [ ] **Step 1: Create `web/settings.html`**

Write to `web/settings.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Settings · SoundTouch Radio Bridge</title>
<style>
  :root {
    --bg: #f5f5f7;
    --card: #ffffff;
    --text: #1d1d1f;
    --text-secondary: #6e6e73;
    --text-tertiary: #86868b;
    --divider: #d2d2d7;
    --divider-soft: #e5e5e7;
    --accent: #0071e3;
    --accent-soft: rgba(0, 113, 227, 0.1);
    --danger: #ff3b30;
    --success: #34c759;
    --shadow: 0 1px 3px rgba(0, 0, 0, 0.04), 0 1px 2px rgba(0, 0, 0, 0.06);
  }
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  html, body {
    font-family: -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", "Helvetica Neue", Helvetica, Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
    min-height: 100vh;
    line-height: 1.47;
  }
  .container { max-width: 760px; margin: 0 auto; padding: 32px 20px 64px; }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 24px; padding: 0 4px; }
  .header h1 { font-size: 28px; font-weight: 600; letter-spacing: -0.022em; }
  .back-link { color: var(--accent); text-decoration: none; font-size: 15px; }
  .card { background: var(--card); border-radius: 14px; padding: 18px 20px; margin-bottom: 16px; box-shadow: var(--shadow); }
  .card-title { font-size: 13px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-tertiary); margin-bottom: 10px; }
  .row { display: flex; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--divider-soft); }
  .row:last-child { border-bottom: none; }
  .row .name { flex: 1; font-size: 16px; }
  .row .ip { color: var(--text-secondary); font-size: 14px; margin-right: 12px; font-variant-numeric: tabular-nums; }
  .row .actions { display: flex; gap: 6px; }
  .row.active .name { font-weight: 600; }
  .radio { width: 18px; height: 18px; border: 2px solid var(--divider); border-radius: 50%; margin-right: 12px; cursor: pointer; }
  .radio.checked { border-color: var(--accent); background: radial-gradient(circle, var(--accent) 0 5px, transparent 6px); }
  .icon-action { background: none; border: none; cursor: pointer; padding: 6px; border-radius: 6px; color: var(--text-secondary); font-size: 15px; }
  .icon-action:hover { background: var(--accent-soft); color: var(--accent); }
  .icon-action[disabled] { color: var(--text-tertiary); cursor: not-allowed; opacity: 0.5; }
  .btn { background: var(--accent); color: white; border: none; border-radius: 980px; padding: 8px 16px; font-size: 14px; font-weight: 500; cursor: pointer; }
  .btn:hover { background: #0077ed; }
  .btn.secondary { background: var(--card); color: var(--accent); box-shadow: var(--shadow); }
  .btn[disabled] { opacity: 0.5; cursor: not-allowed; }
  .toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .empty { color: var(--text-tertiary); font-size: 14px; padding: 12px 0; text-align: center; }
  .toast { position: fixed; top: 16px; right: 16px; background: var(--card); padding: 12px 16px; border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,0.12); font-size: 14px; max-width: 320px; opacity: 0; transform: translateY(-10px); transition: opacity 0.2s ease, transform 0.2s ease; }
  .toast.show { opacity: 1; transform: translateY(0); }
  .toast.error { color: var(--danger); }
  input[type=text] { font: inherit; padding: 6px 10px; border: 1px solid var(--divider); border-radius: 8px; flex: 1; max-width: 240px; }
  .spinner { display: inline-block; width: 14px; height: 14px; border: 2px solid var(--accent-soft); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.7s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Settings</h1>
    <a href="/" class="back-link">← Back</a>
  </div>

  <div class="card">
    <div class="card-title">Active Speaker</div>
    <div id="active-row" class="empty">Loading…</div>
  </div>

  <div class="card">
    <div class="toolbar">
      <div class="card-title">Saved Speakers</div>
      <button id="scan-btn" class="btn secondary">Scan network</button>
    </div>
    <div id="saved-list"></div>
  </div>

  <div class="card" id="discovered-card" style="display:none">
    <div class="card-title">Discovered on network</div>
    <div id="discovered-list"></div>
  </div>
</div>
<div id="toast" class="toast"></div>

<script>
const $ = (sel) => document.querySelector(sel);
const toast = (msg, isError) => {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.toggle('error', !!isError);
  t.classList.add('show');
  setTimeout(() => t.classList.remove('show'), 3000);
};

let activeName = '';
let saved = [];
let discovered = [];

async function loadSpeakers() {
  const res = await fetch('/api/speakers');
  const data = await res.json();
  activeName = data.active || '';
  saved = data.speakers || [];
  render();
}

function render() {
  const active = saved.find(s => s.name === activeName);
  const activeRow = $('#active-row');
  if (active) {
    activeRow.classList.remove('empty');
    activeRow.innerHTML = `
      <div class="row active">
        <div class="radio checked"></div>
        <div class="name">${escape(active.name)}</div>
        <div class="ip">${escape(active.ip)}</div>
      </div>`;
  } else {
    activeRow.className = 'empty';
    activeRow.textContent = 'No speaker selected';
  }

  const savedEl = $('#saved-list');
  if (saved.length === 0) {
    savedEl.innerHTML = '<div class="empty">No saved speakers</div>';
  } else {
    savedEl.innerHTML = saved.map(s => `
      <div class="row${s.name === activeName ? ' active' : ''}" data-name="${escape(s.name)}">
        <div class="radio${s.name === activeName ? ' checked' : ''}" data-action="activate"></div>
        <div class="name" data-action="display">${escape(s.name)}</div>
        <div class="ip">${escape(s.ip)}</div>
        <div class="actions">
          <button class="icon-action" data-action="rename" title="Rename">✏</button>
          <button class="icon-action" data-action="remove" title="Remove" ${s.name === activeName ? 'disabled' : ''}>🗑</button>
        </div>
      </div>`).join('');
  }

  const newOnes = discovered.filter(d => !saved.some(s => s.ip === d.ip));
  $('#discovered-card').style.display = newOnes.length ? 'block' : 'none';
  $('#discovered-list').innerHTML = newOnes.map(d => `
    <div class="row" data-name="${escape(d.name)}" data-ip="${escape(d.ip)}">
      <div class="name">${escape(d.name)}</div>
      <div class="ip">${escape(d.ip)}</div>
      <div class="actions">
        <button class="btn secondary" data-action="add">Add</button>
      </div>
    </div>`).join('');
}

function escape(s) {
  return String(s).replace(/[&<>"']/g, c => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c]));
}

document.body.addEventListener('click', async (e) => {
  const action = e.target.dataset.action;
  if (!action) return;
  const row = e.target.closest('[data-name]');
  if (!row) return;
  const name = row.dataset.name;

  if (action === 'activate') {
    if (name === activeName) return;
    const res = await fetch('/api/speakers/active', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({name}),
    });
    if (res.ok) { await loadSpeakers(); toast('Active speaker switched'); }
    else toast('Switch failed: ' + (await res.text()), true);
    return;
  }
  if (action === 'remove') {
    if (!confirm(`Remove speaker "${name}"?`)) return;
    const res = await fetch('/api/speakers/' + encodeURIComponent(name), {method: 'DELETE'});
    if (res.ok) { await loadSpeakers(); toast('Removed'); }
    else toast('Remove failed: ' + (await res.text()), true);
    return;
  }
  if (action === 'rename') {
    const nameEl = row.querySelector('.name');
    const current = name;
    nameEl.innerHTML = `<input type="text" value="${escape(current)}" data-edit="1">`;
    const input = nameEl.querySelector('input');
    input.focus();
    input.select();
    const commit = async () => {
      const newName = input.value.trim();
      if (!newName || newName === current) { await loadSpeakers(); return; }
      const res = await fetch('/api/speakers/' + encodeURIComponent(current), {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({name: newName}),
      });
      if (res.ok) { await loadSpeakers(); toast('Renamed'); }
      else { toast('Rename failed: ' + (await res.text()), true); await loadSpeakers(); }
    };
    input.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') commit();
      else if (ev.key === 'Escape') loadSpeakers();
    });
    input.addEventListener('blur', commit, {once: true});
    return;
  }
  if (action === 'add') {
    const ip = row.dataset.ip;
    const res = await fetch('/api/speakers', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({name, ip}),
    });
    if (res.ok) { await loadSpeakers(); toast('Added'); }
    else toast('Add failed: ' + (await res.text()), true);
    return;
  }
});

$('#scan-btn').addEventListener('click', async () => {
  const btn = $('#scan-btn');
  const original = btn.textContent;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Scanning…';
  try {
    const res = await fetch('/api/discover', {method: 'POST'});
    const data = await res.json();
    discovered = data.found || [];
    render();
    if (discovered.length === 0) toast('No speakers found');
    else toast(`Found ${discovered.length} speaker${discovered.length === 1 ? '' : 's'}`);
  } catch (err) {
    toast('Scan failed: ' + err, true);
  } finally {
    btn.disabled = false;
    btn.textContent = original;
  }
});

loadSpeakers();
</script>
</body>
</html>
```

- [ ] **Step 2: Manual smoke test**

Run:

```bash
go run . -config /tmp/config-test.yaml -addr :8080
```

Open `http://localhost:8080/settings`. Verify:
1. Active Speaker section renders (may say "No speaker selected" if `/tmp/config-test.yaml` is empty).
2. Clicking *Scan network* shows a spinner; after ~5s either a "Found N" toast or "No speakers found".
3. If on a real LAN with a SoundTouch, the speaker appears in "Discovered on network" with an Add button.
4. Add → speaker moves to Saved Speakers.
5. Click the radio → confirmation, becomes active; ✏ inline-renames; 🗑 removes (disabled on active).

If anything misbehaves (404 on `/settings`, JS errors in console), debug before committing.

- [ ] **Step 3: Commit**

```bash
git add web/settings.html
git commit -m "feat(ui): /settings page for speaker discovery and management"
```

---

## Wrap-up checklist

After Task 17:

- [ ] Run the full test suite: `go test ./...` — all PASS.
- [ ] Build the linux/amd64 image and the linux/arm64 image. (Each deployment site builds its own architecture.)
- [ ] Manual end-to-end on a Mac with a real SoundTouch on the LAN: visit `/settings`, scan, add, switch, rename, remove.
- [ ] Update README to mention the Settings page in the brief feature list.

Plan is complete.

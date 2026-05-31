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

// MDNSDiscoverer uses Apple Bonjour / mDNS-SD via the grandcat/zeroconf library.
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
// (main.go bootstrap) keep working without modification. New code should
// depend on the Discoverer interface.
func Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	return MDNSDiscoverer{}.Discover(ctx, timeout)
}

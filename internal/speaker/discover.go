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

// Discover browses _soundtouch._tcp on the LAN and returns every responder
// up to timeout. The returned slice may be empty if no speakers answer.
func Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
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

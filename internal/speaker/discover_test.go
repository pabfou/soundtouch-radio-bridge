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
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = d.Discover(ctx, 100*time.Millisecond)
}

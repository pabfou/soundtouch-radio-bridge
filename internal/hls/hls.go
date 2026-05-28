// Package hls fetches an HLS audio stream and streams the contained audio
// (AAC frames, already ADTS-framed) to a writer as a continuous audio/aac
// stream. SoundTouch 10 hardware can't play HLS natively, so we transmux.
package hls

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astits"
	"github.com/grafov/m3u8"
)

// Stream resolves playlistURL (master or media), then continuously fetches
// new segments and writes their AAC payload to w. Returns when ctx is
// cancelled or a fatal fetch error occurs.
func Stream(ctx context.Context, w io.Writer, playlistURL string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	mediaURL, err := resolveMedia(ctx, client, playlistURL)
	if err != nil {
		return fmt.Errorf("resolve media playlist: %w", err)
	}

	seen := newSeenSet(64)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		segments, targetDuration, err := fetchSegments(ctx, client, mediaURL)
		if err != nil {
			// Transient errors: wait and retry instead of bailing.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, segURL := range segments {
			if seen.has(segURL) {
				continue
			}
			seen.add(segURL)
			if err := pumpSegment(ctx, client, w, segURL); err != nil {
				return err
			}
		}
		// Refresh playlist at ~half target duration (HLS spec recommendation).
		wait := targetDuration / 2
		if wait < time.Second {
			wait = time.Second
		}
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// resolveMedia returns a media (segment) playlist URL. If playlistURL is a
// master playlist, the first variant is followed.
func resolveMedia(ctx context.Context, client *http.Client, playlistURL string) (string, error) {
	pl, listType, err := fetchPlaylist(ctx, client, playlistURL)
	if err != nil {
		return "", err
	}
	if listType == m3u8.MASTER {
		master := pl.(*m3u8.MasterPlaylist)
		if len(master.Variants) == 0 {
			return "", fmt.Errorf("master playlist has no variants")
		}
		return resolveURL(playlistURL, master.Variants[0].URI), nil
	}
	return playlistURL, nil
}

func fetchSegments(ctx context.Context, client *http.Client, mediaURL string) ([]string, time.Duration, error) {
	pl, listType, err := fetchPlaylist(ctx, client, mediaURL)
	if err != nil {
		return nil, 0, err
	}
	if listType != m3u8.MEDIA {
		return nil, 0, fmt.Errorf("expected media playlist, got %v", listType)
	}
	media := pl.(*m3u8.MediaPlaylist)
	var segs []string
	for _, seg := range media.Segments {
		if seg == nil {
			continue
		}
		segs = append(segs, resolveURL(mediaURL, seg.URI))
	}
	return segs, time.Duration(media.TargetDuration * float64(time.Second)), nil
}

func fetchPlaylist(ctx context.Context, client *http.Client, u string) (m3u8.Playlist, m3u8.ListType, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; soundtouch-radio-bridge)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, 0, fmt.Errorf("playlist status %d", resp.StatusCode)
	}
	return m3u8.DecodeFrom(bufio.NewReader(resp.Body), true)
}

// pumpSegment fetches one segment and writes its AAC payload to w.
// Two segment formats appear in the wild:
//   - .ts / video/MP2T: MPEG-TS container with AAC PES payloads (ADTS-framed).
//     We demux and emit only PES.Data so the result is a plain ADTS stream.
//   - .aac / audio/aac: raw ADTS-framed AAC. Pass through verbatim.
func pumpSegment(ctx context.Context, client *http.Client, w io.Writer, segURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; soundtouch-radio-bridge)")
	resp, err := client.Do(req)
	if err != nil {
		return nil // skip on transient
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	isTS := strings.Contains(ct, "MP2T") || strings.HasSuffix(strings.ToLower(segURL), ".ts")
	if !isTS {
		// Raw AAC segment: copy bytes directly.
		_, err := io.Copy(flushWriter{w: w}, resp.Body)
		return err
	}

	demux := astits.NewDemuxer(ctx, resp.Body)
	for {
		d, err := demux.NextData()
		if err == io.EOF || err == astits.ErrNoMorePackets {
			return nil
		}
		if err != nil {
			return nil // tolerate per-segment errors
		}
		if d.PES == nil {
			continue
		}
		if _, werr := w.Write(d.PES.Data); werr != nil {
			return werr
		}
		flushIfPossible(w)
	}
}

// flushWriter wraps w and flushes after every Write, so audio bytes reach the
// speaker without buffering delay.
type flushWriter struct{ w io.Writer }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	flushIfPossible(f.w)
	return n, err
}

func flushIfPossible(w io.Writer) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	bu, err := url.Parse(base)
	if err != nil {
		return ref
	}
	ru, err := bu.Parse(ref)
	if err != nil {
		return ref
	}
	return ru.String()
}

// seenSet is a small bounded set for deduplicating segment URLs.
type seenSet struct {
	mu    sync.Mutex
	order []string
	set   map[string]struct{}
	cap   int
}

func newSeenSet(cap int) *seenSet {
	return &seenSet{set: make(map[string]struct{}, cap), cap: cap}
}
func (s *seenSet) has(k string) bool {
	s.mu.Lock()
	_, ok := s.set[k]
	s.mu.Unlock()
	return ok
}
func (s *seenSet) add(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[k]; ok {
		return
	}
	s.set[k] = struct{}{}
	s.order = append(s.order, k)
	if len(s.order) > s.cap {
		delete(s.set, s.order[0])
		s.order = s.order[1:]
	}
}

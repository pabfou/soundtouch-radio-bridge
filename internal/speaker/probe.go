package speaker

import (
	"net/http"
	"time"
)

// HeadOK reports whether the URL responds to HEAD with a non-error status.
// Used to detect streams (e.g. BBC) that reject HEAD — those need to be
// proxied because the speaker probes URLs with HEAD before playing.
func HeadOK(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; soundtouch-radio-bridge)")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

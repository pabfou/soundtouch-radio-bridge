package tunein

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Station struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Logo string `json:"logo"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://opml.radiotime.com"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type opmlResponse struct {
	Body []opmlItem `json:"body"`
}

type opmlItem struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	URL   string `json:"URL"`
	Image string `json:"image"`
}

func (c *Client) Search(query string) ([]Station, error) {
	u := fmt.Sprintf("%s/Search.ashx?query=%s&type=audio&render=json",
		c.baseURL, url.QueryEscape(query))
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tunein search: status %d", resp.StatusCode)
	}

	var result opmlResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var stations []Station
	for _, item := range result.Body {
		if item.Type != "audio" || item.URL == "" {
			continue
		}
		streamURL, err := c.resolveStream(item.URL)
		if err != nil || streamURL == "" {
			streamURL = item.URL // fallback: use the redirect URL as-is
		}
		stations = append(stations, Station{
			Name: item.Text,
			URL:  streamURL,
			Logo: item.Image,
		})
	}
	return stations, nil
}

// resolveStream follows a TuneIn redirect URL to get the actual stream URL.
// TuneIn returns the URL as plain text (possibly in an M3U file).
func (c *Client) resolveStream(tuneURL string) (string, error) {
	resp, err := c.http.Get(tuneURL)
	if err != nil {
		return "", err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http") {
			return line, nil
		}
	}
	return "", nil
}

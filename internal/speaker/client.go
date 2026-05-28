package speaker

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(addr string) *Client {
	host := addr
	if !strings.Contains(addr, ":") {
		host = addr + ":8090"
	}
	return &Client{
		baseURL: "http://" + host,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

type contentItem struct {
	XMLName       xml.Name `xml:"ContentItem"`
	Source        string   `xml:"source,attr"`
	Location      string   `xml:"location,attr"`
	SourceAccount string   `xml:"sourceAccount,attr"`
	IsPresetable  bool     `xml:"isPresetable,attr"`
	ItemName      string   `xml:"itemName"`
}

type presetsRequest struct {
	XMLName xml.Name    `xml:"presets"`
	Preset  presetEntry `xml:"preset"`
}

type presetEntry struct {
	ID      int         `xml:"id,attr"`
	Content contentItem `xml:"ContentItem"`
}

func (c *Client) Select(streamURL, name string) error {
	item := contentItem{
		Source:       "INTERNET_RADIO",
		Location:     streamURL,
		IsPresetable: true,
		ItemName:     name,
	}
	body, err := xml.Marshal(item)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/select", "application/xml", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("select: speaker returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetPreset(slot int, streamURL, name string) error {
	req := presetsRequest{
		Preset: presetEntry{
			ID: slot,
			Content: contentItem{
				Source:       "INTERNET_RADIO",
				Location:     streamURL,
				IsPresetable: true,
				ItemName:     name,
			},
		},
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/presets", "application/xml", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("setPreset: speaker returned %d", resp.StatusCode)
	}
	return nil
}

// ProbePresetWrite tests whether the speaker accepts POST /presets (Strategy 1).
// Sends a harmless empty body and checks for non-404 response.
func (c *Client) ProbePresetWrite() bool {
	resp, err := c.http.Post(c.baseURL+"/presets", "application/xml", bytes.NewReader([]byte("<presets/>")))
	if err != nil {
		return false
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	return resp.StatusCode != http.StatusNotFound
}

func (c *Client) GetInfo() error {
	resp, err := c.http.Get(c.baseURL + "/info")
	if err != nil {
		return err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("info: speaker returned %d", resp.StatusCode)
	}
	return nil
}

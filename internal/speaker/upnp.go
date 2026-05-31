package speaker

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// UPnPClient sends AVTransport SOAP commands to the speaker. On firmware where
// the legacy /select endpoint no longer accepts INTERNET_RADIO, UPnP is the
// only way to push a stream URL to the speaker.
type UPnPClient struct {
	controlURL string
	http       *http.Client
}

func NewUPnPClient(addr string) *UPnPClient {
	host := addr
	if !strings.Contains(addr, ":") {
		host = addr + ":8091"
	}
	return &UPnPClient{
		controlURL: "http://" + host + "/AVTransport/Control",
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Stop halts current playback via UPnP AVTransport Stop.
func (u *UPnPClient) Stop() error {
	return u.call("Stop", stopBody())
}

// Play stops any current playback, sets the URI, and starts playback.
// The speaker probes streamURL with HEAD before playing; if HEAD returns an
// error status, playback silently fails (state transitions to INVALID_SOURCE).
func (u *UPnPClient) Play(streamURL, name string) error {
	if err := u.call("Stop", stopBody()); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := u.call("SetAVTransportURI", setURIBody(streamURL, name)); err != nil {
		return fmt.Errorf("setURI: %w", err)
	}
	if err := u.call("Play", playBody()); err != nil {
		return fmt.Errorf("play: %w", err)
	}
	return nil
}

func (u *UPnPClient) call(action, body string) error {
	req, err := http.NewRequest(http.MethodPost, u.controlURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#`+action+`"`)
	resp, err := u.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("speaker returned %d", resp.StatusCode)
	}
	return nil
}

func stopBody() string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:Stop xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID></u:Stop></s:Body></s:Envelope>`
}

func playBody() string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:Play xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID><Speed>1</Speed></u:Play></s:Body></s:Envelope>`
}

func setURIBody(streamURL, name string) string {
	didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<item id="0" parentID="-1" restricted="1">` +
		`<dc:title>` + html.EscapeString(name) + `</dc:title>` +
		`<upnp:class>object.item.audioItem.audioBroadcast</upnp:class>` +
		`<res protocolInfo="http-get:*:audio/mpeg:*">` + html.EscapeString(streamURL) + `</res>` +
		`</item></DIDL-Lite>`
	escapedDIDL := html.EscapeString(didl)
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">` +
		`<InstanceID>0</InstanceID>` +
		`<CurrentURI>` + html.EscapeString(streamURL) + `</CurrentURI>` +
		`<CurrentURIMetaData>` + escapedDIDL + `</CurrentURIMetaData>` +
		`</u:SetAVTransportURI></s:Body></s:Envelope>`
}

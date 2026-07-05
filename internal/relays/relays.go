// Package relays loads the Mullvad relay list from the daemon's local cache
// (preferred) or the Mullvad app API (fallback), and normalizes the raw JSON
// into the typed model.Relay used by the rest of the program.
package relays

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// DefaultAPIURL is the Mullvad app relays endpoint. Confirmed at build time to
// return the locations/wireguard/openvpn fields the model in FR-2 requires.
const DefaultAPIURL = "https://api.mullvad.net/app/v1/relays"

// Source produces the relay list. Implementations sit behind this boundary so
// the pipeline can be tested with a fake that touches no disk or network.
type Source interface {
	Relays(ctx context.Context) ([]model.Relay, error)
}

// DefaultCachePaths returns the daemon relay-cache locations for the running OS,
// in preference order.
func DefaultCachePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Library/Caches/mullvad-vpn/relays.json"}
	default: // linux and others
		return []string{"/var/cache/mullvad-vpn/relays.json"}
	}
}

// rawRelays mirrors the on-the-wire shape of the app relays endpoint (which is
// also what the daemon caches to disk). It is intentionally private: nothing
// outside this package carries the raw JSON shape.
type rawRelays struct {
	Locations map[string]rawLocation `json:"locations"`
	WireGuard rawProtoSection        `json:"wireguard"`
	OpenVPN   rawProtoSection        `json:"openvpn"`
}

type rawLocation struct {
	Country   string   `json:"country"`
	City      string   `json:"city"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type rawProtoSection struct {
	Relays []rawRelay `json:"relays"`
}

type rawRelay struct {
	Hostname   string `json:"hostname"`
	Active     bool   `json:"active"`
	Owned      bool   `json:"owned"`
	Provider   string `json:"provider"`
	Location   string `json:"location"`
	IPv4AddrIn string `json:"ipv4_addr_in"`
	IPv6AddrIn string `json:"ipv6_addr_in"`
	PublicKey  string `json:"public_key"`
	DAITA      bool   `json:"daita"`
}

// parse turns the raw JSON bytes into typed relays. It never panics on bad
// input; malformed JSON returns a descriptive error.
func parse(data []byte) ([]model.Relay, error) {
	var raw rawRelays
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse relays json: %w", err)
	}
	out := make([]model.Relay, 0, len(raw.WireGuard.Relays)+len(raw.OpenVPN.Relays))
	for _, r := range raw.WireGuard.Relays {
		out = append(out, raw.toModel(r, model.WireGuard))
	}
	for _, r := range raw.OpenVPN.Relays {
		out = append(out, raw.toModel(r, model.OpenVPN))
	}
	return out, nil
}

func (raw rawRelays) toModel(r rawRelay, proto model.Protocol) model.Relay {
	cc, city := splitLocation(r.Location)
	m := model.Relay{
		Hostname:    r.Hostname,
		IPv4:        r.IPv4AddrIn,
		IPv6:        r.IPv6AddrIn,
		CountryCode: cc,
		CityCode:    city,
		Provider:    r.Provider,
		Owned:       r.Owned,
		Active:      r.Active,
		Protocol:    proto,
		DAITA:       r.DAITA,
	}
	if proto == model.WireGuard {
		m.PublicKey = r.PublicKey
	}
	if loc, ok := raw.Locations[r.Location]; ok {
		m.CountryName = loc.Country
		m.CityName = loc.City
		if loc.Latitude != nil && loc.Longitude != nil {
			m.Latitude = *loc.Latitude
			m.Longitude = *loc.Longitude
			m.HasCoords = true
		}
	}
	return m
}

// splitLocation splits a location code like "us-qas" into country code "us" and
// city code "qas". The country code is the segment before the first dash.
func splitLocation(loc string) (country, city string) {
	i := strings.IndexByte(loc, '-')
	if i < 0 {
		return loc, ""
	}
	return loc[:i], loc[i+1:]
}

// FileSource reads relays from a JSON file (the daemon cache or an override).
type FileSource struct{ Path string }

func (f FileSource) Relays(ctx context.Context) ([]model.Relay, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	return parse(data)
}

// APISource fetches relays from the Mullvad app API.
type APISource struct {
	URL    string
	Client *http.Client
}

func (a APISource) Relays(ctx context.Context) ([]model.Relay, error) {
	url := a.URL
	if url == "" {
		url = DefaultAPIURL
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch relays from %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch relays from %s: unexpected status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parse(data)
}

// LoadOptions controls how Load selects a relay source.
type LoadOptions struct {
	// RelaysFile, when set, overrides the daemon cache path.
	RelaysFile string
	// Refresh forces the API even when a local cache exists.
	Refresh bool
	// APIURL overrides DefaultAPIURL (for tests).
	APIURL string
	// Client overrides the HTTP client (for tests).
	Client *http.Client
	// Logf receives one-line diagnostics (may be nil).
	Logf func(format string, args ...any)
}

func (o LoadOptions) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Load implements the FR-1 local-first, API-fallback policy and returns the
// chosen source's relays. An explicit --relays-file that fails to read is an
// error; a missing default cache degrades to the API.
func Load(ctx context.Context, o LoadOptions) ([]model.Relay, error) {
	api := APISource{URL: o.APIURL, Client: o.Client}

	if o.Refresh {
		o.logf("refresh requested: fetching relays from API")
		return api.Relays(ctx)
	}

	if o.RelaysFile != "" {
		o.logf("loading relays from %s", o.RelaysFile)
		return FileSource{Path: o.RelaysFile}.Relays(ctx)
	}

	for _, p := range DefaultCachePaths() {
		if _, err := os.Stat(p); err == nil {
			o.logf("loading relays from daemon cache %s", p)
			return FileSource{Path: p}.Relays(ctx)
		}
	}

	o.logf("no local relay cache found; fetching from API")
	return api.Relays(ctx)
}

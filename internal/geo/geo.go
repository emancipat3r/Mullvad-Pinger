// Package geo provides the great-circle distance used by the prefilter and the
// geolocation boundary used to find the user and detect an active tunnel.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

const earthRadiusKm = 6371.0

// Haversine returns the great-circle distance in kilometers between two points
// given in decimal degrees. It is a pure function.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rlat1 := lat1 * math.Pi / 180
	rlat2 := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rlat1)*math.Cos(rlat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

// Location is the user's position plus whether Mullvad reports the connection
// as exiting through the VPN.
type Location struct {
	Latitude  float64
	Longitude float64
}

// Geolocator resolves the user's coordinates and whether traffic currently
// exits through Mullvad. It sits behind an interface so it can be faked.
type Geolocator interface {
	// Locate returns the user's coordinates and whether the connection is
	// already tunneled through Mullvad.
	Locate(ctx context.Context) (Location, bool, error)
}

// DefaultGeoURL is the Mullvad "am I Mullvad" JSON endpoint.
const DefaultGeoURL = "https://am.i.mullvad.net/json"

// MullvadGeolocator queries am.i.mullvad.net.
type MullvadGeolocator struct {
	URL    string
	Client *http.Client
}

// amIMullvad is the subset of the am.i.mullvad.net/json response we consume.
type amIMullvad struct {
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	MullvadExitIP bool    `json:"mullvad_exit_ip"`
}

func (g MullvadGeolocator) Locate(ctx context.Context) (Location, bool, error) {
	url := g.URL
	if url == "" {
		url = DefaultGeoURL
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Location{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Location{}, false, fmt.Errorf("geolocate via %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Location{}, false, fmt.Errorf("geolocate via %s: unexpected status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Location{}, false, err
	}
	var a amIMullvad
	if err := json.Unmarshal(data, &a); err != nil {
		return Location{}, false, fmt.Errorf("parse geolocation json: %w", err)
	}
	return Location{Latitude: a.Latitude, Longitude: a.Longitude}, a.MullvadExitIP, nil
}

// DistanceKm is a convenience wrapper computing the distance from a user
// location to a relay. Relays without coordinates return (0, false).
func DistanceKm(user Location, r model.Relay) (float64, bool) {
	if !r.HasCoords {
		return 0, false
	}
	return Haversine(user.Latitude, user.Longitude, r.Latitude, r.Longitude), true
}

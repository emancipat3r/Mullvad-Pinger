// Package model holds the shared domain types for mullvad-pinger.
//
// These types are deliberately free of I/O so the pure core (filtering,
// haversine, stats, ranking) can operate on them in isolation and be tested
// without a network.
package model

import "time"

// Protocol is the tunnel protocol a relay speaks.
type Protocol string

const (
	WireGuard Protocol = "wireguard"
	OpenVPN   Protocol = "openvpn"
)

// Location is a Mullvad relay location (a city within a country) with the
// coordinates used for the distance prefilter.
type Location struct {
	CountryCode string
	CountryName string
	CityCode    string
	CityName    string
	Latitude    float64
	Longitude   float64
	// HasCoords is false when the source omitted coordinates; such relays are
	// excluded from the distance prefilter rather than treated as (0,0).
	HasCoords bool
}

// Relay is a single Mullvad relay, normalized from the daemon cache or API.
//
// Fields absent from the source are left zero-valued; nothing here is
// fabricated. Booleans (Owned, Active, DAITA) are derived from the source.
type Relay struct {
	Hostname    string
	IPv4        string
	IPv6        string
	CountryCode string
	CountryName string
	CityCode    string
	CityName    string
	Latitude    float64
	Longitude   float64
	HasCoords   bool
	Provider    string
	Owned       bool // Mullvad-owned vs rented
	Active      bool
	Protocol    Protocol
	// PublicKey is the WireGuard public key; empty for OpenVPN relays.
	PublicKey string
	DAITA     bool
}

// ProbeResult is the outcome of probing a single host with N ICMP probes.
type ProbeResult struct {
	Host string
	// RTTs holds one entry per successful reply, in order received.
	RTTs []time.Duration
	// Sent is the number of probes actually sent.
	Sent int
	// Recv is the number of successful replies (len(RTTs)).
	Recv int
}

// Result is a measured, ranked relay ready for output.
type Result struct {
	Relay     Relay
	Rank      int
	MedianMS  float64
	JitterMS  float64
	LossPct   float64
	Sent      int
	Recv      int
	Reachable bool
	// HandshakeMS is populated only when the verify tier ran; zero otherwise.
	HandshakeMS float64
	Verified    bool
}

// TunnelStatus reports whether traffic is currently exiting through Mullvad.
type TunnelStatus struct {
	Connected bool
	// Relay is the hostname of the connected relay, when known.
	Relay string
}

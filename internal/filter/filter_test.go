package filter

import (
	"sort"
	"testing"

	"github.com/emancipat3r/mullvad-pinger/internal/geo"
	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func sample() []model.Relay {
	return []model.Relay{
		{Hostname: "se-got-wg-001", CountryCode: "se", CityCode: "got", Provider: "Mullvad", Owned: true, Active: true, DAITA: true, Protocol: model.WireGuard, HasCoords: true, Latitude: 57.7089, Longitude: 11.9746},
		{Hostname: "se-sto-wg-001", CountryCode: "se", CityCode: "sto", Provider: "Mullvad", Owned: true, Active: true, DAITA: false, Protocol: model.WireGuard, HasCoords: true, Latitude: 59.3293, Longitude: 18.0686},
		{Hostname: "se-sto-wg-002", CountryCode: "se", CityCode: "sto", Provider: "31173", Owned: false, Active: true, DAITA: false, Protocol: model.WireGuard, HasCoords: true, Latitude: 59.3293, Longitude: 18.0686},
		{Hostname: "us-nyc-wg-001", CountryCode: "us", CityCode: "nyc", Provider: "Mullvad", Owned: true, Active: true, DAITA: true, Protocol: model.WireGuard, HasCoords: true, Latitude: 40.7128, Longitude: -74.006},
		{Hostname: "de-fra-wg-002", CountryCode: "de", CityCode: "fra", Provider: "Mullvad", Owned: true, Active: false, DAITA: false, Protocol: model.WireGuard, HasCoords: true, Latitude: 50.1109, Longitude: 8.6821},
		{Hostname: "se-got-ovpn-001", CountryCode: "se", CityCode: "got", Provider: "Mullvad", Owned: true, Active: true, DAITA: false, Protocol: model.OpenVPN, HasCoords: true, Latitude: 57.7089, Longitude: 11.9746},
		{Hostname: "xx-nul-wg-001", CountryCode: "xx", CityCode: "nul", Provider: "ghost", Owned: false, Active: true, DAITA: false, Protocol: model.WireGuard, HasCoords: false},
	}
}

func hosts(rs []model.Relay) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Hostname
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestApplyInclusionFilters(t *testing.T) {
	cases := []struct {
		name string
		crit Criteria
		want []string
	}{
		{
			name: "default wireguard active-only",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true},
			want: []string{"se-got-wg-001", "se-sto-wg-001", "se-sto-wg-002", "us-nyc-wg-001", "xx-nul-wg-001"},
		},
		{
			name: "country se",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, Countries: []string{"se"}},
			want: []string{"se-got-wg-001", "se-sto-wg-001", "se-sto-wg-002"},
		},
		{
			name: "country se or us (case-insensitive)",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, Countries: []string{"SE", "US"}},
			want: []string{"se-got-wg-001", "se-sto-wg-001", "se-sto-wg-002", "us-nyc-wg-001"},
		},
		{
			name: "city sto",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, City: "sto"},
			want: []string{"se-sto-wg-001", "se-sto-wg-002"},
		},
		{
			name: "provider 31173",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, Provider: "31173"},
			want: []string{"se-sto-wg-002"},
		},
		{
			name: "owned only",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, Owned: true},
			want: []string{"se-got-wg-001", "se-sto-wg-001", "us-nyc-wg-001"},
		},
		{
			name: "daita only",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, DAITA: true},
			want: []string{"se-got-wg-001", "us-nyc-wg-001"},
		},
		{
			name: "openvpn protocol",
			crit: Criteria{Protocol: model.OpenVPN, ActiveOnly: true},
			want: []string{"se-got-ovpn-001"},
		},
		{
			name: "include inactive when ActiveOnly false",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: false, Countries: []string{"de"}},
			want: []string{"de-fra-wg-002"},
		},
		{
			name: "empty when contradictory",
			crit: Criteria{Protocol: model.WireGuard, ActiveOnly: true, Countries: []string{"de"}},
			want: []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hosts(Apply(sample(), c.crit))
			if !eq(got, c.want) {
				t.Errorf("Apply() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPrefilterDistanceAndTopN(t *testing.T) {
	// User in Stockholm.
	user := geo.Location{Latitude: 59.3293, Longitude: 18.0686}
	wg := Apply(sample(), Criteria{Protocol: model.WireGuard, ActiveOnly: true})

	// Large radius, cap to 2 nearest: the two Stockholm relays (distance ~0).
	res := Prefilter(wg, user, 20000, 2)
	if len(res.Candidates) != 2 {
		t.Fatalf("topN=2 kept %d", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		if c.Relay.CityCode != "sto" {
			t.Errorf("expected nearest Stockholm relays, got %s", c.Relay.Hostname)
		}
	}
	// The relay without coordinates is excluded and counted.
	if res.NoCoords != 1 {
		t.Errorf("NoCoords = %d, want 1", res.NoCoords)
	}
}

func TestPrefilterMaxDistanceExcludes(t *testing.T) {
	user := geo.Location{Latitude: 59.3293, Longitude: 18.0686} // Stockholm
	wg := Apply(sample(), Criteria{Protocol: model.WireGuard, ActiveOnly: true})
	// 1000 km radius excludes New York.
	res := Prefilter(wg, user, 1000, 0)
	for _, c := range res.Candidates {
		if c.Relay.CountryCode == "us" {
			t.Errorf("US relay should be beyond 1000 km, got %s at %.0f km", c.Relay.Hostname, c.DistanceKm)
		}
	}
}

func TestPrefilterZeroDistanceDisablesCap(t *testing.T) {
	user := geo.Location{Latitude: 59.3293, Longitude: 18.0686}
	wg := Apply(sample(), Criteria{Protocol: model.WireGuard, ActiveOnly: true})
	res := Prefilter(wg, user, 0, 0)
	// 4 wireguard relays have coords (2 sto, 1 got, 1 nyc); xx-nul excluded.
	if len(res.Candidates) != 4 {
		t.Fatalf("distance disabled kept %d, want 4", len(res.Candidates))
	}
	// Sorted by ascending distance.
	for i := 1; i < len(res.Candidates); i++ {
		if res.Candidates[i-1].DistanceKm > res.Candidates[i].DistanceKm {
			t.Errorf("candidates not sorted by distance")
		}
	}
}

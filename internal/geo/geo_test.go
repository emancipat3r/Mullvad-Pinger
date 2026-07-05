package geo

import (
	"math"
	"testing"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func TestHaversineKnownCityPairs(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		wantKm                 float64
		tolKm                  float64
	}{
		{"identical point", 40.0, -70.0, 40.0, -70.0, 0, 0.001},
		// Stockholm <-> Gothenburg ~ 400 km.
		{"sto-got", 59.3293, 18.0686, 57.7089, 11.9746, 398, 15},
		// New York <-> Los Angeles ~ 3936 km.
		{"nyc-lax", 40.7128, -74.006, 34.0522, -118.2437, 3936, 40},
		// London <-> Paris ~ 344 km.
		{"lon-par", 51.5074, -0.1278, 48.8566, 2.3522, 344, 10},
		// Antipodal-ish: half circumference ~ 20015 km.
		{"pole-pole", 90, 0, -90, 0, 20015, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Haversine(c.lat1, c.lon1, c.lat2, c.lon2)
			if math.Abs(got-c.wantKm) > c.tolKm {
				t.Errorf("Haversine = %.2f km, want %.2f ± %.2f", got, c.wantKm, c.tolKm)
			}
		})
	}
}

func TestHaversineSymmetric(t *testing.T) {
	a := Haversine(10, 20, -30, 100)
	b := Haversine(-30, 100, 10, 20)
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("haversine not symmetric: %.6f vs %.6f", a, b)
	}
}

func TestDistanceKmSkipsMissingCoords(t *testing.T) {
	user := Location{Latitude: 40, Longitude: -70}
	no := model.Relay{HasCoords: false, Latitude: 10, Longitude: 10}
	if _, ok := DistanceKm(user, no); ok {
		t.Error("expected ok=false for relay without coordinates")
	}
	yes := model.Relay{HasCoords: true, Latitude: 41, Longitude: -71}
	if d, ok := DistanceKm(user, yes); !ok || d <= 0 {
		t.Errorf("expected positive distance, got %.2f ok=%v", d, ok)
	}
}

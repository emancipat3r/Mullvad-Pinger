// Package filter implements FR-4 stage one: inclusion filtering followed by a
// haversine distance prefilter. Everything here is pure — data in, data out,
// no I/O — so the narrowing logic is exhaustively table-testable.
package filter

import (
	"sort"
	"strings"

	"github.com/emancipat3r/mullvad-pinger/internal/geo"
	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Criteria captures the inclusion filters. Zero values mean "do not filter on
// this dimension" except where noted. Filtering is by inclusion, never
// exclusion.
type Criteria struct {
	Countries  []string // include only these country codes (OR); empty = all
	City       string   // include only this city code; empty = all
	Provider   string   // include only this provider; empty = all
	Owned      bool     // when true, only Mullvad-owned relays
	DAITA      bool     // when true, only DAITA-eligible relays
	Protocol   model.Protocol
	ActiveOnly bool // when true, drop inactive relays
}

// Apply returns the subset of relays matching every set criterion. Comparisons
// on codes and provider are case-insensitive.
func Apply(relays []model.Relay, c Criteria) []model.Relay {
	countrySet := lowerSet(c.Countries)
	city := strings.ToLower(c.City)
	provider := strings.ToLower(c.Provider)

	out := make([]model.Relay, 0, len(relays))
	for _, r := range relays {
		if c.ActiveOnly && !r.Active {
			continue
		}
		if c.Protocol != "" && r.Protocol != c.Protocol {
			continue
		}
		if len(countrySet) > 0 {
			if _, ok := countrySet[strings.ToLower(r.CountryCode)]; !ok {
				continue
			}
		}
		if city != "" && strings.ToLower(r.CityCode) != city {
			continue
		}
		if provider != "" && strings.ToLower(r.Provider) != provider {
			continue
		}
		if c.Owned && !r.Owned {
			continue
		}
		if c.DAITA && !r.DAITA {
			continue
		}
		out = append(out, r)
	}
	return out
}

func lowerSet(xs []string) map[string]struct{} {
	if len(xs) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[strings.ToLower(strings.TrimSpace(x))] = struct{}{}
	}
	return m
}

// Candidate pairs a relay with its computed distance from the user.
type Candidate struct {
	Relay      model.Relay
	DistanceKm float64
	HasDist    bool
}

// PrefilterResult is the outcome of the distance prefilter.
type PrefilterResult struct {
	Candidates []Candidate
	// NoCoords counts relays excluded from distance ranking for lacking
	// coordinates (reported as a warning, not a crash).
	NoCoords int
}

// Prefilter computes haversine distance from the user to each relay, keeps
// those within maxDistanceKm (0 disables the distance cap), sorts by distance,
// and caps to topN nearest (topN <= 0 keeps all). Relays without coordinates
// are excluded from the distance filter and counted.
//
// Distance is a cheap heuristic to shrink the candidate set. It is not the
// answer — measurement in stage two is.
func Prefilter(relays []model.Relay, user geo.Location, maxDistanceKm float64, topN int) PrefilterResult {
	var res PrefilterResult
	cands := make([]Candidate, 0, len(relays))
	for _, r := range relays {
		dist, ok := geo.DistanceKm(user, r)
		if !ok {
			res.NoCoords++
			continue
		}
		if maxDistanceKm > 0 && dist > maxDistanceKm {
			continue
		}
		cands = append(cands, Candidate{Relay: r, DistanceKm: dist, HasDist: true})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].DistanceKm < cands[j].DistanceKm
	})
	if topN > 0 && len(cands) > topN {
		cands = cands[:topN]
	}
	res.Candidates = cands
	return res
}

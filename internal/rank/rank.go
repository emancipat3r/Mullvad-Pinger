// Package rank turns per-host probe results into a ranked result set.
//
// NOTE ON INTENT: this tool optimizes for the LOWEST-LATENCY relay. That is the
// OPPOSITE of unlinkability — pinning yourself to a measured-optimal relay makes
// you more linkable across sessions, not less. This ranking/connect path exists
// for SPEED, not anti-fingerprinting. If your goal is unlinkability, do not
// reach for this tool; DAITA is the lever for that. (See PRD section 11.)
package rank

import (
	"sort"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
	"github.com/emancipat3r/mullvad-pinger/internal/stats"
)

// Rank summarizes each probe result, orders reachable hosts by median RTT
// (ascending), and appends unreachable hosts (100% loss) after the ranked
// winners so they are visible but never ranked first.
//
// Ties on median are broken by lower loss, then lower jitter, then hostname for
// determinism. Ranks are assigned 1..N over reachable hosts only; unreachable
// hosts get Rank 0.
func Rank(relays []model.Relay, probes map[string]model.ProbeResult) []model.Result {
	results := make([]model.Result, 0, len(relays))
	for _, r := range relays {
		p := probes[hostKey(r)]
		p.Host = hostKey(r)
		s := stats.Summarize(p)
		results = append(results, model.Result{
			Relay:     r,
			MedianMS:  s.MedianMS,
			JitterMS:  s.JitterMS,
			LossPct:   s.LossPct,
			Sent:      p.Sent,
			Recv:      p.Recv,
			Reachable: s.Reachable,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		// Reachable hosts sort ahead of unreachable ones.
		if a.Reachable != b.Reachable {
			return a.Reachable
		}
		if !a.Reachable && !b.Reachable {
			return a.Relay.Hostname < b.Relay.Hostname
		}
		if a.MedianMS != b.MedianMS {
			return a.MedianMS < b.MedianMS
		}
		if a.LossPct != b.LossPct {
			return a.LossPct < b.LossPct
		}
		if a.JitterMS != b.JitterMS {
			return a.JitterMS < b.JitterMS
		}
		return a.Relay.Hostname < b.Relay.Hostname
	})

	rank := 0
	for i := range results {
		if results[i].Reachable {
			rank++
			results[i].Rank = rank
		}
	}
	return results
}

// Winners returns only the reachable, ranked results.
func Winners(results []model.Result) []model.Result {
	out := make([]model.Result, 0, len(results))
	for _, r := range results {
		if r.Reachable {
			out = append(out, r)
		}
	}
	return out
}

// hostKey is the probe target for a relay: its hostname with the mullvad.net
// suffix. Probe maps are keyed by this so ranking and measurement agree.
func hostKey(r model.Relay) string {
	return r.Hostname + ".mullvad.net"
}

// HostKey is the exported form used by the measurement stage to key results.
func HostKey(r model.Relay) string { return hostKey(r) }

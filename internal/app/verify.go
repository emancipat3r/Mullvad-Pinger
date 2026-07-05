package app

import (
	"context"
	"sort"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
	"github.com/emancipat3r/mullvad-pinger/internal/verify"
)

// verifyFinalists re-measures the top-k reachable finalists with a real
// handshake and reorders them by handshake RTT. Finalists whose handshake
// succeeds sort ahead (by handshake time); everything else keeps its median
// order. Ranks are reassigned over reachable results.
func verifyFinalists(ctx context.Context, v verify.Verifier, results []model.Result, k int, logf, warnf func(string, ...any)) []model.Result {
	if k < 1 {
		return results
	}
	// Identify the reachable finalists in current rank order.
	reachableIdx := make([]int, 0, len(results))
	for i := range results {
		if results[i].Reachable {
			reachableIdx = append(reachableIdx, i)
		}
	}
	limit := k
	if limit > len(reachableIdx) {
		limit = len(reachableIdx)
	}

	verifiedAny := false
	for _, idx := range reachableIdx[:limit] {
		rtt, err := v.Verify(ctx, results[idx].Relay)
		if err != nil {
			warnf("handshake verify %s failed: %v (keeping ICMP result)", results[idx].Relay.Hostname, err)
			continue
		}
		results[idx].Verified = true
		results[idx].HandshakeMS = float64(rtt.Microseconds()) / 1000.0
		verifiedAny = true
		logf("handshake %s: %.2f ms", results[idx].Relay.Hostname, results[idx].HandshakeMS)
	}
	if !verifiedAny {
		return results
	}

	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.Reachable != b.Reachable {
			return a.Reachable
		}
		if !a.Reachable {
			return false
		}
		// Verified finalists rank ahead, ordered by handshake RTT.
		if a.Verified != b.Verified {
			return a.Verified
		}
		if a.Verified && b.Verified {
			return a.HandshakeMS < b.HandshakeMS
		}
		return a.MedianMS < b.MedianMS
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

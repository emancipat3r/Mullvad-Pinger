// Package stats holds the pure measurement statistics from FR-5: median RTT,
// jitter, and packet loss. These drive ranking, so they are unit-tested with
// table-driven cases including all-loss and single-success edges.
package stats

import (
	"math"
	"sort"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Summary is the derived view of a ProbeResult in milliseconds.
type Summary struct {
	MedianMS  float64
	JitterMS  float64 // standard deviation of RTTs
	LossPct   float64
	Reachable bool
}

// Summarize computes median, jitter, and loss from a probe result. A result
// with zero successful replies is reported as unreachable with 100% loss and
// zero timings — never dropped silently.
func Summarize(p model.ProbeResult) Summary {
	loss := lossPct(p.Sent, p.Recv)
	if len(p.RTTs) == 0 {
		return Summary{MedianMS: 0, JitterMS: 0, LossPct: loss, Reachable: false}
	}
	ms := toMS(p.RTTs)
	return Summary{
		MedianMS:  Median(ms),
		JitterMS:  StdDev(ms),
		LossPct:   loss,
		Reachable: true,
	}
}

func lossPct(sent, recv int) float64 {
	if sent <= 0 {
		return 100
	}
	if recv > sent {
		recv = sent
	}
	return float64(sent-recv) / float64(sent) * 100
}

func toMS(rtts []time.Duration) []float64 {
	out := make([]float64, len(rtts))
	for i, d := range rtts {
		out[i] = float64(d) / float64(time.Millisecond)
	}
	return out
}

// Median returns the median of xs. For an even count it averages the two middle
// values. Empty input returns 0. The input is not mutated.
func Median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	s := make([]float64, n)
	copy(s, xs)
	sort.Float64s(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// StdDev returns the population standard deviation of xs, used as jitter. A
// single sample has zero jitter. Empty input returns 0.
func StdDev(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(n)
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(n))
}

package stats

import (
	"math"
	"testing"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func ms(vals ...float64) []time.Duration {
	out := make([]time.Duration, len(vals))
	for i, v := range vals {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{7}, 7},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{1, 2, 3, 4}, 2.5},
		{"unsorted even", []float64{10, 2, 8, 4}, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Median(c.in); !approx(got, c.want, 1e-9) {
				t.Errorf("Median(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestMedianDoesNotMutate(t *testing.T) {
	in := []float64{3, 1, 2}
	_ = Median(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Errorf("Median mutated its input: %v", in)
	}
}

func TestStdDev(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single has zero jitter", []float64{5}, 0},
		{"two equal", []float64{4, 4}, 0},
		{"known", []float64{2, 4, 4, 4, 5, 5, 7, 9}, 2}, // population stddev = 2
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StdDev(c.in); !approx(got, c.want, 1e-9) {
				t.Errorf("StdDev(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name      string
		p         model.ProbeResult
		wantMed   float64
		wantLoss  float64
		reachable bool
	}{
		{
			name:      "all loss",
			p:         model.ProbeResult{Sent: 5, Recv: 0, RTTs: nil},
			wantMed:   0,
			wantLoss:  100,
			reachable: false,
		},
		{
			name:      "single success",
			p:         model.ProbeResult{Sent: 5, Recv: 1, RTTs: ms(12)},
			wantMed:   12,
			wantLoss:  80,
			reachable: true,
		},
		{
			name:      "full success",
			p:         model.ProbeResult{Sent: 4, Recv: 4, RTTs: ms(10, 20, 30, 40)},
			wantMed:   25,
			wantLoss:  0,
			reachable: true,
		},
		{
			name:      "half loss",
			p:         model.ProbeResult{Sent: 4, Recv: 2, RTTs: ms(10, 30)},
			wantMed:   20,
			wantLoss:  50,
			reachable: true,
		},
		{
			name:      "zero sent",
			p:         model.ProbeResult{Sent: 0, Recv: 0},
			wantMed:   0,
			wantLoss:  100,
			reachable: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Summarize(c.p)
			if !approx(s.MedianMS, c.wantMed, 1e-9) {
				t.Errorf("median = %v, want %v", s.MedianMS, c.wantMed)
			}
			if !approx(s.LossPct, c.wantLoss, 1e-9) {
				t.Errorf("loss = %v, want %v", s.LossPct, c.wantLoss)
			}
			if s.Reachable != c.reachable {
				t.Errorf("reachable = %v, want %v", s.Reachable, c.reachable)
			}
		})
	}
}

func TestSummarizeJitterSurfaced(t *testing.T) {
	// A relay with low median but high jitter should report nonzero jitter.
	s := Summarize(model.ProbeResult{Sent: 5, Recv: 5, RTTs: ms(1, 1, 1, 1, 200)})
	if s.MedianMS != 1 {
		t.Errorf("median = %v, want 1", s.MedianMS)
	}
	if s.JitterMS <= 10 {
		t.Errorf("jitter = %v, expected high jitter surfaced", s.JitterMS)
	}
}

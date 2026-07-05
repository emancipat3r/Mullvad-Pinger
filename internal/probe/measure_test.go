package probe

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// countingPinger records max concurrency and honors context cancellation.
type countingPinger struct {
	active  int32
	maxSeen int32
	delay   time.Duration
	rtts    map[string][]time.Duration
}

func (c *countingPinger) Probe(ctx context.Context, host string, count int) (model.ProbeResult, error) {
	n := atomic.AddInt32(&c.active, 1)
	for {
		m := atomic.LoadInt32(&c.maxSeen)
		if n <= m || atomic.CompareAndSwapInt32(&c.maxSeen, m, n) {
			break
		}
	}
	defer atomic.AddInt32(&c.active, -1)

	select {
	case <-ctx.Done():
		return model.ProbeResult{Host: host, Sent: 0, Recv: 0}, ctx.Err()
	case <-time.After(c.delay):
	}
	r := c.rtts[host]
	return model.ProbeResult{Host: host, RTTs: r, Sent: count, Recv: len(r)}, nil
}

func hostsN(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = string(rune('a'+i)) + ".mullvad.net"
	}
	return out
}

func TestMeasureRecordsEveryHost(t *testing.T) {
	p := &countingPinger{rtts: map[string][]time.Duration{
		"a.mullvad.net": {10 * time.Millisecond},
	}}
	hosts := hostsN(5)
	res := Measure(context.Background(), p, hosts, 3, 2)
	if len(res) != 5 {
		t.Fatalf("got %d results, want 5", len(res))
	}
	for _, h := range hosts {
		if _, ok := res[h]; !ok {
			t.Errorf("missing result for %s", h)
		}
	}
	// Hosts with no replies are recorded as fully lost, not dropped.
	if r := res["b.mullvad.net"]; r.Recv != 0 || r.Sent == 0 {
		t.Errorf("silent host = %+v, want Sent>0 Recv=0", r)
	}
}

func TestMeasureRespectsConcurrencyLimit(t *testing.T) {
	p := &countingPinger{delay: 20 * time.Millisecond}
	Measure(context.Background(), p, hostsN(10), 1, 3)
	if p.maxSeen > 3 {
		t.Errorf("max concurrency = %d, want <= 3", p.maxSeen)
	}
}

func TestMeasureCancellationReturnsPartial(t *testing.T) {
	p := &countingPinger{delay: 500 * time.Millisecond} // longer than the deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	done := make(chan map[string]model.ProbeResult, 1)
	go func() { done <- Measure(ctx, p, hostsN(8), 5, 4) }()

	select {
	case res := <-done:
		// Every host still gets an entry; cancelled ones show as fully lost.
		if len(res) != 8 {
			t.Fatalf("got %d results after cancel, want 8", len(res))
		}
		for h, r := range res {
			if r.Recv != 0 {
				t.Errorf("host %s unexpectedly received replies after cancel", h)
			}
			if r.Sent == 0 {
				t.Errorf("host %s should be marked attempted (Sent>0)", h)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Measure deadlocked after context cancellation")
	}
}

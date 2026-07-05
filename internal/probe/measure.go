package probe

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Measure probes every host under bounded concurrency (errgroup + SetLimit),
// driven by ctx. The caller sets the whole-run deadline on ctx; when it fires,
// outstanding probes are cancelled and whatever completed is returned. Hosts
// that never got to send are recorded as fully lost so they surface as
// unreachable rather than vanishing.
//
// Individual probe errors never abort the group: one unreachable host must not
// cancel the others.
func Measure(ctx context.Context, p Pinger, hosts []string, count, concurrency int) map[string]model.ProbeResult {
	if concurrency < 1 {
		concurrency = 1
	}
	results := make(map[string]model.ProbeResult, len(hosts))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, h := range hosts {
		h := h
		g.Go(func() error {
			pr, _ := p.Probe(gctx, h, count)
			pr.Host = h
			if pr.Sent == 0 && pr.Recv == 0 {
				// Never sent (e.g. cancelled before start): record the attempt
				// so loss reads as 100% instead of the host disappearing.
				pr.Sent = count
			}
			mu.Lock()
			results[h] = pr
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return results
}

// Command mullvad-pinger finds the lowest-latency Mullvad relay for the user's
// current location and can optionally connect to it through the Mullvad daemon.
//
// It narrows before it measures (inclusion filters + a haversine distance
// prefilter), measures natively over ICMP under bounded concurrency, and ranks
// on median RTT while surfacing packet loss and jitter.
//
// SPEED, NOT UNLINKABILITY: pinning to a latency-optimal relay makes a user more
// linkable across sessions. This tool is for speed; DAITA is the lever for
// anti-fingerprinting. See the PRD section 11 caveat echoed in internal/rank
// and internal/app.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/emancipat3r/mullvad-pinger/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

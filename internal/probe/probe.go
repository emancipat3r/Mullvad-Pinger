// Package probe measures relay latency natively over ICMP (never by shelling
// out to /bin/ping) and runs the measurement stage under bounded concurrency.
package probe

import (
	"context"
	"fmt"
	"os"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	"golang.org/x/net/icmp"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Pinger sends ICMP probes to a host and returns per-probe RTTs. It is an
// interface so the measurement stage can be exercised with a fake.
type Pinger interface {
	// Probe sends count ICMP probes to host and returns the per-probe RTTs.
	Probe(ctx context.Context, host string, count int) (model.ProbeResult, error)
}

// ProbingPinger is the concrete Pinger backed by pro-bing.
type ProbingPinger struct {
	// Privileged selects raw ICMP sockets; false uses unprivileged UDP
	// datagram sockets (net.ipv4.ping_group_range).
	Privileged bool
	// Interval is the delay between probes to one host.
	Interval time.Duration
	// PerProbeTimeout is the wait budget attributed to each probe; it maps onto
	// pro-bing's per-run timeout together with Count and Interval.
	PerProbeTimeout time.Duration
}

// NewProbingPinger selects the socket mode per the PRD: prefer unprivileged UDP
// datagram sockets, and if the kernel disallows them, fall back to privileged
// raw sockets when running as root — otherwise return actionable guidance.
// It never regains capability by shelling out to ping.
func NewProbingPinger(perProbeTimeout time.Duration, logf func(string, ...any)) (*ProbingPinger, error) {
	privileged, err := detectMode(logf)
	if err != nil {
		return nil, err
	}
	return &ProbingPinger{
		Privileged:      privileged,
		Interval:        100 * time.Millisecond,
		PerProbeTimeout: perProbeTimeout,
	}, nil
}

// detectMode returns whether privileged mode is required.
func detectMode(logf func(string, ...any)) (bool, error) {
	// Prefer unprivileged UDP datagram sockets.
	if c, err := icmp.ListenPacket("udp4", ""); err == nil {
		_ = c.Close()
		return false, nil
	}
	// Unprivileged ICMP is unavailable (ping_group_range too narrow).
	if os.Geteuid() == 0 {
		if logf != nil {
			logf("unprivileged ICMP unavailable; falling back to privileged raw sockets")
		}
		return true, nil
	}
	return false, fmt.Errorf("unprivileged ICMP is disabled and this process is not root; " +
		"either widen the kernel ping group range " +
		"(sudo sysctl -w net.ipv4.ping_group_range=\"0 2147483647\") " +
		"or grant CAP_NET_RAW (sudo setcap cap_net_raw+ep <binary>)")
}

func (pp ProbingPinger) Probe(ctx context.Context, host string, count int) (model.ProbeResult, error) {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return model.ProbeResult{Host: host, Sent: count}, err
	}
	pinger.SetPrivileged(pp.Privileged)
	pinger.Count = count
	pinger.RecordRtts = true
	interval := pp.Interval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	pinger.Interval = interval
	perProbe := pp.PerProbeTimeout
	if perProbe <= 0 {
		perProbe = 2 * time.Second
	}
	// Overall per-host budget: time to emit all probes plus a tail wait for the
	// last reply.
	pinger.Timeout = time.Duration(count)*interval + perProbe

	runErr := pinger.RunWithContext(ctx)
	st := pinger.Statistics()
	res := model.ProbeResult{
		Host: host,
		RTTs: st.Rtts,
		Sent: st.PacketsSent,
		Recv: st.PacketsRecv,
	}
	// A context cancellation is expected on deadline; surface only real errors.
	if runErr != nil && ctx.Err() == nil {
		return res, runErr
	}
	return res, nil
}

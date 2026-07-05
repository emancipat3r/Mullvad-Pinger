package rank

import (
	"testing"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func relay(host string) model.Relay { return model.Relay{Hostname: host} }

func ms(vals ...float64) []time.Duration {
	out := make([]time.Duration, len(vals))
	for i, v := range vals {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

func TestRankOrdersByMedian(t *testing.T) {
	relays := []model.Relay{relay("a"), relay("b"), relay("c")}
	probes := map[string]model.ProbeResult{
		"a.mullvad.net": {Sent: 3, Recv: 3, RTTs: ms(30, 30, 30)},
		"b.mullvad.net": {Sent: 3, Recv: 3, RTTs: ms(10, 10, 10)},
		"c.mullvad.net": {Sent: 3, Recv: 3, RTTs: ms(20, 20, 20)},
	}
	got := Rank(relays, probes)
	order := []string{got[0].Relay.Hostname, got[1].Relay.Hostname, got[2].Relay.Hostname}
	want := []string{"b", "c", "a"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
		if got[i].Rank != i+1 {
			t.Errorf("%s rank = %d, want %d", got[i].Relay.Hostname, got[i].Rank, i+1)
		}
	}
}

func TestRankUnreachableExcludedFromWinnersButShown(t *testing.T) {
	relays := []model.Relay{relay("up"), relay("down")}
	probes := map[string]model.ProbeResult{
		"up.mullvad.net":   {Sent: 3, Recv: 3, RTTs: ms(15, 15, 15)},
		"down.mullvad.net": {Sent: 3, Recv: 0},
	}
	got := Rank(relays, probes)
	if len(got) != 2 {
		t.Fatalf("expected both relays shown, got %d", len(got))
	}
	// Reachable first with a rank; unreachable last with rank 0 and 100% loss.
	if got[0].Relay.Hostname != "up" || got[0].Rank != 1 {
		t.Errorf("winner = %+v", got[0])
	}
	if got[1].Relay.Hostname != "down" {
		t.Errorf("unreachable should sort last, got %s", got[1].Relay.Hostname)
	}
	if got[1].Reachable || got[1].Rank != 0 || got[1].LossPct != 100 {
		t.Errorf("unreachable relay = %+v, want reachable=false rank=0 loss=100", got[1])
	}
	if w := Winners(got); len(w) != 1 || w[0].Relay.Hostname != "up" {
		t.Errorf("winners = %+v, want only 'up'", w)
	}
}

func TestRankAllLoss(t *testing.T) {
	relays := []model.Relay{relay("x"), relay("y")}
	probes := map[string]model.ProbeResult{
		"x.mullvad.net": {Sent: 5, Recv: 0},
		"y.mullvad.net": {Sent: 5, Recv: 0},
	}
	got := Rank(relays, probes)
	if len(Winners(got)) != 0 {
		t.Error("all-loss should yield no winners")
	}
	for _, r := range got {
		if r.LossPct != 100 || r.Rank != 0 {
			t.Errorf("relay %s = %+v, want 100%% loss rank 0", r.Relay.Hostname, r)
		}
	}
}

func TestRankSingleSuccess(t *testing.T) {
	relays := []model.Relay{relay("solo")}
	probes := map[string]model.ProbeResult{
		"solo.mullvad.net": {Sent: 5, Recv: 1, RTTs: ms(42)},
	}
	got := Rank(relays, probes)
	if len(got) != 1 || got[0].Rank != 1 {
		t.Fatalf("single candidate not ranked: %+v", got)
	}
	if got[0].MedianMS != 42 || got[0].LossPct != 80 {
		t.Errorf("solo = %+v, want median 42 loss 80", got[0])
	}
}

func TestRankTieBreakByLossThenJitterThenName(t *testing.T) {
	// Equal median; 'clean' has lower loss so it should win the tie.
	relays := []model.Relay{relay("noisy"), relay("clean")}
	probes := map[string]model.ProbeResult{
		"noisy.mullvad.net": {Sent: 4, Recv: 2, RTTs: ms(20, 20)}, // loss 50
		"clean.mullvad.net": {Sent: 4, Recv: 4, RTTs: ms(20, 20, 20, 20)},
	}
	got := Rank(relays, probes)
	if got[0].Relay.Hostname != "clean" {
		t.Errorf("tie should break to lower loss 'clean', got %s", got[0].Relay.Hostname)
	}
}

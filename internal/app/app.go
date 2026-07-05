// Package app wires the concrete implementations into the two-stage pipeline
// and maps outcomes onto the exit-code contract. All network, geolocation,
// relay-source, and connect actions sit behind interfaces so the whole pipeline
// is exercisable on fakes.
//
// SPEED, NOT UNLINKABILITY: this program ranks relays by lowest latency and can
// pin the user to the measured-optimal one. That is the OPPOSITE of
// unlinkability — a latency-optimal pin makes a user more linkable across
// sessions. Do not reach for this tool when the goal is anti-fingerprinting;
// DAITA is the lever for that. (PRD section 11.)
package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/emancipat3r/mullvad-pinger/internal/connect"
	"github.com/emancipat3r/mullvad-pinger/internal/filter"
	"github.com/emancipat3r/mullvad-pinger/internal/geo"
	"github.com/emancipat3r/mullvad-pinger/internal/model"
	"github.com/emancipat3r/mullvad-pinger/internal/output"
	"github.com/emancipat3r/mullvad-pinger/internal/probe"
	"github.com/emancipat3r/mullvad-pinger/internal/rank"
	"github.com/emancipat3r/mullvad-pinger/internal/relays"
	"github.com/emancipat3r/mullvad-pinger/internal/verify"
)

// Exit codes per PRD section 7.
const (
	ExitOK           = 0
	ExitGeneric      = 1
	ExitNoCandidates = 2
	ExitNoProbes     = 3
	ExitActiveTunnel = 4
	ExitNoClient     = 5
)

// Deps holds the boundary implementations. Any nil field is filled with the
// real implementation; tests supply fakes.
type Deps struct {
	Source    relays.Source
	Geo       geo.Geolocator
	Pinger    probe.Pinger
	Connector connect.Connector
	Verifier  verify.Verifier
}

// Run is the CLI entrypoint: parse flags then execute the pipeline.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, fs, err := parseFlags(args, stderr)
	if err != nil {
		if o.ShowHelp {
			return ExitOK
		}
		return ExitGeneric
	}
	_ = fs
	return runWithDeps(ctx, o, Deps{}, stdout, stderr)
}

func runWithDeps(ctx context.Context, o *Options, deps Deps, stdout, stderr io.Writer) int {
	logf := func(format string, args ...any) {
		if o.Verbose > 0 {
			fmt.Fprintf(stderr, "[*] "+format+"\n", args...)
		}
	}
	warnf := func(format string, args ...any) {
		fmt.Fprintf(stderr, "[!] "+format+"\n", args...)
	}

	if err := o.validate(); err != nil {
		warnf("%v", err)
		return ExitGeneric
	}

	// --- Relay source (FR-1) ---
	rels, err := loadRelays(ctx, o, deps, logf)
	if err != nil {
		warnf("loading relays: %v", err)
		return ExitGeneric
	}
	logf("loaded %d relays", len(rels))

	// --- List operations (FR-9): no geo, no probes ---
	if o.ListCountries {
		printCountries(stdout, rels)
		return ExitOK
	}
	if o.ListCities {
		printCities(stdout, rels, o.Countries)
		return ExitOK
	}
	if o.ListProviders {
		printProviders(stdout, rels)
		return ExitOK
	}

	// --- Geolocation + active-tunnel detection (FR-3) ---
	user, code := locateAndGuard(ctx, o, deps, logf, warnf)
	if code != ExitOK {
		return code
	}

	// --- Stage one: prefilter (FR-4) ---
	crit := filter.Criteria{
		Countries:  o.Countries,
		City:       o.City,
		Provider:   o.Provider,
		Owned:      o.Owned,
		DAITA:      o.DAITA,
		Protocol:   model.Protocol(o.Protocol),
		ActiveOnly: true,
	}
	filtered := filter.Apply(rels, crit)
	if len(filtered) == 0 {
		warnf("no relays remain after filtering; %s emptied the set", emptyingFilter(rels, crit))
		return ExitNoCandidates
	}
	pf := filter.Prefilter(filtered, user, o.MaxDistance, o.TopN)
	if pf.NoCoords > 0 {
		warnf("%d relay(s) excluded from distance prefilter (missing coordinates)", pf.NoCoords)
	}
	if len(pf.Candidates) == 0 {
		warnf("no candidates within --max-distance %.0f km of you; widen it or set 0 to disable", o.MaxDistance)
		return ExitNoCandidates
	}
	logf("prefilter kept %d candidate(s)", len(pf.Candidates))

	// --- Stage two: measurement (FR-5) ---
	candRelays := make([]model.Relay, len(pf.Candidates))
	hosts := make([]string, len(pf.Candidates))
	for i, c := range pf.Candidates {
		candRelays[i] = c.Relay
		hosts[i] = rank.HostKey(c.Relay)
	}

	pinger, code := buildPinger(o, deps, logf, warnf)
	if code != ExitOK {
		return code
	}

	mctx, cancel := context.WithTimeout(ctx, o.Deadline)
	defer cancel()
	logf("probing %d host(s), count=%d concurrency=%d", len(hosts), o.Count, o.Concurrency)
	probes := probe.Measure(mctx, pinger, hosts, o.Count, o.Concurrency)

	results := rank.Rank(candRelays, probes)

	// --- Stage three: optional handshake verify (FR-6) ---
	if o.Verify {
		results = runVerify(ctx, o, deps, results, logf, warnf)
	}

	winners := rank.Winners(results)
	if len(winners) == 0 {
		// All candidates unreachable: still show the attempted set (FR-5 / edge).
		emit(o, stdout, results)
		warnf("no successful probes to any candidate; all %d shown with 100%% loss", len(results))
		return ExitNoProbes
	}

	// --- Output (FR-7) ---
	emit(o, stdout, results)

	// --- Connect action (FR-8) ---
	if o.Connect {
		return doConnect(ctx, o, deps, winners[0].Relay, warnf, logf)
	}
	return ExitOK
}

func loadRelays(ctx context.Context, o *Options, deps Deps, logf func(string, ...any)) ([]model.Relay, error) {
	if deps.Source != nil {
		return deps.Source.Relays(ctx)
	}
	return relays.Load(ctx, relays.LoadOptions{
		RelaysFile: o.RelaysFile,
		Refresh:    o.Refresh,
		Logf:       logf,
	})
}

// locateAndGuard geolocates the user and refuses to measure through an active
// tunnel unless --force. It returns the user location and an exit code (ExitOK
// to proceed).
func locateAndGuard(ctx context.Context, o *Options, deps Deps, logf, warnf func(string, ...any)) (geo.Location, int) {
	gl := deps.Geo
	if gl == nil {
		gl = geo.MullvadGeolocator{}
	}
	user, connected, err := gl.Locate(ctx)
	if err != nil {
		if o.MaxDistance > 0 {
			warnf("geolocation failed: %v (needed for the distance prefilter; set --max-distance 0 to skip)", err)
			return geo.Location{}, ExitGeneric
		}
		warnf("geolocation unavailable: %v; proceeding without distance prefilter", err)
	} else {
		logf("located at %.4f,%.4f (mullvad exit: %v)", user.Latitude, user.Longitude, connected)
	}

	// Also consult the daemon status when available.
	conn := deps.Connector
	if conn == nil {
		conn = connect.MullvadConnector{}
	}
	if st, serr := conn.Status(ctx); serr == nil && st.Connected {
		connected = true
	}

	if connected {
		if !o.Force {
			warnf("an active Mullvad tunnel was detected; probes would traverse the tunnel and be meaningless. Disconnect, or pass --force to measure anyway.")
			return geo.Location{}, ExitActiveTunnel
		}
		warnf("active tunnel detected; measuring anyway due to --force (numbers may be meaningless)")
	}
	return user, ExitOK
}

func buildPinger(o *Options, deps Deps, logf, warnf func(string, ...any)) (probe.Pinger, int) {
	if deps.Pinger != nil {
		return deps.Pinger, ExitOK
	}
	pp, err := probe.NewProbingPinger(o.Timeout, logf)
	if err != nil {
		warnf("%v", err)
		return nil, ExitGeneric
	}
	return pp, ExitOK
}

func runVerify(ctx context.Context, o *Options, deps Deps, results []model.Result, logf, warnf func(string, ...any)) []model.Result {
	v := deps.Verifier
	if v == nil {
		key, err := verify.LoadKey()
		if err != nil {
			warnf("--verify: %v; falling back to ICMP ranking", err)
			return results
		}
		v = verify.WireGuardVerifier{PrivateKey: key, Timeout: o.Timeout}
	}
	return verifyFinalists(ctx, v, results, o.VerifyK, logf, warnf)
}

func doConnect(ctx context.Context, o *Options, deps Deps, winner model.Relay, warnf, logf func(string, ...any)) int {
	conn := deps.Connector
	if conn == nil {
		mc := connect.MullvadConnector{}
		if !mc.Available() {
			warnf("--connect requested but the %q client was not found in PATH", connect.ClientName)
			return ExitNoClient
		}
		conn = mc
	}
	logf("connecting to %s (%s, %s)", winner.Hostname, winner.CityName, winner.CountryName)
	if err := conn.Connect(ctx, winner); err != nil {
		if errors.Is(err, connect.ErrClientNotFound) {
			warnf("--connect: %v", err)
			return ExitNoClient
		}
		warnf("connect failed: %v", err)
		return ExitGeneric
	}
	warnf("connected to %s", winner.Hostname)
	return ExitOK
}

func emit(o *Options, stdout io.Writer, results []model.Result) {
	if o.JSON {
		_ = output.WriteJSON(stdout, results)
		return
	}
	_ = output.WriteTable(stdout, results, o.Verify)
}

// emptyingFilter applies the inclusion filters cumulatively and names the first
// one that empties the result set.
func emptyingFilter(rels []model.Relay, c filter.Criteria) string {
	cum := filter.Criteria{ActiveOnly: c.ActiveOnly}
	type step struct {
		name  string
		apply func(*filter.Criteria)
	}
	steps := []step{
		{"active/--protocol", func(x *filter.Criteria) { x.Protocol = c.Protocol }},
		{"--country", func(x *filter.Criteria) { x.Countries = c.Countries }},
		{"--city", func(x *filter.Criteria) { x.City = c.City }},
		{"--provider", func(x *filter.Criteria) { x.Provider = c.Provider }},
		{"--owned", func(x *filter.Criteria) { x.Owned = c.Owned }},
		{"--daita", func(x *filter.Criteria) { x.DAITA = c.DAITA }},
	}
	for _, s := range steps {
		s.apply(&cum)
		if len(filter.Apply(rels, cum)) == 0 {
			return s.name
		}
	}
	return "the combined filters"
}

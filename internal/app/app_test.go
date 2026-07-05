package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emancipat3r/mullvad-pinger/internal/connect"
	"github.com/emancipat3r/mullvad-pinger/internal/geo"
	"github.com/emancipat3r/mullvad-pinger/internal/model"
	"github.com/emancipat3r/mullvad-pinger/internal/output"
)

// --- fakes ---

type fakeSource struct {
	relays []model.Relay
	err    error
}

func (f fakeSource) Relays(context.Context) ([]model.Relay, error) { return f.relays, f.err }

type fakeGeo struct {
	loc       geo.Location
	connected bool
	err       error
}

func (f fakeGeo) Locate(context.Context) (geo.Location, bool, error) {
	return f.loc, f.connected, f.err
}

type fakePinger struct {
	rtts map[string][]time.Duration
}

func (f fakePinger) Probe(_ context.Context, host string, count int) (model.ProbeResult, error) {
	r := f.rtts[host]
	return model.ProbeResult{Host: host, RTTs: r, Sent: count, Recv: len(r)}, nil
}

type fakeConnector struct {
	status    model.TunnelStatus
	connected *model.Relay
	connErr   error
}

func (f *fakeConnector) Connect(_ context.Context, r model.Relay) error {
	if f.connErr != nil {
		return f.connErr
	}
	rr := r
	f.connected = &rr
	return nil
}
func (f *fakeConnector) Status(context.Context) (model.TunnelStatus, error) { return f.status, nil }

type fakeVerifier struct {
	rtts map[string]time.Duration
}

func (f fakeVerifier) Verify(_ context.Context, r model.Relay) (time.Duration, error) {
	if d, ok := f.rtts[r.Hostname]; ok {
		return d, nil
	}
	return 0, errors.New("no handshake")
}

// --- fixtures ---

func ms(vals ...float64) []time.Duration {
	out := make([]time.Duration, len(vals))
	for i, v := range vals {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

func testRelays() []model.Relay {
	return []model.Relay{
		{Hostname: "se-got-wg-001", CountryCode: "se", CountryName: "Sweden", CityCode: "got", CityName: "Gothenburg", Provider: "Mullvad", Owned: true, Active: true, DAITA: true, Protocol: model.WireGuard, PublicKey: "k1", HasCoords: true, Latitude: 57.7089, Longitude: 11.9746},
		{Hostname: "se-sto-wg-001", CountryCode: "se", CountryName: "Sweden", CityCode: "sto", CityName: "Stockholm", Provider: "Mullvad", Owned: true, Active: true, DAITA: false, Protocol: model.WireGuard, PublicKey: "k2", HasCoords: true, Latitude: 59.3293, Longitude: 18.0686},
		{Hostname: "us-nyc-wg-001", CountryCode: "us", CountryName: "USA", CityCode: "nyc", CityName: "New York", Provider: "xtom", Owned: false, Active: true, DAITA: false, Protocol: model.WireGuard, PublicKey: "k3", HasCoords: true, Latitude: 40.7128, Longitude: -74.006},
	}
}

func defaultOpts() *Options {
	return &Options{
		Protocol:    string(model.WireGuard),
		MaxDistance: 2000,
		TopN:        50,
		Count:       5,
		Concurrency: 25,
		Timeout:     2 * time.Second,
		Deadline:    30 * time.Second,
		VerifyK:     5,
	}
}

func stockholm() fakeGeo {
	return fakeGeo{loc: geo.Location{Latitude: 59.3293, Longitude: 18.0686}}
}

// --- integration ---

func TestPipelineHappyPath(t *testing.T) {
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    stockholm(),
		Pinger: fakePinger{rtts: map[string][]time.Duration{
			"se-got-wg-001.mullvad.net": ms(30, 30, 31),
			"se-sto-wg-001.mullvad.net": ms(9, 10, 11),
		}},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), defaultOpts(), deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0. stderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	// US relay is beyond 2000 km of Stockholm, so it should not be measured.
	if strings.Contains(out, "us-nyc-wg-001") {
		t.Error("US relay should have been prefiltered out by distance")
	}
	// Winner is the lower-median Stockholm relay, marked.
	var winnerLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "se-sto-wg-001") {
			winnerLine = l
		}
	}
	if !strings.Contains(winnerLine, "*") {
		t.Errorf("expected se-sto-wg-001 as marked winner, table:\n%s", out)
	}
}

func TestPipelineJSONCleanStdout(t *testing.T) {
	o := defaultOpts()
	o.JSON = true
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    stockholm(),
		Pinger: fakePinger{rtts: map[string][]time.Duration{
			"se-got-wg-001.mullvad.net": ms(30, 30, 31),
			"se-sto-wg-001.mullvad.net": ms(9, 10, 11),
		}},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var got []output.JSONResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout not clean JSON: %v\n%s", err, stdout.String())
	}
	if got[0].Hostname != "se-sto-wg-001" || !got[0].Winner {
		t.Errorf("json winner = %+v", got[0])
	}
}

func TestActiveTunnelRefusesWithoutForce(t *testing.T) {
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       fakeGeo{loc: geo.Location{Latitude: 59.3, Longitude: 18}, connected: true},
		Pinger:    fakePinger{},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), defaultOpts(), deps, &stdout, &stderr)
	if code != ExitActiveTunnel {
		t.Fatalf("exit = %d, want %d", code, ExitActiveTunnel)
	}
	if stdout.Len() != 0 {
		t.Errorf("no probes should run / no stdout on refusal, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "tunnel") {
		t.Error("expected tunnel guidance on stderr")
	}
}

func TestActiveTunnelProceedsWithForce(t *testing.T) {
	o := defaultOpts()
	o.Force = true
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    fakeGeo{loc: geo.Location{Latitude: 59.3, Longitude: 18}, connected: true},
		Pinger: fakePinger{rtts: map[string][]time.Duration{
			"se-sto-wg-001.mullvad.net": ms(9, 10, 11),
		}},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 with --force. stderr:\n%s", code, stderr.String())
	}
}

func TestTunnelDetectedViaConnectorStatus(t *testing.T) {
	// Geo says not connected, but the daemon status does.
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{},
		Connector: &fakeConnector{status: model.TunnelStatus{Connected: true, Relay: "se-sto-wg-001"}},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), defaultOpts(), deps, &stdout, &stderr)
	if code != ExitActiveTunnel {
		t.Fatalf("exit = %d, want %d (tunnel via connector status)", code, ExitActiveTunnel)
	}
}

func TestNoCandidatesAfterFilterExits2(t *testing.T) {
	o := defaultOpts()
	o.Countries = []string{"jp"} // no jp relays in fixture
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitNoCandidates {
		t.Fatalf("exit = %d, want %d", code, ExitNoCandidates)
	}
	if !strings.Contains(stderr.String(), "--country") {
		t.Errorf("expected the emptying filter named, stderr:\n%s", stderr.String())
	}
}

func TestAllUnreachableExits3(t *testing.T) {
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{rtts: map[string][]time.Duration{}}, // zero replies for all
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), defaultOpts(), deps, &stdout, &stderr)
	if code != ExitNoProbes {
		t.Fatalf("exit = %d, want %d", code, ExitNoProbes)
	}
	// Attempted set still shown with 100% loss.
	if !strings.Contains(stdout.String(), "100.00") {
		t.Errorf("attempted set should be shown with 100%% loss:\n%s", stdout.String())
	}
}

func TestConnectInvokesConnectorWithWinner(t *testing.T) {
	o := defaultOpts()
	o.Connect = true
	fc := &fakeConnector{}
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    stockholm(),
		Pinger: fakePinger{rtts: map[string][]time.Duration{
			"se-got-wg-001.mullvad.net": ms(30, 30),
			"se-sto-wg-001.mullvad.net": ms(9, 10),
		}},
		Connector: fc,
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if fc.connected == nil {
		t.Fatal("connector was not called")
	}
	if fc.connected.Hostname != "se-sto-wg-001" {
		t.Errorf("connected to %s, want se-sto-wg-001", fc.connected.Hostname)
	}
}

func TestNoConnectByDefault(t *testing.T) {
	fc := &fakeConnector{}
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{rtts: map[string][]time.Duration{"se-sto-wg-001.mullvad.net": ms(9, 10)}},
		Connector: fc,
	}
	var stdout, stderr bytes.Buffer
	runWithDeps(context.Background(), defaultOpts(), deps, &stdout, &stderr)
	if fc.connected != nil {
		t.Error("connector must not be called without --connect")
	}
}

func TestConnectMissingClientExits5(t *testing.T) {
	o := defaultOpts()
	o.Connect = true
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{rtts: map[string][]time.Duration{"se-sto-wg-001.mullvad.net": ms(9, 10)}},
		Connector: &fakeConnector{connErr: connect.ErrClientNotFound},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitNoClient {
		t.Fatalf("exit = %d, want %d", code, ExitNoClient)
	}
}

func TestVerifyReordersFinalists(t *testing.T) {
	o := defaultOpts()
	o.Verify = true
	// ICMP ranks se-sto first (9-11) then se-got (30). Handshake inverts it.
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    stockholm(),
		Pinger: fakePinger{rtts: map[string][]time.Duration{
			"se-got-wg-001.mullvad.net": ms(30, 30, 31),
			"se-sto-wg-001.mullvad.net": ms(9, 10, 11),
		}},
		Connector: &fakeConnector{},
		Verifier: fakeVerifier{rtts: map[string]time.Duration{
			"se-got-wg-001": 5 * time.Millisecond,
			"se-sto-wg-001": 50 * time.Millisecond,
		}},
	}
	o.JSON = true
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var got []output.JSONResult
	_ = json.Unmarshal(stdout.Bytes(), &got)
	if got[0].Hostname != "se-got-wg-001" {
		t.Errorf("verify should reorder winner to se-got-wg-001 (lower handshake), got %s", got[0].Hostname)
	}
	if !got[0].Verified || got[0].HandshakeMS != 5 {
		t.Errorf("winner handshake = %+v", got[0])
	}
}

func TestVerifyDegradesWithoutKey(t *testing.T) {
	o := defaultOpts()
	o.Verify = true
	// No Verifier in deps and no MULLVAD_WG_PRIVATE_KEY set -> degrade to ICMP.
	deps := Deps{
		Source:    fakeSource{relays: testRelays()},
		Geo:       stockholm(),
		Pinger:    fakePinger{rtts: map[string][]time.Duration{"se-sto-wg-001.mullvad.net": ms(9, 10)}},
		Connector: &fakeConnector{},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (degrade to ICMP)", code)
	}
	if !strings.Contains(stderr.String(), "falling back to ICMP") {
		t.Errorf("expected degrade warning, stderr:\n%s", stderr.String())
	}
}

func TestListCountriesNoProbes(t *testing.T) {
	o := defaultOpts()
	o.ListCountries = true
	// A pinger/geo that would panic if used ensures list ops skip them.
	deps := Deps{
		Source: fakeSource{relays: testRelays()},
		Geo:    fakeGeo{err: errors.New("geo must not be called")},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "se") || !strings.Contains(out, "us") {
		t.Errorf("expected country list, got:\n%s", out)
	}
}

func TestListCitiesScopedByCountry(t *testing.T) {
	o := defaultOpts()
	o.ListCities = true
	o.Countries = []string{"se"}
	deps := Deps{Source: fakeSource{relays: testRelays()}}
	var stdout, stderr bytes.Buffer
	runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	out := stdout.String()
	if !strings.Contains(out, "got") || !strings.Contains(out, "sto") {
		t.Errorf("expected se cities, got:\n%s", out)
	}
	if strings.Contains(out, "nyc") {
		t.Error("us city should be excluded when scoped to se")
	}
}

// --- flag parsing / exit-code mapping ---

func TestParseFlagsDefaults(t *testing.T) {
	o, _, err := parseFlags(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Protocol != "wireguard" || o.Count != 5 || o.Concurrency != 25 ||
		o.MaxDistance != 2000 || o.TopN != 50 || o.Timeout != 2*time.Second || o.Deadline != 30*time.Second {
		t.Errorf("unexpected defaults: %+v", o)
	}
}

func TestParseFlagsRepeatableCountryAndShorthands(t *testing.T) {
	o, _, err := parseFlags([]string{"-c", "se", "-c", "us", "-n", "3"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Countries) != 2 || o.Countries[0] != "se" || o.Countries[1] != "us" {
		t.Errorf("countries = %v", o.Countries)
	}
	if o.Count != 3 {
		t.Errorf("count = %d, want 3", o.Count)
	}
}

func TestRunUnknownFlagExits1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--nope"}, &stdout, &stderr)
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestRunHelpExits0(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--help"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestInvalidProtocolExits1(t *testing.T) {
	o := defaultOpts()
	o.Protocol = "carrierpigeon"
	deps := Deps{Source: fakeSource{relays: testRelays()}}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), o, deps, &stdout, &stderr)
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want 1", code)
	}
}
